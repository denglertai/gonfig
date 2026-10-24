package file

import (
	"bytes"
	"os"
	"path"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/denglertai/gonfig/internal/general"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/assert"
)

// TestPropertiesFileHandler reads a properties file, counts its entries and writes it back including its comments
func TestPropertiesFileHandler(t *testing.T) {
	wd, err := os.Getwd()
	assert.NoError(t, err)

	file := path.Join(wd, "/testdata/properties/props.properties")

	input, err := os.Open(file)
	assert.NoError(t, err)

	defer func() { assert.NoError(t, input.Close()) }()

	handler := NewPropertiesConfigFileHandler()

	err = handler.Read(input)
	assert.NoError(t, err)

	entries, err := handler.Process()
	assert.NoError(t, err)
	assert.NotNil(t, entries)

	count := 0
	for entry := range entries {
		count++
		assert.NotNil(t, entry)
	}

	assert.Equal(t, 6, count)

	output := new(bytes.Buffer)
	assert.NoError(t, handler.Write(output))

	snaps.MatchSnapshot(t, output.String())
}

// TestPropertiesFileHandlerEdit sets values of a properties file and compares the output with the snapshot
func TestPropertiesFileHandlerEdit(t *testing.T) {
	wd, err := os.Getwd()
	assert.NoError(t, err)

	file := path.Join(wd, "/testdata/properties/props.properties")

	input, err := os.Open(file)
	assert.NoError(t, err)

	defer func() { assert.NoError(t, input.Close()) }()

	handler := NewPropertiesConfigFileHandler()

	err = handler.Read(input)
	assert.NoError(t, err)

	entries, err := handler.Process()
	assert.NoError(t, err)
	assert.NotNil(t, entries)

	count := 0
	for entry := range entries {
		count++
		assert.NotNil(t, entry)

		if entry.Key() == "server.port" {
			entry.SetValue("12345")
		}
	}

	assert.Equal(t, 6, count)

	output := new(bytes.Buffer)
	assert.NoError(t, handler.Write(output))

	snaps.MatchSnapshot(t, output.String())
}

// TestPropertiesKeepsFormatting checks that only the values of entries with placeholders change,
// while everything else is written as read
func TestPropertiesKeepsFormatting(t *testing.T) {
	t.Setenv("PROPS_VALUE", "value")
	t.Setenv("PROPS_SPECIAL", " lead\\ing\n\ttab=x:y #z ä")

	testCases := []struct {
		desc    string
		content string
		want    string
	}{
		{desc: "separators", content: "a=${PROPS_VALUE}\nb:2\nc 3\nd = 4\n  e\t:\t5\n", want: "a=value\nb:2\nc 3\nd = 4\n  e\t:\t5\n"},
		{desc: "separator with spaces kept", content: "a  =  ${PROPS_VALUE}\n", want: "a  =  value\n"},
		{desc: "separator of whitespace", content: "a ${PROPS_VALUE}\n", want: "a value\n"},
		{desc: "comments and blank lines", content: "# head ${PROPS_VALUE}\n\n! bang\\\na=${PROPS_VALUE}\n#\n   \n", want: "# head ${PROPS_VALUE}\n\n! bang\\\na=value\n#\n   \n"},
		{desc: "continuation lines kept", content: "a=1\\\n  2\nb=${PROPS_VALUE}\n", want: "a=1\\\n  2\nb=value\n"},
		{desc: "continued value joined", content: "a=x \\\n   ${PROPS_VALUE}\\\n   y\nb=2\n", want: "a=x valuey\nb=2\n"},
		{desc: "value on the next line", content: "a = \\\n  ${PROPS_VALUE}\n", want: "a = value\n"},
		{desc: "key spanning lines", content: "a\\\n  b=${PROPS_VALUE}\n", want: "ab=value\n"},
		{desc: "duplicate keys", content: "a=1\na=${PROPS_VALUE}\n", want: "a=1\na=value\n"},
		{desc: "keys without values", content: "empty\nempty2=\na=${PROPS_VALUE}\n", want: "empty\nempty2=\na=value\n"},
		{desc: "escapes kept in unchanged lines", content: "k\\:x=\\u00e4\\t\na=${PROPS_VALUE}\n", want: "k\\:x=\\u00e4\\t\na=value\n"},
		{desc: "escapes decoded in changed lines", content: "a=\\u00e4 ${PROPS_VALUE}\n", want: "a=ä value\n"},
		{desc: "special characters escaped", content: "a=${PROPS_SPECIAL}\n", want: "a=\\ lead\\\\ing\\n\\ttab=x:y #z ä\n"},
		{desc: "no line ending at the end", content: "a=${PROPS_VALUE}", want: "a=value"},
		{desc: "mixed line endings", content: "a=${PROPS_VALUE}\r\nb=2\nc=${PROPS_VALUE}\rd=4", want: "a=value\r\nb=2\nc=value\rd=4"},
		{desc: "bom", content: "\xEF\xBB\xBFa=${PROPS_VALUE}\r\n", want: "\xEF\xBB\xBFa=value\r\n"},
		{desc: "empty key", content: "=${PROPS_VALUE}\n", want: "=value\n"},
		{desc: "empty file", content: "", want: ""},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			assert.Equal(t, tC.want, processContent(t, general.PROPERTIES, tC.content))
		})
	}
}

