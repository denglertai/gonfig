package file

import (
	"bytes"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/assert"
)

// TestYamlProcessor reads a YAML file, counts its entries and writes it back unchanged
func TestYamlProcessor(t *testing.T) {
	wd, err := os.Getwd()
	assert.NoError(t, err)

	file := path.Join(wd, "/testdata/yaml/deployment.yaml")

	input, err := os.Open(file)
	assert.NoError(t, err)

	defer func() { assert.NoError(t, input.Close()) }()

	handler := NewYamlConfigFileHandler()

	err = handler.Read(input)
	assert.NoError(t, err)

	entries, err := handler.Process()
	assert.NoError(t, err)
	assert.NotNil(t, entries)

	count := 0
	for entry := range entries {
		ye, ok := entry.(*YamlConfigEntry)
		if ok && !ye.IsKey() {
			count++
			assert.NotNil(t, entry)
		}
	}

	assert.Equal(t, 9, count)

	output := new(bytes.Buffer)
	assert.NoError(t, handler.Write(output))

	snaps.MatchSnapshot(t, output.String())
}

// TestYamlProcessorEdit sets values of a YAML file and compares the output with the snapshot
func TestYamlProcessorEdit(t *testing.T) {
	wd, err := os.Getwd()
	assert.NoError(t, err)

	file := path.Join(wd, "/testdata/yaml/deployment.yaml")

	input, err := os.Open(file)
	assert.NoError(t, err)

	defer func() { assert.NoError(t, input.Close()) }()

	handler := NewYamlConfigFileHandler()

	err = handler.Read(input)
	assert.NoError(t, err)

	entries, err := handler.Process()
	assert.NoError(t, err)
	assert.NotNil(t, entries)

	count := 0
	for entry := range entries {
		ye, ok := entry.(*YamlConfigEntry)
		if ok && !ye.IsKey() {
			count++
			assert.NotNil(t, entry)

			if ye.Path() == "spec.replicas" {
				entry.SetValue("777")
			}
			if ye.Key() == "app" {
				entry.SetValue("edited")
			}
		}
	}

	assert.Equal(t, 9, count)

	output := new(bytes.Buffer)
	assert.NoError(t, handler.Write(output))

	snaps.MatchSnapshot(t, output.String())
}

// TestYamlKeyTree checks that keys of YAML mappings are entries as well, and counts values and keys separately
func TestYamlKeyTree(t *testing.T) {
	wd, err := os.Getwd()
	assert.NoError(t, err)

	file := path.Join(wd, "/testdata/yaml/key.yaml")

	input, err := os.Open(file)
	assert.NoError(t, err)

	defer func() { assert.NoError(t, input.Close()) }()

	handler := NewYamlConfigFileHandler()

	err = handler.Read(input)
	assert.NoError(t, err)

	entries, err := handler.Process()
	assert.NoError(t, err)
	assert.NotNil(t, entries)

	count := 0
	keyCount := 0
	for entry := range entries {
		ye, ok := entry.(*YamlConfigEntry)
		if ok && !ye.IsKey() {
			count++
			assert.NotNil(t, entry)
		}

		if ok && ye.IsKey() {
			keyCount++
			assert.NotNil(t, entry)
		}
	}

	assert.Equal(t, 3, count)
	assert.Equal(t, 4, keyCount)

	output := new(bytes.Buffer)
	err = handler.Write(output)
	assert.NoError(t, err)

	snaps.MatchSnapshot(t, output.String())
}

// TestYamlKeyTreeEdit renames a key of a YAML mapping and compares the output with the snapshot
func TestYamlKeyTreeEdit(t *testing.T) {
	wd, err := os.Getwd()
	assert.NoError(t, err)

	file := path.Join(wd, "/testdata/yaml/key.yaml")

	input, err := os.Open(file)
	assert.NoError(t, err)

	defer func() { assert.NoError(t, input.Close()) }()

	handler := NewYamlConfigFileHandler()

	err = handler.Read(input)
	assert.NoError(t, err)

	entries, err := handler.Process()
	assert.NoError(t, err)
	assert.NotNil(t, entries)

	count := 0
	keyCount := 0
	for entry := range entries {
		ye, ok := entry.(*YamlConfigEntry)
		if ok && !ye.IsKey() {
			count++
			assert.NotNil(t, entry)
		}

		if ok && ye.IsKey() {
			keyCount++
			assert.NotNil(t, entry)

			if ye.Key() == "backend_roles" {
				ye.SetValue("frontend_roles")
			}
		}
	}

	assert.Equal(t, 3, count)
	assert.Equal(t, 4, keyCount)

	output := new(bytes.Buffer)
	err = handler.Write(output)
	assert.NoError(t, err)

	snaps.MatchSnapshot(t, output.String())
}

// processYaml reads the given YAML, sets every value to the result of fn and returns the written output
func processYaml(t *testing.T, input string, fn func(ConfigEntry) string) string {
	t.Helper()

	handler := NewYamlConfigFileHandler()
	assert.NoError(t, handler.Read(strings.NewReader(input)))

	entries, err := handler.Process()
	assert.NoError(t, err)
	for entry := range entries {
		entry.SetValue(fn(entry))
	}

	output := new(bytes.Buffer)
	assert.NoError(t, handler.Write(output))
	return output.String()
}

