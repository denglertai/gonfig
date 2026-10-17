package cmd

import (
	"context"
	"log/slog"
	"testing"

	"github.com/denglertai/gonfig/internal/config"
	"github.com/denglertai/gonfig/internal/file"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

func TestLogLevelFlagIsApplied(t *testing.T) {
	resetFlags(t)
	rootCmd.SetArgs([]string{"-l", "error", "value", "x"})
	assert.NoError(t, rootCmd.Execute())
	assert.False(t, slog.Default().Enabled(context.Background(), slog.LevelWarn))

	resetFlags(t)
	rootCmd.SetArgs([]string{"--log-level", "debug", "value", "x"})
	assert.NoError(t, rootCmd.Execute())
	assert.True(t, slog.Default().Enabled(context.Background(), slog.LevelDebug))
}

// TestDefaultAppConfig ensures that the defaults of the CLI flags, the config defaults and the fallback of GetAppConfig match
func TestDefaultAppConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Chdir(t.TempDir())
	for _, key := range []string{"GONFIG_LOG_LEVEL", "GONFIG_LOG_SOURCE", "GONFIG_CONFIG_PATH", "GONFIG_PLUGIN_PATH", "GONFIG_ALLOW_UNKNOWN_FILTERS"} {
		// Viper ignores empty environment variables
		t.Setenv(key, "")
	}
	resetFlags(t)

	want := &config.AppConfig{
		LogLevel:            "info",
		LogSource:           false,
		ConfigPath:          "",
		PluginPath:          "./plugins",
		AllowUnknownFilters: false,
		LogValues:           false,
	}

	// Defaults as used by the CLI, with all flags at their default values
	v, err := config.SetupViper(rootCmd.PersistentFlags())
	assert.NoError(t, err)
	cfg, err := config.LoadAppConfig(v)
	assert.NoError(t, err)
	assert.Equal(t, want, cfg)

	// Fallback of commands without a loaded config
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	assert.Equal(t, want, GetAppConfig(cmd))

	// The config defaults take precedence over the flag defaults, which are only shown by --help, so check them separately
	flagDefaults := map[string]string{
		"log-level":             "info",
		"log-source":            "false",
		"config-path":           "",
		"plugin-path":           "./plugins",
		"allow-unknown-filters": "false",
		"log-values":            "false",
	}
	for name, want := range flagDefaults {
		flag := rootCmd.PersistentFlags().Lookup(name)
		if assert.NotNil(t, flag, name) {
			assert.Equal(t, want, flag.DefValue, name)
		}
	}
}

// TestLogValuesFlagIsApplied checks that --log-values and GONFIG_LOG_VALUES enable logging values in the file processor
func TestLogValuesFlagIsApplied(t *testing.T) {
	t.Cleanup(func() { file.SetLogValues(false) })

	resetFlags(t)
	rootCmd.SetArgs([]string{"--log-values", "value", "x"})
	assert.NoError(t, rootCmd.Execute())
	assert.True(t, file.LogValues())

	resetFlags(t)
	rootCmd.SetArgs([]string{"value", "x"})
	assert.NoError(t, rootCmd.Execute())
	assert.False(t, file.LogValues())

	t.Setenv("GONFIG_LOG_VALUES", "true")
	resetFlags(t)
	rootCmd.SetArgs([]string{"value", "x"})
	assert.NoError(t, rootCmd.Execute())
	assert.True(t, file.LogValues())
}
