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

// InitLogging sets the default logger, writing to stderr at the given level including trace and fatal, optionally with
// the source location. The format is json, text, or auto, which is text if stderr is a terminal and json otherwise.
// It fails for unknown levels and formats.
func InitLogging(logLevel string, logFormat string, addSource bool) error {
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

	if logFormat == "auto" {
		logFormat = "json"
		if stderrIsTerminal() {
			logFormat = "text"
		}
	}

	options := &slog.HandlerOptions{
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
	}

	var handler slog.Handler
	switch logFormat {
	case "json":
		handler = slog.NewJSONHandler(os.Stderr, options)
	case "text":
		handler = slog.NewTextHandler(os.Stderr, options)
	default:
		return fmt.Errorf("invalid log format: %s, use json, text or auto", logFormat)
	}

	slog.SetDefault(slog.New(handler))

	return nil
}

// stderrIsTerminal returns whether stderr is a terminal, see isTerminal. Tests replace it, as they don't run in one.
var stderrIsTerminal = func() bool { return isTerminal(os.Stderr) }

// isTerminal returns whether the file is a terminal, i.e. a character device, rather than a file or pipe.
// Other character devices like /dev/null count as well, which doesn't matter for logs nobody reads.
func isTerminal(file *os.File) bool {
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
