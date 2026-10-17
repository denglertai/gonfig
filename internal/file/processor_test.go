package file

import (
	"io"
	"os"
	"path"
	"testing"

	"github.com/denglertai/gonfig/internal/general"
	"github.com/stretchr/testify/assert"
)

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}

func TestFileProcessor_Process(t *testing.T) {
	type fields struct {
		FileName string
		FileType general.FileType
		Output   io.Writer
	}
	tests := []struct {
		name    string
		fields  fields
		wantErr bool
	}{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fp := &FileProcessor{
				FileName: tt.fields.FileName,
				FileType: tt.fields.FileType,
				Output:   tt.fields.Output,
			}
			if err := fp.Process(); (err != nil) != tt.wantErr {
				t.Errorf("FileProcessor.Process() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestFileProcessorWithoutExtension(t *testing.T) {
	file := path.Join(t.TempDir(), "Dockerfile")
	assert.NoError(t, os.WriteFile(file, []byte("FROM scratch"), 0o644))

	err := NewFileProcessor(file, general.Undefined, io.Discard).Process()
	assert.ErrorContains(t, err, "could not infer the file type")
}
