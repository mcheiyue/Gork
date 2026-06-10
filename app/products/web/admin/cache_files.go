package admin

import (
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jiujiu532/grok2api/app/platform"
	"github.com/jiujiu532/grok2api/app/platform/storage"
)

var (
	adminCacheImageExts = map[string]struct{}{".jpg": {}, ".jpeg": {}, ".png": {}, ".gif": {}, ".webp": {}, ".bmp": {}}
	adminCacheVideoExts = map[string]struct{}{".mp4": {}, ".mov": {}, ".m4v": {}, ".webm": {}, ".avi": {}, ".mkv": {}}
)

type adminCacheFile struct {
	Name      string
	SizeBytes int64
	Modified  float64
}

func adminCacheStats(mediaType storage.MediaType) map[string]any {
	files := adminCacheFiles(mediaType)
	totalSize := int64(0)
	for _, file := range files {
		totalSize += file.SizeBytes
	}
	limitMB := maxAdminBatchInt(0, adminCacheConfigInt("cache.local."+string(mediaType)+"_max_mb", 0))
	limitBytes := int64(limitMB) * 1024 * 1024
	return adminCacheStatsPayload(len(files), totalSize, limitMB, limitBytes)
}

func adminCacheStatsPayload(count int, totalSize int64, limitMB int, limitBytes int64) map[string]any {
	var usageRatio any
	var usagePercent any
	if limitBytes > 0 {
		ratio := float64(totalSize) / float64(limitBytes)
		usageRatio = roundAdminCache(ratio, 4)
		usagePercent = roundAdminCache(ratio*100, 1)
	}
	return map[string]any{
		"count": count, "size_mb": roundAdminCache(float64(totalSize)/1024/1024, 2),
		"size_bytes": totalSize, "limit_mb": limitMB, "limit_bytes": limitBytes,
		"limited": limitBytes > 0, "usage_ratio": usageRatio, "usage_percent": usagePercent,
	}
}

func adminCacheListFiles(mediaType storage.MediaType, page int, pageSize int) map[string]any {
	files := adminCacheFiles(mediaType)
	start := (page - 1) * pageSize
	end := start + pageSize
	if start > len(files) {
		start = len(files)
	}
	if end > len(files) {
		end = len(files)
	}
	return map[string]any{"total": len(files), "page": page, "page_size": pageSize, "items": adminCacheListItems(files[start:end])}
}

func adminCacheFiles(mediaType storage.MediaType) []adminCacheFile {
	dir, err := adminCacheDir(mediaType)
	if err != nil {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	files := adminCacheFilesFromEntries(mediaType, dir, entries)
	sort.Slice(files, func(i int, j int) bool { return files[i].Modified > files[j].Modified })
	return files
}

func adminCacheFilesFromEntries(mediaType storage.MediaType, dir string, entries []os.DirEntry) []adminCacheFile {
	files := []adminCacheFile{}
	for _, entry := range entries {
		if entry.IsDir() || !adminCacheAllowed(mediaType, filepath.Ext(entry.Name())) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		files = append(files, adminCacheFile{Name: entry.Name(), SizeBytes: info.Size(), Modified: float64(info.ModTime().UnixNano()) / 1e9})
	}
	return files
}

func adminCacheListItems(files []adminCacheFile) []map[string]any {
	items := make([]map[string]any, 0, len(files))
	for _, file := range files {
		items = append(items, map[string]any{"name": file.Name, "size_bytes": file.SizeBytes, "modified_at": file.Modified})
	}
	return items
}

func adminCacheDeleteNames(mediaType storage.MediaType, names []string) (int, int) {
	deleted, missing := 0, 0
	for _, name := range names {
		removed, err := adminCacheStoreProvider().Delete(mediaType, name)
		if err != nil || !removed {
			missing++
			continue
		}
		deleted++
	}
	return deleted, missing
}

func adminCacheCleanNames(raw []string) []string {
	names := make([]string, 0, len(raw))
	for _, name := range raw {
		if trimmed := strings.TrimSpace(name); trimmed != "" {
			names = append(names, trimmed)
		}
	}
	return names
}

func adminCacheNormalizeType(mediaType *storage.MediaType) error {
	if *mediaType == "" {
		*mediaType = storage.MediaTypeImage
	}
	if *mediaType != storage.MediaTypeImage && *mediaType != storage.MediaTypeVideo {
		return platform.NewValidationError("Invalid cache type", "type", "invalid_value")
	}
	return nil
}

func adminCacheDir(mediaType storage.MediaType) (string, error) {
	if mediaType == storage.MediaTypeVideo {
		return adminCacheVideoDir()
	}
	return adminCacheImageDir()
}

func adminCacheAllowed(mediaType storage.MediaType, ext string) bool {
	ext = strings.ToLower(ext)
	if mediaType == storage.MediaTypeVideo {
		_, ok := adminCacheVideoExts[ext]
		return ok
	}
	_, ok := adminCacheImageExts[ext]
	return ok
}

func adminCacheInvalidFileName(err error) error {
	return platform.NewAppError(err.Error(), platform.ErrorKindValidation, "invalid_file_name", 400, nil)
}

func adminCacheFileNotFound() error {
	return platform.NewAppError("File not found", platform.ErrorKindValidation, "file_not_found", 404, nil)
}

func roundAdminCache(value float64, places int) float64 {
	scale := math.Pow10(places)
	return math.Round(value*scale) / scale
}
