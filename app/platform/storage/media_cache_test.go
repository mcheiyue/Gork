package storage

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

type staticMediaConfig map[string]int

func (c staticMediaConfig) GetInt(key string, defaultValue int) int {
	if value, ok := c[key]; ok {
		return value
	}
	return defaultValue
}

func TestLocalMediaCacheSavesWithoutLimitAndDoesNotOverwriteExisting(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("DATA_DIR", dataDir)
	store := NewLocalMediaCacheStore(LocalMediaCacheOptions{Config: staticMediaConfig{}})

	id, err := store.SaveImage([]byte("first"), "image/png", "img-1")
	if err != nil {
		t.Fatalf("SaveImage returned error: %v", err)
	}
	if id != "img-1" {
		t.Fatalf("SaveImage id = %q", id)
	}
	imagePath := filepath.Join(dataDir, "files", "images", "img-1.png")
	if got := readTestFile(t, imagePath); got != "first" {
		t.Fatalf("image contents = %q", got)
	}

	if _, err := store.SaveImage([]byte("second"), "image/png", "img-1"); err != nil {
		t.Fatalf("second SaveImage returned error: %v", err)
	}
	if got := readTestFile(t, imagePath); got != "first" {
		t.Fatalf("existing image should not be overwritten, got %q", got)
	}

	videoPath, err := store.SaveVideo([]byte("video"), "vid-1")
	if err != nil {
		t.Fatalf("SaveVideo returned error: %v", err)
	}
	if videoPath != filepath.Join(dataDir, "files", "videos", "vid-1.mp4") {
		t.Fatalf("video path = %q", videoPath)
	}
	if got := readTestFile(t, videoPath); got != "video" {
		t.Fatalf("video contents = %q", got)
	}
}

