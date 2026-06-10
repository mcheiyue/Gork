package storage

import (
	"os"
	"path/filepath"

	"github.com/jiujiu532/grok2api/app/platform"
)

func filesDir() string {
	return platform.DataPath("files")
}

func cacheDir() string {
	return platform.DataPath("cache")
}

// ImageFilesDir returns the local image storage directory and creates it.
func ImageFilesDir() (string, error) {
	path := filepath.Join(filesDir(), "images")
	if err := os.MkdirAll(path, 0o755); err != nil {
		return "", err
	}
	return path, nil
}

// VideoFilesDir returns the local video storage directory and creates it.
func VideoFilesDir() (string, error) {
	path := filepath.Join(filesDir(), "videos")
	if err := os.MkdirAll(path, 0o755); err != nil {
		return "", err
	}
	return path, nil
}

// LocalMediaCacheDBPath returns the SQLite index path for local media cache bookkeeping.
func LocalMediaCacheDBPath() (string, error) {
	path := cacheDir()
	if err := os.MkdirAll(path, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(path, "local_media_cache.db"), nil
}

// LocalMediaLockPath returns the advisory lock file used by one media-type cache operation.
func LocalMediaLockPath(mediaType string) (string, error) {
	path := filepath.Join(cacheDir(), "locks")
	if err := os.MkdirAll(path, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(path, "local_media_"+mediaType+".lock"), nil
}
