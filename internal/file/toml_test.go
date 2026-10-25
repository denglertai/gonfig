package file

import (
	"bytes"
	"math"
	"os"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/denglertai/gonfig/internal/filter"
	"github.com/denglertai/gonfig/internal/general"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/pelletier/go-toml/v2"
	"github.com/stretchr/testify/assert"
)

// readToml reads the content with a TomlFileHandler and returns the handler and its entries
func readToml(t *testing.T, content string) (*TomlFileHandler, []ConfigEntry) {
	t.Helper()

	handler := NewTomlConfigFileHandler()
	assert.NoError(t, handler.Read(strings.NewReader(content)))
	entries, err := handler.Process()
	assert.NoError(t, err)

	var collected []ConfigEntry
	for entry := range entries {
		collected = append(collected, entry)
	}
	return handler, collected
}

// writeToml writes the document of the handler and returns it
func writeToml(t *testing.T, handler *TomlFileHandler) string {
	t.Helper()

	var output bytes.Buffer
	assert.NoError(t, handler.Write(&output))
	return output.String()
}

// TestTomlFileHandler reads the test data, checks the keys and paths of its entries and writes it back unchanged
func TestTomlFileHandler(t *testing.T) {
	content, err := os.ReadFile("testdata/toml/config.toml")
	assert.NoError(t, err)

	handler, entries := readToml(t, string(content))

	var paths []string
	for _, entry := range entries {
		paths = append(paths, entry.Key()+" @ "+entry.Path())
	}
	assert.Equal(t, []string{
		"title @ title",
		"host @ database.host",
		"port @ database.port",
		"0 @ database.ports.0",
		"0 @ database.ports.2.0",
		"user @ database.inline.user",
		"${KEY} @ database.inline.${KEY}",
		"${KEY} @ database.inline.${KEY}",
		"cert @ database.cert",
		"pattern @ database.pattern",
		"name @ servers.0.name",
		"mode @ servers.0.tls.mode",
		"name @ servers.1.name",
		"${KEY} @ servers.1.a.${KEY}",
		"c @ servers.1.a.${KEY}.c",
		"${ENVIRONMENT} @ ${ENVIRONMENT}",
		"size @ ${ENVIRONMENT}.cache.size",
	}, paths)

	assert.Equal(t, string(content), writeToml(t, handler))
}

// TestTomlFileHandlerEdit processes the test data and compares the output with the snapshot
func TestTomlFileHandlerEdit(t *testing.T) {
	env := map[string]string{
		"TITLE": `My "app"`, "DB_HOST": "db.local", "DB_PORT": "5432", "DB_USER": "it's me", "KEY": "renamed",
		"CERT": "-----BEGIN-----\nabc\n-----END-----", "NAME": "primary", "ENVIRONMENT": "prod",
	}
	for name, value := range env {
		t.Setenv(name, value)
	}

	var output bytes.Buffer
	assert.NoError(t, NewFileProcessor("testdata/toml/config.toml", general.Undefined, &output).Process())

	var document map[string]any
	assert.NoError(t, toml.Unmarshal(output.Bytes(), &document))
	snaps.MatchSnapshot(t, output.String())
}

// TestTomlKeepsFormat checks that the BOM and CRLF line endings are kept, including in rewritten multi-line strings
func TestTomlKeepsFormat(t *testing.T) {
	handler, entries := readToml(t, "\xEF\xBB\xBFa = \"${A}\"\r\nb = \"\"\"\r\n${B}\"\"\"\r\n")
	entries[0].SetValue("x")
	entries[1].SetValue("\ny")

	assert.Equal(t, "\xEF\xBB\xBFa = \"x\"\r\nb = \"\"\"\r\n\ny\"\"\"\r\n", writeToml(t, handler))
}

