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
		assert.EqualError(t, rootCmd.Execute(), "--schema validates JSON, YAML, TOML, dotenv and INI files only, not "+xmlFile+" of type xml")

		resetFlags(t)
		rootCmd.SetArgs([]string{"config", command, "-f", yamlFile, "--schema", filepath.Join(dir, "none.json")})
		assert.ErrorContains(t, rootCmd.Execute(), "failed to load the schema: open ")
	}
}

// TestCheckSchema checks that violations are reported as errors without values, and that files failing the check
// otherwise are reported as not validated
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
	assert.Contains(t, output, `    SCHEMA_TEST_MISSING      missing
  schema: not validated, since the file has other problems
1 missing variable, 0 errors
`)
	assert.NotContains(t, output, "schema: database")

	output, err = runCheck(t, "database: [\n", "--schema", configSchemaPath(t))
	assert.EqualError(t, err, "check failed: 0 missing variables, 1 error")
	assert.Regexp(t, `cannot be checked\n  error: .+\n  schema: not validated, since the file has other problems\n0 missing variables, 1 error\n$`, output)

	output, err = runCheck(t, "database:\n  host: ${SCHEMA_TEST_HOST}\n  port: 1\n", "--schema", configSchemaPath(t))
	assert.NoError(t, err)
	assert.NotContains(t, output, "schema:")
}

// TestCheckSchemaValidatedJson checks that JSON reports tell validated files from files that were not validated, and
// that the field is left out without --schema
func TestCheckSchemaValidatedJson(t *testing.T) {
	t.Setenv("SCHEMA_TEST_PORT", "80")

	for _, tc := range []struct {
		name     string
		input    string
		args     []string
		expected *bool
	}{
		{"validated", `{"database": {"host": "db", "port": "${SCHEMA_TEST_PORT | to_int}"}}`, []string{"--schema", configSchemaPath(t)}, new(true)},
		{"not validated", `{"database": {"host": "${SCHEMA_TEST_MISSING}", "port": 1}}`, []string{"--schema", configSchemaPath(t)}, new(false)},
		{"cannot be read", `{"database": `, []string{"--schema", configSchemaPath(t)}, new(false)},
		{"without schema", `{"database": {"host": "db", "port": 1}}`, nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, _ := runWithStdin(t, tc.input, append([]string{"config", "check", "-f", "-", "-t", "json", "-o", "json"}, tc.args...)...)

			var result struct {
				Files []file.CheckReport `json:"files"`
			}
			assert.NoError(t, json.Unmarshal([]byte(out), &result), out)
			if assert.Len(t, result.Files, 1) {
				assert.Equal(t, tc.expected, result.Files[0].SchemaValidated)
			}
		})
	}
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
	removed := filepath.Join(t.TempDir(), "removed.yaml")
	report := &file.CheckReport{File: "removed.yaml"}
	validateChecked(report, file.NewFileProcessor(removed, general.Undefined, nil), []*schema.Schema{sch})
	// The message of a missing file depends on the operating system, e.g. "The system cannot find the file specified." on Windows
	_, openErr := os.Open(removed)
	assert.Equal(t, openErr.Error(), report.Error)
}

// TestCheckSchemaStdinReadError checks that an error reading stdin, which is read at once for the validation, is returned
func TestCheckSchemaStdinReadError(t *testing.T) {
	resetFlags(t)
	rootCmd.SetIn(iotest.ErrReader(assert.AnError))
	t.Cleanup(func() { rootCmd.SetIn(nil) })
	rootCmd.SetArgs([]string{"config", "check", "-f", "-", "-t", "yaml", "--schema", configSchemaPath(t)})
	assert.ErrorIs(t, rootCmd.Execute(), assert.AnError)
}

// loggingSchema is a second schema for the tests of schema mappings, which config files don't match
const loggingSchema = `{"type": "object", "required": ["level"], "properties": {"level": {"enum": ["debug", "info"]}}}`

// writeSchemaFiles writes the files into a new directory, which becomes the working directory, and returns it
func writeSchemaFiles(t *testing.T, files map[string]string) string {
	t.Helper()

	dir := t.TempDir()
	for name, content := range files {
		assert.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755))
		assert.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
	}
	t.Chdir(dir)
	return dir
}

// runSchemaCommand runs the command with the arguments and returns the output and error
func runSchemaCommand(t *testing.T, args ...string) (string, error) {
	t.Helper()

	var out bytes.Buffer
	rootCmd.SetOut(&out)
	t.Cleanup(func() { rootCmd.SetOut(nil) })
	resetFlags(t)
	rootCmd.SetArgs(args)
	err := rootCmd.Execute()
	return out.String(), err
}

