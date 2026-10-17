package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/pflag"
)

func TestLoadAppConfig_DefaultPluginPath(t *testing.T) {
	v := SetupViper(nil)
	cfg := LoadAppConfig(v)

	if cfg.PluginPath != "./plugins" {
		t.Fatalf("expected default plugin path %q, got %q", "./plugins", cfg.PluginPath)
	}
}

func TestLoadAppConfig_PluginPathFromEnv(t *testing.T) {
	t.Setenv("GONFIG_PLUGIN_PATH", "/tmp/custom-plugins")

	v := SetupViper(nil)
	cfg := LoadAppConfig(v)

	if cfg.PluginPath != "/tmp/custom-plugins" {
		t.Fatalf("expected plugin path from env %q, got %q", "/tmp/custom-plugins", cfg.PluginPath)
	}
}

func TestSetupViper_ConfigPathFromFlag(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".gonfig.yaml"), []byte("plugin-path: /from/config-file\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	flags.String("config-path", "", "")
	flags.String("plugin-path", "./plugins", "")
	if err := flags.Parse([]string{"--config-path", dir}); err != nil {
		t.Fatal(err)
	}

	cfg := LoadAppConfig(SetupViper(flags))

	if cfg.PluginPath != "/from/config-file" {
		t.Fatalf("expected plugin path from config file %q, got %q", "/from/config-file", cfg.PluginPath)
	}
}

func TestSetupViper_FlagOverridesEnv(t *testing.T) {
	t.Setenv("GONFIG_PLUGIN_PATH", "/from/env")

	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	flags.String("plugin-path", "./plugins", "")
	if err := flags.Parse([]string{"--plugin-path", "/from/flag"}); err != nil {
		t.Fatal(err)
	}

	cfg := LoadAppConfig(SetupViper(flags))

	if cfg.PluginPath != "/from/flag" {
		t.Fatalf("expected plugin path from flag %q, got %q", "/from/flag", cfg.PluginPath)
	}
}

func TestLoadAppConfig_AllowUnknownFilters(t *testing.T) {
	testCases := []struct {
		desc       string
		env        string
		configFile string
		flag       []string
		want       bool
	}{
		{desc: "default", want: false},
		{desc: "env var", env: "true", want: true},
		{desc: "config file", configFile: "allow-unknown-filters: true\n", want: true},
		{desc: "flag overrides config file", configFile: "allow-unknown-filters: true\n", flag: []string{"--allow-unknown-filters=false"}, want: false},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			dir := t.TempDir()
			if tC.configFile != "" {
				if err := os.WriteFile(filepath.Join(dir, ".gonfig.yaml"), []byte(tC.configFile), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if tC.env != "" {
				t.Setenv("GONFIG_ALLOW_UNKNOWN_FILTERS", tC.env)
			}

			flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
			flags.String("config-path", "", "")
			flags.Bool("allow-unknown-filters", false, "")
			if err := flags.Parse(append([]string{"--config-path", dir}, tC.flag...)); err != nil {
				t.Fatal(err)
			}

			cfg := LoadAppConfig(SetupViper(flags))
			if cfg.AllowUnknownFilters != tC.want {
				t.Fatalf("expected AllowUnknownFilters %v, got %v", tC.want, cfg.AllowUnknownFilters)
			}
		})
	}
}

// isolateConfig ensures that neither a config file nor environment variables of the machine running the tests are used
func isolateConfig(t *testing.T) {
	t.Helper()

	t.Setenv("HOME", t.TempDir())
	t.Chdir(t.TempDir())
	for _, key := range []string{"GONFIG_LOG_LEVEL", "GONFIG_LOG_SOURCE", "GONFIG_CONFIG_PATH", "GONFIG_PLUGIN_PATH", "GONFIG_ALLOW_UNKNOWN_FILTERS"} {
		// Viper ignores empty environment variables
		t.Setenv(key, "")
	}
}

func TestLoadAppConfig_Defaults(t *testing.T) {
	isolateConfig(t)

	cfg := LoadAppConfig(SetupViper(nil))

	want := AppConfig{
		LogLevel:            "info",
		LogSource:           false,
		ConfigPath:          "",
		PluginPath:          "./plugins",
		AllowUnknownFilters: false,
	}
	if *cfg != want {
		t.Fatalf("expected default config %+v, got %+v", want, *cfg)
	}
}