// TestTomlEncode checks how changed strings are written depending on the quotes they were written with
func TestTomlEncode(t *testing.T) {
	testCases := []struct {
		raw   string
		value string
		want  string
	}{
		{raw: `"a"`, value: "x\"y\\z\n\t\r\x01\x7f", want: `"x\"y\\z\n\t\r\u0001\u007F"`},
		{raw: `"a"`, value: "\xff", want: "\"\uFFFD\""},
		{raw: `"a"`, value: "\b\f", want: `"\b\f"`},
		{raw: `'a'`, value: `C:\path`, want: `'C:\path'`},
		{raw: `'a'`, value: "it's", want: `"it's"`},
		{raw: `'a'`, value: "a\nb", want: `"a\nb"`},
		{raw: `'a'`, value: "\x00", want: `"\u0000"`},
		{raw: `'''a'''`, value: "a\n'b'c", want: "'''a\n'b'c'''"},
		{raw: "'''\na'''", value: "b", want: "'''\nb'''"},
		{raw: `'''a'''`, value: "\nb", want: "'''\n\nb'''"},
		{raw: `'''a'''`, value: "a'''b", want: `"""a'''b"""`},
		{raw: `'''a'''`, value: "b'", want: `"""b'"""`},
		{raw: `'''a'''`, value: "a\rb", want: `"""a\rb"""`},
		{raw: `"""a"""`, value: "a\n\tb\\", want: "\"\"\"a\n\tb\\\\\"\"\""},
		{raw: `"""a"""`, value: `a""""b"`, want: `"""a""\""b\""""`},
		{raw: "\"\"\"\r\na\"\"\"", value: "b", want: "\"\"\"\r\nb\"\"\""},
		{raw: `"""a"""`, value: "\nb", want: "\"\"\"\n\nb\"\"\""},
	}
	for _, tC := range testCases {
		entry := &TomlConfigEntry{raw: tC.raw, val: tC.value}
		assert.Equal(t, tC.want, entry.encode(), "%q as %s", tC.value, tC.raw)
	}
}

// TestTomlSetTypedValue checks which typed values replace a string
func TestTomlSetTypedValue(t *testing.T) {
	handler, entries := readToml(t, "a = \"${A}\"\nb = \"${B}\"\nc = \"${C}\"\nd = \"${D}\"\ne = \"${E}\"\n\"${K}\" = 1\n")

	for i, value := range []any{8080, 2.0, true, uint64(math.MaxUint64), math.Inf(1)} {
		typed, ok := entries[i].(TypedConfigEntry)
		assert.True(t, ok)
		assert.Equal(t, i < 3, typed.SetTypedValue(value), "%v", value)
	}
	assert.False(t, entries[5].(TypedConfigEntry).SetTypedValue(1))

	// A string set later replaces a typed value
	entries[2].(TypedConfigEntry).SetTypedValue(false)
	entries[2].SetValue("text")

	assert.Equal(t, "a = 8080\nb = 2.0\nc = \"text\"\nd = \"${D}\"\ne = \"${E}\"\n\"${K}\" = 1\n", writeToml(t, handler))
}

// TestTomlRenamedKeyCollision checks that renaming a key to an existing key fails,
// and that the key is only part of the error if values are allowed in errors
func TestTomlRenamedKeyCollision(t *testing.T) {
	handler, entries := readToml(t, "a = 1\n\"${K}\" = 2\n")
	entries[0].SetValue("a")

	assert.EqualError(t, handler.Write(&bytes.Buffer{}),
		"the renamed keys make the TOML document invalid on line 2, e.g. because a key is defined twice")

	filter.SetValuesInErrors(true)
	t.Cleanup(func() { filter.SetValuesInErrors(false) })
	assert.EqualError(t, handler.Write(&bytes.Buffer{}),
		"the renamed keys make the TOML document invalid: toml: key a is already defined")
}

// TestTomlReadErrors checks that invalid documents are rejected, including those the parser providing the positions accepts
func TestTomlReadErrors(t *testing.T) {
	for content, wantErr := range map[string]string{
		"a = \n":             "line 1: toml: unexpected character U+000A at start of value",
		"a = 1\na = 2\n":     "line 2: toml: key a is already defined",
		"[t]\n[t]\n":         "line 2: toml: table t already exists",
		"a = \"\\q\"\n":      "line 1: toml: invalid escape character U+0071 'q'",
		"a = \"unterminated": "line 1: toml: unterminated basic string",
	} {
		err := NewTomlConfigFileHandler().Read(strings.NewReader(content))
		assert.EqualError(t, err, wantErr, content)
	}
	assert.ErrorIs(t, NewTomlConfigFileHandler().Read(iotest.ErrReader(assert.AnError)), assert.AnError)
}

// TestTomlEntriesStop checks that the entries stop when the consumer stops
func TestTomlEntriesStop(t *testing.T) {
	handler, _ := readToml(t, "a = \"1\"\nb = \"2\"\n")
	entries, err := handler.Process()
	assert.NoError(t, err)

	count := 0
	for range entries {
		count++
		break
	}
	assert.Equal(t, 1, count)
}

// TestTomlWriteError checks that errors writing the output are returned
func TestTomlWriteError(t *testing.T) {
	handler, _ := readToml(t, "a = 1\n")
	assert.ErrorIs(t, handler.Write(failingWriter{}), assert.AnError)
}
