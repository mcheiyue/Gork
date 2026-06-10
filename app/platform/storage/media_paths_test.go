package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMediaPathsCreateExpectedDirectories(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("DATA_DIR", dataDir)

	imageDir, err := ImageFilesDir()
	if err != nil {
		t.Fatalf("ImageFilesDir returned error: %v", err)
	}
	if want := filepath.Join(dataDir, "files", "images"); imageDir != want {
		t.Fatalf("ImageFilesDir() = %q, want %q", imageDir, want)
	}
	assertDirExists(t, imageDir)

	videoDir, err := VideoFilesDir()
	if err != nil {
		t.Fatalf("VideoFilesDir returned error: %v", err)
	}
	if want := filepath.Join(dataDir, "files", "videos"); videoDir != want {
		t.Fatalf("VideoFilesDir() = %q, want %q", videoDir, want)
	}
	assertDirExists(t, videoDir)
}

func TestMediaCachePathsCreateCacheDirectories(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("DATA_DIR", dataDir)

	dbPath, err := LocalMediaCacheDBPath()
	if err != nil {
		t.Fatalf("LocalMediaCacheDBPath returned error: %v", err)
	}
	if want := filepath.Join(dataDir, "cache", "local_media_cache.db"); dbPath != want {
		t.Fatalf("LocalMediaCacheDBPath() = %q, want %q", dbPath, want)
	}
	assertDirExists(t, filepath.Dir(dbPath))

	lockPath, err := LocalMediaLockPath("image")
	if err != nil {
		t.Fatalf("LocalMediaLockPath returned error: %v", err)
	}
	if want := filepath.Join(dataDir, "cache", "locks", "local_media_image.lock"); lockPath != want {
		t.Fatalf("LocalMediaLockPath() = %q, want %q", lockPath, want)
	}
	assertDirExists(t, filepath.Dir(lockPath))
}

func assertDirExists(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("expected directory %q to exist: %v", path, err)
	}
	if !info.IsDir() {
		t.Fatalf("%q exists but is not a directory", path)
	}
}
