package cmd

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/denglertai/gonfig/internal/config"
	"github.com/denglertai/gonfig/internal/file"
	"github.com/denglertai/gonfig/internal/filter"
	"github.com/denglertai/gonfig/internal/testutil"
	"github.com/denglertai/gonfig/internal/version"
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
	testutil.SetHome(t, t.TempDir())
	t.Chdir(t.TempDir())
	// TestMain removes all other GONFIG_* variables, but points the config path to an empty directory,
	// which this test clears to check the default search paths. Viper ignores empty environment variables.
	t.Setenv("GONFIG_CONFIG_PATH", "")
	resetFlags(t)

	want := &config.AppConfig{
		LogLevel:            "info",
		LogSource:           false,
		ConfigPath:          "",
		PluginPath:          "./plugins",
		AllowUnknownFilters: false,
		LogValues:           false,
		FailOnMissing:       false,
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
		"fail-on-missing":       "false",
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
	t.Setenv("GONFIG_LOG_VALUES", "")
	t.Cleanup(func() {
		file.SetLogValues(false)
		filter.SetValuesInErrors(false)
	})

	resetFlags(t)
	rootCmd.SetArgs([]string{"--log-values", "value", "x"})
	assert.NoError(t, rootCmd.Execute())
	assert.True(t, file.LogValues())
	assert.True(t, filter.ValuesInErrors())

	resetFlags(t)
	rootCmd.SetArgs([]string{"value", "x"})
	assert.NoError(t, rootCmd.Execute())
	assert.False(t, file.LogValues())
	assert.False(t, filter.ValuesInErrors())

	t.Setenv("GONFIG_LOG_VALUES", "true")
	resetFlags(t)
	rootCmd.SetArgs([]string{"value", "x"})
	assert.NoError(t, rootCmd.Execute())
	assert.True(t, file.LogValues())
}

// TestVersionFlag checks that --version and -v print the version without loading the config, so a broken config file does not prevent it
func TestVersionFlag(t *testing.T) {
	dir := t.TempDir()
	assert.NoError(t, os.WriteFile(filepath.Join(dir, ".gonfig.yaml"), []byte("log-level: [\n"), 0o600))
	t.Setenv("GONFIG_CONFIG_PATH", dir)

	for _, flag := range []string{"--version", "-v"} {
		resetFlags(t)
		out := new(bytes.Buffer)
		rootCmd.SetOut(out)
		t.Cleanup(func() { rootCmd.SetOut(nil) })

		rootCmd.SetArgs([]string{flag})
		assert.NoError(t, rootCmd.Execute(), flag)
		assert.Equal(t, "gonfig version "+version.Get().String()+"\n", out.String(), flag)
	}

	// Other commands still fail for the broken config file
	resetFlags(t)
	rootCmd.SetArgs([]string{"value", "x"})
	assert.ErrorContains(t, rootCmd.Execute(), "failed to read config file")
}
