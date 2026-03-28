/*
Copyright © 2024 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"context"
	"os"

	"github.com/denglertai/gonfig/internal/config"
	"github.com/denglertai/gonfig/internal/logging"
	"github.com/denglertai/gonfig/internal/plugin"
	"github.com/spf13/cobra"
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "gonfig",
	Short: "A configuration management tool for various formats and sources",
	Long: `Gonfig is a powerful configuration management tool that supports multiple formats (YAML, JSON, TOML, etc.) and sources (files, environment variables, remote services). 
	It provides a flexible plugin system for extending functionality and a robust filtering mechanism for dynamic configuration processing.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Initialize Viper with configuration from multiple sources
		v := config.SetupViper()

		// Bind CLI flags to Viper so they override env vars & config files
		v.BindPFlag("log-level", cmd.PersistentFlags().Lookup("log-level"))
		v.BindPFlag("log-source", cmd.PersistentFlags().Lookup("log-source"))
		v.BindPFlag("config-path", cmd.PersistentFlags().Lookup("config-path"))
		v.BindPFlag("plugin-path", cmd.PersistentFlags().Lookup("plugin-path"))

		// Reload Viper after binding config-path flag to apply custom config path
		v = config.SetupViper()
		v.BindPFlag("log-level", cmd.PersistentFlags().Lookup("log-level"))
		v.BindPFlag("log-source", cmd.PersistentFlags().Lookup("log-source"))
		v.BindPFlag("config-path", cmd.PersistentFlags().Lookup("config-path"))
		v.BindPFlag("plugin-path", cmd.PersistentFlags().Lookup("plugin-path"))

		// Load into AppConfig
		cfg := config.LoadAppConfig(v)

		// Store in context for access in all subcommands
		ctx := context.WithValue(cmd.Context(), "appConfig", cfg)
		cmd.SetContext(ctx)

		// Initialize the Plugin system
		plugin.InitPlugins(cfg.PluginPath)

		return logging.InitLogging(cfg.LogLevel, cfg.LogSource)
	},
	TraverseChildren: true,
}

// GetAppConfig retrieves the AppConfig from the command context
func GetAppConfig(cmd *cobra.Command) *config.AppConfig {
	if cfg, ok := cmd.Context().Value("appConfig").(*config.AppConfig); ok {
		return cfg
	}
	// Fallback to defaults if not found
	return &config.AppConfig{
		LogLevel:   "info",
		LogSource:  false,
		ConfigPath: "",
		PluginPath: "./plugins",
	}
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringP("log-level", "l", "info", "Log level (trace, debug, info, warn, error, fatal)")
	rootCmd.PersistentFlags().BoolP("log-source", "s", false, "Whether to include source location in log output or not")
	rootCmd.PersistentFlags().String("config-path", "", "Path to the directory containing .gonfig config file (env var: GONFIG_CONFIG_PATH)")
	rootCmd.PersistentFlags().String("plugin-path", "./plugins", "Path to plugin directory (env var: GONFIG_PLUGIN_PATH)")
}
