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
	"testing/iotest"

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

// TestFileProcessorWithoutExtension checks that files without an extension need -t
func TestFileProcessorWithoutExtension(t *testing.T) {
	file := path.Join(t.TempDir(), "Dockerfile")
	assert.NoError(t, os.WriteFile(file, []byte("FROM scratch"), 0o644))

	err := NewFileProcessor(file, general.Undefined, io.Discard).Process()
	assert.EqualError(t, err, "could not infer the file type of "+file+"; use -t / --file-type to set it explicitly, "+
		"e.g. -t plain to process any text file line by line")
}

// TestFileProcessorTypeIgnoresCase checks that file types and extensions are matched case-insensitively
func TestFileProcessorTypeIgnoresCase(t *testing.T) {
	t.Setenv("PROCESSOR_TEST_CASE", "x")
	dir := t.TempDir()

	tests := []struct {
		name     string
		file     string
		fileType general.FileType
		content  string
		want     string
	}{
		{"upper case extension", "C.YAML", general.Undefined, "a: ${PROCESSOR_TEST_CASE}\n", "a: x\n"},
		{"mixed case extension", "config.Json", general.Undefined, `{"a": "${PROCESSOR_TEST_CASE}"}`, "{\n  \"a\": \"x\"\n}\n"},
		{"upper case type", "config.txt", "PLAIN", "a=${PROCESSOR_TEST_CASE}\n", "a=x\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file := path.Join(dir, tt.file)
			assert.NoError(t, os.WriteFile(file, []byte(tt.content), 0o644))

			var out bytes.Buffer
			processor := NewFileProcessor(file, tt.fileType, &out)
			assert.NoError(t, processor.Process())
			assert.Equal(t, tt.want, out.String())
			assert.Equal(t, general.FileType(strings.ToLower(string(processor.FileType))), processor.FileType)
		})
	}
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

// processContent processes the content as a file of the given type and returns the output
func processContent(t *testing.T, fileType general.FileType, content string) string {
	t.Helper()

	file := path.Join(t.TempDir(), "config."+string(fileType))
	assert.NoError(t, os.WriteFile(file, []byte(content), 0o644))

	var output bytes.Buffer
	assert.NoError(t, NewFileProcessor(file, fileType, &output).Process())
	return output.String()
}

// TestProcessKeepsFormat checks that the BOM and CRLF line endings of a file are written as read for every file type
func TestProcessKeepsFormat(t *testing.T) {
	t.Setenv("FORMAT_TEST", "value")
	t.Setenv("FORMAT_FRAGMENT", "x: 1\ny: 2")
	bom := "\xEF\xBB\xBF"

	testCases := []struct {
		desc     string
		fileType general.FileType
		content  string
		want     string
	}{
		{desc: "yaml", fileType: general.YAML, content: "a: ${FORMAT_TEST}\r\nb: |\r\n  l1\r\n  l2\r\n", want: "a: value\r\nb: |\r\n  l1\r\n  l2\r\n"},
		{desc: "yaml with BOM", fileType: general.YAML, content: bom + "a: ${FORMAT_TEST}\r\n", want: bom + "a: value\r\n"},
		{desc: "yaml with BOM and LF", fileType: general.YAML, content: bom + "a: ${FORMAT_TEST}\n", want: bom + "a: value\n"},
		{desc: "yaml fragment", fileType: general.YAML, content: "a: ${FORMAT_FRAGMENT | fragment}\r\n", want: "a:\r\n  x: 1\r\n  y: 2\r\n"},
		{desc: "json", fileType: general.JSON, content: "{\r\n  \"a\": \"${FORMAT_TEST}\"\r\n}\r\n", want: "{\r\n  \"a\": \"value\"\r\n}\r\n"},
		// The JSON parser rejects a BOM, so it is removed before parsing
		{desc: "json with BOM", fileType: general.JSON, content: bom + "{\"a\": \"${FORMAT_TEST}\"}\r\n", want: bom + "{\r\n  \"a\": \"value\"\r\n}\r\n"},
		{desc: "xml", fileType: general.XML, content: bom + "<a>\r\n  <b x=\"${FORMAT_TEST}\">${FORMAT_TEST}</b>\r\n</a>\r\n", want: bom + "<a>\r\n  <b x=\"value\">value</b>\r\n</a>\r\n"},
		{desc: "properties", fileType: general.PROPERTIES, content: bom + "a=${FORMAT_TEST}\r\nb=2\r\n", want: bom + "a=value\r\nb=2\r\n"},
		{desc: "plain", fileType: general.PLAIN, content: bom + "a ${FORMAT_TEST}\r\nb\nc\r\n", want: bom + "a value\r\nb\nc\r\n"},
		{desc: "lf", fileType: general.YAML, content: "a: ${FORMAT_TEST}\nb: 2\n", want: "a: value\nb: 2\n"},
		{desc: "single line", fileType: general.YAML, content: "a: ${FORMAT_TEST}", want: "a: value\n"},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			assert.Equal(t, tC.want, processContent(t, tC.fileType, tC.content))
		})
	}
}

