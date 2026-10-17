/*
Copyright © 2024 Daniel Englert
*/
package cmd

import (
	"fmt"
	"io"

	"github.com/denglertai/gonfig/internal/value"
	"github.com/spf13/cobra"
)

// valueCmd represents the value command
var valueCmd = &cobra.Command{
	Use:   "value",
	Short: "Extract or process a specific configuration value",
	Long: `The value command allows you to extract and process individual configuration values.
It supports various filters for transforming the output, such as uppercasing, lowercasing,
trimming whitespace, or applying cryptographic functions like bcrypt or md5.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// The flag is defined in init, so reading it cannot fail
		newline, _ := cmd.Flags().GetBool("newline")

		for _, arg := range args {
			result, err := value.ProcessValue(arg)

			if err != nil {
				return err
			}

			// Filters may produce non-string results (e.g. to_int)
			text := fmt.Sprint(result)

			// Results are printed exactly as they are by default, since scripts may write them to files or pipe them into other tools
			if newline {
				text += "\n"
			}

			if _, err := io.WriteString(cmd.OutOrStdout(), text); err != nil {
				return err
			}
		}

		return nil
	},
}

// init registers the value command and its flags
func init() {
	rootCmd.AddCommand(valueCmd)

	// No -n shorthand, since echo -n means the opposite
	valueCmd.Flags().Bool("newline", false, "End each result with a newline, e.g. for interactive use (by default, results are printed exactly as they are)")
}
