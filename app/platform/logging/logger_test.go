package logging

import (
	"log/slog"
	"path/filepath"
	"testing"
)

func TestLoggerIsCanonicalDefaultLogger(t *testing.T) {
	if Logger == nil {
		t.Fatal("Logger is nil")
	}

	if err := SetupLogging(LoggingOptions{JSONConsole: true, FileLogging: boolPtr(false)}); err != nil {
		t.Fatalf("SetupLogging returned error: %v", err)
	}
	defer func() { _ = SetupLogging(LoggingOptions{FileLogging: boolPtr(false)}) }()

	if Logger == nil || Logger != slog.Default() {
		t.Fatalf("Logger is not the default slog logger: logger=%p default=%p", Logger, slog.Default())
	}
}

func TestSetupLoggingConfiguresConsoleAndFileSinks(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logs")

	if err := SetupLogging(LoggingOptions{
		Level:       "debug",
		FileLevel:   "warning",
		JSONConsole: true,
		FileLogging: boolPtr(true),
		LogDir:      dir,
		MaxFiles:    3,
	}); err != nil {
		t.Fatalf("SetupLogging returned error: %v", err)
	}
	defer func() { _ = SetupLogging(LoggingOptions{FileLogging: boolPtr(false)}) }()

	state := CurrentLoggingState()
	if !state.Configured || state.ConsoleLevel != "DEBUG" || !state.JSONConsole || !state.FileLogging {
		t.Fatalf("state = %#v", state)
	}
	if state.ConsoleSink == nil || state.ConsoleSink.Level != "DEBUG" ||
		state.ConsoleSink.Format != "{time} | {level} | {name}:{function}:{line} | {message}" ||
		state.ConsoleSink.Colorize {
		t.Fatalf("console sink = %#v", state.ConsoleSink)
	}
	if state.FileSink == nil || state.FileSink.Level != "WARNING" ||
		state.FileSink.PathPattern != filepath.Join(dir, "app_{time:YYYY-MM-DD}.log") ||
		state.FileSink.Rotation != "00:00" || state.FileSink.Retention != 3 ||
		!state.FileSink.Enqueue || state.FileSink.Encoding != "utf-8" {
		t.Fatalf("file sink = %#v", state.FileSink)
	}
}

func TestSetupLoggingResetsExistingSinks(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logs")
	if err := SetupLogging(LoggingOptions{
		Level:       "debug",
		FileLogging: boolPtr(true),
		LogDir:      dir,
		MaxFiles:    3,
	}); err != nil {
		t.Fatalf("initial SetupLogging returned error: %v", err)
	}
	if CurrentLoggingState().FileSink == nil {
		t.Fatal("initial file sink was not configured")
	}

	if err := SetupLogging(LoggingOptions{Level: "error", FileLogging: boolPtr(false)}); err != nil {
		t.Fatalf("second SetupLogging returned error: %v", err)
	}
	defer func() { _ = SetupLogging(LoggingOptions{FileLogging: boolPtr(false)}) }()

	state := CurrentLoggingState()
	if !state.Configured || state.ConsoleSink == nil || state.ConsoleSink.ID != 1 ||
		state.ConsoleLevel != "ERROR" || state.FileLogging || state.FileSink != nil ||
		state.LogDirOverride != "" {
		t.Fatalf("state after reset = %#v", state)
	}
}

func TestReloadLoggingUsesEnvLevelAndFileEnabledFlag(t *testing.T) {
	t.Setenv("LOG_LEVEL", "warning")
	t.Setenv("LOG_FILE_ENABLED", "off")

	if err := ReloadLogging(ReloadLoggingOptions{DefaultLevel: "INFO"}); err != nil {
		t.Fatalf("ReloadLogging returned error: %v", err)
	}

	state := CurrentLoggingState()
	if state.ConsoleLevel != "WARNING" || state.FileLogging || state.FileSink != nil {
		t.Fatalf("state = %#v", state)
	}
}

func TestReloadFileLoggingWhenUnconfiguredDelegatesToReloadLogging(t *testing.T) {
	resetLoggingSinks()
	defer func() { _ = SetupLogging(LoggingOptions{FileLogging: boolPtr(false)}) }()

	t.Setenv("LOG_LEVEL", "warning")
	t.Setenv("LOG_FILE_ENABLED", "off")

	if err := ReloadFileLogging(ReloadFileLoggingOptions{FileLevel: "error", MaxFiles: 4}); err != nil {
		t.Fatalf("ReloadFileLogging returned error: %v", err)
	}

	state := CurrentLoggingState()
	if !state.Configured || state.ConsoleLevel != "WARNING" || state.JSONConsole ||
		state.FileLogging || state.FileSink != nil || state.LogDirOverride != "" {
		t.Fatalf("delegated state = %#v", state)
	}
}

func TestReloadFileLoggingPreservesConsoleAndUsesOverrideDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logs")
	if err := SetupLogging(LoggingOptions{
		Level:       "info",
		FileLevel:   "info",
		FileLogging: boolPtr(true),
		LogDir:      dir,
		MaxFiles:    5,
	}); err != nil {
		t.Fatalf("SetupLogging returned error: %v", err)
	}
	defer func() { _ = SetupLogging(LoggingOptions{FileLogging: boolPtr(false)}) }()

	t.Setenv("LOG_FILE_ENABLED", "false")
	if err := ReloadFileLogging(ReloadFileLoggingOptions{FileLevel: "debug", MaxFiles: 2}); err != nil {
		t.Fatalf("ReloadFileLogging disable returned error: %v", err)
	}
	state := CurrentLoggingState()
	if state.ConsoleSink == nil || state.ConsoleLevel != "INFO" || state.FileLogging || state.FileSink != nil {
		t.Fatalf("disabled state = %#v", state)
	}

	t.Setenv("LOG_FILE_ENABLED", "yes")
	if err := ReloadFileLogging(ReloadFileLoggingOptions{FileLevel: "debug", MaxFiles: 2}); err != nil {
		t.Fatalf("ReloadFileLogging enable returned error: %v", err)
	}
	state = CurrentLoggingState()
	if state.ConsoleLevel != "INFO" || !state.FileLogging || state.FileSink == nil ||
		state.FileSink.Level != "DEBUG" || state.FileSink.Retention != 2 ||
		state.FileSink.PathPattern != filepath.Join(dir, "app_{time:YYYY-MM-DD}.log") {
		t.Fatalf("enabled state = %#v", state)
	}
}

func TestEnvBoolMatchesPythonTruthSet(t *testing.T) {
	t.Setenv("LOG_TEST_BOOL", "yes")
	if !envBool("LOG_TEST_BOOL", false) {
		t.Fatal("yes should be true")
	}
	t.Setenv("LOG_TEST_BOOL", "0")
	if envBool("LOG_TEST_BOOL", true) {
		t.Fatal("0 should be false even when default is true")
	}
	if !envBool("LOG_TEST_BOOL_MISSING", true) {
		t.Fatal("missing env should return default true")
	}
	if envBool("LOG_TEST_BOOL_MISSING", false) {
		t.Fatal("missing env should return default false")
	}
}

func boolPtr(value bool) *bool {
	return &value
}
