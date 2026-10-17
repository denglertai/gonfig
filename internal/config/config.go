package config

import (
	"errors"
	"fmt"
	"strings"

	"github.com/denglertai/gonfig/internal/general"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// Settings represents the configuration settings
type Settings struct {
	// File is the path to the configuration file
	File string

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
	LogLevel   string
	LogSource  bool
	ConfigPath string
	PluginPath string
	// AllowUnknownFilters makes unknown filters pass values on unchanged instead of failing
	AllowUnknownFilters bool
	// LogValues includes processed values in debug logs, which may expose secrets
	LogValues bool
	// Templates are reusable filter chains and placeholders, which can only be defined in the config file
	Templates Templates
}

// LoadAppConfig loads configuration from multiple sources
// Precedence: CLI flags > env vars > config file > defaults
func LoadAppConfig(v *viper.Viper) (*AppConfig, error) {
	cfg := &AppConfig{
		LogLevel:   v.GetString("log-level"),
		LogSource:  v.GetBool("log-source"),
		ConfigPath: v.GetString("config-path"),
		PluginPath: v.GetString("plugin-path"),

		AllowUnknownFilters: v.GetBool("allow-unknown-filters"),
		LogValues:           v.GetBool("log-values"),
	}

	// Template names are keys of the config file, which viper converts to lower case
	if err := v.UnmarshalKey("templates", &cfg.Templates); err != nil {
		return nil, fmt.Errorf("invalid templates in config file: %w", err)
	}

	return cfg, nil
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
	v.SetDefault("log-source", false)
	v.SetDefault("config-path", "")
	v.SetDefault("plugin-path", "./plugins")
	v.SetDefault("allow-unknown-filters", false)
	v.SetDefault("log-values", false)

	// Environment variables
	v.SetEnvPrefix("GONFIG")
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	v.AutomaticEnv() // Bind all env vars

	// Config file search
	v.SetConfigName(".gonfig") // looks for .gonfig.yaml, .gonfig.json, etc.
	v.SetConfigType("yaml")

	// If a custom config path is provided, use only that path
	// Otherwise, use default search paths
	configPath := v.GetString("config-path")
	if configPath != "" {
		v.AddConfigPath(configPath)
	} else {
		v.AddConfigPath("$HOME")       // home directory
		v.AddConfigPath(".")           // current directory
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
