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

// TestPlainFileHandler reads a shell script line by line, counts its lines and writes it back unchanged
func TestPlainFileHandler(t *testing.T) {
	wd, err := os.Getwd()
	assert.NoError(t, err)

	file := path.Join(wd, "/testdata/plain/test.sh")
	input, err := os.Open(file)
	assert.NoError(t, err)

	defer func() { assert.NoError(t, input.Close()) }()

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
	assert.NoError(t, handler.Write(output))

	snaps.MatchSnapshot(t, output.String())
}

func TestPlainFileHandlerPreservesTrailingNewline(t *testing.T) {
	for _, input := range []string{"a\nb", "a\nb\n", "", "\n"} {
		handler := NewPlainFileProcessor()
		assert.NoError(t, handler.Read(strings.NewReader(input)))

		output := new(bytes.Buffer)
		assert.NoError(t, handler.Write(output))
		assert.Equal(t, input, output.String())
	}
}
