package file

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
	"strings"
	"testing"

	"github.com/denglertai/gonfig/internal/general"
	"github.com/denglertai/gonfig/internal/logging"
	"github.com/denglertai/gonfig/internal/testutil"
	"github.com/stretchr/testify/assert"
)

// TestMain runs the tests isolated from the GONFIG_* environment variables and config files of the machine
func TestMain(m *testing.M) {
	restore, err := testutil.IsolateFromEnvironment()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	code := m.Run()
	restore()
	os.Exit(code)
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

// TestProcessedValuesAreNotLogged ensures that processed values, which may contain secrets, never appear in the logs
func TestProcessedValuesAreNotLogged(t *testing.T) {
	const secret = "top-secret-value"
	t.Setenv("LOG_TEST_SECRET", secret)
	t.Setenv("LOG_TEST_PORT", "8080")
	t.Setenv("LOG_TEST_FRAGMENT", "nested: ${LOG_TEST_SECRET}")

	// Log at the most verbose level
	var logs bytes.Buffer
	defaultLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: logging.LevelTrace})))
	t.Cleanup(func() { slog.SetDefault(defaultLogger) })

	file := path.Join(t.TempDir(), "config.yaml")
	content := "plain: ${LOG_TEST_SECRET}\nembedded: user:${LOG_TEST_SECRET}\nport: ${LOG_TEST_PORT | to_int}\nfragment: ${LOG_TEST_FRAGMENT | fragment}\n"
	assert.NoError(t, os.WriteFile(file, []byte(content), 0o644))

	var output bytes.Buffer
	assert.NoError(t, NewFileProcessor(file, general.Undefined, &output).Process())

	assert.Contains(t, output.String(), secret)
	assert.NotContains(t, logs.String(), secret)
	// Check the records rather than searching for the number, since timestamps contain arbitrary digits
	for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		var record map[string]any
		assert.NoError(t, json.Unmarshal([]byte(line), &record))
		assert.NotContains(t, record, "value", line)
	}
	// The type of each result is logged instead
	assert.Contains(t, logs.String(), `"entry":"plain","type":"string"`)
	assert.Contains(t, logs.String(), `"entry":"port","type":"int"`)
	assert.Contains(t, logs.String(), `"entry":"fragment","type":"filter.Fragment"`)
}

// TestProcessedValuesAreLoggedIfEnabled checks that values appear in debug logs only after opting in with SetLogValues
func TestProcessedValuesAreLoggedIfEnabled(t *testing.T) {
	t.Setenv("LOG_TEST_SECRET", "visible-value")
	assert.False(t, LogValues())
	SetLogValues(true)
	t.Cleanup(func() { SetLogValues(false) })
	assert.True(t, LogValues())

	var logs bytes.Buffer
	defaultLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(defaultLogger) })

	file := path.Join(t.TempDir(), "config.yaml")
	assert.NoError(t, os.WriteFile(file, []byte("plain: ${LOG_TEST_SECRET}\n"), 0o644))
	assert.NoError(t, NewFileProcessor(file, general.Undefined, io.Discard).Process())

	assert.Contains(t, logs.String(), `"entry":"plain","type":"string","value":"visible-value"`)
}