// TestProcessSchemaMappings checks that each file is validated against the schemas whose patterns it matches, by base
// name or by path, and that files matching no pattern, of any type, are not validated
func TestProcessSchemaMappings(t *testing.T) {
	// The working directory changes, so the path is taken before
	configSchema := configSchemaPath(t)
	t.Setenv("SCHEMA_TEST_LEVEL", "info")
	writeSchemaFiles(t, map[string]string{
		"logging.schema.json": loggingSchema,
		"app.yaml":            "database:\n  host: db\n  port: 5432\n",
		"conf.d/logging.yaml": "level: ${SCHEMA_TEST_LEVEL}\n",
		"notes.xml":           "<notes/>",
	})
	run := func(args ...string) error {
		_, err := runSchemaCommand(t, append([]string{"config", "process", "-i", "-f", "*", "-f", "conf.d/*"}, args...)...)
		return err
	}

	assert.NoError(t, run("--schema", "app*.yaml="+configSchema, "--schema", "conf.d/*.yaml=logging.schema.json"))

	t.Setenv("SCHEMA_TEST_LEVEL", "trace")
	assert.NoError(t, os.WriteFile("conf.d/logging.yaml", []byte("level: ${SCHEMA_TEST_LEVEL}\n"), 0o644))
	err := run("--schema", "app*.yaml="+configSchema, "--schema", "conf.d/*.yaml=logging.schema.json")
	// Errors name the file with the separator of the OS
	loggingFile := filepath.FromSlash("conf.d/logging.yaml")
	assert.EqualError(t, err, loggingFile+` does not match the schema logging.schema.json:
  - level: value must be one of 'debug', 'info'`)

	// Every matching schema applies, so the config schema rejects the logging file as well
	err = run("--schema", "*.yaml="+configSchema, "--schema", "conf.d/*.yaml=logging.schema.json")
	assert.EqualError(t, err, loggingFile+" does not match the schema "+configSchema+`:
  - (root): missing property 'database'`)

	// A pattern matching no file validates nothing, while a schema for every file includes the XML file
	assert.NoError(t, run("--schema", "none*.yaml=none.schema.json"))
	assert.EqualError(t, run("--schema", configSchema),
		"--schema validates JSON, YAML, TOML, dotenv and INI files only, not notes.xml of type xml")
	assert.EqualError(t, run("--schema", "*.xml="+configSchema),
		"--schema *.xml="+configSchema+" validates JSON, YAML, TOML, dotenv and INI files only, not notes.xml of type xml")
}

// TestCheckSchemaMappings checks that check validates each file against its schemas and leaves out schemaValidated
// for files without a schema
func TestCheckSchemaMappings(t *testing.T) {
	// The working directory changes, so the path is taken before
	configSchema := configSchemaPath(t)
	writeSchemaFiles(t, map[string]string{
		"logging.schema.json": loggingSchema,
		"app.yaml":            "database:\n  host: db\n  port: 70000\n",
		"logging.yaml":        "level: info\n",
		"other.yaml":          "a: 1\n",
	})

	out, err := runSchemaCommand(t, "config", "check", "-f", "*.yaml", "-o", "json",
		"--schema", "app.yaml="+configSchema, "--schema", "logging.yaml=logging.schema.json",
		// The same schema twice validates the file once
		"--schema", "log*.yaml=logging.schema.json")
	assert.EqualError(t, err, "check failed: 0 missing variables, 1 error")

	var result struct {
		Files []file.CheckReport `json:"files"`
	}
	assert.NoError(t, json.Unmarshal([]byte(out), &result), out)
	if assert.Len(t, result.Files, 3) {
		assert.Equal(t, "app.yaml", result.Files[0].File)
		assert.Equal(t, new(true), result.Files[0].SchemaValidated)
		assert.Equal(t, []schema.Violation{{Path: "database.port", Message: "value must be <= 65535"}}, result.Files[0].SchemaViolations)
		assert.Equal(t, new(true), result.Files[1].SchemaValidated)
		assert.Empty(t, result.Files[1].SchemaViolations)
		assert.Nil(t, result.Files[2].SchemaValidated)
	}
}

