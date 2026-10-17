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

func TestJsonFileHandlerList(t *testing.T) {
	wd, err := os.Getwd()
	assert.NoError(t, err)

	file := path.Join(wd, "/testdata/json/list.json")

	input, err := os.Open(file)
	assert.NoError(t, err)

	defer input.Close()

	handler := NewJsonConfigFileHandler()

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

	assert.Equal(t, 3940, count)

	output := new(bytes.Buffer)
	handler.Write(output)

	snaps.MatchSnapshot(t, output.String())
}

func TestJsonFileHandlerListEdit(t *testing.T) {
	wd, err := os.Getwd()
	assert.NoError(t, err)

	file := path.Join(wd, "/testdata/json/list.json")

	input, err := os.Open(file)
	assert.NoError(t, err)

	defer input.Close()

	handler := NewJsonConfigFileHandler()

	err = handler.Read(input)
	assert.NoError(t, err)

	entries, err := handler.Process()
	assert.NoError(t, err)
	assert.NotNil(t, entries)

	count := 0
	for entry := range entries {
		count++
		assert.NotNil(t, entry)
		if entry.Key() == "language" {
			entry.SetValue("en")
		}
	}

	assert.Equal(t, 3940, count)

	output := new(bytes.Buffer)
	err = handler.Write(output)
	assert.NoError(t, err)

	snaps.MatchSnapshot(t, output.String())
}

func TestJsonFileHandlerObject(t *testing.T) {
	wd, err := os.Getwd()
	assert.NoError(t, err)

	file := path.Join(wd, "/testdata/json/quiz.json")

	input, err := os.Open(file)
	assert.NoError(t, err)

	defer input.Close()

	handler := NewJsonConfigFileHandler()

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

	assert.Equal(t, 18, count)

	output := new(bytes.Buffer)
	handler.Write(output)

	snaps.MatchSnapshot(t, output.String())
}

func TestJsonFileHandlerObjectEdit(t *testing.T) {
	wd, err := os.Getwd()
	assert.NoError(t, err)

	file := path.Join(wd, "/testdata/json/quiz.json")

	input, err := os.Open(file)
	assert.NoError(t, err)

	defer input.Close()

	handler := NewJsonConfigFileHandler()

	err = handler.Read(input)
	assert.NoError(t, err)

	entries, err := handler.Process()
	assert.NoError(t, err)
	assert.NotNil(t, entries)

	count := 0
	for entry := range entries {
		count++
		assert.NotNil(t, entry)
		if entry.Key() == "3" {
			entry.SetValue("333333333333333333333")
		}
	}

	assert.Equal(t, 18, count)

	output := new(bytes.Buffer)
	handler.Write(output)

	snaps.MatchSnapshot(t, output.String())
}

func TestJsonFileHandlerDeeplyNestedSiblings(t *testing.T) {
	handler := NewJsonConfigFileHandler()
	err := handler.Read(strings.NewReader(`{"a":{"b":{"c":{"w":"0","x":"0","y":"0","z":"0"}}}}`))
	assert.NoError(t, err)

	entries, err := handler.Process()
	assert.NoError(t, err)
	for entry := range entries {
		entry.SetValue(entry.Key())
	}

	output := new(bytes.Buffer)
	assert.NoError(t, handler.Write(output))
	assert.JSONEq(t, `{"a":{"b":{"c":{"w":"w","x":"x","y":"y","z":"z"}}}}`, output.String())
}

// processJson reads the given JSON, sets values using fn and returns the written output
func processJson(t *testing.T, input string, fn func(ConfigEntry) string) (string, error) {
	t.Helper()

	handler := NewJsonConfigFileHandler()
	if err := handler.Read(strings.NewReader(input)); err != nil {
		return "", err
	}

	entries, err := handler.Process()
	assert.NoError(t, err)
	for entry := range entries {
		entry.SetValue(fn(entry))
	}

	output := new(bytes.Buffer)
	err = handler.Write(output)
	return output.String(), err
}

func TestJsonRoundTrip(t *testing.T) {
	testCases := []struct {
		desc  string
		input string
	}{
		{desc: "key order", input: "{\n  \"z\": 1,\n  \"a\": {\n    \"c\": \"x\",\n    \"b\": \"y\"\n  }\n}"},
		{desc: "number formatting and precision", input: "[\n  1.50,\n  1e3,\n  12345678901234567890\n]"},
		{desc: "bool and null", input: "{\n  \"a\": true,\n  \"b\": null\n}"},
		{desc: "empty containers", input: "{\n  \"a\": {},\n  \"b\": []\n}"},
		{desc: "top-level scalar", input: "\"value\""},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			output, err := processJson(t, tC.input, ConfigEntry.GetValue)
			assert.NoError(t, err)
			assert.Equal(t, tC.input, output)
		})
	}
}

func TestJsonSetValues(t *testing.T) {
	testCases := []struct {
		desc     string
		input    string
		value    string
		expected string
		wantErr  bool
	}{
		{desc: "string stays string", input: `{"a":"x"}`, value: "1", expected: "{\n  \"a\": \"1\"\n}"},
		{desc: "number", input: `{"a":1}`, value: "2.5", expected: "{\n  \"a\": 2.5\n}"},
		{desc: "invalid number", input: `{"a":1}`, value: "NaN", wantErr: true},
		{desc: "bool", input: `{"a":true}`, value: "false", expected: "{\n  \"a\": false\n}"},
		{desc: "invalid bool", input: `{"a":true}`, value: "x", wantErr: true},
		{desc: "null", input: `{"a":null}`, value: "x", wantErr: true},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			output, err := processJson(t, tC.input, func(ConfigEntry) string { return tC.value })
			if tC.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tC.expected, output)
		})
	}
}

func TestJsonInvalidInput(t *testing.T) {
	for _, input := range []string{`{"a":`, `{"a":1} {"b":2}`, ``} {
		_, err := processJson(t, input, ConfigEntry.GetValue)
		assert.Error(t, err, input)
	}
}
