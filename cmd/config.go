/*
Copyright © 2024 Daniel Englert
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
	Use:   "config",
	Short: "Work with configuration files",
	Long: `Work with configuration files in JSON, YAML, XML, properties or plain text format.
The format is inferred from the file extension unless set via --file-type.`,
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

// init registers the config command and its persistent flags, of which --file is required
func init() {
	rootCmd.AddCommand(configCmd)

	configCmd.PersistentFlags().StringP("file", "f", "", "Path to the configuration file")
	// Marking fails only for flags that are not defined
	cobra.CheckErr(configCmd.MarkPersistentFlagRequired("file"))

	configCmd.PersistentFlags().StringP("file-type", "t", "", "Type of file to be read. If not set, the file type will be inferred from the file extension")
}