// TestSchemaMappingsOfConfigFile checks that the schemas of the config file apply without --schema, with paths
// relative to the config file, and that --schema replaces them
func TestSchemaMappingsOfConfigFile(t *testing.T) {
	// The working directory changes, so the path is taken before
	configSchema := configSchemaPath(t)
	configDir := t.TempDir()
	assert.NoError(t, os.WriteFile(filepath.Join(configDir, "logging.schema.json"), []byte(loggingSchema), 0o644))
	assert.NoError(t, os.WriteFile(filepath.Join(configDir, ".gonfig.yaml"), []byte(`schemas:
  - files: logging.yaml
    schema: logging.schema.json
  - files: other.yaml
    schema: missing.schema.json
`), 0o644))
	t.Setenv("GONFIG_CONFIG_PATH", configDir)
	writeSchemaFiles(t, map[string]string{"logging.yaml": "level: trace\n", "app.yaml": "a: 1\n"})

	// The schema of other.yaml is not loaded, since no file of the run matches it
	_, err := runSchemaCommand(t, "config", "process", "-f", "logging.yaml")
	assert.EqualError(t, err, `logging.yaml does not match the schema `+filepath.Join(configDir, "logging.schema.json")+`:
  - level: value must be one of 'debug', 'info'`)

	out, err := runSchemaCommand(t, "config", "check", "-f", "logging.yaml", "-f", "app.yaml")
	assert.EqualError(t, err, "check failed: 0 missing variables, 1 error")
	assert.Contains(t, out, `  schema: level: value must be one of 'debug', 'info'`)

	out, err = runSchemaCommand(t, "config", "process", "-f", "logging.yaml", "--schema", "app.yaml="+configSchema)
	assert.NoError(t, err)
	assert.Equal(t, "level: trace\n", out)
}

// TestSchemaMappingUsageErrors checks that invalid --schema values fail, and that patterns don't match stdin
func TestSchemaMappingUsageErrors(t *testing.T) {
	writeSchemaFiles(t, map[string]string{"app.yaml": "a: 1\n"})

	for _, value := range []string{"=a.json", "*.yaml=", "="} {
		_, err := runSchemaCommand(t, "config", "process", "-f", "app.yaml", "--schema", value)
		assert.EqualError(t, err, `invalid --schema "`+value+`", use <schema> or <pattern>=<schema>`)
	}
	_, err := runSchemaCommand(t, "config", "check", "-f", "app.yaml", "--schema", "[a=a.json")
	assert.EqualError(t, err, `invalid pattern "[a" of --schema "[a=a.json": syntax error in pattern`)

	out, err := runWithStdin(t, "a: 1\n", "config", "process", "-f", "-", "-t", "yaml", "--schema", "*=none.schema.json")
	assert.NoError(t, err)
	assert.Equal(t, "a: 1\n", out)
}

// TestSchemaRuleMatches checks the matching of base names and paths, including files given with relative paths
func TestSchemaRuleMatches(t *testing.T) {
	dir := writeSchemaFiles(t, nil)

	rule, err := parseSchemaFlag("conf.d/*.yaml=s.json")
	assert.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "conf.d", "*.yaml"), rule.pattern)
	assert.True(t, rule.matches("conf.d/app.yaml"))
	assert.True(t, rule.matches("./conf.d/app.yaml"))
	assert.True(t, rule.matches(filepath.Join(dir, "conf.d", "app.yaml")))
	assert.False(t, rule.matches("app.yaml"))
	assert.False(t, rule.matches("other/conf.d/app.yaml"))

	rule, err = parseSchemaFlag("app.yaml=s.json")
	assert.NoError(t, err)
	assert.True(t, rule.matches("conf.d/app.yaml"))
	assert.True(t, rule.matches("app.yaml"))
	assert.False(t, rule.matches(stdinFile))

	rule, err = parseSchemaFlag("s.json")
	assert.NoError(t, err)
	assert.True(t, rule.matches(stdinFile))
}

// TestSchemaDotenvAndIni checks that dotenv files are validated as objects of strings and INI files as objects of
// sections, which are objects of strings
func TestSchemaDotenvAndIni(t *testing.T) {
	t.Setenv("SCHEMA_TEST_PORT", "eighty")
	writeSchemaFiles(t, map[string]string{
		"env.schema.json": `{"type": "object", "required": ["DB_HOST"], "properties": {"DB_PORT": {"type": "string", "pattern": "^[0-9]+$"}}}`,
		"ini.schema.json": `{"type": "object", "properties": {"name": {"const": "app"}, "database": {"type": "object", "required": ["host"],
			"properties": {"port": {"type": "string", "pattern": "^[0-9]+$"}}}}}`,
		".env":    "DB_PORT=${SCHEMA_TEST_PORT}\n",
		"app.ini": "name = app\n[database]\nport = \"${SCHEMA_TEST_PORT}\"\n",
	})

	out, err := runSchemaCommand(t, "config", "check", "-f", ".env", "-f", "app.ini",
		"--schema", ".env=env.schema.json", "--schema", "*.ini=ini.schema.json")
	assert.EqualError(t, err, "check failed: 0 missing variables, 4 errors")
	assert.Contains(t, out, `  schema: (root): missing property 'DB_HOST'
  schema: DB_PORT: value does not match pattern '^[0-9]+$'
`)
	assert.Contains(t, out, `  schema: database: missing property 'host'
  schema: database.port: value does not match pattern '^[0-9]+$'
`)
}