// TestPropertiesEntries checks the keys and values read, which follow Java's Properties
func TestPropertiesEntries(t *testing.T) {
	handler := NewPropertiesConfigFileHandler()
	assert.NoError(t, handler.Read(strings.NewReader("\xEF\xBB\xBFa=1\n b : 2 \nc\\ d=\\u00e4\\n\\q\ne\\\n  f=x\\\n  y\ng\\u0\\\n  000=1\n=empty\nkey\r\n")))
	entries, err := handler.Process()
	assert.NoError(t, err)

	var got [][2]string
	for entry := range entries {
		got = append(got, [2]string{entry.Key(), entry.GetValue()})
		assert.Equal(t, entry.Key(), entry.(*PropertiesConfigEntry).Path())
	}
	assert.Equal(t, [][2]string{{"a", "1"}, {"b", "2 "}, {"c d", "ä\nq"}, {"ef", "xy"}, {"g\x00", "1"}, {"", "empty"}, {"key", ""}}, got)
}

// TestPropertiesEntriesStop checks that the iteration of the entries can be stopped
func TestPropertiesEntriesStop(t *testing.T) {
	handler := NewPropertiesConfigFileHandler()
	assert.NoError(t, handler.Read(strings.NewReader("a=1\nb=2\n")))
	entries, err := handler.Process()
	assert.NoError(t, err)

	for entry := range entries {
		assert.Equal(t, "a", entry.Key())
		break
	}
}

// TestPropertiesReadErrors checks the errors of invalid escape sequences, which name the line
func TestPropertiesReadErrors(t *testing.T) {
	for content, wantErr := range map[string]string{
		"a=1\nb=\\u00g0\n":    `line 2: invalid value of b: invalid unicode escape "\\u00g0"`,
		"a=\\u12":             `line 1: invalid value of a: invalid unicode escape "\\u12"`,
		"\\uxyz=1\n":          `line 1: invalid key: invalid unicode escape "\\uxyz"`,
		"a=1\nb=2\\":          "line 2 ends with an escape character at the end of the file",
		"a=1\\\n  2\\\n  3\\": "line 3 ends with an escape character at the end of the file",
	} {
		err := NewPropertiesConfigFileHandler().Read(strings.NewReader(content))
		assert.EqualError(t, err, wantErr, content)
	}
}

// TestPropertiesReadError checks that errors reading the source are returned
func TestPropertiesReadError(t *testing.T) {
	assert.ErrorIs(t, NewPropertiesConfigFileHandler().Read(iotest.ErrReader(assert.AnError)), assert.AnError)
}

// TestPropertiesWriteError checks that errors writing the output are returned
func TestPropertiesWriteError(t *testing.T) {
	handler := NewPropertiesConfigFileHandler()
	assert.NoError(t, handler.Read(strings.NewReader("a=1\n")))
	assert.ErrorIs(t, handler.Write(failingWriter{}), assert.AnError)
}
