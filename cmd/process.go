/*
Copyright © 2024 Daniel Englert
*/
package cmd

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"strconv"

	"github.com/denglertai/gonfig/internal/file"
	"github.com/denglertai/gonfig/internal/output"
	"github.com/denglertai/gonfig/pkg/logging"
	"github.com/spf13/cobra"
)

// processCmd represents the process command
var processCmd = &cobra.Command{
	Use:   "process",
	Short: "Processes a file",
	Long: `Processes a file by replacing all placeholders like ${ENV_VAR | filter} and outputs the result
to the desired output. Defaults to stdout`,
	TraverseChildren: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		configSettings := getConfigSettings(cmd)

		outputFile, err := cmd.Flags().GetString("output")
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

		// The flag is defined by init, so reading it cannot fail
		modeFlag, _ := cmd.Flags().GetString("mode")
		mode, err := parseMode(modeFlag)
		if err != nil {
			return err
		}

		// Successful runs are silent by default, so their details are only logged at debug level
		logging.Debug("Running command", "command", cmd.Name(), "args", args, "configSettings", configSettings)

		// In case we want to write the output to the source file directly
		if inline {
			logging.Debug("Inline processing enabled")
			outputFile = configSettings.File
			overwriteExistingFile = true
		}

		// Store the output temporarily in a buffer
		var o = new(bytes.Buffer)
		logging.Debug("Processing file", "file", configSettings.File, "type", configSettings.FileType)
		processor := file.NewFileProcessor(configSettings.File, configSettings.FileType, o)
		err = processor.Process()
		if err != nil {
			return err
		}

		if outputFile != "-" {
			// If the file exists and we don't want to overwrite it, return an error
			if _, err := os.Stat(outputFile); err == nil && !overwriteExistingFile {
				return ErrFileExists(fmt.Errorf("file %s already exists; Use -w / --overwrite if this is intended", outputFile))
			}

			// New files get the mode of the input file unless --mode is given, so restricting a template restricts its output
			opts := output.Options{Mode: mode, ForceMode: modeFlag != ""}
			if !opts.ForceMode {
				// The input file has just been read, so this only fails if it was removed meanwhile, using a safe mode then
				opts.Mode = 0o600
				if input, err := os.Stat(configSettings.File); err == nil {
					opts.Mode = input.Mode().Perm()
				}
			}

			logging.Debug("Writing output", "file", outputFile, "mode", opts.Mode, "forceMode", opts.ForceMode)
			return output.Write(outputFile, o.Bytes(), opts)
		}

		// Dump the content to stdout
		_, err = cmd.OutOrStdout().Write(o.Bytes())
		return err
	},
}

// init registers the process command and its flags
func init() {
	configCmd.AddCommand(processCmd)

	processCmd.Flags().StringP("output", "o", "-", "Controls where to put the results (defaults to stdout)")

	processCmd.Flags().BoolP("inline", "i", false, "Controls if the output should get written to the source file directly (defaults to false)")

	processCmd.Flags().BoolP("overwrite", "w", false, "Controls if an already existing output file may be overwritten (defaults to false). Not needed for -i (--inline)")

	processCmd.Flags().String("mode", "", "File mode of the output file in octal, e.g. 0600 (defaults to the mode of an existing output file, or of the input file for new ones)")
}

type ErrFileExists error

// parseMode parses the octal file mode of --mode, e.g. "0600" or "600". An empty value returns 0.
func parseMode(value string) (fs.FileMode, error) {
	if value == "" {
		return 0, nil
	}
	mode, err := strconv.ParseUint(value, 8, 32)
	if err != nil || mode > 0o777 {
		return 0, fmt.Errorf("invalid file mode %q for --mode, use octal permissions like 0600 or 0644", value)
	}
	return fs.FileMode(mode), nil
}
