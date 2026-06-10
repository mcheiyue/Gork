package logging

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jiujiu532/grok2api/app/platform"
)

const (
	textLogFormat = "<green>{time:YYYY-MM-DD HH:mm:ss.SSS}</green> | <level>{level: <8}</level> | <cyan>{name}</cyan>:<cyan>{function}</cyan>:<cyan>{line}</cyan> - <level>{message}</level>"
	jsonLogFormat = "{time} | {level} | {name}:{function}:{line} | {message}"
)

var Logger = slog.Default()

type LoggingOptions struct {
	Level       string
	FileLevel   string
	JSONConsole bool
	FileLogging *bool
	LogDir      string
	MaxFiles    int
}

type ReloadLoggingOptions struct {
	Level        string
	DefaultLevel string
	JSONConsole  bool
	MaxFiles     int
	FileLevel    string
}

type ReloadFileLoggingOptions struct {
	FileLevel string
	MaxFiles  int
}

type LoggingSinkState struct {
	ID          int
	Kind        string
	Level       string
	Format      string
	Colorize    bool
	Enqueue     bool
	Backtrace   bool
	Diagnose    bool
	PathPattern string
	Rotation    string
	Retention   int
	Encoding    string
}

type LoggingState struct {
	Configured     bool
	ConsoleSink    *LoggingSinkState
	FileSink       *LoggingSinkState
	ConsoleLevel   string
	JSONConsole    bool
	FileLogging    bool
	LogDirOverride string
}

var currentState = LoggingState{
	ConsoleLevel: "INFO",
	FileLogging:  true,
}
var nextSinkID int
var fileHandle *os.File

func SetupLogging(options LoggingOptions) error {
	resetLoggingSinks()

	resolvedLevel := normalizeLevel(defaultString(options.Level, "INFO"))
	maxFiles := defaultInt(options.MaxFiles, 7)
	fileLogging := true
	if options.FileLogging != nil {
		fileLogging = *options.FileLogging
	}

	currentState.ConsoleSink = newConsoleSink(resolvedLevel, options.JSONConsole)
	if fileLogging {
		fileLevel := normalizeLevel(defaultString(options.FileLevel, resolvedLevel))
		if err := addFileSink(fileLevel, maxFiles, options.LogDir); err != nil {
			return err
		}
	}

	currentState.Configured = true
	currentState.ConsoleLevel = resolvedLevel
	currentState.JSONConsole = options.JSONConsole
	currentState.FileLogging = fileLogging
	currentState.LogDirOverride = options.LogDir
	rebuildLogger(resolvedLevel, options.JSONConsole)
	return nil
}

func ReloadLogging(options ReloadLoggingOptions) error {
	defaultLevel := defaultString(options.DefaultLevel, "INFO")
	resolvedLevel := strings.TrimSpace(options.Level)
	if resolvedLevel == "" {
		resolvedLevel = os.Getenv("LOG_LEVEL")
		if resolvedLevel == "" {
			resolvedLevel = defaultLevel
		}
	}
	fileLogging := envBool("LOG_FILE_ENABLED", true)
	fileLevel := options.FileLevel
	if fileLevel == "" {
		fileLevel = resolvedLevel
	}
	return SetupLogging(LoggingOptions{
		Level:       resolvedLevel,
		FileLevel:   fileLevel,
		JSONConsole: options.JSONConsole,
		FileLogging: &fileLogging,
		MaxFiles:    options.MaxFiles,
	})
}

func ReloadFileLogging(options ReloadFileLoggingOptions) error {
	if !currentState.Configured {
		return ReloadLogging(ReloadLoggingOptions{
			FileLevel: options.FileLevel,
			MaxFiles:  options.MaxFiles,
		})
	}

	removeFileSink()
	currentState.FileLogging = envBool("LOG_FILE_ENABLED", true)
	if !currentState.FileLogging {
		rebuildLogger(currentState.ConsoleLevel, currentState.JSONConsole)
		return nil
	}

	fileLevel := normalizeLevel(defaultString(options.FileLevel, currentState.ConsoleLevel))
	if err := addFileSink(fileLevel, defaultInt(options.MaxFiles, 7), currentState.LogDirOverride); err != nil {
		return err
	}
	rebuildLogger(currentState.ConsoleLevel, currentState.JSONConsole)
	return nil
}

func CurrentLoggingState() LoggingState {
	state := currentState
	state.ConsoleSink = cloneSink(currentState.ConsoleSink)
	state.FileSink = cloneSink(currentState.FileSink)
	return state
}

func newConsoleSink(level string, jsonConsole bool) *LoggingSinkState {
	nextSinkID++
	return &LoggingSinkState{
		ID:        nextSinkID,
		Kind:      "console",
		Level:     level,
		Format:    chooseFormat(jsonConsole),
		Colorize:  !jsonConsole,
		Enqueue:   false,
		Backtrace: false,
		Diagnose:  false,
	}
}

func addFileSink(fileLevel string, maxFiles int, logDir string) error {
	dir := logDir
	if dir == "" {
		dir = platform.LogDir()
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	handle, err := os.OpenFile(filepath.Join(dir, "app_"+time.Now().Format("2006-01-02")+".log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	removeFileSink()
	fileHandle = handle
	nextSinkID++
	currentState.FileSink = &LoggingSinkState{
		ID:          nextSinkID,
		Kind:        "file",
		Level:       fileLevel,
		Format:      textLogFormat,
		Colorize:    false,
		Enqueue:     true,
		Backtrace:   false,
		Diagnose:    false,
		PathPattern: filepath.Join(dir, "app_{time:YYYY-MM-DD}.log"),
		Rotation:    "00:00",
		Retention:   maxFiles,
		Encoding:    "utf-8",
	}
	return nil
}

func resetLoggingSinks() {
	removeFileSink()
	currentState = LoggingState{
		ConsoleLevel: "INFO",
		FileLogging:  true,
	}
	nextSinkID = 0
}

func removeFileSink() {
	if fileHandle != nil {
		_ = fileHandle.Close()
		fileHandle = nil
	}
	currentState.FileSink = nil
}

func rebuildLogger(level string, jsonConsole bool) {
	writer := io.Writer(os.Stdout)
	if fileHandle != nil {
		writer = io.MultiWriter(os.Stdout, fileHandle)
	}
	options := &slog.HandlerOptions{Level: slogLevel(level)}
	if jsonConsole {
		Logger = slog.New(slog.NewJSONHandler(writer, options))
	} else {
		Logger = slog.New(slog.NewTextHandler(writer, options))
	}
	slog.SetDefault(Logger)
}

func slogLevel(level string) slog.Level {
	switch normalizeLevel(level) {
	case "DEBUG", "TRACE":
		return slog.LevelDebug
	case "WARNING", "WARN":
		return slog.LevelWarn
	case "ERROR":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func chooseFormat(jsonConsole bool) string {
	if jsonConsole {
		return jsonLogFormat
	}
	return textLogFormat
}

func envBool(name string, defaultValue bool) bool {
	value, ok := os.LookupEnv(name)
	if !ok {
		return defaultValue
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func defaultInt(value, fallback int) int {
	if value == 0 {
		return fallback
	}
	return value
}

func normalizeLevel(level string) string {
	return strings.ToUpper(strings.TrimSpace(level))
}

func cloneSink(sink *LoggingSinkState) *LoggingSinkState {
	if sink == nil {
		return nil
	}
	copied := *sink
	return &copied
}
