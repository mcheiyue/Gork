package storage

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	platformconfig "github.com/jiujiu532/grok2api/app/platform/config"
)

type MediaType string

const (
	MediaTypeImage MediaType = "image"
	MediaTypeVideo MediaType = "video"
)

var (
	imageExts = map[string]struct{}{".jpg": {}, ".jpeg": {}, ".png": {}, ".gif": {}, ".webp": {}, ".bmp": {}}
	videoExts = map[string]struct{}{".mp4": {}, ".mov": {}, ".m4v": {}, ".webm": {}, ".avi": {}, ".mkv": {}}
)

type MediaCacheConfig interface {
	GetInt(key string, defaultValue int) int
}

type LocalMediaCacheOptions struct {
	Config MediaCacheConfig
}

type LocalMediaCacheStore struct {
	config MediaCacheConfig
	locks  map[MediaType]*sync.Mutex
}

func NewLocalMediaCacheStore(options LocalMediaCacheOptions) *LocalMediaCacheStore {
	config := options.Config
	if config == nil {
		config = platformconfig.GlobalConfig
	}
	return &LocalMediaCacheStore{
		config: config,
		locks: map[MediaType]*sync.Mutex{
			MediaTypeImage: {},
			MediaTypeVideo: {},
		},
	}
}

func (s *LocalMediaCacheStore) SaveImage(raw []byte, mime, fileID string) (string, error) {
	ext := ".jpg"
	if strings.Contains(strings.ToLower(mime), "png") {
		ext = ".png"
	}
	_, err := s.save(MediaTypeImage, fileID, raw, ext)
	return fileID, err
}

func (s *LocalMediaCacheStore) SaveVideo(raw []byte, fileID string) (string, error) {
	return s.save(MediaTypeVideo, fileID, raw, ".mp4")
}

func (s *LocalMediaCacheStore) Delete(mediaType MediaType, name string) (bool, error) {
	safeName, err := s.validateName(mediaType, name)
	if err != nil {
		return false, err
	}
	unlock, err := s.guard(mediaType)
	if err != nil {
		return false, err
	}
	defer unlock()

	path, err := s.pathForName(mediaType, safeName)
	if err != nil {
		return false, err
	}
	existed := true
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		existed = false
	} else if err != nil {
		return false, err
	} else if !info.Mode().IsRegular() {
		existed = false
	}
	if existed {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return false, err
		}
	}
	if err := s.deleteIndexRowIfPresent(mediaType, safeName); err != nil {
		return false, err
	}
	return existed, nil
}

func (s *LocalMediaCacheStore) Clear(mediaType MediaType) (int, error) {
	unlock, err := s.guard(mediaType)
	if err != nil {
		return 0, err
	}
	defer unlock()

	dir, err := s.mediaDir(mediaType)
	if err != nil {
		return 0, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, entry := range entries {
		if entry.IsDir() || !s.allowed(mediaType, filepath.Ext(entry.Name())) {
			continue
		}
		if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil && !os.IsNotExist(err) {
			return removed, err
		}
		removed++
	}
	if err := s.deleteIndexRowsIfPresent(mediaType); err != nil {
		return removed, err
	}
	return removed, nil
}

func (s *LocalMediaCacheStore) Reconcile(mediaType MediaType) error {
	if s.limitBytes(mediaType) <= 0 {
		return nil
	}
	unlock, err := s.guard(mediaType)
	if err != nil {
		return err
	}
	defer unlock()
	return s.reconcileIndexed(mediaType)
}

func (s *LocalMediaCacheStore) save(mediaType MediaType, fileID string, raw []byte, suffix string) (string, error) {
	dir, err := s.mediaDir(mediaType)
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, fileID+suffix)
	if s.limitBytes(mediaType) <= 0 {
		if err := writeIfMissing(path, raw); err != nil {
			return "", err
		}
		return path, nil
	}
	unlock, err := s.guard(mediaType)
	if err != nil {
		return "", err
	}
	defer unlock()
	if err := s.saveIndexed(mediaType, path, raw); err != nil {
		return "", err
	}
	return path, nil
}

func (s *LocalMediaCacheStore) limitBytes(mediaType MediaType) int {
	mb := s.config.GetInt(fmt.Sprintf("cache.local.%s_max_mb", mediaType), 0)
	if mb < 0 {
		mb = 0
	}
	return mb * 1024 * 1024
}

func (s *LocalMediaCacheStore) mediaDir(mediaType MediaType) (string, error) {
	switch mediaType {
	case MediaTypeImage:
		return ImageFilesDir()
	case MediaTypeVideo:
		return VideoFilesDir()
	default:
		return "", fmt.Errorf("unsupported media type: %s", mediaType)
	}
}

func (s *LocalMediaCacheStore) pathForName(mediaType MediaType, name string) (string, error) {
	dir, err := s.mediaDir(mediaType)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name), nil
}

func (s *LocalMediaCacheStore) validateName(mediaType MediaType, name string) (string, error) {
	value := strings.TrimSpace(name)
	if value == "" {
		return "", errors.New("missing file name")
	}
	if filepath.Base(value) != value {
		return "", errors.New("invalid file name")
	}
	if !s.allowed(mediaType, filepath.Ext(value)) {
		return "", errors.New("unsupported file type")
	}
	return value, nil
}

func (s *LocalMediaCacheStore) allowed(mediaType MediaType, ext string) bool {
	ext = strings.ToLower(ext)
	switch mediaType {
	case MediaTypeImage:
		_, ok := imageExts[ext]
		return ok
	case MediaTypeVideo:
		_, ok := videoExts[ext]
		return ok
	default:
		return false
	}
}

func (s *LocalMediaCacheStore) lockFor(mediaType MediaType) (*sync.Mutex, error) {
	lock, ok := s.locks[mediaType]
	if !ok {
		return nil, fmt.Errorf("unsupported media type: %s", mediaType)
	}
	return lock, nil
}

func (s *LocalMediaCacheStore) guard(mediaType MediaType) (func(), error) {
	lock, err := s.lockFor(mediaType)
	if err != nil {
		return nil, err
	}
	lock.Lock()
	lockPath, err := LocalMediaLockPath(string(mediaType))
	if err != nil {
		lock.Unlock()
		return nil, err
	}
	fd, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		lock.Unlock()
		return nil, err
	}
	if err := lockMediaFile(fd); err != nil {
		_ = fd.Close()
		lock.Unlock()
		return nil, err
	}
	return func() {
		_ = unlockMediaFile(fd)
		_ = fd.Close()
		lock.Unlock()
	}, nil
}

func writeIfMissing(path string, raw []byte) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.part")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	_, writeErr := tmp.Write(raw)
	closeErr := tmp.Close()
	if writeErr != nil || closeErr != nil {
		_ = os.Remove(tmpName)
		if writeErr != nil {
			return writeErr
		}
		return closeErr
	}
	if _, err := os.Stat(path); err == nil {
		return os.Remove(tmpName)
	} else if !os.IsNotExist(err) {
		_ = os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}
