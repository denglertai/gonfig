package cmd

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLogLevelFlagIsApplied(t *testing.T) {
	t.Cleanup(func() {
		rootCmd.PersistentFlags().Set("log-level", "info")
	})

	rootCmd.SetArgs([]string{"-l", "error", "value", "x"})
	assert.NoError(t, rootCmd.Execute())
	assert.False(t, slog.Default().Enabled(context.Background(), slog.LevelWarn))

	rootCmd.SetArgs([]string{"--log-level", "debug", "value", "x"})
	assert.NoError(t, rootCmd.Execute())
	assert.True(t, slog.Default().Enabled(context.Background(), slog.LevelDebug))
}
