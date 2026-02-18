/*
Copyright © 2024 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"os"

	"github.com/denglertai/gonfig/internal/logging"
	"github.com/denglertai/gonfig/internal/plugin"
	"github.com/spf13/cobra"
)

var logLevel string
var logSource bool

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "gonfig",
	Short: "A configuration management tool for various formats and sources",
	Long: `Gonfig is a powerful configuration management tool that supports multiple formats (YAML, JSON, TOML, etc.) and sources (files, environment variables, remote services). 
	It provides a flexible plugin system for extending functionality and a robust filtering mechanism for dynamic configuration processing.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Initialze the Plugin system
		plugin.InitPlugins()

		return logging.InitLogging(logLevel, logSource)
	},
	TraverseChildren: true,
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
	rootCmd.PersistentFlags().StringVarP(&logLevel, "log-level", "l", "info", "Log level (trace, debug, info, warn, error, fatal)")
	rootCmd.PersistentFlags().BoolVarP(&logSource, "log-source", "s", false, "Whether to include source location in log output or not")
}
