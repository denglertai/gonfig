/*
Copyright © 2024 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"github.com/denglertai/gonfig/internal/config"
	"github.com/denglertai/gonfig/internal/general"
	"github.com/denglertai/gonfig/pkg/logging"
	"github.com/spf13/cobra"
)

// configCmd represents the config command
var configCmd = &cobra.Command{
	Use:              "config",
	Short:            "",
	Long:             ``,
	TraverseChildren: true,
}

func getConfigSettings(args []string) *config.Settings {
	err := configCmd.ParseFlags(args)
	logging.Error("Error parsing flags", "error", err)

	configSettings := config.NewSettings()

	configSettings.File = fileName

	if len(fileType) > 0 {
		configSettings.FileType = general.FileType(fileType)
	} else {
		configSettings.FileType = general.Undefined
	}

	// Unset the global variables after reading the values to prevent them from being reused in subsequent tests
	fileType = ""
	fileName = ""

	logging.Debug("Config settings", "settings", configSettings)

	return configSettings
}

var fileType string
var fileName string

func init() {
	rootCmd.AddCommand(configCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// configCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// configCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")

	configCmd.PersistentFlags().StringVarP(&fileName, "file", "f", "", "Path to the configuration file")
	configCmd.MarkFlagRequired("file")

	configCmd.PersistentFlags().StringVarP(&fileType, "file-type", "t", "", "Type of file to be read. If not set, the file type will be inferred from the file extension")
}
