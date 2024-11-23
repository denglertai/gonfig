/*
Copyright © 2024 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"github.com/denglertai/gonfig/internal/config"
	"github.com/denglertai/gonfig/internal/general"
	"github.com/spf13/cobra"
)

// configCmd represents the config command
var configCmd = &cobra.Command{
	Use:              "config",
	Short:            "",
	Long:             ``,
	TraverseChildren: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		configSettings.FileType = general.FileType(fileType)
		return nil
	},
}

var configSettings = config.NewSettings()
var fileType string

func init() {
	rootCmd.AddCommand(configCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// configCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// configCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")

	configCmd.Flags().StringVarP(&configSettings.File, "file", "f", "", "Path to the configuration file")
	configCmd.MarkFlagRequired("file")

	configCmd.Flags().StringVarP(&fileType, "file-type", "t", "", "Type of file to be read. If not set, the file type will be inferred from the file extension")
}
