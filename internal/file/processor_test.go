package file

import (
	"bytes"
	"io"
	"log/slog"
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

func TestFileProcessorProcessesEntries(t *testing.T) {
	t.Setenv("PROCESSOR_TEST_HOST", "db")
	t.Setenv("PROCESSOR_TEST_PORT", "5")

	// Capture debug logs to check that entry paths are logged
	var logs bytes.Buffer
	defaultLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(defaultLogger) })

	file := path.Join(t.TempDir(), "config.yaml")
	content := "static: value\nhost: ${PROCESSOR_TEST_HOST}\nport: ${PROCESSOR_TEST_PORT | to_int}\n"
	assert.NoError(t, os.WriteFile(file, []byte(content), 0o644))

	var output bytes.Buffer
	err := NewFileProcessor(file, general.Undefined, &output).Process()

	assert.NoError(t, err)
	// Entries without placeholders are kept, non-string results keep their type
	assert.Equal(t, "static: value\nhost: db\nport: 5\n", output.String())
	assert.Contains(t, logs.String(), `msg="Setting new value" entry=host`)
	assert.NotContains(t, logs.String(), "entry=static")
}

// failingWriter fails on every write
type failingWriter struct{}

func (failingWriter) Write(_ []byte) (int, error) {
	return 0, assert.AnError
}

func TestFileProcessorWriteError(t *testing.T) {
	file := path.Join(t.TempDir(), "config.yaml")
	assert.NoError(t, os.WriteFile(file, []byte("key: value\n"), 0o644))

	err := NewFileProcessor(file, general.Undefined, failingWriter{}).Process()
	assert.ErrorContains(t, err, "failed to write")
	// The YAML encoder wraps the error of the writer as text
	assert.ErrorContains(t, err, assert.AnError.Error())
}
