package logging

import (
	"os"
	"path/filepath"
	"sync"

	"github.com/baibeicha/bblib/logger"
)

var (
	globalLogger *logger.Logger
	mu           sync.RWMutex
	logFilePath  string
)

// Init initializes the application logger strictly writing to file (no stdout/stderr pollution).
// If projectDir is valid, logs to <projectDir>/.tahr/tahr.log, otherwise to ~/.config/tahr/logs/tahr.log.
func Init(projectDir string) (*logger.Logger, error) {
	mu.Lock()
	defer mu.Unlock()

	if globalLogger != nil {
		_ = globalLogger.Close()
	}

	targetLogFile := ""
	if projectDir != "" {
		if fi, err := os.Stat(projectDir); err == nil && fi.IsDir() {
			tahrDir := filepath.Join(projectDir, ".tahr")
			_ = os.MkdirAll(tahrDir, 0755)
			targetLogFile = filepath.Join(tahrDir, "tahr.log")
		}
	}

	if targetLogFile == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = os.TempDir()
		}
		logsDir := filepath.Join(home, ".config", "tahr", "logs")
		_ = os.MkdirAll(logsDir, 0755)
		targetLogFile = filepath.Join(logsDir, "tahr.log")
	}

	logFilePath = targetLogFile

	l, err := logger.New(
		logger.WithLevel("debug"),
		logger.WithEnv("production"),
		logger.WithDisableStdout(),
		logger.WithFileOutput(targetLogFile, 10, 5, 30, true),
	)
	if err != nil {
		return nil, err
	}

	globalLogger = l
	return l, nil
}

// Get returns the global logger instance.
func Get() *logger.Logger {
	mu.RLock()
	defer mu.RUnlock()
	return globalLogger
}

// LogFilePath returns the current target log file path.
func LogFilePath() string {
	mu.RLock()
	defer mu.RUnlock()
	return logFilePath
}

// Close closes the logger file descriptor.
func Close() error {
	mu.Lock()
	defer mu.Unlock()
	if globalLogger != nil {
		err := globalLogger.Close()
		globalLogger = nil
		return err
	}
	return nil
}

// Info logs an informational message.
func Info(msg string, args ...any) {
	globalRing.Append("INFO", msg, args...)
	mu.RLock()
	l := globalLogger
	mu.RUnlock()
	if l != nil {
		l.Info(msg, args...)
	}
}

// Debug logs a debug message.
func Debug(msg string, args ...any) {
	globalRing.Append("DEBG", msg, args...)
	mu.RLock()
	l := globalLogger
	mu.RUnlock()
	if l != nil {
		l.Debug(msg, args...)
	}
}

// Warn logs a warning message.
func Warn(msg string, args ...any) {
	globalRing.Append("WARN", msg, args...)
	mu.RLock()
	l := globalLogger
	mu.RUnlock()
	if l != nil {
		l.Warn(msg, args...)
	}
}

// Error logs an error message.
func Error(msg string, args ...any) {
	globalRing.Append("ERRO", msg, args...)
	mu.RLock()
	l := globalLogger
	mu.RUnlock()
	if l != nil {
		l.Error(msg, args...)
	}
}
