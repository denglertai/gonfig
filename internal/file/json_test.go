package file

import (
	"bytes"
	"os"
	"path"
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
