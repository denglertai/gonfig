package file

import (
	"bytes"
	"iter"
	"os"
	"path"
	"testing"

	"github.com/denglertai/gonfig/internal/config"
	"github.com/denglertai/gonfig/internal/filter"
	"github.com/denglertai/gonfig/internal/general"
	"github.com/denglertai/gonfig/internal/value"
	"github.com/stretchr/testify/assert"
)

// checkFile checks the content as a file of the given type and returns the report
func checkFile(t *testing.T, fileType general.FileType, content string) *CheckReport {
	t.Helper()

	file := path.Join(t.TempDir(), "config."+string(fileType))
	assert.NoError(t, os.WriteFile(file, []byte(content), 0o644))

	var output bytes.Buffer
	report, err := NewFileProcessor(file, fileType, &output).Check()
	assert.NoError(t, err)
	assert.Empty(t, output.String(), "check must not write anything")

	written, err := os.ReadFile(file)
	assert.NoError(t, err)
	assert.Equal(t, content, string(written), "check must not change the file")

	return report
}

// TestCheck checks the report of a file with set, empty and missing variables, defaults and an error
func TestCheck(t *testing.T) {
	t.Setenv("CHECK_SET", "value")
	t.Setenv("CHECK_EMPTY", "")
	t.Setenv("CHECK_PORT", "eighty")

	report := checkFile(t, general.YAML, `static: value
set: ${CHECK_SET}
missing: ${CHECK_MISSING}
empty: ${CHECK_EMPTY}
default: ${CHECK_MISSING_TOO | default(x)}
port: ${CHECK_PORT | to_int}
both: ${CHECK_MISSING_TOO}${CHECK_MISSING_TOO | default(x)}
`)

	assert.Equal(t, []EntryReport{
		{Path: "set", Variables: []VariableReport{{Name: "CHECK_SET", Status: VariableSet}}},
		{Path: "missing", Variables: []VariableReport{{Name: "CHECK_MISSING", Status: VariableMissing}}},
		{Path: "empty", Variables: []VariableReport{{Name: "CHECK_EMPTY", Status: VariableEmpty}}},
		{Path: "default", Variables: []VariableReport{{Name: "CHECK_MISSING_TOO", Status: VariableMissing, HasDefault: true}}},
		{Path: "port", Variables: []VariableReport{{Name: "CHECK_PORT", Status: VariableSet}}, Error: "filter to_int: the value is not an integer"},
		// A variable read several times has a default only if every placeholder provides one
		{Path: "both", Variables: []VariableReport{{Name: "CHECK_MISSING_TOO", Status: VariableMissing}}},
	}, report.Entries)
	assert.Equal(t, 2, report.MissingVariables())
	assert.Equal(t, 1, report.Errors())
	assert.True(t, report.Failed())
}

// TestCheckWithoutProblems checks that a report without errors and missing variables does not fail
func TestCheckWithoutProblems(t *testing.T) {
	t.Setenv("CHECK_SET", "value")

	report := checkFile(t, general.JSON, `{"a": "${CHECK_SET}", "b": "${CHECK_MISSING | default(1) | to_int}", "c": 1}`)

	assert.Len(t, report.Entries, 2)
	assert.Equal(t, 0, report.MissingVariables())
	assert.Equal(t, 0, report.Errors())
	assert.False(t, report.Failed())
}

// TestCheckFragments checks that the entries of fragments are reported and errors of fragments are collected
func TestCheckFragments(t *testing.T) {
	t.Setenv("CHECK_FRAGMENT", "inner: ${CHECK_INNER}\nnested: ${CHECK_NESTED | fragment}")
	t.Setenv("CHECK_NESTED", "a: 1")
	t.Setenv("CHECK_INVALID", "a: [1")

	report := checkFile(t, general.YAML, "outer: ${CHECK_FRAGMENT | fragment}\ninvalid: ${CHECK_INVALID | fragment}\n")

	assert.Equal(t, []EntryReport{
		{Path: "outer", Variables: []VariableReport{{Name: "CHECK_FRAGMENT", Status: VariableSet}}},
		{Path: "outer.inner", Variables: []VariableReport{{Name: "CHECK_INNER", Status: VariableMissing}}},
		{Path: "outer.nested", Variables: []VariableReport{{Name: "CHECK_NESTED", Status: VariableSet}}, Error: "fragments cannot contain further fragments"},
		{Path: "invalid", Variables: []VariableReport{{Name: "CHECK_INVALID", Status: VariableSet}}, Error: "invalid YAML fragment on line 1"},
	}, report.Entries)
}

