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
			assert.NoError(t, InitLogging(tC.level, false))

			ctx := context.Background()
			assert.True(t, slog.Default().Enabled(ctx, tC.enabled))
			assert.False(t, slog.Default().Enabled(ctx, tC.below))
		})
	}
}

func TestInitLoggingInvalidLevel(t *testing.T) {
	assert.Error(t, InitLogging("verbose", false))
}

func TestInitLoggingCustomLevelNames(t *testing.T) {
	// The handler writes to os.Stderr as it is at initialization
	old := os.Stderr
	r, w, err := os.Pipe()
	assert.NoError(t, err)
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = old })

	assert.NoError(t, InitLogging("trace", false))
	slog.Log(context.Background(), LevelTrace, "trace message")
	slog.Log(context.Background(), LevelFatal, "fatal message")
	w.Close()

	var buf bytes.Buffer
	_, err = io.Copy(&buf, r)
	assert.NoError(t, err)

	assert.Contains(t, buf.String(), `"level":"TRACE","msg":"trace message"`)
	assert.Contains(t, buf.String(), `"level":"FATAL","msg":"fatal message"`)
}
