package file

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/denglertai/gonfig/internal/general"
	"github.com/stretchr/testify/assert"
)

// lineEntries reads the content with the handler and returns the paths and values of its entries
func lineEntries(t *testing.T, handler ConfigFileHandler, content string) (paths []string, values []string) {
	t.Helper()

	assert.NoError(t, handler.Read(strings.NewReader(content)))
	entries, err := handler.Process()
	assert.NoError(t, err)
	for entry := range entries {
		paths = append(paths, entry.Path())
		values = append(values, entry.GetValue())
	}
	return paths, values
}

// writeLines writes the handler's file and returns it
func writeLines(t *testing.T, handler ConfigFileHandler) string {
	t.Helper()

	var output bytes.Buffer
	assert.NoError(t, handler.Write(&output))
	return output.String()
}

// TestDotenvFileHandler reads the test data, checks the keys and values of its entries and writes it back unchanged
func TestDotenvFileHandler(t *testing.T) {
	content, err := os.ReadFile("testdata/env/app.env")
	assert.NoError(t, err)

	handler := NewDotenvConfigFileHandler()
	paths, values := lineEntries(t, handler, string(content))
	entries, _ := handler.Process()
	for entry := range entries {
		assert.Equal(t, entry.Path(), entry.Key(), "the keys of dotenv files are their paths")
	}
	assert.Equal(t, []string{"DB_HOST", "DB_PORT", "DB_USER", "DB_PASSWORD", "URL", "EMPTY", "CERT", "export"}, paths)
	assert.Equal(t, []string{"${DB_HOST}", "5432", "app", "${DB_PASSWORD}", "http://localhost/#anchor", "",
		"-----BEGIN-----\nline \"2\"\n-----END-----", "1"}, values)
	assert.Equal(t, string(content), writeLines(t, handler))
}

// TestDotenvWrite checks that changed values keep their quotes, comments and line endings where possible, and are
// quoted where they would be read differently otherwise
func TestDotenvWrite(t *testing.T) {
	for _, tc := range []struct {
		name    string
		content string
		value   string
		want    string
	}{
		{"unquoted", "export A=x # c\r\n", "y z", "export A=y z # c\r\n"},
		{"unquoted with a comment character", "A=x\n", "a #b", "A=\"a #b\"\n"},
		{"unquoted with a quote", "A=x", "it's", `A="it's"`},
		{"unquoted with leading whitespace", "A = x\n", " y", "A = \" y\"\n"},
		{"unquoted with a line break", "A=x\n", "a\nb\\", "A=\"a\\nb\\\\\"\n"},
		{"unquoted empty", "A=x\n", "", "A=\n"},
		{"single-quoted", "A='x' # c\n", `a "b" \n`, "A='a \"b\" \\n' # c\n"},
		{"single-quoted with a single quote", "A='x'\n", "it's", "A=\"it's\"\n"},
		{"double-quoted", "A=\"x\"\n", "a\"b\\c\r\nd\te", "A=\"a\\\"b\\\\c\\r\\nd\te\"\n"},
		{"multi-line value", "A=\"x\ny\" # c\nB=1\n", "z", "A=\"z\" # c\nB=1\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler := NewDotenvConfigFileHandler()
			assert.NoError(t, handler.Read(strings.NewReader(tc.content)))
			entries, _ := handler.Process()
			for entry := range entries {
				entry.SetValue(tc.value)
				break
			}
			output := writeLines(t, handler)
			assert.Equal(t, tc.want, output)

			// The value is read as it was written
			_, values := lineEntries(t, NewDotenvConfigFileHandler(), output)
			assert.Equal(t, tc.value, values[0])
		})
	}
}

// TestDotenvRead checks the parsing of keys, values and comments, including the escapes of double-quoted values
func TestDotenvRead(t *testing.T) {
	paths, values := lineEntries(t, NewDotenvConfigFileHandler(),
		"  # comment\n\texport\tA = a b  \nB=\"\\n\\r\\t\\\\\\\"\\$x\"\nC='\\n'#c\nD=a#b\nE= #c\nexport = 1\nF=\"a\r\nb\"\n")
	assert.Equal(t, []string{"A", "B", "C", "D", "E", "export", "F"}, paths)
	assert.Equal(t, []string{"a b", "\n\r\t\\\"\\$x", `\n`, "a#b", "", "1", "a\r\nb"}, values)
}

// TestDotenvReadErrors checks that lines that are no pairs and unterminated quoted values fail with their line
func TestDotenvReadErrors(t *testing.T) {
	for content, want := range map[string]string{
		"A=1\nB\n":                "line 2: expected KEY=value or a comment starting with #",
		"A B=1\n":                 "line 1: expected KEY=value or a comment starting with #",
		"=1\n":                    "line 1: expected KEY=value or a comment starting with #",
		"A=\"x\ny\"\nB='1\n":      "line 3: the quoted value of B is not terminated",
		"A=\"1\\\"":               "line 1: the quoted value of A is not terminated",
		"A=\"x\r\ny\" z\n":        "line 1: unexpected text after the quoted value of A",
		"A=\"x\ny\"\rB=\"1\" 2\n": "line 3: unexpected text after the quoted value of B",
	} {
		err := NewDotenvConfigFileHandler().Read(strings.NewReader(content))
		assert.EqualError(t, err, want, content)
	}
	assert.ErrorIs(t, NewDotenvConfigFileHandler().Read(iotest.ErrReader(assert.AnError)), assert.AnError)
}

// TestLineFileHandlerFormat checks that the BOM is kept, that the entries stop when the consumer stops and that errors
// writing the output are returned
func TestLineFileHandlerFormat(t *testing.T) {
	handler := NewDotenvConfigFileHandler()
	paths, _ := lineEntries(t, handler, "\xEF\xBB\xBFA=1\nB=2\n")
	assert.Equal(t, []string{"A", "B"}, paths)
	assert.True(t, handler.keepsFormat())
	assert.Equal(t, "\xEF\xBB\xBFA=1\nB=2\n", writeLines(t, handler))
	assert.ErrorIs(t, handler.Write(failingWriter{}), assert.AnError)

	entries, _ := handler.Process()
	count := 0
	for range entries {
		count++
		break
	}
	assert.Equal(t, 1, count)
}

// TestDotenvFileType checks that dotenv files are recognized by their name, including the files per environment
// named like .env.production, unless their extension is a known type
func TestDotenvFileType(t *testing.T) {
	for name, want := range map[string]general.FileType{
		".env":                general.ENV,
		"app.env":             general.ENV,
		"conf/APP.ENV":        general.ENV,
		".env.production":     general.ENV,
		"/srv/app/.env.local": general.ENV,
		".env.yaml":           general.YAML,
		".env.JSON":           general.JSON,
		"app.ini":             general.INI,
		"x.env.production":    "production",
	} {
		assert.Equal(t, want, NewFileProcessor(name, general.Undefined, nil).FileType, name)
	}
	assert.Equal(t, general.DOTENV, NewFileProcessor("x", "dotenv", nil).FileType)
}
