package schema

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/denglertai/gonfig/internal/filter"
	"github.com/denglertai/gonfig/internal/general"
	"github.com/stretchr/testify/assert"
)

// writeSchema writes the schema into a temporary directory and loads it
func writeSchema(t *testing.T, content string) *Schema {
	t.Helper()

	path := filepath.Join(t.TempDir(), "schema.json")
	assert.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	loaded, err := Load(path)
	assert.NoError(t, err)
	return loaded
}

// allowValuesInErrors allows values in errors for the rest of the test
func allowValuesInErrors(t *testing.T) {
	t.Helper()

	filter.SetValuesInErrors(true)
	t.Cleanup(func() { filter.SetValuesInErrors(false) })
}

// configSchema requires a database with a host and a port and allows nothing else
const configSchema = `{
  "type": "object",
  "required": ["database"],
  "properties": {
    "database": {
      "type": "object",
      "required": ["host", "port"],
      "properties": {
        "host": {"type": "string"},
        "port": {"type": "integer", "maximum": 65535}
      },
      "additionalProperties": false
    }
  }
}`

// TestValidateFileTypes validates a valid and an invalid document of each supported file type
func TestValidateFileTypes(t *testing.T) {
	s := writeSchema(t, configSchema)

	testCases := []struct {
		fileType general.FileType
		valid    string
		invalid  string
	}{
		{general.JSON, `{"database": {"host": "db", "port": 5432}}`, `{"database": {"host": "db", "port": "5432", "user": "app"}}`},
		{general.YAML, "database:\n  host: db\n  port: 5432\n", "database:\n  host: db\n  port: '5432'\n  user: app\n"},
		{general.YML, "database: {host: db, port: 5432}\n", "database: {host: db, port: '5432', user: app}\n"},
		{general.TOML, "[database]\nhost = \"db\"\nport = 5432\n", "[database]\nhost = \"db\"\nport = \"5432\"\nuser = \"app\"\n"},
	}
	for _, tC := range testCases {
		t.Run(string(tC.fileType), func(t *testing.T) {
			violations, err := s.Validate(tC.fileType, []byte(tC.valid))
			assert.NoError(t, err)
			assert.Empty(t, violations)

			violations, err = s.Validate(tC.fileType, []byte(tC.invalid))
			assert.NoError(t, err)
			assert.Equal(t, []Violation{
				{Path: "database", Message: "additional properties 'user' not allowed"},
				{Path: "database.port", Message: "got string, want integer"},
			}, violations)
		})
	}
}

// TestValidateYamlDocuments checks that every document of a YAML file is validated and named in the paths,
// and that an empty file is validated as null
func TestValidateYamlDocuments(t *testing.T) {
	s := writeSchema(t, configSchema)

	violations, err := s.Validate(general.YAML, []byte("database: {host: db, port: 1}\n---\ndatabase: {host: db}\n---\nother: 1\n"))
	assert.NoError(t, err)
	assert.Equal(t, []string{
		"document 2: database: missing property 'port'",
		"document 3: (root): missing property 'database'",
	}, violationStrings(violations))

	violations, err = s.Validate(general.YAML, nil)
	assert.NoError(t, err)
	assert.Equal(t, []string{"(root): got null, want object"}, violationStrings(violations))
}

// violationStrings returns the violations as they are printed
func violationStrings(violations []Violation) []string {
	var lines []string
	for _, violation := range violations {
		lines = append(lines, violation.String())
	}
	return lines
}

// TestValidateNormalizes checks that YAML mappings with other keys than strings and TOML dates and times are validated
// like their text, and that the BOM of a processed file is ignored. Local times have no offset, which the format time
// requires, so their text is compared.
func TestValidateNormalizes(t *testing.T) {
	s := writeSchema(t, `{
  "properties": {
    "ports": {"type": "object", "required": ["8080"]},
    "dates": {"type": "array", "items": {"type": "string"}, "prefixItems": [
      {"format": "date-time"}, {"format": "date-time"}, {"format": "date"}, {"const": "07:32:00"}
    ]}
  }
}`)

	violations, err := s.Validate(general.YAML, []byte("\xEF\xBB\xBFports:\n  8080: http\n"))
	assert.NoError(t, err)
	assert.Empty(t, violations)

	toml := "\xEF\xBB\xBFdates = [1979-05-27T07:32:00Z, 1979-05-27T07:32:00.5-07:00, 1979-05-27, 07:32:00]\n"
	violations, err = s.Validate(general.TOML, []byte(toml))
	assert.NoError(t, err)
	assert.Empty(t, violations)

	// A local date-time has no offset, which the format date-time requires
	violations, err = s.Validate(general.TOML, []byte("dates = [1979-05-27T07:32:00]\n"))
	assert.NoError(t, err)
	assert.Equal(t, []string{"dates.0: value is not valid date-time"}, violationStrings(violations))
}

