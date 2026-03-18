package log

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

// Logger provides structured logging with contextual fields.
type Logger struct {
	logger *slog.Logger
	file   *os.File
}

// New creates a structured logger. Logs to ~/.jorm/logs/<runID>.log.
// If debug is true, also logs at debug level.
func New(runID string, debug bool) (*Logger, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("getting home dir: %w", err)
	}

	logDir := filepath.Join(home, ".jorm", "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating log dir: %w", err)
	}

	logPath := filepath.Join(logDir, runID+".log")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("opening log file: %w", err)
	}

	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}

	handler := slog.NewJSONHandler(f, &slog.HandlerOptions{Level: level})
	logger := slog.New(handler)

	return &Logger{logger: logger, file: f}, nil
}

// Close closes the log file.
func (l *Logger) Close() error {
	if l.file != nil {
		return l.file.Close()
	}
	return nil
}

// Debug logs at debug level.
func (l *Logger) Debug(msg string, args ...any) {
	l.logger.Debug(msg, args...)
}

// Info logs at info level.
func (l *Logger) Info(msg string, args ...any) {
	l.logger.Info(msg, args...)
}

// Warn logs at warn level.
func (l *Logger) Warn(msg string, args ...any) {
	l.logger.Warn(msg, args...)
}

// Error logs at error level.
func (l *Logger) Error(msg string, args ...any) {
	l.logger.Error(msg, args...)
}

// With returns a new Logger with the given attributes added.
func (l *Logger) With(args ...any) *Logger {
	return &Logger{
		logger: l.logger.With(args...),
		file:   l.file,
	}
}
