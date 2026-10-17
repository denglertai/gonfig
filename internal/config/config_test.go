package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// mustSetupViper sets up viper and fails the test on errors
func mustSetupViper(t *testing.T, flags *pflag.FlagSet) *viper.Viper {
	t.Helper()

	v, err := SetupViper(flags)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// mustLoadAppConfig loads the config and fails the test on errors
func mustLoadAppConfig(t *testing.T, v *viper.Viper) *AppConfig {
	t.Helper()

	cfg, err := LoadAppConfig(v)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestLoadAppConfig_DefaultPluginPath(t *testing.T) {
	v := mustSetupViper(t, nil)
	cfg := mustLoadAppConfig(t, v)

	if cfg.PluginPath != "./plugins" {
		t.Fatalf("expected default plugin path %q, got %q", "./plugins", cfg.PluginPath)
	}
}

func TestLoadAppConfig_PluginPathFromEnv(t *testing.T) {
	t.Setenv("GONFIG_PLUGIN_PATH", "/tmp/custom-plugins")

	v := mustSetupViper(t, nil)
	cfg := mustLoadAppConfig(t, v)

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

	cfg := mustLoadAppConfig(t, mustSetupViper(t, flags))

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

	cfg := mustLoadAppConfig(t, mustSetupViper(t, flags))

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

			cfg := mustLoadAppConfig(t, mustSetupViper(t, flags))
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

	cfg := mustLoadAppConfig(t, mustSetupViper(t, nil))

	want := AppConfig{
		LogLevel:            "info",
		LogSource:           false,
		ConfigPath:          "",
		PluginPath:          "./plugins",
		AllowUnknownFilters: false,
	}
	if !reflect.DeepEqual(*cfg, want) {
		t.Fatalf("expected default config %+v, got %+v", want, *cfg)
	}
}

func TestLoadAppConfig_Templates(t *testing.T) {
	dir := t.TempDir()
	content := `templates:
  filters:
    secret:
      description: Trims a secret and encodes it to base64
      filters: trim | base64
  placeholders:
    DB_Host:
      env: DB_HOST
      default: localhost
    db_url:
      value: "jdbc:postgresql://${@db_host}/app"
      filters: trim
`
	if err := os.WriteFile(filepath.Join(dir, ".gonfig.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GONFIG_CONFIG_PATH", dir)

	cfg := mustLoadAppConfig(t, mustSetupViper(t, nil))

	want := Templates{
		Filters: map[string]FilterTemplate{
			"secret": {Description: "Trims a secret and encodes it to base64", Filters: "trim | base64"},
		},
		Placeholders: map[string]PlaceholderTemplate{
			// Viper converts keys to lower case
			"db_host": {Env: "DB_HOST", Default: "localhost"},
			"db_url":  {Value: "jdbc:postgresql://${@db_host}/app", Filters: "trim"},
		},
	}
	if !reflect.DeepEqual(cfg.Templates, want) {
		t.Fatalf("expected templates %+v, got %+v", want, cfg.Templates)
	}
}

func TestLoadAppConfig_InvalidTemplates(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".gonfig.yaml"), []byte("templates:\n  filters: not a map\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GONFIG_CONFIG_PATH", dir)

	if _, err := LoadAppConfig(mustSetupViper(t, nil)); err == nil {
		t.Fatal("expected an error for invalid templates")
	}
}

func TestSetupViper_InvalidConfigFile(t *testing.T) {
	dir := t.TempDir()
	// "@" cannot start a plain YAML value, so template references at the start have to be quoted
	content := "templates:\n  filters:\n    upper_clean:\n      filters: @clean | upper\n"
	if err := os.WriteFile(filepath.Join(dir, ".gonfig.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GONFIG_CONFIG_PATH", dir)

	_, err := SetupViper(nil)
	if err == nil || !strings.Contains(err.Error(), filepath.Join(dir, ".gonfig.yaml")) {
		t.Fatalf("expected an error naming the config file, got %v", err)
	}
}

func TestSetupViper_MissingConfigFile(t *testing.T) {
	t.Setenv("GONFIG_CONFIG_PATH", t.TempDir())

	if _, err := SetupViper(nil); err != nil {
		t.Fatalf("expected no error for a missing config file, got %v", err)
	}
}
