/*
Copyright © 2024 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"bytes"
	"fmt"
	"os"

	"github.com/denglertai/gonfig/internal/file"
	"github.com/denglertai/gonfig/pkg/logging"
	"github.com/spf13/cobra"
)

// processCmd represents the process command
var processCmd = &cobra.Command{
	Use:              "process",
	Short:            "Processes a file",
	Long:             `Processes a file and outputs the result to the dersired output. Defaults to stdout`,
	TraverseChildren: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		configSettings, err := getConfigSettings(cmd)
		if err != nil {
			return err
		}

		output, err := cmd.Flags().GetString("output")
		if err != nil {
			return err
		}

		inline, err := cmd.Flags().GetBool("inline")
		if err != nil {
			return err
		}

		overwriteExistingFile, err := cmd.Flags().GetBool("overwrite")
		if err != nil {
			return err
		}

		logging.Info("RunE", "command", cmd.Name(), "args", args, "configSettings", configSettings)

		// In case we want to write the output to the source file directly
		if inline {
			logging.Debug("Inline processing enabled")
			output = configSettings.File
			overwriteExistingFile = true
		}

		// Store the output temporarily in a buffer
		var o = new(bytes.Buffer)
		logging.Info("Processing file", "file", configSettings.File, "type", configSettings.FileType)
		processor := file.NewFileProcessor(configSettings.File, configSettings.FileType, o)
		err = processor.Process()
		if err != nil {
			return err
		}

		if output != "-" {
			// If the file exists and we don't want to overwrite it, return an error
			if _, err := os.Stat(output); err == nil && !overwriteExistingFile {
				return ErrFileExists(fmt.Errorf("file %s already exists; Use -w / --overwrite if this is intended", output))
			}

			// Write the output to the file
			logging.Info("Writing output", "file", output)

			// Unlike a deferred Close, WriteFile also reports errors on closing the file
			return os.WriteFile(output, o.Bytes(), 0o666)
		}

		// Dump the content to stdout
		_, err = cmd.OutOrStdout().Write(o.Bytes())
		return err
	},
}

func init() {
	configCmd.AddCommand(processCmd)

	processCmd.Flags().StringP("output", "o", "-", "Controls where to put the results (defaults to stdout)")

	processCmd.Flags().BoolP("inline", "i", false, "Controls if the output should get written to the source file directly (defaults to false)")

	processCmd.Flags().BoolP("overwrite", "w", false, "Controls if the output should overwrite the source file (defaults to false). This implies -i (--inline). If the source file does not exist, it will be created.")
}

type ErrFileExists error
