package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/denglertai/gonfig/internal/testutil"
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

// TestLoadAppConfig_DefaultPluginPath checks that the plugin path is empty by default, so the default plugin paths are searched
func TestLoadAppConfig_DefaultPluginPath(t *testing.T) {
	v := mustSetupViper(t, nil)
	cfg := mustLoadAppConfig(t, v)

	// Empty, so the default plugin paths are searched
	if cfg.PluginPath != "" {
		t.Fatalf("expected an empty default plugin path, got %q", cfg.PluginPath)
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

// TestSetupViper_ConfigPathFromFlag checks that --config-path selects the directory of the config file
func TestSetupViper_ConfigPathFromFlag(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".gonfig.yaml"), []byte("plugin-path: /from/config-file\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	flags.String("config-path", "", "")
	flags.String("plugin-path", "", "")
	if err := flags.Parse([]string{"--config-path", dir}); err != nil {
		t.Fatal(err)
	}

	cfg := mustLoadAppConfig(t, mustSetupViper(t, flags))

	if cfg.PluginPath != "/from/config-file" {
		t.Fatalf("expected plugin path from config file %q, got %q", "/from/config-file", cfg.PluginPath)
	}
}

// TestSetupViper_FlagOverridesEnv checks that a flag takes precedence over an environment variable
func TestSetupViper_FlagOverridesEnv(t *testing.T) {
	t.Setenv("GONFIG_PLUGIN_PATH", "/from/env")

	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	flags.String("plugin-path", "", "")
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

// isolateConfig prepares a test of the default config search: HOME and the working directory are empty directories,
// and the config path set by TestMain is cleared, so the default search paths are used without finding a config file
func isolateConfig(t *testing.T) {
	t.Helper()

	testutil.SetHome(t, t.TempDir())
	t.Chdir(t.TempDir())
	// TestMain removes all other GONFIG_* variables, but points the config path to an empty directory,
	// which this test clears to check the default search paths. Viper ignores empty environment variables.
	t.Setenv("GONFIG_CONFIG_PATH", "")
}

// TestSetupViper_SearchOrder checks that the config file of the current directory is used before the one in the home
// directory, which is used if the current directory has none. Only one config file is used, so they aren't merged.
func TestSetupViper_SearchOrder(t *testing.T) {
	isolateConfig(t)
	home := t.TempDir()
	testutil.SetHome(t, home)
	work := t.TempDir()
	t.Chdir(work)

	write := func(dir string, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, ".gonfig.yaml"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(home, "log-level: warn\nlog-source: true\n")
	write(work, "log-level: debug\n")

	cfg := mustLoadAppConfig(t, mustSetupViper(t, nil))
	if cfg.LogLevel != "debug" || cfg.LogSource {
		t.Fatalf("expected the config file of the current directory only, got log level %q and log source %v", cfg.LogLevel, cfg.LogSource)
	}
	if want := filepath.Join(work, ".gonfig.yaml"); !sameFile(t, cfg.ConfigFile, want) {
		t.Fatalf("expected the config file %q, got %q", want, cfg.ConfigFile)
	}

	if err := os.Remove(filepath.Join(work, ".gonfig.yaml")); err != nil {
		t.Fatal(err)
	}
	cfg = mustLoadAppConfig(t, mustSetupViper(t, nil))
	if cfg.LogLevel != "warn" || !sameFile(t, cfg.ConfigFile, filepath.Join(home, ".gonfig.yaml")) {
		t.Fatalf("expected the config file of the home directory, got log level %q from %q", cfg.LogLevel, cfg.ConfigFile)
	}
}

// sameFile returns whether the paths refer to the same file, e.g. a relative and an absolute path
func sameFile(t *testing.T, a string, b string) bool {
	t.Helper()
	infoA, errA := os.Stat(a)
	infoB, errB := os.Stat(b)
	return errA == nil && errB == nil && os.SameFile(infoA, infoB)
}

// TestLoadAppConfig_Defaults checks the complete default config without any config file or environment variables
func TestLoadAppConfig_Defaults(t *testing.T) {
	isolateConfig(t)

	cfg := mustLoadAppConfig(t, mustSetupViper(t, nil))

	want := AppConfig{
		LogLevel:            "info",
		LogSource:           false,
		ConfigPath:          "",
		PluginPath:          "",
		AllowUnknownFilters: false,
		LogValues:           false,
		FailOnMissing:       false,
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

// TestLoadAppConfig_LogValues checks that logging values is disabled by default and can be enabled by every source
func TestLoadAppConfig_LogValues(t *testing.T) {
	testCases := []struct {
		desc       string
		env        string
		configFile string
		flag       []string
		want       bool
	}{
		{desc: "default", want: false},
		{desc: "env var", env: "true", want: true},
		{desc: "config file", configFile: "log-values: true\n", want: true},
		{desc: "flag overrides config file", configFile: "log-values: true\n", flag: []string{"--log-values=false"}, want: false},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			dir := t.TempDir()
			if tC.configFile != "" {
				if err := os.WriteFile(filepath.Join(dir, ".gonfig.yaml"), []byte(tC.configFile), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("GONFIG_LOG_VALUES", tC.env)

			flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
			flags.String("config-path", "", "")
			flags.Bool("log-values", false, "")
			if err := flags.Parse(append([]string{"--config-path", dir}, tC.flag...)); err != nil {
				t.Fatal(err)
			}

			cfg := mustLoadAppConfig(t, mustSetupViper(t, flags))
			if cfg.LogValues != tC.want {
				t.Fatalf("expected LogValues %v, got %v", tC.want, cfg.LogValues)
			}
		})
	}
}

// TestLoadAppConfig_FailOnMissing checks that failing on missing variables is disabled by default and can be enabled by every source
func TestLoadAppConfig_FailOnMissing(t *testing.T) {
	testCases := []struct {
		desc       string
		env        string
		configFile string
		flag       []string
		want       bool
	}{
		{desc: "default", want: false},
		{desc: "env var", env: "true", want: true},
		{desc: "config file", configFile: "fail-on-missing: true\n", want: true},
		{desc: "flag overrides config file", configFile: "fail-on-missing: true\n", flag: []string{"--fail-on-missing=false"}, want: false},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			dir := t.TempDir()
			if tC.configFile != "" {
				if err := os.WriteFile(filepath.Join(dir, ".gonfig.yaml"), []byte(tC.configFile), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("GONFIG_FAIL_ON_MISSING", tC.env)

			flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
			flags.String("config-path", "", "")
			flags.Bool("fail-on-missing", false, "")
			if err := flags.Parse(append([]string{"--config-path", dir}, tC.flag...)); err != nil {
				t.Fatal(err)
			}

			cfg := mustLoadAppConfig(t, mustSetupViper(t, flags))
			if cfg.FailOnMissing != tC.want {
				t.Fatalf("expected FailOnMissing %v, got %v", tC.want, cfg.FailOnMissing)
			}
		})
	}
}

// TestLoadAppConfig_UnknownKeys checks that unknown top level keys of the config file are returned sorted, while
// options and templates are known, including keys of nested maps like a.b
func TestLoadAppConfig_UnknownKeys(t *testing.T) {
	dir := t.TempDir()
	content := "log-levl: debug\nlog-level: warn\nfail-on-missing: true\nA.b: 1\ntemplates:\n  filters:\n    s:\n      filters: trim\n"
	if err := os.WriteFile(filepath.Join(dir, ".gonfig.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GONFIG_CONFIG_PATH", dir)

	cfg := mustLoadAppConfig(t, mustSetupViper(t, nil))
	if want := []string{"a", "log-levl"}; !reflect.DeepEqual(cfg.UnknownKeys, want) {
		t.Fatalf("expected unknown keys %v, got %v", want, cfg.UnknownKeys)
	}
	if cfg.LogLevel != "warn" {
		t.Fatalf("expected the known options to be read, got log level %q", cfg.LogLevel)
	}
}

// TestLoadAppConfig_NoUnknownKeys checks that no keys are unknown without a config file and with every option set,
// which fails if an option is added without adding it to optionKeys
func TestLoadAppConfig_NoUnknownKeys(t *testing.T) {
	cfg := mustLoadAppConfig(t, mustSetupViper(t, nil))
	if cfg.UnknownKeys != nil {
		t.Fatalf("expected no unknown keys without a config file, got %v", cfg.UnknownKeys)
	}

	// The defaults name every option, so a config file setting each of them has no unknown keys
	var content strings.Builder
	for key, value := range mustSetupViper(t, nil).AllSettings() {
		content.WriteString(key + ": " + fmtValue(value) + "\n")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".gonfig.yaml"), []byte(content.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GONFIG_CONFIG_PATH", dir)

	cfg = mustLoadAppConfig(t, mustSetupViper(t, nil))
	if cfg.UnknownKeys != nil {
		t.Fatalf("expected the options to be known, got unknown keys %v", cfg.UnknownKeys)
	}
}

// fmtValue formats a default value as a YAML value
func fmtValue(value any) string {
	if s, ok := value.(string); ok {
		return strconv.Quote(s)
	}
	return fmt.Sprint(value)
}

// TestLoadAppConfig_UnknownTemplateKeys checks that unknown keys within the templates fail, naming each key with its
// path, the config file and the expected keys
func TestLoadAppConfig_UnknownTemplateKeys(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, ".gonfig.yaml")
	content := `templates:
  filter:
    secret:
      filters: trim
  filters:
    a:
      filter: trim
      description: ok
  placeholders:
    b:
      envv: B
`
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GONFIG_CONFIG_PATH", dir)

	_, err := LoadAppConfig(mustSetupViper(t, nil))
	want := "invalid templates in config file " + file + `: unknown key "templates.filter", expected filters or placeholders
unknown key "templates.filters.a.filter", expected description or filters
unknown key "templates.placeholders.b.envv", expected default or description or env or filters or value`
	if err == nil || err.Error() != want {
		t.Fatalf("expected error\n%s\ngot\n%v", want, err)
	}
}

// TestLoadAppConfig_ConfigFileRemoved checks that a config file removed after it was found fails to be read
func TestLoadAppConfig_ConfigFileRemoved(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, ".gonfig.yaml")
	if err := os.WriteFile(file, []byte("log-level: info\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GONFIG_CONFIG_PATH", dir)
	v := mustSetupViper(t, nil)
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadAppConfig(v); err == nil || !strings.Contains(err.Error(), "failed to read config file "+file) {
		t.Fatalf("expected an error naming the config file, got %v", err)
	}
}
