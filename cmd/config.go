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

// getConfigSettings reads the config settings from the flags of the command
func getConfigSettings(cmd *cobra.Command) (*config.Settings, error) {
	fileName, err := cmd.Flags().GetString("file")
	if err != nil {
		return nil, err
	}

	fileType, err := cmd.Flags().GetString("file-type")
	if err != nil {
		return nil, err
	}

	configSettings := config.NewSettings()
	configSettings.File = fileName
	// An empty file type equals general.Undefined, so the file type gets inferred from the file extension
	configSettings.FileType = general.FileType(fileType)

	logging.Debug("Config settings", "settings", configSettings)

	return configSettings, nil
}

func init() {
	rootCmd.AddCommand(configCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// configCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// configCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")

	configCmd.PersistentFlags().StringP("file", "f", "", "Path to the configuration file")
	configCmd.MarkPersistentFlagRequired("file")

	configCmd.PersistentFlags().StringP("file-type", "t", "", "Type of file to be read. If not set, the file type will be inferred from the file extension")
}
