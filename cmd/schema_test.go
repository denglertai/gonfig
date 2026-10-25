package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"testing"
	"testing/iotest"

	"github.com/denglertai/gonfig/internal/file"
	"github.com/denglertai/gonfig/internal/general"
	"github.com/denglertai/gonfig/internal/schema"
	"github.com/stretchr/testify/assert"
)

// configSchemaPath returns the path of the schema of the test data, which refers to a second schema file
func configSchemaPath(t *testing.T) string {
	t.Helper()

	wd, err := os.Getwd()
	assert.NoError(t, err)
	return path.Join(wd, "testdata/schemas/config-schema.json")
}

// TestProcessSchema checks that processed files are validated before anything is written,
// and that files matching the schema are written
func TestProcessSchema(t *testing.T) {
	t.Setenv("SCHEMA_TEST_HOST", "db")
	t.Setenv("SCHEMA_TEST_PORT", "5432")
	dir := t.TempDir()
	files := map[string]string{
		"a.yaml": "database:\n  host: ${SCHEMA_TEST_HOST}\n  port: ${SCHEMA_TEST_PORT | to_int}\n",
		"b.toml": "[database]\nhost = \"${SCHEMA_TEST_HOST}\"\nport = \"${SCHEMA_TEST_PORT}\"\n",
	}
	for name, content := range files {
		assert.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
	}

	resetFlags(t)
	rootCmd.SetArgs([]string{"config", "process", "-i", "-f", filepath.Join(dir, "*"), "--schema", configSchemaPath(t)})
	err := rootCmd.Execute()
	assert.ErrorContains(t, err, "b.toml does not match the schema ")
	assert.ErrorContains(t, err, "\n  - database.port: got string, want integer")
	for name, content := range files {
		written, readErr := os.ReadFile(filepath.Join(dir, name))
		assert.NoError(t, readErr)
		assert.Equal(t, content, string(written), "no file may be written if a file does not match")
	}

	assert.NoError(t, os.WriteFile(filepath.Join(dir, "b.toml"), []byte("[database]\nhost = \"${SCHEMA_TEST_HOST}\"\nport = \"${SCHEMA_TEST_PORT | to_int}\"\n"), 0o644))
	resetFlags(t)
	rootCmd.SetArgs([]string{"config", "process", "-i", "-f", filepath.Join(dir, "*"), "--schema", configSchemaPath(t)})
	assert.NoError(t, rootCmd.Execute())
	written, err := os.ReadFile(filepath.Join(dir, "b.toml"))
	assert.NoError(t, err)
	assert.Equal(t, "[database]\nhost = \"db\"\nport = 5432\n", string(written))
}

// TestSchemaUsageErrors checks that unsupported file types and schemas that cannot be loaded fail before processing
func TestSchemaUsageErrors(t *testing.T) {
	dir := t.TempDir()
	xmlFile := filepath.Join(dir, "a.xml")
	assert.NoError(t, os.WriteFile(xmlFile, []byte("<a/>"), 0o644))
	yamlFile := filepath.Join(dir, "a.yaml")
	assert.NoError(t, os.WriteFile(yamlFile, []byte("a: 1\n"), 0o644))

	for _, command := range []string{"process", "check"} {
		resetFlags(t)
		rootCmd.SetArgs([]string{"config", command, "-f", xmlFile, "--schema", configSchemaPath(t)})
		assert.EqualError(t, rootCmd.Execute(), "--schema validates JSON, YAML and TOML files only, not "+xmlFile+" of type xml")

		resetFlags(t)
		rootCmd.SetArgs([]string{"config", command, "-f", yamlFile, "--schema", filepath.Join(dir, "none.json")})
		assert.ErrorContains(t, rootCmd.Execute(), "failed to load the schema: open ")
	}
}

