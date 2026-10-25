/*
Copyright © 2024 Daniel Englert
*/
package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

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

// getConfigSettings reads the config settings from the flags of the command, expanding the glob patterns of --file
func getConfigSettings(cmd *cobra.Command) (*config.Settings, error) {
	// Both flags are persistent flags of the config command, so reading them cannot fail
	fileValues, _ := cmd.Flags().GetStringArray("file")
	fileType, _ := cmd.Flags().GetString("file-type")

	files, err := expandFiles(fileValues)
	if err != nil {
		return nil, err
	}

	configSettings := config.NewSettings()
	configSettings.Files = files
	// An empty file type equals general.Undefined, so the file type gets inferred from the file extension
	configSettings.FileType = general.FileType(fileType)

	logging.Debug("Config settings", "settings", configSettings)

	return configSettings, nil
}

// expandFiles returns the files of the --file values in their order, without duplicates.
// Values containing glob characters are expanded unless they name an existing file, see filepath.Match.
// The sorted matches of a pattern include files only, and a pattern matching no file is an error,
// so a typo cannot silently skip files.
func expandFiles(values []string) ([]string, error) {
	var files []string
	seen := map[string]bool{}
	add := func(name string) {
		// Paths of the same file are compared by the file, e.g. a file and a symbolic link to it
		key := resolvedPath(name)
		if !seen[key] {
			seen[key] = true
			files = append(files, name)
		}
	}

	for _, value := range values {
		if _, err := os.Stat(value); err == nil || !strings.ContainsAny(value, "*?[") {
			// Files that don't exist are reported when they are read, like before
			add(value)
			continue
		}

		matches, err := filepath.Glob(value)
		if err != nil {
			return nil, fmt.Errorf("invalid pattern %q for -f / --file: %w", value, err)
		}
		matched := false
		for _, match := range matches {
			// Glob returns matches in lexical order. Directories are skipped, as patterns like conf/* match them as well.
			if info, err := os.Stat(match); err == nil && info.IsDir() {
				continue
			}
			add(match)
			matched = true
		}
		if !matched {
			return nil, fmt.Errorf("no files match the pattern %q of -f / --file", value)
		}
	}
	return files, nil
}

// resolvedPath returns the absolute path of the file with symbolic links resolved, so two paths of the same file are equal.
// Links that cannot be resolved, e.g. as the file does not exist yet, are kept.
func resolvedPath(name string) string {
	if resolved, err := filepath.EvalSymlinks(name); err == nil {
		name = resolved
	}
	// Abs only fails if the working directory is unknown, comparing the path as given then
	if abs, err := filepath.Abs(name); err == nil {
		name = abs
	}
	return name
}

// noArgs rejects positional arguments, which used to be ignored silently, e.g. the second file of -f a.yaml b.yaml
func noArgs(_ *cobra.Command, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("unexpected argument %q; use -f for each file, e.g. -f a.yaml -f b.yaml", args[0])
	}
	return nil
}

// init registers the config command and its persistent flags, of which --file is required
func init() {
	rootCmd.AddCommand(configCmd)

	configCmd.PersistentFlags().StringArrayP("file", "f", nil, "Path of a configuration file or a glob pattern like 'conf/*.yaml'; can be repeated")
	// Marking fails only for flags that are not defined
	cobra.CheckErr(configCmd.MarkPersistentFlagRequired("file"))

	configCmd.PersistentFlags().StringP("file-type", "t", "", "Type of file to be read. If not set, the file type will be inferred from the file extension, ignoring case")
}
