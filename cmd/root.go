/*
Copyright © 2024 Daniel Englert
*/
package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/denglertai/gonfig/internal/config"
	"github.com/denglertai/gonfig/internal/file"
	"github.com/denglertai/gonfig/internal/filter"
	"github.com/denglertai/gonfig/internal/logging"
	"github.com/denglertai/gonfig/internal/plugin"
	"github.com/denglertai/gonfig/internal/value"
	"github.com/denglertai/gonfig/internal/version"
	pkglogging "github.com/denglertai/gonfig/pkg/logging"
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
		v, err := config.SetupViper(cmd.Root().PersistentFlags())
		if err != nil {
			return err
		}

		// Load into AppConfig
		cfg, err := config.LoadAppConfig(v)
		if err != nil {
			return err
		}

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
		filter.SetFailOnMissing(cfg.FailOnMissing)

		file.SetLogValues(cfg.LogValues)
		filter.SetValuesInErrors(cfg.LogValues)
		if cfg.LogValues {
			pkglogging.Warn("Processed values are included in debug logs and error messages and may expose secrets")
		}

		// Templates are validated after the filters of plugins are registered, so they are known
		if err := value.SetTemplates(cfg.Templates); err != nil {
			return fmt.Errorf("invalid templates in config file: %w", err)
		}

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
		LogValues:           false,
		FailOnMissing:       false,
	}
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	// Executable plugins are stopped before exiting, so they can finish and their logs are complete
	plugin.ClosePlugins()
	if err != nil {
		os.Exit(1)
	}
}

// init defines the global flags, which are resolved together with environment variables and the config file
func init() {
	// Cobra handles --version / -v before PersistentPreRunE, so printing the version neither reads the config nor starts plugins
	rootCmd.Version = version.Get().String()
	rootCmd.SetVersionTemplate("{{.Name}} version {{.Version}}\n")

	rootCmd.PersistentFlags().StringP("log-level", "l", "info", "Log level (trace, debug, info, warn, error, fatal)")
	rootCmd.PersistentFlags().BoolP("log-source", "s", false, "Whether to include source location in log output or not")
	rootCmd.PersistentFlags().String("config-path", "", "Path to the directory containing .gonfig config file (env var: GONFIG_CONFIG_PATH)")
	rootCmd.PersistentFlags().String("plugin-path", "./plugins", "Path to plugin directory (env var: GONFIG_PLUGIN_PATH)")
	rootCmd.PersistentFlags().Bool("log-values", false, "Include processed values in debug logs and error messages, which may expose secrets (env var: GONFIG_LOG_VALUES)")
	rootCmd.PersistentFlags().Bool("fail-on-missing", false, "Fail for environment variables that are not set instead of using empty strings, unless a default is provided (env var: GONFIG_FAIL_ON_MISSING)")
	rootCmd.PersistentFlags().Bool("allow-unknown-filters", false, "Pass values on unchanged instead of failing for unknown filters, which may write secrets unhashed (env var: GONFIG_ALLOW_UNKNOWN_FILTERS)")
}
