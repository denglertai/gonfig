package config

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"reflect"
	"slices"
	"strings"

	"github.com/denglertai/gonfig/internal/general"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"go.yaml.in/yaml/v3"
)

// Settings represents the configuration settings
type Settings struct {
	// Files are the paths of the configuration files, with glob patterns expanded
	Files []string

	// FileType is the type of file to be read. If not set, the file type will be inferred from the file extension
	FileType general.FileType
}

// NewSettings returns a new Settings instance
func NewSettings() *Settings {
	return &Settings{
		FileType: general.Undefined,
	}
}

// FilterTemplate is a reusable filter chain, used as ${VAR | @name}
type FilterTemplate struct {
	// Description documents the template
	Description string `mapstructure:"description"`
	// Filters is the filter chain, e.g. "trim | base64"
	Filters string `mapstructure:"filters"`
}

// PlaceholderTemplate is a reusable placeholder, used as ${@name}.
// It is either based on an environment variable (Env) or on a value containing placeholders (Value).
type PlaceholderTemplate struct {
	// Description documents the template
	Description string `mapstructure:"description"`
	// Env is the name of the environment variable to read
	Env string `mapstructure:"env"`
	// Default is used if the environment variable is not set or empty
	Default string `mapstructure:"default"`
	// Value is processed like any config value, e.g. "jdbc:postgresql://${@db_host}/app"
	Value string `mapstructure:"value"`
	// Filters is the filter chain applied to the value of the environment variable or to the processed value
	Filters string `mapstructure:"filters"`
}

// Templates holds the templates defined in the config file
type Templates struct {
	Filters      map[string]FilterTemplate      `mapstructure:"filters"`
	Placeholders map[string]PlaceholderTemplate `mapstructure:"placeholders"`
}

// AppConfig holds all global application settings
type AppConfig struct {
	LogLevel string
	// LogFormat is json, text or auto, which is text if stderr is a terminal
	LogFormat  string
	LogSource  bool
	ConfigPath string
	// PluginPath is the plugin directory. If empty, the default plugin paths are searched, see plugin.InitPlugins.
	PluginPath string
	// AllowUnknownFilters makes unknown filters pass values on unchanged instead of failing
	AllowUnknownFilters bool
	// LogValues includes processed values in debug logs, which may expose secrets
	LogValues bool
	// FailOnMissing makes environment variables that are not set fail processing, unless a default is provided
	FailOnMissing bool
	// Templates are reusable filter chains and placeholders, which can only be defined in the config file
	Templates Templates
	// ConfigFile is the config file used, or empty if none was found
	ConfigFile string
	// UnknownKeys are the top level keys of the config file that are no options, sorted. They are ignored, so config
	// files shared between gonfig versions keep working when a newer version adds options, and logged as warnings.
	UnknownKeys []string
}

// optionKeys are the keys of the options, which the config file may set besides the templates
var optionKeys = []string{
	"log-level", "log-format", "log-source", "config-path", "plugin-path", "allow-unknown-filters", "log-values", "fail-on-missing",
}

// LoadAppConfig loads configuration from multiple sources
// Precedence: CLI flags > env vars > config file > defaults
func LoadAppConfig(v *viper.Viper) (*AppConfig, error) {
	cfg := &AppConfig{
		LogLevel:   v.GetString("log-level"),
		LogFormat:  v.GetString("log-format"),
		LogSource:  v.GetBool("log-source"),
		ConfigPath: v.GetString("config-path"),
		PluginPath: v.GetString("plugin-path"),

		AllowUnknownFilters: v.GetBool("allow-unknown-filters"),
		LogValues:           v.GetBool("log-values"),
		FailOnMissing:       v.GetBool("fail-on-missing"),
		ConfigFile:          v.ConfigFileUsed(),
	}

	if cfg.ConfigFile != "" {
		unknown, err := unknownKeys(cfg.ConfigFile)
		if err != nil {
			return nil, err
		}
		cfg.UnknownKeys = unknown
	}

	// Template names are keys of the config file, which viper converts to lower case
	if err := v.UnmarshalKey("templates", &cfg.Templates); err != nil {
		return nil, fmt.Errorf("invalid templates in config file: %w", err)
	}

	return cfg, nil
}