// TestProcessDetectsFormatOnEveryRead checks that a processor used again detects the format of the file anew
func TestProcessDetectsFormatOnEveryRead(t *testing.T) {
	file := path.Join(t.TempDir(), "config.yaml")
	assert.NoError(t, os.WriteFile(file, []byte("\xEF\xBB\xBFa: 1\r\n"), 0o644))

	var output bytes.Buffer
	fp := NewFileProcessor(file, general.YAML, &output)
	assert.NoError(t, fp.Process())
	assert.Equal(t, "\xEF\xBB\xBFa: 1\r\n", output.String())

	assert.NoError(t, os.WriteFile(file, []byte("a: 1"), 0o644))
	output.Reset()
	assert.NoError(t, fp.Process())
	assert.Equal(t, "a: 1\n", output.String())
}

// TestProcessKeepsFormatWriteError checks that errors of the handler are returned when the format is restored
func TestProcessKeepsFormatWriteError(t *testing.T) {
	file := path.Join(t.TempDir(), "config.yaml")
	assert.NoError(t, os.WriteFile(file, []byte("a: 1\r\n"), 0o644))

	err := NewFileProcessor(file, general.YAML, failingWriter{}).Process()
	assert.ErrorContains(t, err, "failed to write")
}

// TestProcessKeepsFormatHandlerWriteError checks that an error of the handler's Write is returned when the format is restored
func TestProcessKeepsFormatHandlerWriteError(t *testing.T) {
	file := path.Join(t.TempDir(), "config.yaml")
	assert.NoError(t, os.WriteFile(file, []byte("a: 1\r\n"), 0o644))

	var output bytes.Buffer
	fp := NewFileProcessor(file, general.YAML, &output)
	handler, _, err := fp.readWith(failingWriteHandler{NewYamlConfigFileHandler()})
	assert.NoError(t, err)

	assert.ErrorIs(t, fp.write(handler, &output), assert.AnError)
	assert.Empty(t, output.String())
}

// TestTextFormatRestore checks restoring the BOM and line ending of written content
func TestTextFormatRestore(t *testing.T) {
	bom := "\xEF\xBB\xBF"

	testCases := []struct {
		desc    string
		format  textFormat
		written string
		want    string
	}{
		{desc: "nothing to restore", format: textFormat{}, written: "a\nb\n", want: "a\nb\n"},
		{desc: "crlf", format: textFormat{crlf: true}, written: "a\nb\n", want: "a\r\nb\r\n"},
		{desc: "crlf kept", format: textFormat{crlf: true}, written: "a\r\nb\n", want: "a\r\nb\r\n"},
		{desc: "leading newline", format: textFormat{crlf: true}, written: "\na", want: "\r\na"},
		{desc: "bom", format: textFormat{bom: true}, written: "a\n", want: bom + "a\n"},
		{desc: "bom kept", format: textFormat{bom: true, crlf: true}, written: bom + "a\n", want: bom + "a\r\n"},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			assert.Equal(t, tC.want, string(tC.format.restore([]byte(tC.written))))
		})
	}
}

// readFormat reads the content through a formatReader reading one byte at a time and returns the content read and its format
func readFormat(t *testing.T, content string) (string, textFormat) {
	t.Helper()

	var format textFormat
	read, err := io.ReadAll(iotest.OneByteReader(newFormatReader(io.NopCloser(strings.NewReader(content)), &format)))
	assert.NoError(t, err)
	return string(read), format
}

// TestFormatReader checks detecting the BOM and the line ending of the first line, also when split across reads
func TestFormatReader(t *testing.T) {
	testCases := []struct {
		desc     string
		content  string
		wantRead string
		want     textFormat
	}{
		{desc: "empty", content: "", wantRead: "", want: textFormat{}},
		{desc: "no line break", content: "a\r", wantRead: "a\r", want: textFormat{}},
		{desc: "lf", content: "a\nb\r\n", wantRead: "a\nb\r\n", want: textFormat{}},
		{desc: "crlf", content: "a\r\nb\n", wantRead: "a\r\nb\n", want: textFormat{crlf: true}},
		{desc: "leading lf", content: "\nb\r\n", wantRead: "\nb\r\n", want: textFormat{}},
		{desc: "bom", content: "\xEF\xBB\xBFa\r\n", wantRead: "a\r\n", want: textFormat{bom: true, crlf: true}},
		{desc: "bom only", content: "\xEF\xBB\xBF", wantRead: "", want: textFormat{bom: true}},
		{desc: "shorter than a bom", content: "\xEF\xBB", wantRead: "\xEF\xBB", want: textFormat{}},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			read, format := readFormat(t, tC.content)
			assert.Equal(t, tC.wantRead, read)
			assert.Equal(t, tC.want, format)
		})
	}
}
