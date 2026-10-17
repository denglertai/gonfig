package file

import (
	"bytes"
	"os"
	"path"
	"testing"

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
