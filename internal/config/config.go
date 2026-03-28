package config

import (
	"strings"

	"github.com/denglertai/gonfig/internal/general"
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
}

// LoadAppConfig loads configuration from multiple sources
// Precedence: CLI flags > env vars > config file > defaults
func LoadAppConfig(v *viper.Viper) *AppConfig {
	return &AppConfig{
		LogLevel:   v.GetString("log-level"),
		LogSource:  v.GetBool("log-source"),
		ConfigPath: v.GetString("config-path"),
		PluginPath: v.GetString("plugin-path"),
	}
}

// SetupViper configures Viper with defaults and paths
func SetupViper() *viper.Viper {
	v := viper.New()

	// Set defaults
	v.SetDefault("log-level", "info")
	v.SetDefault("log-source", false)
	v.SetDefault("config-path", "")
	v.SetDefault("plugin-path", "./plugins")

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
