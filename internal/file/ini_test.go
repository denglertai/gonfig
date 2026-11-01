package file

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/denglertai/gonfig/internal/general"
	"github.com/denglertai/gonfig/internal/schema"
	"github.com/stretchr/testify/assert"
)

// TestIniFileHandler reads the test data, checks the paths and values of its entries and writes it back unchanged
func TestIniFileHandler(t *testing.T) {
	content, err := os.ReadFile("testdata/ini/app.ini")
	assert.NoError(t, err)

	handler := NewIniConfigFileHandler()
	paths, values := lineEntries(t, handler, string(content))
	assert.Equal(t, []string{"name", "database.host", "database.port", "database.password", `remote "origin".url`}, paths)
	assert.Equal(t, []string{"app", "${DB_HOST}", "5432", "${DB_PASSWORD}",
		"https://example.com/repo.git ; inline comments are part of the value"}, values)
	assert.Equal(t, string(content), writeLines(t, handler))
}

// TestIniRead checks the parsing of sections, separators, quoted values and lines without a value
func TestIniRead(t *testing.T) {
	paths, values := lineEntries(t, NewIniConfigFileHandler(),
		"a=1\r\n[ s ] ; comment\r\nb : x = y\r\nc = \"q\\\"\\\\\\n\\t\\x\"  \r\n=d\r\ne =\r\nflag\rf = \"\"\n[t]\ng=\"")
	assert.Equal(t, []string{"a", "s.b", "s.c", "s.e", "s.f", "t.g"}, paths)
	assert.Equal(t, []string{"1", "x = y", "q\"\\\n\t\\x", "", "", `"`}, values)
}

// TestIniWrite checks that changed values keep their quotes, comments and line endings, and that line breaks fail
// in unquoted values, naming the entry
func TestIniWrite(t *testing.T) {
	handler := NewIniConfigFileHandler()
	assert.NoError(t, handler.Read(strings.NewReader("[s]\r\na = x ; c  \r\nb = \"x\" ; c\r\n")))
	entries, _ := handler.Process()
	var all []ConfigEntry
	for entry := range entries {
		all = append(all, entry)
	}

	all[0].SetValue("y")
	all[1].SetValue("a\"b\\c\r\nd\te")
	output := writeLines(t, handler)
	assert.Equal(t, "[s]\r\na = y  \r\nb = \"a\\\"b\\\\c\\r\\nd\te\" ; c\r\n", output)
	_, values := lineEntries(t, NewIniConfigFileHandler(), output)
	assert.Equal(t, "a\"b\\c\r\nd\te", values[1])

	all[0].SetValue("a\nb")
	assert.EqualError(t, handler.Write(&strings.Builder{}), `the value of s.a contains a line break, which an unquoted `+
		`INI value cannot hold; quote the value in the file, e.g. key = "${VAR}", to write it as \n`)
}

// TestIniReadErrors checks that a section header without a closing bracket fails with its line
func TestIniReadErrors(t *testing.T) {
	err := NewIniConfigFileHandler().Read(strings.NewReader("a = 1\n[section\n"))
	assert.EqualError(t, err, "line 2: the section header has no closing ]")
}

// TestLineSchemaDecoders checks the documents dotenv and INI files are validated as: objects of strings, with the
// sections of INI files as objects, where the last value of a key wins, and that invalid files fail
func TestLineSchemaDecoders(t *testing.T) {
	schemaFile := filepath.Join(t.TempDir(), "schema.json")
	for _, tc := range []struct {
		fileType general.FileType
		content  string
		document string
	}{
		{general.ENV, "A=1\nB='x'\nA=2\n", `{"A": "2", "B": "x"}`},
		{general.DOTENV, "# only a comment\n", `{}`},
		{general.INI, "a = 1\n[s]\nb = 2\nflag\n[t]\nc = \"3\"\n[s]\nb = 4\n", `{"a": "1", "s": {"b": "4"}, "t": {"c": "3"}}`},
		{general.INI, "s = 1\n[s]\nb = 2\n", `{"s": {"b": "2"}}`},
	} {
		// The schema only matches the expected document
		assert.NoError(t, os.WriteFile(schemaFile, []byte(`{"const": `+tc.document+`}`), 0o644))
		sch, err := schema.Load(schemaFile)
		assert.NoError(t, err)
		violations, err := sch.Validate(tc.fileType, []byte(tc.content))
		assert.NoError(t, err, tc.content)
		assert.Empty(t, violations, tc.content)
	}

	sch, err := schema.Load(schemaFile)
	assert.NoError(t, err)
	_, err = sch.Validate(general.ENV, []byte("A\n"))
	assert.EqualError(t, err, "line 1: expected KEY=value or a comment starting with #")
	_, err = sch.Validate(general.INI, []byte("[s\n"))
	assert.EqualError(t, err, "line 1: the section header has no closing ]")
}
