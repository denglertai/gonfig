package logging

import (
	"fmt"
	"log/slog"
	"os"
)

const (
	LevelTrace = slog.Level(-8)
	LevelFatal = slog.Level(12)
)

var levelNames = map[slog.Leveler]string{
	LevelTrace: "TRACE",
	LevelFatal: "FATAL",
}

// InitLogging sets the default logger, writing JSON to stderr at the given level including trace and fatal,
// optionally with the source location. It fails for unknown levels.
func InitLogging(logLevel string, addSource bool) error {
	var slogLogLevel slog.Level
	switch logLevel {
	case "debug":
		slogLogLevel = slog.LevelDebug
	case "info":
		slogLogLevel = slog.LevelInfo
	case "warn":
		slogLogLevel = slog.LevelWarn
	case "error":
		slogLogLevel = slog.LevelError
	case "trace":
		slogLogLevel = LevelTrace
	case "fatal":
		slogLogLevel = LevelFatal
	default:
		return fmt.Errorf("invalid log level: %s", logLevel)
	}

	defaultLogger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		AddSource: addSource,
		Level:     slogLogLevel,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.LevelKey {
				level := a.Value.Any().(slog.Level)
				levelLabel, exists := levelNames[level]
				if !exists {
					levelLabel = level.String()
				}

				a.Value = slog.StringValue(levelLabel)
			}

			return a
		},
	}))

	slog.SetDefault(defaultLogger)

	return nil
}