// TestCheckSchema checks that violations are reported as errors without values, and that files failing the check
// otherwise are not validated
func TestCheckSchema(t *testing.T) {
	t.Setenv("SCHEMA_TEST_HOST", "db")
	t.Setenv("SCHEMA_TEST_PORT", "70000")
	t.Setenv("SCHEMA_TEST_PASSWORD", "s3cret")

	output, err := runCheck(t, "database:\n  host: ${SCHEMA_TEST_HOST}\n  port: ${SCHEMA_TEST_PORT | to_int}\n  password: ${SCHEMA_TEST_PASSWORD}\n",
		"--schema", configSchemaPath(t))
	assert.EqualError(t, err, "check failed: 0 missing variables, 2 errors")
	assert.Contains(t, output, `  schema: database.password: minLength: got 6, want 8
  schema: database.port: value must be <= 65535
0 missing variables, 2 errors
`)
	assert.NotContains(t, output, "70000", "values must never be printed")
	assert.NotContains(t, output, "s3cret", "values must never be printed")

	output, err = runCheck(t, "database:\n  host: ${SCHEMA_TEST_MISSING}\n  port: 1\n", "--schema", configSchemaPath(t))
	assert.EqualError(t, err, "check failed: 1 missing variable, 0 errors")
	assert.NotContains(t, output, "schema:")
}

// TestCheckSchemaStdin checks that stdin is validated in JSON reports, although it is processed twice
func TestCheckSchemaStdin(t *testing.T) {
	t.Setenv("SCHEMA_TEST_PORT", "80")

	out, err := runWithStdin(t, `{"database": {"port": "${SCHEMA_TEST_PORT}"}}`,
		"config", "check", "-f", "-", "-t", "json", "-o", "json", "--schema", configSchemaPath(t))
	assert.EqualError(t, err, "check failed: 0 missing variables, 2 errors")

	var result struct {
		Files []file.CheckReport `json:"files"`
	}
	assert.NoError(t, json.Unmarshal([]byte(out), &result), out)
	if assert.Len(t, result.Files, 1) {
		assert.Equal(t, `[{"path":"database","message":"missing property 'host'"},{"path":"database.port","message":"got string, want integer"}]`,
			mustJson(t, result.Files[0].SchemaViolations))
	}
}

// mustJson returns the value encoded as JSON
func mustJson(t *testing.T, value any) string {
	t.Helper()

	var encoded bytes.Buffer
	assert.NoError(t, json.NewEncoder(&encoded).Encode(value))
	return string(bytes.TrimSpace(encoded.Bytes()))
}

// TestCheckSchemaErrors checks that files that cannot be validated are reported as errors: YAML files with duplicate keys,
// which processing keeps but cannot be decoded for the validation, and files that cannot be processed again
func TestCheckSchemaErrors(t *testing.T) {
	output, err := runCheck(t, "database: {host: a, port: 1}\ndatabase: {host: b, port: 2}\n", "--schema", configSchemaPath(t))
	assert.EqualError(t, err, "check failed: 0 missing variables, 1 error")
	assert.Contains(t, output, "error: failed to validate ")
	assert.Contains(t, output, `mapping key "database" already defined`)

	sch, err := schema.Load(configSchemaPath(t))
	assert.NoError(t, err)
	report := &file.CheckReport{File: "removed.yaml"}
	validateChecked(report, file.NewFileProcessor(filepath.Join(t.TempDir(), "removed.yaml"), general.Undefined, nil), sch)
	assert.Contains(t, report.Error, "no such file or directory")
}

// TestCheckSchemaStdinReadError checks that an error reading stdin, which is read at once for the validation, is returned
func TestCheckSchemaStdinReadError(t *testing.T) {
	resetFlags(t)
	rootCmd.SetIn(iotest.ErrReader(assert.AnError))
	t.Cleanup(func() { rootCmd.SetIn(nil) })
	rootCmd.SetArgs([]string{"config", "check", "-f", "-", "-t", "yaml", "--schema", configSchemaPath(t)})
	assert.ErrorIs(t, rootCmd.Execute(), assert.AnError)
}
