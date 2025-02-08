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
		hce, ok := entry.(*HierarchicalConfigEntry)
		if ok {
			assert.Equal(t, hce.path, strings.Join(hce.hierarchy, "."))
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
		hce, ok := entry.(*HierarchicalConfigEntry)
		if ok {
			assert.Equal(t, hce.path, strings.Join(hce.hierarchy, "."))
			count++
			assert.NotNil(t, entry)

			if hce.path == "spec.replicas" {
				entry.SetValue("777")
			}
			if hce.Key() == "app" {
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
		hce, ok := entry.(*HierarchicalConfigEntry)
		if ok {
			assert.Equal(t, hce.path, strings.Join(hce.hierarchy, "."))
			count++
			assert.NotNil(t, entry)
		}

		hck, ok := entry.(*HierarchicalConfigKey)
		if ok {
			assert.Equal(t, hck.path, strings.Join(hck.hierarchy, "."))
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
		hce, ok := entry.(*HierarchicalConfigEntry)
		if ok {
			assert.Equal(t, hce.path, strings.Join(hce.hierarchy, "."))
			count++
			assert.NotNil(t, entry)
		}

		hck, ok := entry.(*HierarchicalConfigKey)
		if ok {
			assert.Equal(t, hck.path, strings.Join(hck.hierarchy, "."))
			keyCount++
			assert.NotNil(t, entry)

			if hck.key == "backend_roles" {
				hck.SetValue("frontend_roles")
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
