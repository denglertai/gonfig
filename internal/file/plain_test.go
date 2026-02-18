package file

import (
	"bytes"
	"os"
	"path"
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/assert"
)

func TestPlainFileHandler(t *testing.T) {
	wd, err := os.Getwd()
	assert.NoError(t, err)

	file := path.Join(wd, "/testdata/plain/test.sh")
	input, err := os.Open(file)
	assert.NoError(t, err)

	defer input.Close()

	handler := NewPlainFileProcessor()

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

	assert.Equal(t, 16, count)

	output := new(bytes.Buffer)
	handler.Write(output)

	snaps.MatchSnapshot(t, output.String())
}
