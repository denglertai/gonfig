/*
Copyright © 2024 Daniel Englert
*/
package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/denglertai/gonfig/internal/file"
	"github.com/spf13/cobra"
)

// checkCmd represents the check command
var checkCmd = &cobra.Command{
	Use:   "check",
	Short: "Check a file for missing variables and errors without writing it",
	Long: `Processes a file like process, but writes nothing and reports every entry containing placeholders
with the environment variables and templates it uses. Values are not printed unless --log-values is set.

The check fails, with exit code 1, if processing the file would fail or if a variable is not set
and the placeholder provides no default, independent of --fail-on-missing.`,
	TraverseChildren: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		configSettings := getConfigSettings(cmd)

		// The flag is defined in init, so reading it cannot fail
		output, _ := cmd.Flags().GetString("output")
		if output != "text" && output != "json" {
			return fmt.Errorf("unsupported output %q, use text or json", output)
		}

		report, err := file.NewFileProcessor(configSettings.File, configSettings.FileType, io.Discard).Check()
		if err != nil {
			return err
		}

		if output == "json" {
			err = writeCheckJson(cmd.OutOrStdout(), report)
		} else {
			err = writeCheckText(cmd.OutOrStdout(), report)
		}
		if err != nil {
			return err
		}

		if report.Failed() {
			return fmt.Errorf("check failed: %s", checkSummary(report))
		}
		return nil
	},
}

// checkSummary summarizes the problems of the report, e.g. "1 missing variable, 2 errors"
func checkSummary(report *file.CheckReport) string {
	return fmt.Sprintf("%s, %s",
		plural(report.MissingVariables(), "missing variable", "missing variables"), plural(report.Errors(), "error", "errors"))
}

// plural returns the count with the singular or plural form of the noun
func plural(count int, singular string, pluralForm string) string {
	if count == 1 {
		return "1 " + singular
	}
	return fmt.Sprintf("%d %s", count, pluralForm)
}

// writeCheckText writes the report in a readable format
func writeCheckText(w io.Writer, report *file.CheckReport) error {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s: %s with placeholders\n", report.File, plural(len(report.Entries), "entry", "entries"))

	for _, entry := range report.Entries {
		fmt.Fprintf(&sb, "  %s\n", entry.Path)
		for _, variable := range entry.Variables {
			// The default only applies to variables that are not set or empty
			status := variable.Status
			if variable.HasDefault && variable.Status != file.VariableSet {
				status += ", default used"
			}
			fmt.Fprintf(&sb, "    %-24s %s\n", variable.Name, status)
		}
		if len(entry.Templates) > 0 {
			fmt.Fprintf(&sb, "    templates: %s\n", strings.Join(entry.Templates, ", "))
		}
		if entry.Error != "" {
			fmt.Fprintf(&sb, "    error: %s\n", entry.Error)
		}
	}

	fmt.Fprintf(&sb, "%s\n", checkSummary(report))
	_, err := io.WriteString(w, sb.String())
	return err
}

// writeCheckJson writes the report as JSON, including the counts of the problems
func writeCheckJson(w io.Writer, report *file.CheckReport) error {
	// The report consists of strings, booleans, numbers and slices of them, so encoding it cannot fail
	data, _ := json.MarshalIndent(struct {
		*file.CheckReport
		MissingVariables int  `json:"missingVariables"`
		Errors           int  `json:"errors"`
		Failed           bool `json:"failed"`
	}{report, report.MissingVariables(), report.Errors(), report.Failed()}, "", "  ")

	_, err := fmt.Fprintln(w, string(data))
	return err
}

// init registers the check command and its flags
func init() {
	configCmd.AddCommand(checkCmd)

	checkCmd.Flags().StringP("output", "o", "text", "Format of the report: text or json")
}
