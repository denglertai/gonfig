package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

// runCheck runs config check with the arguments on a file with the content and returns the output and error
func runCheck(t *testing.T, content string, args ...string) (string, error) {
	t.Helper()

	fileName := path.Join(t.TempDir(), "config.yaml")
	assert.NoError(t, os.WriteFile(fileName, []byte(content), 0o644))

	var out bytes.Buffer
	rootCmd.SetOut(&out)
	t.Cleanup(func() { rootCmd.SetOut(nil) })

	resetFlags(t)
	rootCmd.SetArgs(append([]string{"config", "check", "-f", fileName}, args...))
	err := rootCmd.Execute()

	written, readErr := os.ReadFile(fileName)
	assert.NoError(t, readErr)
	assert.Equal(t, content, string(written), "check must not change the file")

	return out.String(), err
}

// TestCheckCommand checks the text report and the exit error of config check
func TestCheckCommand(t *testing.T) {
	t.Setenv("CHECK_CMD_USER", "app")
	t.Setenv("CHECK_CMD_PORT", "eighty")
	t.Setenv("CHECK_CMD_EMPTY", "")

	output, err := runCheck(t, `user: ${CHECK_CMD_USER}
password: ${CHECK_CMD_PASWORD}
port: ${CHECK_CMD_PORT | to_int}
host: ${CHECK_CMD_HOST | default(localhost)}
empty: ${CHECK_CMD_EMPTY}
static: value
`)

	assert.EqualError(t, err, "check failed: 1 missing variable, 1 error")
	assert.Regexp(t, `^\S+config\.yaml: 5 entries with placeholders\n`, output)
	assert.Contains(t, output, `  user
    CHECK_CMD_USER           set
  password
    CHECK_CMD_PASWORD        missing
  port
    CHECK_CMD_PORT           set
    error: filter to_int: the value is not an integer
  host
    CHECK_CMD_HOST           missing, default used
  empty
    CHECK_CMD_EMPTY          empty
1 missing variable, 1 error
`)
	assert.NotContains(t, output, "eighty", "values must never be printed")
}

// TestCheckCommandMissingFileReference checks that a variable referring to a missing file, e.g. a secret that is not mounted,
// is reported as an error instead of being written as the path
func TestCheckCommandMissingFileReference(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "db_password")
	t.Setenv("CHECK_CMD_PASSWORD", "@"+missing)

	output, err := runCheck(t, "password: ${CHECK_CMD_PASSWORD | trim}\n")

	assert.EqualError(t, err, "check failed: 0 missing variables, 1 error")
	assert.Contains(t, output, "    CHECK_CMD_PASSWORD       set\n    error: failed to read the file referenced with @: ")
	assert.Contains(t, output, missing)
}

// TestCheckCommandSucceeds checks that a file without problems passes the check
func TestCheckCommandSucceeds(t *testing.T) {
	t.Setenv("CHECK_CMD_USER", "app")

	output, err := runCheck(t, "user: ${CHECK_CMD_USER}\n")

	assert.NoError(t, err)
	assert.Contains(t, output, "1 entry with placeholders\n")
	assert.Contains(t, output, "0 missing variables, 0 errors\n")
}

// TestCheckCommandJson checks the JSON report including the counts of the problems
func TestCheckCommandJson(t *testing.T) {
	t.Setenv("CHECK_CMD_PORT", "eighty")

	output, err := runCheck(t, "password: ${CHECK_CMD_PASWORD}\nport: ${CHECK_CMD_PORT | to_int}\n", "-o", "json")
	assert.Error(t, err)

	var report struct {
		Entries []struct {
			Path      string `json:"path"`
			Variables []struct {
				Name   string `json:"name"`
				Status string `json:"status"`
			} `json:"variables"`
			Error string `json:"error"`
		} `json:"entries"`
		MissingVariables int  `json:"missingVariables"`
		Errors           int  `json:"errors"`
		Failed           bool `json:"failed"`
	}
	assert.NoError(t, json.Unmarshal([]byte(output), &report))
	assert.Equal(t, 1, report.MissingVariables)
	assert.Equal(t, 1, report.Errors)
	assert.True(t, report.Failed)
	assert.Equal(t, "password", report.Entries[0].Path)
	assert.Equal(t, "missing", report.Entries[0].Variables[0].Status)
	assert.Equal(t, "filter to_int: the value is not an integer", report.Entries[1].Error)
	assert.NotContains(t, output, "eighty", "values must never be printed")
}

// TestCheckCommandInvalidOutput checks that an unsupported output format is an error
func TestCheckCommandInvalidOutput(t *testing.T) {
	_, err := runCheck(t, "a: b\n", "-o", "xml")
	assert.EqualError(t, err, `unsupported output "xml", use text or json`)
}

// TestCheckCommandUnreadableFile checks that a file that cannot be parsed is an error without a report
func TestCheckCommandUnreadableFile(t *testing.T) {
	output, err := runCheck(t, "a: [1\n")
	assert.ErrorContains(t, err, "failed to read")
	assert.Empty(t, output)
}

// failingCheckWriter fails on every write
type failingCheckWriter struct{}

// Write returns an error without writing anything
func (failingCheckWriter) Write(_ []byte) (int, error) {
	return 0, assert.AnError
}

// TestCheckCommandWriteError checks that an error writing the report is returned for both output formats
func TestCheckCommandWriteError(t *testing.T) {
	fileName := path.Join(t.TempDir(), "config.yaml")
	assert.NoError(t, os.WriteFile(fileName, []byte("a: ${CHECK_CMD_VALUE | default(x)}\n"), 0o644))

	rootCmd.SetOut(failingCheckWriter{})
	t.Cleanup(func() { rootCmd.SetOut(nil) })

	for _, output := range []string{"text", "json"} {
		resetFlags(t)
		rootCmd.SetArgs([]string{"config", "check", "-f", fileName, "-o", output})
		assert.ErrorIs(t, rootCmd.Execute(), assert.AnError, output)
	}
}

// TestCheckCommandTemplates checks that the templates used by an entry are listed
func TestCheckCommandTemplates(t *testing.T) {
	configDir := t.TempDir()
	assert.NoError(t, os.WriteFile(path.Join(configDir, ".gonfig.yaml"), []byte("templates:\n  placeholders:\n    host:\n      env: CHECK_CMD_HOST\n      default: localhost\n"), 0o644))
	t.Setenv("GONFIG_CONFIG_PATH", configDir)

	output, err := runCheck(t, "host: ${@host}\n")

	assert.NoError(t, err)
	assert.Contains(t, output, `  host
    CHECK_CMD_HOST           missing, default used
    templates: @host
`)
}