func TestLocalMediaCacheDeleteAndClearValidateMediaNames(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("DATA_DIR", dataDir)
	store := NewLocalMediaCacheStore(LocalMediaCacheOptions{Config: staticMediaConfig{}})
	if _, err := store.SaveImage([]byte("image"), "image/jpeg", "img-2"); err != nil {
		t.Fatalf("SaveImage returned error: %v", err)
	}
	imagePath := filepath.Join(dataDir, "files", "images", "img-2.jpg")

	deleted, err := store.Delete(MediaTypeImage, "../img-2.jpg")
	if err == nil || deleted {
		t.Fatalf("Delete path traversal deleted=%t err=%v", deleted, err)
	}
	deleted, err = store.Delete(MediaTypeImage, "img-2.mp4")
	if err == nil || deleted {
		t.Fatalf("Delete unsupported extension deleted=%t err=%v", deleted, err)
	}
	deleted, err = store.Delete(MediaTypeImage, "img-2.jpg")
	if err != nil || !deleted {
		t.Fatalf("Delete valid image deleted=%t err=%v", deleted, err)
	}
	if _, err := os.Stat(imagePath); !os.IsNotExist(err) {
		t.Fatalf("image should be removed, stat err=%v", err)
	}

	imagesDir, err := ImageFilesDir()
	if err != nil {
		t.Fatalf("ImageFilesDir returned error: %v", err)
	}
	fakeImageDir := filepath.Join(imagesDir, "folder.jpg")
	if err := os.Mkdir(fakeImageDir, 0o755); err != nil {
		t.Fatalf("mkdir fake image dir: %v", err)
	}
	deleted, err = store.Delete(MediaTypeImage, "folder.jpg")
	if err != nil || deleted {
		t.Fatalf("Delete directory-like image deleted=%t err=%v", deleted, err)
	}
	if info, err := os.Stat(fakeImageDir); err != nil || !info.IsDir() {
		t.Fatalf("fake image directory should remain, info=%v err=%v", info, err)
	}
	if err := os.WriteFile(filepath.Join(imagesDir, "keep.txt"), []byte("text"), 0o644); err != nil {
		t.Fatalf("write text file: %v", err)
	}
	if _, err := store.SaveImage([]byte("png"), "image/png", "clear-me"); err != nil {
		t.Fatalf("SaveImage returned error: %v", err)
	}
	removed, err := store.Clear(MediaTypeImage)
	if err != nil || removed != 1 {
		t.Fatalf("Clear removed=%d err=%v", removed, err)
	}
	if got := readTestFile(t, filepath.Join(imagesDir, "keep.txt")); got != "text" {
		t.Fatalf("non-media file should remain, got %q", got)
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func TestLocalMediaCacheIndexedSaveCreatesSchemaRowsAndLock(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("DATA_DIR", dataDir)
	store := NewLocalMediaCacheStore(LocalMediaCacheOptions{Config: staticMediaConfig{
		"cache.local.image_max_mb": 1,
	}})

	if _, err := store.SaveImage([]byte("indexed"), "image/png", "indexed"); err != nil {
		t.Fatalf("SaveImage returned error: %v", err)
	}
	db := openMediaCacheDB(t)
	defer db.Close()

	var size int
	var created, updated int64
	err := db.QueryRow(`
		SELECT size_bytes, created_at_ns, updated_at_ns
		FROM local_media_files
		WHERE media_type = ? AND name = ?
	`, string(MediaTypeImage), "indexed.png").Scan(&size, &created, &updated)
	if err != nil {
		t.Fatalf("query indexed row: %v", err)
	}
	if size != len("indexed") || created <= 0 || updated <= 0 {
		t.Fatalf("row size=%d created=%d updated=%d", size, created, updated)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "cache", "locks", "local_media_image.lock")); err != nil {
		t.Fatalf("expected image lock file: %v", err)
	}

	if _, err := store.SaveImage([]byte("replacement"), "image/png", "indexed"); err != nil {
		t.Fatalf("second SaveImage returned error: %v", err)
	}
	var createdAgain int64
	if err := db.QueryRow(`
		SELECT created_at_ns FROM local_media_files
		WHERE media_type = ? AND name = ?
	`, string(MediaTypeImage), "indexed.png").Scan(&createdAgain); err != nil {
		t.Fatalf("query created_at_ns: %v", err)
	}
	if createdAgain != created {
		t.Fatalf("created_at_ns changed from %d to %d", created, createdAgain)
	}
	if got := readTestFile(t, filepath.Join(dataDir, "files", "images", "indexed.png")); got != "indexed" {
		t.Fatalf("existing indexed file should not be overwritten, got %q", got)
	}
}

func TestLocalMediaCacheReconcileRebuildsIndex(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("DATA_DIR", dataDir)
	store := NewLocalMediaCacheStore(LocalMediaCacheOptions{Config: staticMediaConfig{
		"cache.local.image_max_mb": 1,
	}})
	imagesDir, err := ImageFilesDir()
	if err != nil {
		t.Fatalf("ImageFilesDir returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(imagesDir, "one.jpg"), []byte("one"), 0o644); err != nil {
		t.Fatalf("write one: %v", err)
	}
	if err := os.WriteFile(filepath.Join(imagesDir, "skip.txt"), []byte("skip"), 0o644); err != nil {
		t.Fatalf("write skip: %v", err)
	}

	if err := store.Reconcile(MediaTypeImage); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}
	db := openMediaCacheDB(t)
	defer db.Close()
	if got := countRows(t, db, MediaTypeImage); got != 1 {
		t.Fatalf("indexed rows = %d", got)
	}
	var size int
	if err := db.QueryRow(`SELECT size_bytes FROM local_media_files WHERE name = ?`, "one.jpg").Scan(&size); err != nil {
		t.Fatalf("query one.jpg: %v", err)
	}
	if size != 3 {
		t.Fatalf("one.jpg size = %d", size)
	}
}

func TestLocalMediaCacheEnforcesLimitToLowWatermarkAndProtectsNewest(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("DATA_DIR", dataDir)
	store := NewLocalMediaCacheStore(LocalMediaCacheOptions{Config: staticMediaConfig{
		"cache.local.image_max_mb": 1,
	}})
	chunk := make([]byte, 400*1024)
	for _, id := range []string{"old-a", "old-b", "new-c"} {
		if _, err := store.SaveImage(chunk, "image/jpeg", id); err != nil {
			t.Fatalf("SaveImage(%s) returned error: %v", id, err)
		}
	}
	imagesDir := filepath.Join(dataDir, "files", "images")
	if _, err := os.Stat(filepath.Join(imagesDir, "new-c.jpg")); err != nil {
		t.Fatalf("newest file should remain: %v", err)
	}
	if _, err := os.Stat(filepath.Join(imagesDir, "old-a.jpg")); !os.IsNotExist(err) {
		t.Fatalf("old-a should be evicted, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(imagesDir, "old-b.jpg")); !os.IsNotExist(err) {
		t.Fatalf("old-b should be evicted, stat err=%v", err)
	}
	db := openMediaCacheDB(t)
	defer db.Close()
	if got := countRows(t, db, MediaTypeImage); got != 1 {
		t.Fatalf("indexed rows after trim = %d", got)
	}
}

func TestLocalMediaCacheDeleteRemovesStaleIndexRow(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("DATA_DIR", dataDir)
	store := NewLocalMediaCacheStore(LocalMediaCacheOptions{Config: staticMediaConfig{
		"cache.local.image_max_mb": 1,
	}})
	if _, err := store.SaveImage([]byte("stale"), "image/png", "stale"); err != nil {
		t.Fatalf("SaveImage returned error: %v", err)
	}
	if err := os.Remove(filepath.Join(dataDir, "files", "images", "stale.png")); err != nil {
		t.Fatalf("remove media file: %v", err)
	}
	deleted, err := store.Delete(MediaTypeImage, "stale.png")
	if err != nil || deleted {
		t.Fatalf("Delete stale row deleted=%t err=%v", deleted, err)
	}
	db := openMediaCacheDB(t)
	defer db.Close()
	if got := countRows(t, db, MediaTypeImage); got != 0 {
		t.Fatalf("stale indexed rows = %d", got)
	}
}

func openMediaCacheDB(t *testing.T) *sql.DB {
	t.Helper()
	dbPath, err := LocalMediaCacheDBPath()
	if err != nil {
		t.Fatalf("LocalMediaCacheDBPath returned error: %v", err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	return db
}

func countRows(t *testing.T, db *sql.DB, mediaType MediaType) int {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM local_media_files WHERE media_type = ?`, string(mediaType)).Scan(&count); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	return count
}