// TestValidateErrors checks the errors for unsupported file types and content that cannot be decoded
func TestValidateErrors(t *testing.T) {
	s := writeSchema(t, `{}`)

	for fileType, content := range map[general.FileType]string{
		general.XML:  "<a/>",
		general.JSON: "{",
		general.YAML: "a: [",
		general.TOML: "a =",
	} {
		_, err := s.Validate(fileType, []byte(content))
		assert.Error(t, err, fileType)
	}

	_, err := s.Validate(general.PROPERTIES, nil)
	assert.EqualError(t, err, "files of type properties cannot be validated, only JSON, YAML and TOML files")
}

// TestDescribe checks that values are only part of the messages if values are allowed in errors
func TestDescribe(t *testing.T) {
	s := writeSchema(t, `{
  "properties": {
    "pattern": {"pattern": "^[a-z]+$"},
    "format": {"format": "email"},
    "minimum": {"minimum": 10},
    "maximum": {"maximum": 1.5},
    "exclusiveMinimum": {"exclusiveMinimum": 10},
    "exclusiveMaximum": {"exclusiveMaximum": 0},
    "multipleOf": {"multipleOf": 0.5},
    "enum": {"enum": ["a", "b"]}
  }
}`)
	document := []byte(`{"pattern": "S3cret", "format": "secret", "minimum": 5, "maximum": 100000, "exclusiveMinimum": 10,` +
		` "exclusiveMaximum": 0.25, "multipleOf": 0.3, "enum": "c"}`)

	violations, err := s.Validate(general.JSON, document)
	assert.NoError(t, err)
	assert.Equal(t, []string{
		"enum: value must be one of 'a', 'b'",
		"exclusiveMaximum: value must be < 0",
		"exclusiveMinimum: value must be > 10",
		"format: value is not valid email",
		"maximum: value must be <= 1.5",
		"minimum: value must be >= 10",
		"multipleOf: value must be a multiple of 0.5",
		"pattern: value does not match pattern '^[a-z]+$'",
	}, violationStrings(violations))

	allowValuesInErrors(t)
	violations, err = s.Validate(general.JSON, document)
	assert.NoError(t, err)
	assert.Equal(t, []string{
		"enum: value must be one of 'a', 'b'",
		"exclusiveMaximum: value must be < 0, got 0.25",
		"exclusiveMinimum: value must be > 10, got 10",
		"format: 'secret' is not valid email: missing @",
		"maximum: value must be <= 1.5, got 100000",
		"minimum: value must be >= 10, got 5",
		"multipleOf: value must be a multiple of 0.5, got 0.3",
		"pattern: 'S3cret' does not match pattern '^[a-z]+$'",
	}, violationStrings(violations))
}

// TestCheck checks the error of Check for a file that does not match the schema and for content that cannot be decoded
func TestCheck(t *testing.T) {
	s := writeSchema(t, configSchema)

	assert.NoError(t, s.Check("app.json", general.JSON, []byte(`{"database": {"host": "db", "port": 1}}`)))

	err := s.Check("app.json", general.JSON, []byte(`{"database": {"port": 70000}}`))
	var schemaError *Error
	if assert.ErrorAs(t, err, &schemaError) {
		assert.Len(t, schemaError.Violations, 2)
	}
	assert.Regexp(t, `^app\.json does not match the schema \S+schema\.json:
  - database: missing property 'host'
  - database\.port: value must be <= 65535$`, err.Error())

	assert.ErrorContains(t, s.Check("app.json", general.JSON, []byte("{")), "failed to validate app.json against the schema ")
}

// TestLoadErrors checks the errors of schemas that cannot be loaded, including remote references, which are never fetched
func TestLoadErrors(t *testing.T) {
	dir := t.TempDir()
	for name, content := range map[string]string{
		"invalid.json": "{",
		"type.json":    `{"type": 5}`,
		"remote.json":  `{"$ref": "https://example.com/schema.json"}`,
		"missing.json": `{"$ref": "missing-ref.json"}`,
	} {
		assert.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
	}

	_, err := Load(filepath.Join(dir, "invalid.json"))
	assert.ErrorContains(t, err, "invalid.json: invalid JSON: ")
	_, err = Load(filepath.Join(dir, "type.json"))
	assert.ErrorContains(t, err, "is not valid against metaschema")
	_, err = Load(filepath.Join(dir, "remote.json"))
	assert.ErrorContains(t, err, `failing loading "https://example.com/schema.json"`)
	_, err = Load(filepath.Join(dir, "missing.json"))
	assert.ErrorContains(t, err, "missing-ref.json")
	_, err = Load(filepath.Join(dir, "none.json"))
	assert.ErrorContains(t, err, "failed to load the schema: open ")
}
