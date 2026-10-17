package config

import (
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

// AppConfig holds all global application settings
type AppConfig struct {
	LogLevel   string
	LogSource  bool
	ConfigPath string
	PluginPath string
	// AllowUnknownFilters makes unknown filters pass values on unchanged instead of failing
	AllowUnknownFilters bool
}

// LoadAppConfig loads configuration from multiple sources
// Precedence: CLI flags > env vars > config file > defaults
func LoadAppConfig(v *viper.Viper) *AppConfig {
	return &AppConfig{
		LogLevel:   v.GetString("log-level"),
		LogSource:  v.GetBool("log-source"),
		ConfigPath: v.GetString("config-path"),
		PluginPath: v.GetString("plugin-path"),

		AllowUnknownFilters: v.GetBool("allow-unknown-filters"),
	}
}

// SetupViper configures Viper with defaults and paths.
// The given flags (may be nil) are bound before the config file is located,
// so that flags such as --config-path take effect.
func SetupViper(flags *pflag.FlagSet) *viper.Viper {
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
	_ = v.ReadInConfig() // Ignore "file not found" errors

	return v
}
