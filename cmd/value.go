/*
Copyright © 2024 Daniel Englert
*/
package cmd

import (
	"errors"
	"fmt"
	"io"
	"strings"

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
	Args: requireValues,
	RunE: func(cmd *cobra.Command, args []string) error {
		// The flag is defined in init, so reading it cannot fail
		newline, _ := cmd.Flags().GetBool("newline")

		// Collect all results first, so an error processing a later value does not leave partial output
		var output strings.Builder
		for _, arg := range args {
			result, err := value.ProcessValue(arg)

			if err != nil {
				return err
			}

			// Filters may produce non-string results (e.g. to_int)
			fmt.Fprint(&output, result)

			// Results are printed exactly as they are by default, since scripts may write them to files or pipe them into other tools
			if newline {
				output.WriteString("\n")
			}
		}

		_, err := io.WriteString(cmd.OutOrStdout(), output.String())
		return err
	},
}

// requireValues requires at least one value. Without one, nothing was printed with exit code 0, e.g. for a variable of
// a script that expanded to nothing (gonfig value $TEMPLATE), so a script would go on with an empty result.
// An empty value (gonfig value ”) is still allowed.
func requireValues(_ *cobra.Command, args []string) error {
	if len(args) == 0 {
		return errors.New("no value given; pass the values to process, e.g. gonfig value '${VAR}'")
	}
	return nil
}

// init registers the value command and its flags
func init() {
	rootCmd.AddCommand(valueCmd)

	// No -n shorthand, since echo -n means the opposite
	valueCmd.Flags().Bool("newline", false, "End each result with a newline, e.g. for interactive use (by default, results are printed exactly as they are)")
}