// unknownKeys returns the top level keys of the config file that are no options, sorted. Unknown keys within the
// templates are an error instead, like other invalid templates, since a typo like filter: instead of filters: would
// drop a template or its filters silently.
func unknownKeys(file string) ([]string, error) {
	// The config file is parsed on its own, since the settings of v include the defaults and flags, and viper drops
	// keys with empty values like log-levl: or log-levl: {}
	content, err := os.ReadFile(file) //nolint:gosec // the config file found by viper
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", file, err)
	}
	var parsed map[string]any
	if err := yaml.Unmarshal(content, &parsed); err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", file, err)
	}
	// Keys differing only in case are the same key for viper, which keeps one of them, so the other one would not be
	// checked. They are rejected at every level checked here.
	var duplicates []error
	lower := func(path string, value any) map[string]any {
		lowered, duplicate := lowerKeys(value)
		if duplicate != "" {
			duplicates = append(duplicates, fmt.Errorf("%s contains keys differing only in case, e.g. %s", path, duplicate))
		}
		return lowered
	}
	settings := lower("the file", parsed)

	var unknown []string
	for _, key := range slices.Sorted(maps.Keys(settings)) {
		if key != "templates" && !slices.Contains(optionKeys, key) {
			unknown = append(unknown, key)
		}
	}

	// Values that are no maps are reported by decoding the templates, which names the expected type
	templates := lower("templates", settings["templates"])
	var errs []error
	check := func(path string, values map[string]any, known []string) {
		for _, key := range slices.Sorted(maps.Keys(values)) {
			if !slices.Contains(known, key) {
				errs = append(errs, fmt.Errorf("unknown key %q, expected %s", path+"."+key, strings.Join(known, " or ")))
			}
		}
	}
	check("templates", templates, fieldKeys(Templates{}))
	kinds := map[string][]string{"filters": fieldKeys(FilterTemplate{}), "placeholders": fieldKeys(PlaceholderTemplate{})}
	for _, kind := range slices.Sorted(maps.Keys(kinds)) {
		named := lower("templates."+kind, templates[kind])
		for _, name := range slices.Sorted(maps.Keys(named)) {
			path := "templates." + kind + "." + name
			check(path, lower(path, named[name]), kinds[kind])
		}
	}
	if len(duplicates) > 0 {
		return nil, fmt.Errorf("invalid config file %s: %w", file, errors.Join(duplicates...))
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("invalid templates in config file %s: %w", file, errors.Join(errs...))
	}
	return unknown, nil
}

// lowerKeys returns the map with lower case keys, like viper reads them, or nil if the value is no map.
// If keys differ only in case, the first two of them in sorted order are returned as well, e.g. "Templates" and "templates".
func lowerKeys(value any) (lowered map[string]any, duplicate string) {
	values, ok := value.(map[string]any)
	if !ok {
		return nil, ""
	}
	lowered = make(map[string]any, len(values))
	original := make(map[string]string, len(values))
	// Sorted, so the reported keys are the same on every run
	for _, key := range slices.Sorted(maps.Keys(values)) {
		lower := strings.ToLower(key)
		if other, found := original[lower]; found && duplicate == "" {
			duplicate = fmt.Sprintf("%q and %q", other, key)
		}
		original[lower] = key
		lowered[lower] = values[key]
	}
	return lowered, duplicate
}

// fieldKeys returns the config file keys of the fields of a struct, i.e. their mapstructure tags, sorted
func fieldKeys(value any) []string {
	t := reflect.TypeOf(value)
	keys := make([]string, 0, t.NumField())
	for i := range t.NumField() {
		keys = append(keys, t.Field(i).Tag.Get("mapstructure"))
	}
	slices.Sort(keys)
	return keys
}

// SetupViper configures Viper with defaults and paths.
// The given flags (may be nil) are bound before the config file is located,
// so that flags such as --config-path take effect.
// A missing config file is fine, but a config file that cannot be read, e.g. because of invalid YAML, is an error.
func SetupViper(flags *pflag.FlagSet) (*viper.Viper, error) {
	v := viper.New()

	// Bind CLI flags so they override env vars & config files
	if flags != nil {
		_ = v.BindPFlags(flags)
	}

	// Set defaults
	v.SetDefault("log-level", "info")
	// JSON by default, so existing log processing keeps working
	v.SetDefault("log-format", "json")
	v.SetDefault("log-source", false)
	v.SetDefault("config-path", "")
	// Empty, so the default plugin paths are searched, see plugin.InitPlugins
	v.SetDefault("plugin-path", "")
	v.SetDefault("allow-unknown-filters", false)
	v.SetDefault("log-values", false)
	v.SetDefault("fail-on-missing", false)

	// Environment variables
	v.SetEnvPrefix("GONFIG")
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	v.AutomaticEnv() // Bind all env vars

	// Config file search
	v.SetConfigName(".gonfig") // looks for .gonfig.yaml, .gonfig.json, etc.
	v.SetConfigType("yaml")

	// If a custom config path is provided, use only that path. Otherwise, the most specific config file is used:
	// the one of the current directory, e.g. of a project, before the user's and the system's. Viper uses the first
	// config file found and doesn't merge them.
	configPath := v.GetString("config-path")
	if configPath != "" {
		v.AddConfigPath(configPath)
	} else {
		v.AddConfigPath(".")           // current directory
		v.AddConfigPath("$HOME")       // home directory
		v.AddConfigPath("/etc/gonfig") // system directory
	}

	// Load config file if it exists (optional, doesn't error if missing)
	if err := v.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if !errors.As(err, &notFound) {
			return nil, fmt.Errorf("failed to read config file %s: %w", v.ConfigFileUsed(), err)
		}
	}

	return v, nil
}
