package file

import (
	"bytes"
	"encoding/json"
	"os"
	"path"
	"testing"

	"github.com/denglertai/gonfig/internal/general"
	"github.com/stretchr/testify/assert"
	"go.yaml.in/yaml/v3"
)

// processTyped processes the content as a file of the given type and returns the output
func processTyped(t *testing.T, fileType general.FileType, content string) string {
	t.Helper()
	t.Setenv("TYPED_PORT", "8080")
	t.Setenv("TYPED_RATIO", "2")
	t.Setenv("TYPED_ON", "true")
	t.Setenv("TYPED_ZIP", "01701")

	file := path.Join(t.TempDir(), "config."+string(fileType))
	assert.NoError(t, os.WriteFile(file, []byte(content), 0o644))

	var output bytes.Buffer
	assert.NoError(t, NewFileProcessor(file, fileType, &output).Process())
	return output.String()
}

func TestTypedValuesYaml(t *testing.T) {
	output := processTyped(t, general.YAML, `port: ${TYPED_PORT | to_int}
quoted: "${TYPED_PORT | to_int}"
ratio: ${TYPED_RATIO | to_float}
enabled: ${TYPED_ON | to_bool}
zip: ${TYPED_ZIP}
url: http://host:${TYPED_PORT | to_int}/api
${TYPED_PORT | to_int}: key
list: ["${TYPED_PORT | to_int}", '${TYPED_ON | to_bool}']
`)

	var result map[any]any
	assert.NoError(t, yaml.Unmarshal([]byte(output), &result))
	assert.Equal(t, map[any]any{
		"port":    8080,
		"quoted":  8080,
		"ratio":   2.0,
		"enabled": true,
		// Results of filters returning strings keep the type string
		"zip": "01701",
		// Embedded results and keys are text
		"url":  "http://host:8080/api",
		"8080": "key",
		"list": []any{8080, true},
	}, result)
}

func TestTypedValuesJson(t *testing.T) {
	output := processTyped(t, general.JSON, `{
  "port": "${TYPED_PORT | to_int}",
  "ratio": "${TYPED_RATIO | to_float}",
  "enabled": "${TYPED_ON | to_bool}",
  "zip": "${TYPED_ZIP}",
  "url": "http://host:${TYPED_PORT | to_int}/api",
  "list": ["${TYPED_PORT | to_int}"]
}`)

	assert.Contains(t, output, `"ratio": 2.0`)

	var result map[string]any
	assert.NoError(t, json.Unmarshal([]byte(output), &result))
	assert.Equal(t, map[string]any{
		"port":    8080.0,
		"ratio":   2.0,
		"enabled": true,
		"zip":     "01701",
		"url":     "http://host:8080/api",
		"list":    []any{8080.0},
	}, result)
}

func TestTypedValuesWithoutTypeSupport(t *testing.T) {
	testCases := []struct {
		fileType general.FileType
		content  string
		want     string
	}{
		{general.XML, `<r a="${TYPED_PORT | to_int}"><b>${TYPED_ON | to_bool}</b></r>`, `<r a="8080"><b>true</b></r>`},
		{general.PROPERTIES, "port=${TYPED_PORT | to_int}\n", "port = 8080\n"},
		{general.PLAIN, "port ${TYPED_PORT | to_int}\n", "port 8080\n"},
	}
	for _, tC := range testCases {
		t.Run(string(tC.fileType), func(t *testing.T) {
			assert.Equal(t, tC.want, processTyped(t, tC.fileType, tC.content))
		})
	}
}

func TestNewTypedValue(t *testing.T) {
	testCases := []struct {
		desc  string
		value any
		want  typedValue
		ok    bool
	}{
		{desc: "int", value: 5, want: typedValue{typedInt, "5"}, ok: true},
		{desc: "int64", value: int64(-5), want: typedValue{typedInt, "-5"}, ok: true},
		{desc: "uint8", value: uint8(5), want: typedValue{typedInt, "5"}, ok: true},
		{desc: "float with fraction", value: 1.5, want: typedValue{typedFloat, "1.5"}, ok: true},
		{desc: "whole float", value: 2.0, want: typedValue{typedFloat, "2.0"}, ok: true},
		{desc: "large float", value: 1e21, want: typedValue{typedFloat, "1e+21"}, ok: true},
		{desc: "float32", value: float32(0.1), want: typedValue{typedFloat, "0.1"}, ok: true},
		{desc: "bool", value: false, want: typedValue{typedBool, "false"}, ok: true},
		{desc: "string", value: "5", ok: false},
		{desc: "list", value: []string{"a"}, ok: false},
		{desc: "nil", value: nil, ok: false},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			got, ok := newTypedValue(tC.value)
			assert.Equal(t, tC.ok, ok)
			assert.Equal(t, tC.want, got)
		})
	}
}

// TestTypedValuesJsonValid ensures the JSON output of negative, large and small numbers is valid
func TestTypedValuesJsonValid(t *testing.T) {
	entry := &JsonConfigEntry{node: &jsonNode{value: "${X}"}}
	for _, value := range []any{-5, 1e21, 0.1, uint64(18446744073709551615)} {
		assert.True(t, entry.SetTypedValue(value))
		data, err := json.Marshal(entry.typed)
		assert.NoError(t, err)
		assert.True(t, json.Valid(data), string(data))
	}
}
