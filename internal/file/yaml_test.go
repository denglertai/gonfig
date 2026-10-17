package file

import (
	"bytes"
	"os"
	"path"
	"strings"
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/assert"
)

func TestYamlProcessor(t *testing.T) {
	wd, err := os.Getwd()
	assert.NoError(t, err)

	file := path.Join(wd, "/testdata/yaml/deployment.yaml")

	input, err := os.Open(file)
	assert.NoError(t, err)

	defer input.Close()

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
	handler.Write(output)

	snaps.MatchSnapshot(t, output.String())
}

func TestYamlProcessorEdit(t *testing.T) {
	wd, err := os.Getwd()
	assert.NoError(t, err)

	file := path.Join(wd, "/testdata/yaml/deployment.yaml")

	input, err := os.Open(file)
	assert.NoError(t, err)

	defer input.Close()

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
	handler.Write(output)

	snaps.MatchSnapshot(t, output.String())
}

func TestYamlKeyTree(t *testing.T) {
	wd, err := os.Getwd()
	assert.NoError(t, err)

	file := path.Join(wd, "/testdata/yaml/key.yaml")

	input, err := os.Open(file)
	assert.NoError(t, err)

	defer input.Close()

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

func TestYamlKeyTreeEdit(t *testing.T) {
	wd, err := os.Getwd()
	assert.NoError(t, err)

	file := path.Join(wd, "/testdata/yaml/key.yaml")

	input, err := os.Open(file)
	assert.NoError(t, err)

	defer input.Close()

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
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			output := processYaml(t, tC.input, ConfigEntry.GetValue)
			assert.Equal(t, tC.input, output)
		})
	}
}

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
