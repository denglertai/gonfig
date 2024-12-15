package logging

import (
	"context"
	"log/slog"
	"os"

	"github.com/denglertai/gonfig/internal/logging"
)

// Fatal logs a message with severity Fatal on the standard logger then calls os.Exit(1)
func Fatal(msg string, args ...any) {
	slog.Log(context.Background(), logging.LevelFatal, msg, args...)
	os.Exit(1)
}

// Error logs a message with severity Error on the standard logger
func Error(msg string, args ...any) {
	slog.Error(msg, args...)
}

// Warn logs a message with severity Warn on the standard logger
func Warn(msg string, args ...any) {
	slog.Warn(msg, args...)
}

// Info logs a message with severity Info on the standard logger
func Info(msg string, args ...any) {
	slog.Info(msg, args...)
}

// Debug logs a message with severity Debug on the standard logger
func Debug(msg string, args ...any) {
	slog.Debug(msg, args...)
}

// Trace logs a message with severity Trace on the standard logger
func Trace(msg string, args ...any) {
	slog.Log(context.Background(), logging.LevelTrace, msg, args...)
}
