package file

import (
	"bytes"
	"encoding/json"
	"os"
	"path"
	"strings"
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/assert"
)

func TestXmlProcessor(t *testing.T) {
	wd, err := os.Getwd()
	assert.NoError(t, err)

	file := path.Join(wd, "/testdata/xml/customers.xml")

	input, err := os.Open(file)
	assert.NoError(t, err)

	defer input.Close()

	handler := NewXmlConfigFileHandler()

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

	assert.Equal(t, 17, count)

	output := new(bytes.Buffer)
	handler.Write(output)

	snaps.MatchSnapshot(t, output.String())
}

func TestXmlProcessorEdit(t *testing.T) {
	wd, err := os.Getwd()
	assert.NoError(t, err)

	file := path.Join(wd, "/testdata/xml/customers.xml")

	input, err := os.Open(file)
	assert.NoError(t, err)

	defer input.Close()

	handler := NewXmlConfigFileHandler()

	err = handler.Read(input)
	assert.NoError(t, err)

	entries, err := handler.Process()
	assert.NoError(t, err)
	assert.NotNil(t, entries)

	count := 0
	for entry := range entries {
		count++
		if entry.Key() == "id" {
			entry.SetValue("123")
		}

		if entry.Key() == "name" {
			entry.SetValue("John & Doe")
		}

		assert.NotNil(t, entry)
	}

	assert.Equal(t, 17, count)

	output := new(bytes.Buffer)
	handler.Write(output)

	snaps.MatchSnapshot(t, output.String())
}

func TestXmlCDataWithJsonNotParsed(t *testing.T) {
	os.Setenv("SERVICE_NAME", "Service")
	defer os.Unsetenv("SERVICE_NAME")

	wd, err := os.Getwd()
	assert.NoError(t, err)

	file := path.Join(wd, "/testdata/xml/cdata.xml")
	input, err := os.Open(file)
	assert.NoError(t, err)
	defer input.Close()

	handler := NewXmlConfigFileHandler()
	err = handler.Read(input)
	assert.NoError(t, err)

	entries, err := handler.Process()
	assert.NoError(t, err)
	assert.NotNil(t, entries)

	var cdataText string
	var foundPattern bool
	for entry := range entries {
		if entry.Key() == "pattern" {
			foundPattern = true
			cdataText = entry.GetValue()
		}
	}
	assert.True(t, foundPattern, "Element <pattern> should be found")
	expected := `
						       {
							 "timestamp": "%date{ISO8601}",
							 "logger": "%logger{0}",
							 "service_name": "Service",
							 "component": "Component",
							 "level": "%level",
							 "thread": "%thread",
							 "ndc": "%X{NDC}",
							 "message": "%message",
							 "traceId": "%mdc{traceId}"
						       }`
	normalize := func(s string) string {
		return strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(s), "\t", ""), " ", "")
	}
	assert.Equal(t, normalize(expected), normalize(cdataText), "CDATA JSON content should be present as text, not parsed")

	output := new(bytes.Buffer)
	handler.Write(output)

	snaps.MatchSnapshot(t, output.String())
}

func TestCDataJsonSyntaxValid(t *testing.T) {
	wd, err := os.Getwd()
	assert.NoError(t, err)

	file := path.Join(wd, "/testdata/xml/cdata.xml")
	input, err := os.Open(file)
	assert.NoError(t, err)
	defer input.Close()

	handler := NewXmlConfigFileHandler()
	err = handler.Read(input)
	assert.NoError(t, err)

	entries, err := handler.Process()
	assert.NoError(t, err)
	assert.NotNil(t, entries)

	var cdataJson string
	var foundPattern bool
	for entry := range entries {
		if entry.Key() == "pattern" {
			cdataJson = entry.GetValue()
			foundPattern = true
		}
	}
	assert.True(t, foundPattern, "Element <pattern> should be found")

	// Check if CDATA content is valid JSON
	var js map[string]interface{}
	err = json.Unmarshal([]byte(strings.TrimSpace(cdataJson)), &js)
	assert.NoError(t, err, "CDATA content should be valid JSON")
}
