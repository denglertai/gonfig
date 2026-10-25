package logging

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestInitLoggingLevels checks that each level enables itself and the levels above it
func TestInitLoggingLevels(t *testing.T) {
	testCases := []struct {
		level   string
		enabled slog.Level
		below   slog.Level
	}{
		{level: "trace", enabled: LevelTrace, below: LevelTrace - 1},
		{level: "debug", enabled: slog.LevelDebug, below: LevelTrace},
		{level: "info", enabled: slog.LevelInfo, below: slog.LevelDebug},
		{level: "warn", enabled: slog.LevelWarn, below: slog.LevelInfo},
		{level: "error", enabled: slog.LevelError, below: slog.LevelWarn},
		{level: "fatal", enabled: LevelFatal, below: slog.LevelError},
	}
	for _, tC := range testCases {
		t.Run(tC.level, func(t *testing.T) {
			assert.NoError(t, InitLogging(tC.level, "json", false))

			ctx := context.Background()
			assert.True(t, slog.Default().Enabled(ctx, tC.enabled))
			assert.False(t, slog.Default().Enabled(ctx, tC.below))
		})
	}
}

// TestInitLoggingInvalidLevel checks that unknown levels fail
func TestInitLoggingInvalidLevel(t *testing.T) {
	assert.Error(t, InitLogging("verbose", "json", false))
}

// TestInitLoggingCustomLevelNames checks that the custom levels trace and fatal are logged with their names
func TestInitLoggingCustomLevelNames(t *testing.T) {
	// The handler writes to os.Stderr as it is at initialization
	old := os.Stderr
	r, w, err := os.Pipe()
	assert.NoError(t, err)
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = old })

	assert.NoError(t, InitLogging("trace", "json", false))
	slog.Log(context.Background(), LevelTrace, "trace message")
	slog.Log(context.Background(), LevelFatal, "fatal message")
	assert.NoError(t, w.Close())

	var buf bytes.Buffer
	_, err = io.Copy(&buf, r)
	assert.NoError(t, err)

	assert.Contains(t, buf.String(), `"level":"TRACE","msg":"trace message"`)
	assert.Contains(t, buf.String(), `"level":"FATAL","msg":"fatal message"`)
}

// captureLog initializes logging with the format while stderr is a pipe, logs a message and returns what was written
func captureLog(t *testing.T, format string) string {
	t.Helper()
	// The handler writes to os.Stderr as it is at initialization
	old := os.Stderr
	r, w, err := os.Pipe()
	assert.NoError(t, err)
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = old })

	assert.NoError(t, InitLogging("trace", format, false))
	slog.Log(context.Background(), LevelTrace, "trace message", "key", "value")
	assert.NoError(t, w.Close())

	var buf bytes.Buffer
	_, err = io.Copy(&buf, r)
	assert.NoError(t, err)
	return buf.String()
}

// TestInitLoggingFormats checks the json and text formats, with the custom level names, and that auto chooses json
// for stderr that is no terminal, e.g. a pipe
func TestInitLoggingFormats(t *testing.T) {
	assert.Contains(t, captureLog(t, "json"), `"level":"TRACE","msg":"trace message","key":"value"}`)
	assert.Regexp(t, `^time=\S+ level=TRACE msg="trace message" key=value\n$`, captureLog(t, "text"))
	assert.Contains(t, captureLog(t, "auto"), `"level":"TRACE","msg":"trace message","key":"value"}`)

	stderrIsTerminal = func() bool { return true }
	t.Cleanup(func() { stderrIsTerminal = func() bool { return isTerminal(os.Stderr) } })
	assert.Contains(t, captureLog(t, "auto"), `level=TRACE msg="trace message" key=value`)
}

// TestInitLoggingInvalidFormat checks that unknown formats fail, naming the valid ones
func TestInitLoggingInvalidFormat(t *testing.T) {
	assert.EqualError(t, InitLogging("info", "yaml", false), "invalid log format: yaml, use json, text or auto")
}

// TestIsTerminal checks that files and pipes are no terminals, while character devices like /dev/null are
func TestIsTerminal(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "log")
	assert.NoError(t, err)
	t.Cleanup(func() { _ = file.Close() })
	assert.False(t, isTerminal(file))

	r, w, err := os.Pipe()
	assert.NoError(t, err)
	t.Cleanup(func() { _ = r.Close(); _ = w.Close() })
	assert.False(t, isTerminal(w))

	devNull, err := os.Open(os.DevNull)
	assert.NoError(t, err)
	t.Cleanup(func() { _ = devNull.Close() })
	assert.True(t, isTerminal(devNull))

	// Closed files cannot be stat'ed
	assert.NoError(t, file.Close())
	assert.False(t, isTerminal(file))
}