// TestCheckWithFailOnMissing checks that with fail-on-missing, a missing variable is reported as missing and as an error
func TestCheckWithFailOnMissing(t *testing.T) {
	filter.SetFailOnMissing(true)
	t.Cleanup(func() { filter.SetFailOnMissing(false) })

	report := checkFile(t, general.YAML, "a: ${CHECK_MISSING}\n")

	assert.Equal(t, []EntryReport{
		{Path: "a", Variables: []VariableReport{{Name: "CHECK_MISSING", Status: VariableMissing}}, Error: `environment variable "CHECK_MISSING" is not set`},
	}, report.Entries)
}

// TestCheckTemplates checks that templates are listed and variables read within them are reported
func TestCheckTemplates(t *testing.T) {
	assert.NoError(t, value.SetTemplates(config.Templates{
		Filters: map[string]config.FilterTemplate{"clean": {Filters: "trim"}},
		Placeholders: map[string]config.PlaceholderTemplate{
			"host": {Env: "CHECK_TEMPLATE_HOST", Default: "localhost"},
			"url":  {Value: "jdbc:postgresql://${@host}:${CHECK_TEMPLATE_PORT}/app"},
		},
	}))
	t.Cleanup(func() { assert.NoError(t, value.SetTemplates(config.Templates{})) })

	report := checkFile(t, general.PLAIN, "url=${@url}\nuser=${CHECK_TEMPLATE_USER | @clean}\n")

	assert.Equal(t, []EntryReport{
		// Lines of plain text files are counted from 0
		{Path: "0", Templates: []string{"@url"}, Variables: []VariableReport{
			{Name: "CHECK_TEMPLATE_HOST", Status: VariableMissing, HasDefault: true},
			{Name: "CHECK_TEMPLATE_PORT", Status: VariableMissing},
		}},
		{Path: "1", Templates: []string{"@clean"}, Variables: []VariableReport{{Name: "CHECK_TEMPLATE_USER", Status: VariableMissing}}},
	}, report.Entries)
}

// TestCheckUnreadableFile checks that a file that cannot be read is an error instead of a report
func TestCheckUnreadableFile(t *testing.T) {
	_, err := NewFileProcessor(path.Join(t.TempDir(), "missing.yaml"), general.Undefined, nil).Check()
	assert.Error(t, err)

	file := path.Join(t.TempDir(), "invalid.yaml")
	assert.NoError(t, os.WriteFile(file, []byte("a: [1"), 0o644))
	_, err = NewFileProcessor(file, general.Undefined, nil).Check()
	assert.ErrorContains(t, err, "failed to read")
}

// failingProcessHandler is a YAML handler whose Process fails, which none of the real handlers does
type failingProcessHandler struct {
	*YamlConfigFileHandler
}

// Process returns an error instead of the entries
func (failingProcessHandler) Process() (iter.Seq[ConfigEntry], error) {
	return nil, assert.AnError
}

// TestReadProcessError checks that an error of the handler's Process names the file
func TestReadProcessError(t *testing.T) {
	file := path.Join(t.TempDir(), "config.yaml")
	assert.NoError(t, os.WriteFile(file, []byte("a: 1\n"), 0o644))

	_, _, err := NewFileProcessor(file, general.Undefined, nil).readWith(failingProcessHandler{NewYamlConfigFileHandler()})
	assert.ErrorIs(t, err, assert.AnError)
	assert.ErrorContains(t, err, "failed to process "+file)
}
