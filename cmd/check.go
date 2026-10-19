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
	Short: "Check files for missing variables and errors without writing them",
	Long: `Processes files like process, but writes nothing and reports every entry containing placeholders
with the environment variables and templates it uses. Values are not printed unless --log-values is set.
Several files are checked with -f per file or glob patterns like -f 'conf/*.yaml'.

The check fails, with exit code 1, if processing a file would fail or if a variable is not set
and the placeholder provides no default, independent of --fail-on-missing.`,
	Args:             noArgs,
	TraverseChildren: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		configSettings, err := getConfigSettings(cmd)
		if err != nil {
			return err
		}

		// The flag is defined in init, so reading it cannot fail
		output, _ := cmd.Flags().GetString("output")
		if output != "text" && output != "json" {
			return fmt.Errorf("unsupported output %q, use text or json", output)
		}

		// Files that cannot be read are reported as errors as well, so the other files are still checked
		reports := make(checkReports, 0, len(configSettings.Files))
		for _, name := range configSettings.Files {
			report, err := file.NewFileProcessor(name, configSettings.FileType, io.Discard).Check()
			if err != nil {
				report = &file.CheckReport{File: name, Entries: []file.EntryReport{}, Error: err.Error()}
			}
			reports = append(reports, report)
		}

		if output == "json" {
			err = writeCheckJson(cmd.OutOrStdout(), reports)
		} else {
			err = writeCheckText(cmd.OutOrStdout(), reports)
		}
		if err != nil {
			return err
		}

		if reports.failed() {
			return fmt.Errorf("check failed: %s", reports.summary())
		}
		return nil
	},
}

// checkReports are the reports of the checked files
type checkReports []*file.CheckReport

// missingVariables returns the number of missing variables in all files
func (r checkReports) missingVariables() int {
	count := 0
	for _, report := range r {
		count += report.MissingVariables()
	}
	return count
}

// errors returns the number of errors in all files
func (r checkReports) errors() int {
	count := 0
	for _, report := range r {
		count += report.Errors()
	}
	return count
}

// failed returns whether the check of any file failed
func (r checkReports) failed() bool {
	for _, report := range r {
		if report.Failed() {
			return true
		}
	}
	return false
}

// summary summarizes the problems of all files, e.g. "1 missing variable, 2 errors"
func (r checkReports) summary() string {
	return problemSummary(r.missingVariables(), r.errors())
}

// checkSummary summarizes the problems of the report, e.g. "1 missing variable, 2 errors"
func checkSummary(report *file.CheckReport) string {
	return problemSummary(report.MissingVariables(), report.Errors())
}

// problemSummary summarizes the numbers of missing variables and errors, e.g. "1 missing variable, 2 errors"
func problemSummary(missingVariables int, errors int) string {
	return fmt.Sprintf("%s, %s",
		plural(missingVariables, "missing variable", "missing variables"), plural(errors, "error", "errors"))
}

// plural returns the count with the singular or plural form of the noun
func plural(count int, singular string, pluralForm string) string {
	if count == 1 {
		return "1 " + singular
	}
	return fmt.Sprintf("%d %s", count, pluralForm)
}

// writeCheckText writes the reports in a readable format, followed by the total for several files
func writeCheckText(w io.Writer, reports checkReports) error {
	var sb strings.Builder
	for _, report := range reports {
		writeCheckReportText(&sb, report)
	}
	if len(reports) > 1 {
		fmt.Fprintf(&sb, "%s: %s\n", plural(len(reports), "file", "files"), reports.summary())
	}
	_, err := io.WriteString(w, sb.String())
	return err
}

// writeCheckReportText writes the report of a file in a readable format
func writeCheckReportText(sb *strings.Builder, report *file.CheckReport) {
	if report.Error != "" {
		fmt.Fprintf(sb, "%s: cannot be checked\n  error: %s\n%s\n", report.File, report.Error, checkSummary(report))
		return
	}
	fmt.Fprintf(sb, "%s: %s with placeholders\n", report.File, plural(len(report.Entries), "entry", "entries"))

	for _, entry := range report.Entries {
		fmt.Fprintf(sb, "  %s\n", entry.Path)
		for _, variable := range entry.Variables {
			// The default only applies to variables that are not set or empty
			status := variable.Status
			if variable.HasDefault && variable.Status != file.VariableSet {
				status += ", default used"
			}
			fmt.Fprintf(sb, "    %-24s %s\n", variable.Name, status)
		}
		if len(entry.Templates) > 0 {
			fmt.Fprintf(sb, "    templates: %s\n", strings.Join(entry.Templates, ", "))
		}
		if entry.Error != "" {
			fmt.Fprintf(sb, "    error: %s\n", entry.Error)
		}
	}

	fmt.Fprintf(sb, "%s\n", checkSummary(report))
}

// checkFileJson is the JSON report of a file, including the counts of its problems
type checkFileJson struct {
	*file.CheckReport
	MissingVariables int  `json:"missingVariables"`
	Errors           int  `json:"errors"`
	Failed           bool `json:"failed"`
}

// writeCheckJson writes the reports as JSON: the report of each file and the total counts of the problems.
// The files are a list for a single file as well, so scripts handle one format however many files a pattern matches.
func writeCheckJson(w io.Writer, reports checkReports) error {
	files := make([]checkFileJson, 0, len(reports))
	for _, report := range reports {
		files = append(files, checkFileJson{report, report.MissingVariables(), report.Errors(), report.Failed()})
	}
	// The reports consist of strings, booleans, numbers and slices of them, so encoding them cannot fail
	data, _ := json.MarshalIndent(struct {
		Files            []checkFileJson `json:"files"`
		MissingVariables int             `json:"missingVariables"`
		Errors           int             `json:"errors"`
		Failed           bool            `json:"failed"`
	}{files, reports.missingVariables(), reports.errors(), reports.failed()}, "", "  ")

	_, err := fmt.Fprintln(w, string(data))
	return err
}

// init registers the check command and its flags
func init() {
	configCmd.AddCommand(checkCmd)

	checkCmd.Flags().StringP("output", "o", "text", "Format of the report: text or json")
}
