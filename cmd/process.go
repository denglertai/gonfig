/*
Copyright © 2024 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"os"

	"github.com/denglertai/gonfig/internal/file"
	"github.com/spf13/cobra"
)

var output string

// processCmd represents the process command
var processCmd = &cobra.Command{
	Use:   "process",
	Short: "Processes a file",
	Long:  `Processes a file and outputs the result to the dersired output. Defaults to stdout`,
	RunE: func(cmd *cobra.Command, args []string) error {
		var o = os.Stdout
		if output != "-" {
			f, err := os.Create(output)
			if err != nil {
				return err
			}
			defer f.Close()
			o = f
		}

		processor := file.NewFileProcessor(configSettings.File, configSettings.FileType, o)
		err := processor.Process()
		if err != nil {
			return err
		}

		return nil
	},
}

func init() {
	configCmd.AddCommand(processCmd)

	processCmd.Flags().StringVarP(&output, "output", "o", "-", "Controls where to put the results (defaults to stdout)")

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// processCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// processCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}