// TestYamlRoundTrip checks that unchanged documents are written back exactly as they were read
func TestYamlRoundTrip(t *testing.T) {
	testCases := []struct {
		desc  string
		input string
	}{
		{desc: "comments and key order", input: "# head comment\nz: 1 # inline\na:\n  # nested comment\n  c: x\n  b: y\n"},
		{desc: "null and timestamp values", input: "a: null\nb:\nc: 2024-01-01\n"},
		{desc: "top-level list", input: "- a\n- b: c\n"},
		{desc: "multiple documents", input: "a: 1\n---\nb: 2\n"},
		{desc: "anchors and aliases", input: "base: &base\n  a: 1\nderived:\n  <<: *base\n  b: 2\n"},
		{desc: "block scalar", input: "script: |\n  echo a\n  echo b\n"},
		{desc: "literal scalar with long line", input: "script: |\n  " + strings.Repeat("echo a; ", 15) + "\n  echo b\n"},
		// Folded lines are joined when written, so only the joined form is written back unchanged
		{desc: "folded scalar", input: "text: >\n  first line second line\nnext: 1\n"},
		{desc: "folded scalar with long line", input: "text: >-\n  " + strings.Repeat("word ", 25) + "end\n"},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			output := processYaml(t, tC.input, ConfigEntry.GetValue)
			assert.Equal(t, tC.input, output)
		})
	}
}

// TestYamlLongLinesAreNotWrapped checks that lines longer than 80 columns are kept on one line, like yaml v3 did,
// because v4 wraps at 80 columns by default and wrapped values break line-based tools reading the output
func TestYamlLongLinesAreNotWrapped(t *testing.T) {
	long := strings.Repeat("abcdefghij ", 10) + "end"
	input := "static: " + long + "\nplain: placeholder\nquoted: \"placeholder\"\n"

	output := processYaml(t, input, func(entry ConfigEntry) string {
		if entry.(*YamlConfigEntry).IsKey() || entry.Path() == "static" {
			return entry.GetValue()
		}
		return long
	})

	assert.Equal(t, "static: "+long+"\nplain: "+long+"\nquoted: \""+long+"\"\n", output)
}

// TestYamlSetValues checks that set values are written with the style of the replaced value
func TestYamlSetValues(t *testing.T) {
	testCases := []struct {
		desc     string
		input    string
		values   map[string]string
		expected string
	}{
		{
			desc:     "top-level value",
			input:    "key: placeholder\n",
			values:   map[string]string{"key": "value"},
			expected: "key: value\n",
		},
		{
			desc:     "string keeps its type",
			input:    "port: placeholder\nflag: placeholder\n",
			values:   map[string]string{"port": "8080", "flag": "true"},
			expected: "port: \"8080\"\nflag: \"true\"\n",
		},
		{
			desc:     "value in later document",
			input:    "a: 1\n---\nb: placeholder\n",
			values:   map[string]string{"b": "value"},
			expected: "a: 1\n---\nb: value\n",
		},
		{
			desc:     "value in top-level list",
			input:    "- placeholder\n",
			values:   map[string]string{"0": "value"},
			expected: "- value\n",
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			output := processYaml(t, tC.input, func(entry ConfigEntry) string {
				if entry.(*YamlConfigEntry).IsKey() {
					return entry.GetValue()
				}
				if value, found := tC.values[entry.Path()]; found {
					return value
				}
				return entry.GetValue()
			})
			assert.Equal(t, tC.expected, output)
		})
	}
}

// TestYamlInvalidUTF8IsRejected checks that values and keys which are not valid UTF-8 fail writing and name the entry
// without the value, since yaml v4 would otherwise write them base64-encoded as !!binary
func TestYamlInvalidUTF8IsRejected(t *testing.T) {
	testCases := []struct {
		desc    string
		content string
		wantErr string
	}{
		{desc: "plain value", content: "a: ${INVALID_UTF8}\n", wantErr: "the value of a is not valid UTF-8"},
		{desc: "quoted value", content: "a: \"${INVALID_UTF8}\"\n", wantErr: "the value of a is not valid UTF-8"},
		{desc: "literal value", content: "a: |\n  ${INVALID_UTF8}\n", wantErr: "the value of a is not valid UTF-8"},
		{desc: "list item", content: "a:\n  - ${INVALID_UTF8}\n", wantErr: "the value of a.0 is not valid UTF-8"},
		{desc: "key", content: "${INVALID_UTF8}: keyed\n", wantErr: "the key ${INVALID_UTF8} is not valid UTF-8"},
		{desc: "fragment", content: "a: ${INVALID_UTF8 | fragment}\n", wantErr: "invalid YAML fragment"},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			// The value is read from a file referenced with @, because Windows stores environment variables as
			// UTF-16 and would replace the invalid byte with U+FFFD
			dir := t.TempDir()
			secret := filepath.Join(dir, "secret")
			assert.NoError(t, os.WriteFile(secret, []byte("s3cret\xffutf8"), 0o600))
			t.Setenv("INVALID_UTF8", "@"+secret)
			file := path.Join(dir, "config.yaml")
			assert.NoError(t, os.WriteFile(file, []byte(tC.content), 0o644))

			var output bytes.Buffer
			err := NewFileProcessor(file, "yaml", &output).Process()

			if !assert.ErrorContains(t, err, tC.wantErr) {
				return
			}
			assert.NotContains(t, err.Error(), "s3cret")
			assert.Empty(t, output.String())
		})
	}
}
