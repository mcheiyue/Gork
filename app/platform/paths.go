package platform

import (
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
)

func rootDir() string {
	_, file, _, ok := goruntime.Caller(0)
	if ok && filepath.IsAbs(file) {
		return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	}
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return filepath.Clean(wd)
}

func resolveEnvPath(name, defaultValue string) string {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		raw = defaultValue
	}
	if filepath.IsAbs(raw) {
		return filepath.Clean(raw)
	}
	return filepath.Join(rootDir(), raw)
}

// DataDir returns the configured data directory.
func DataDir() string {
	return resolveEnvPath("DATA_DIR", "data")
}

// LogDir returns the configured log directory.
func LogDir() string {
	return resolveEnvPath("LOG_DIR", "logs")
}

// DataPath joins path parts under DataDir.
func DataPath(parts ...string) string {
	all := append([]string{DataDir()}, parts...)
	return filepath.Join(all...)
}

// LogPath joins path parts under LogDir.
func LogPath(parts ...string) string {
	all := append([]string{LogDir()}, parts...)
	return filepath.Join(all...)
}
