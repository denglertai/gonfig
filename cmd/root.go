/*
Copyright © 2024 Daniel Englert
*/
package cmd

import (
	"context"
	"os"

	"github.com/denglertai/gonfig/internal/config"
	"github.com/denglertai/gonfig/internal/filter"
	"github.com/denglertai/gonfig/internal/logging"
	"github.com/denglertai/gonfig/internal/plugin"
	"github.com/spf13/cobra"
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "gonfig",
	Short: "A configuration management tool for various formats and sources",
	Long: `Gonfig is a configuration management tool that replaces placeholders in configuration files
of multiple formats (JSON, YAML, XML, properties and plain text) with values from environment variables or files.
It provides a flexible plugin system for extending functionality and a filtering mechanism for transforming values.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Cobra validates required flags only after this hook, so validate them while the usage is still printed
		if err := cmd.ValidateRequiredFlags(); err != nil {
			return err
		}

		// Flags and arguments are valid at this point, so don't print the usage for errors occurring from here on
		cmd.SilenceUsage = true

		// Initialize Viper with configuration from multiple sources.
		// The root's persistent flags are used, as cmd is the executed subcommand.
		v := config.SetupViper(cmd.Root().PersistentFlags())

		// Load into AppConfig
		cfg := config.LoadAppConfig(v)

		// Store in context for access in all subcommands
		ctx := context.WithValue(cmd.Context(), appConfigKey{}, cfg)
		cmd.SetContext(ctx)

		// Initialize logging first so plugin loading is logged as configured
		if err := logging.InitLogging(cfg.LogLevel, cfg.LogSource); err != nil {
			return err
		}

		// Initialize the Plugin system
		plugin.InitPlugins(cfg.PluginPath)

		filter.SetAllowUnknownFilters(cfg.AllowUnknownFilters)

		return nil
	},
	TraverseChildren: true,
}

// appConfigKey is the context key under which the AppConfig is stored
type appConfigKey struct{}

// GetAppConfig retrieves the AppConfig from the command context
func GetAppConfig(cmd *cobra.Command) *config.AppConfig {
	if cfg, ok := cmd.Context().Value(appConfigKey{}).(*config.AppConfig); ok {
		return cfg
	}
	// Fallback to defaults if not found
	return &config.AppConfig{
		LogLevel:   "info",
		LogSource:  false,
		ConfigPath: "",
		PluginPath: "./plugins",

		AllowUnknownFilters: false,
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
	rootCmd.PersistentFlags().Bool("allow-unknown-filters", false, "Pass values on unchanged instead of failing for unknown filters, which may write secrets unhashed (env var: GONFIG_ALLOW_UNKNOWN_FILTERS)")
}
