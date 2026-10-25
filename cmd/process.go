/*
Copyright © 2024 Daniel Englert
*/
package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/denglertai/gonfig/internal/file"
	"github.com/denglertai/gonfig/internal/output"
	"github.com/denglertai/gonfig/pkg/logging"
	"github.com/spf13/cobra"
)

// processCmd represents the process command
var processCmd = &cobra.Command{
	Use:   "process",
	Short: "Processes files",
	Long: `Processes files by replacing all placeholders like ${ENV_VAR | filter} and outputs the results
to the desired output. Defaults to stdout, which only works for a single file.

Several files are processed with -f per file or glob patterns like -f 'conf/*.yaml', and written back
with -i or into a directory with -o <dir>/. All files are processed before any is written, so an error
leaves all files unchanged.`,
	Args:             noArgs,
	TraverseChildren: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		configSettings, err := getConfigSettings(cmd)
		if err != nil {
			return err
		}

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

		// In case we want to write the output to the source files directly
		if inline {
			logging.Debug("Inline processing enabled")
			overwriteExistingFile = true
		}
		outputs, outputDir, err := outputFiles(configSettings.Files, outputFile, inline)
		if err != nil {
			return err
		}

		// Existing files are checked before any file is processed or written, so a single existing file writes nothing
		if !overwriteExistingFile {
			for _, name := range outputs {
				if _, err := os.Stat(name); err == nil {
					return fmt.Errorf("%w: %s; use -w / --overwrite if this is intended", ErrFileExists, name)
				}
			}
		}

		// All files are processed before any is written, so an error while processing leaves all files unchanged
		results := make([]output.File, 0, len(configSettings.Files))
		for i, input := range configSettings.Files {
			var o = new(bytes.Buffer)
			logging.Debug("Processing file", "file", input, "type", configSettings.FileType)
			if err := file.NewFileProcessor(input, configSettings.FileType, o).Process(); err != nil {
				return err
			}
			if outputs == nil {
				// Dump the content to stdout, which is only used for a single file
				_, err = cmd.OutOrStdout().Write(o.Bytes())
				return err
			}

			// New files get the mode of their input file unless --mode is given, so restricting a template restricts its output
			opts := output.Options{Mode: mode, ForceMode: modeFlag != ""}
			if !opts.ForceMode {
				// The input file has just been read, so this only fails if it was removed meanwhile, using a safe mode then
				opts.Mode = 0o600
				if info, err := os.Stat(input); err == nil {
					opts.Mode = info.Mode().Perm()
				}
			}
			logging.Debug("Writing output", "file", outputs[i], "mode", opts.Mode, "forceMode", opts.ForceMode)
			results = append(results, output.File{Path: outputs[i], Data: o.Bytes(), Options: opts})
		}

		if outputDir != "" {
			// --mode applies to files only, as a directory without the execute permission cannot be entered
			if err := os.MkdirAll(outputDir, 0o755); err != nil {
				return fmt.Errorf("failed to create the output directory: %w", err)
			}
		}
		return output.WriteAll(results)
	},
}

// outputFiles returns the output file of each input file, or nil for stdout, and the output directory if -o names one.
// -o names a directory if it ends with a path separator or is an existing directory, so its meaning doesn't depend on
// how many files a pattern matches. Each file is written into it under its base name.
// Two files written to the same file are an error, including through symbolic links in the directory.
func outputFiles(inputs []string, outputFlag string, inline bool) (outputs []string, outputDir string, err error) {
	switch {
	case inline:
		return inputs, "", nil
	case outputFlag == "-":
		if len(inputs) > 1 {
			return nil, "", fmt.Errorf("%d files cannot be written to stdout; use -i to write them back or -o <dir>/ to write them into a directory", len(inputs))
		}
		return nil, "", nil
	case !isDirectory(outputFlag):
		if len(inputs) > 1 {
			return nil, "", fmt.Errorf("-o %s is not a directory, which %d files need; end it with / to create it, or use -i", outputFlag, len(inputs))
		}
		return []string{outputFlag}, "", nil
	}

	writtenBy := map[string]string{}
	for _, input := range inputs {
		name := filepath.Join(outputFlag, filepath.Base(input))
		target := resolvedPath(name)
		if other, ok := writtenBy[target]; ok {
			return nil, "", fmt.Errorf("%s and %s would both be written to %s", other, input, target)
		}
		writtenBy[target] = input
		outputs = append(outputs, name)
	}
	return outputs, outputFlag, nil
}

// isDirectory returns whether -o names a directory: an existing one or a path ending with a separator
func isDirectory(path string) bool {
	if strings.HasSuffix(path, "/") || strings.HasSuffix(path, string(os.PathSeparator)) {
		return true
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// init registers the process command and its flags
func init() {
	configCmd.AddCommand(processCmd)

	processCmd.Flags().StringP("output", "o", "-", "Controls where to put the results: a file, or a directory ending with / for several files (defaults to stdout)")

	processCmd.Flags().BoolP("inline", "i", false, "Controls if the output should get written to the source files directly (defaults to false)")

	processCmd.Flags().BoolP("overwrite", "w", false, "Controls if an already existing output file may be overwritten (defaults to false). Not needed for -i (--inline)")

	processCmd.Flags().String("mode", "", "File mode of the output files in octal, e.g. 0600 (defaults to the mode of an existing output file, or of the input file for new ones)")

	// -i writes to the input files, so an output given as well would be ignored
	processCmd.MarkFlagsMutuallyExclusive("inline", "output")
}

// ErrFileExists is returned if an output file exists and may not be overwritten, see -w / --overwrite.
// It is wrapped with the name of the file, so callers detect it with errors.Is.
var ErrFileExists = errors.New("file already exists")

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
