package platform

import (
	"path/filepath"
	"runtime"
	"testing"
)

func repoRootForPathTest(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func TestDataAndLogDirsUseDefaultsFromRepoRoot(t *testing.T) {
	t.Setenv("DATA_DIR", "")
	t.Setenv("LOG_DIR", "")
	root := repoRootForPathTest(t)

	if got, want := DataDir(), filepath.Join(root, "data"); got != want {
		t.Fatalf("DataDir() = %q, want %q", got, want)
	}
	if got, want := LogDir(), filepath.Join(root, "logs"); got != want {
		t.Fatalf("LogDir() = %q, want %q", got, want)
	}
}

func TestPathsTrimEnvAndResolveRelativeValues(t *testing.T) {
	t.Setenv("DATA_DIR", " custom-data ")
	t.Setenv("LOG_DIR", " custom-logs ")
	root := repoRootForPathTest(t)

	if got, want := DataDir(), filepath.Join(root, "custom-data"); got != want {
		t.Fatalf("DataDir() = %q, want %q", got, want)
	}
	if got, want := LogDir(), filepath.Join(root, "custom-logs"); got != want {
		t.Fatalf("LogDir() = %q, want %q", got, want)
	}
	if got, want := DataPath("nested", "file.db"), filepath.Join(root, "custom-data", "nested", "file.db"); got != want {
		t.Fatalf("DataPath() = %q, want %q", got, want)
	}
	if got, want := LogPath("app.log"), filepath.Join(root, "custom-logs", "app.log"); got != want {
		t.Fatalf("LogPath() = %q, want %q", got, want)
	}
}

func TestPathHelpersWithoutPartsReturnBaseDirs(t *testing.T) {
	t.Setenv("DATA_DIR", "data")
	t.Setenv("LOG_DIR", "logs")

	if got, want := DataPath(), DataDir(); got != want {
		t.Fatalf("DataPath() = %q, want %q", got, want)
	}
	if got, want := LogPath(), LogDir(); got != want {
		t.Fatalf("LogPath() = %q, want %q", got, want)
	}
}

func TestPathsPreserveAbsoluteEnvValues(t *testing.T) {
	dataAbs := filepath.Join(t.TempDir(), "data")
	logAbs := filepath.Join(t.TempDir(), "logs")
	t.Setenv("DATA_DIR", dataAbs)
	t.Setenv("LOG_DIR", logAbs)

	if got := DataDir(); got != dataAbs {
		t.Fatalf("DataDir() = %q, want %q", got, dataAbs)
	}
	if got := LogDir(); got != logAbs {
		t.Fatalf("LogDir() = %q, want %q", got, logAbs)
	}
}
