package file

import (
	"bytes"
	"io"
	"iter"
	"os"
	"path"
	"strings"
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

// TestCheckEscapes checks that entries with escaped placeholders only are not reported, since they have no placeholders,
// while the diff shows that they are unescaped
func TestCheckEscapes(t *testing.T) {
	t.Setenv("CHECK_SET", "value")
	content := "escaped: $${CHECK_MISSING}\nboth: $${CHECK_MISSING} ${CHECK_SET}\nunterminated: $${CHECK_MISSING} ${CHECK_SET\n"

	report := checkFile(t, general.YAML, content)
	assert.Equal(t, []EntryReport{
		{Path: "both", Variables: []VariableReport{{Name: "CHECK_SET", Status: VariableSet}}},
		{Path: "unterminated", Variables: []VariableReport{}, Error: `unterminated placeholder "${CHECK_SET", the closing } is missing`},
	}, report.Entries)

	assert.Equal(t, "@@ -1,3 +1,3 @@\n-escaped: $${CHECK_MISSING}\n-both: $${CHECK_MISSING} ${CHECK_SET}\n+escaped: ${CHECK_MISSING}\n+both: ${CHECK_MISSING} ***\n unterminated: $${CHECK_MISSING} ${CHECK_SET\n",
		checkDiff(t, general.YAML, content))
}

// TestCheckUnterminatedParams checks that params without a closing bracket are reported as errors of their entries
func TestCheckUnterminatedParams(t *testing.T) {
	t.Setenv("CHECK_SET", "value")

	report := checkFile(t, general.YAML, "filter: ${CHECK_SET | default(a}\nnested: ${CHECK_SET | default(${CHECK_OTHER})}\n")
	assert.Equal(t, []EntryReport{
		{Path: "filter", Variables: []VariableReport{{Name: "CHECK_SET", Status: VariableSet, HasDefault: true}},
			Error: `unterminated params "(a" of default, the closing ) is missing`},
		{Path: "nested", Variables: []VariableReport{{Name: "CHECK_SET", Status: VariableSet, HasDefault: true}},
			Error: `unterminated params "(${CHECK_OTHER" of default, the closing ) is missing; params cannot contain placeholders, escape { and } with a backslash`},
	}, report.Entries)
	assert.Equal(t, 2, report.Errors())
	assert.True(t, report.Failed())
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

// TestCheckReportFileError checks that a report with the error of a file that cannot be read counts it as an error and fails
func TestCheckReportFileError(t *testing.T) {
	report := &CheckReport{File: "missing.yaml", Entries: []EntryReport{{Path: "a", Error: "invalid"}}, Error: "open missing.yaml: no such file"}
	assert.Equal(t, 2, report.Errors())
	assert.True(t, report.Failed())
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

// checkDiff checks the content as a file of the given type with the diff requested and returns the diff
// without its header, which names the temporary file
func checkDiff(t *testing.T, fileType general.FileType, content string) string {
	t.Helper()

	file := path.Join(t.TempDir(), "config."+string(fileType))
	assert.NoError(t, os.WriteFile(file, []byte(content), 0o644))

	processor := NewFileProcessor(file, fileType, io.Discard)
	processor.Diff = true
	report, err := processor.Check()
	assert.NoError(t, err)
	assert.Empty(t, report.Error)
	if !assert.NotNil(t, report.Diff) || *report.Diff == "" {
		return ""
	}

	header := "--- " + file + "\n+++ " + file + "\n"
	assert.True(t, strings.HasPrefix(*report.Diff, header), "diff %q starts with the file names", *report.Diff)
	return strings.TrimPrefix(*report.Diff, header)
}

// TestCheckDiffMasksValues checks that the diff masks the results of placeholders and every value inserted by
// fragments, while keys and elements of fragments are shown
func TestCheckDiffMasksValues(t *testing.T) {
	t.Setenv("DIFF_HOST", "db.internal")
	t.Setenv("DIFF_PORT", "5432")
	t.Setenv("DIFF_KEY", "user")

	tests := []struct {
		name     string
		fileType general.FileType
		env      map[string]string
		content  string
		want     string
	}{
		{
			name:     "yaml values and fragments",
			fileType: general.YAML,
			env:      map[string]string{"DIFF_FRAGMENT": "port: ${DIFF_PORT | to_int}\nssl: true\nempty:"},
			content:  "host: ${DIFF_HOST}\nurl: pg://${DIFF_HOST}:${DIFF_PORT}\nname: static\n${DIFF_FRAGMENT | fragment}:\n",
			want:     "@@ -1,4 +1,6 @@\n-host: ${DIFF_HOST}\n-url: pg://${DIFF_HOST}:${DIFF_PORT}\n+host: '***'\n+url: pg://***:***\n name: static\n-${DIFF_FRAGMENT | fragment}:\n+port: '***'\n+ssl: '***'\n+empty: '***'\n",
		},
		{
			name:     "json fragment with a key containing a placeholder",
			fileType: general.JSON,
			env:      map[string]string{"DIFF_FRAGMENT": `{"${DIFF_KEY}": 1, "ssl": true, "none": null, "list": ["a"]}`},
			content:  "{\n  \"db\": \"${DIFF_FRAGMENT | fragment}\"\n}\n",
			want:     "@@ -1,3 +1,10 @@\n {\n-  \"db\": \"${DIFF_FRAGMENT | fragment}\"\n+  \"db\": {\n+    \"***\": \"***\",\n+    \"ssl\": \"***\",\n+    \"none\": \"***\",\n+    \"list\": [\n+      \"***\"\n+    ]\n+  }\n }\n",
		},
		{
			name:     "xml attributes and elements",
			fileType: general.XML,
			env:      map[string]string{"DIFF_FRAGMENT": `<user name="${DIFF_KEY}">sa</user><port>${DIFF_PORT}</port>`},
			content:  "<db host=\"${DIFF_HOST}\">${DIFF_FRAGMENT | fragment}</db>\n",
			want:     "@@ -1 +1 @@\n-<db host=\"${DIFF_HOST}\">${DIFF_FRAGMENT | fragment}</db>\n+<db host=\"***\"><user name=\"***\">***</user><port>***</port></db>\n",
		},
		{
			name:     "properties",
			fileType: general.PROPERTIES,
			content:  "host=${DIFF_HOST}\nport=5432\n",
			// The properties writer separates keys and values by " = ", which the diff shows as well
			want: "@@ -1,2 +1,2 @@\n-host=${DIFF_HOST}\n-port=5432\n+host = ***\n+port = 5432\n",
		},
		{
			name:     "without changes",
			fileType: general.YAML,
			content:  "host: localhost\n",
			want:     "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for name, value := range tt.env {
				t.Setenv(name, value)
			}
			assert.Equal(t, tt.want, checkDiff(t, tt.fileType, tt.content))
		})
	}
}

// TestCheckDiffMasksFailedFragmentValues checks that values of fragments are masked even if they cannot be processed,
// since their text is part of the fragment, while the error is reported
func TestCheckDiffMasksFailedFragmentValues(t *testing.T) {
	t.Setenv("DIFF_FRAGMENT", "port: ${DIFF_INVALID | to_int}")
	t.Setenv("DIFF_INVALID", "eighty")

	file := path.Join(t.TempDir(), "config.yaml")
	assert.NoError(t, os.WriteFile(file, []byte("db: ${DIFF_FRAGMENT | fragment}\n"), 0o644))

	processor := NewFileProcessor(file, general.Undefined, nil)
	processor.Diff = true
	report, err := processor.Check()
	assert.NoError(t, err)
	assert.Equal(t, "filter to_int: the value is not an integer", report.Entries[1].Error)
	assert.Contains(t, *report.Diff, "+  port: '***'\n")
	assert.NotContains(t, *report.Diff, "DIFF_INVALID")
}

// TestCheckDiffWithLogValues checks that the diff shows the values if values may be logged
func TestCheckDiffWithLogValues(t *testing.T) {
	t.Setenv("DIFF_HOST", "db.internal")
	t.Setenv("DIFF_FRAGMENT", "port: 5432")
	SetLogValues(true)
	t.Cleanup(func() { SetLogValues(false) })

	got := checkDiff(t, general.YAML, "host: ${DIFF_HOST}\ndb: ${DIFF_FRAGMENT | fragment}\n")
	assert.Equal(t, "@@ -1,2 +1,3 @@\n-host: ${DIFF_HOST}\n-db: ${DIFF_FRAGMENT | fragment}\n+host: db.internal\n+db:\n+  port: 5432\n", got)
}

// TestCheckWithoutDiff checks that the report has no diff unless it is requested
func TestCheckWithoutDiff(t *testing.T) {
	t.Setenv("DIFF_HOST", "db.internal")
	report := checkFile(t, general.YAML, "host: ${DIFF_HOST}\n")
	assert.Nil(t, report.Diff)
}

// failingWriteHandler is a YAML handler whose Write fails, which none of the real handlers does for checked files
type failingWriteHandler struct {
	*YamlConfigFileHandler
}

// Write returns an error instead of writing the document
func (failingWriteHandler) Write(io.Writer) error {
	return assert.AnError
}

// TestCheckDiffWriteError checks that a file whose processed content cannot be written is reported as an error
func TestCheckDiffWriteError(t *testing.T) {
	file := path.Join(t.TempDir(), "config.yaml")
	assert.NoError(t, os.WriteFile(file, []byte("a: 1\n"), 0o644))

	processor := NewFileProcessor(file, general.Undefined, nil)
	processor.Diff = true
	report, err := processor.checkWith(failingWriteHandler{NewYamlConfigFileHandler()})
	assert.NoError(t, err)
	assert.Equal(t, "failed to write "+file+": "+assert.AnError.Error(), report.Error)
	assert.Nil(t, report.Diff)
	assert.True(t, report.Failed())
}

// TestCheckUnsupportedFileType checks that a file of an unsupported type cannot be checked
func TestCheckUnsupportedFileType(t *testing.T) {
	_, err := NewFileProcessor("config.abc", general.Undefined, nil).Check()
	assert.EqualError(t, err, "unsupported file type: abc")
}

// TestCheckDiffMissingFile checks that a file that cannot be read is an error with the diff requested as well
func TestCheckDiffMissingFile(t *testing.T) {
	processor := NewFileProcessor(path.Join(t.TempDir(), "missing.yaml"), general.Undefined, nil)
	processor.Diff = true
	_, err := processor.Check()
	assert.ErrorIs(t, err, os.ErrNotExist)
}
