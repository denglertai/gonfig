package file

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"iter"
	"log/slog"
	"os"
	"path"
	"strings"

	"github.com/denglertai/gonfig/internal/filter"
	"github.com/denglertai/gonfig/internal/general"
	"github.com/denglertai/gonfig/internal/value"
	"github.com/denglertai/gonfig/pkg/logging"
)

// ConfigEntry represents a single configuration entry, i.e. a value that may contain placeholders.
// Depending on the file type this is a scalar or key (YAML), a scalar (JSON, properties),
// an element's text or an attribute (XML) or a line (plain).
type ConfigEntry interface {
	// Key returns the key of the configuration entry
	Key() string
	// Path returns the path of the configuration entry
	Path() string
	// GetValue returns the value of the configuration entry
	GetValue() string
	// SetValue sets the value of the configuration entry
	SetValue(value string)
}

// ConfigFileHandler represents a configuration file handler, which is implemented once per file type.
// The handler keeps the parsed document, so changes made to its entries are part of what Write writes.
type ConfigFileHandler interface {
	// Read reads the configuration file
	Read(source io.Reader) error
	// Process processes the configuration file and returns the configuration entries
	Process() (iter.Seq[ConfigEntry], error)
	// Write writes the configuration entries to the target
	Write(target io.Writer) error
}

// FileProcessor represents a file processor
type FileProcessor struct {
	// FileName represents the name of the file to be processed
	FileName string
	// FileType represents the type of the file to be processed
	FileType general.FileType
	// Output represents the output writer
	Output io.Writer
	// Diff adds the changes processing the file would make to the report of Check
	Diff bool
	// report collects the problems of all entries while checking the file, see Check
	report *CheckReport
	// source is the content of the file as read, which is only kept for the diff
	source []byte
	// format is the BOM and line ending of the file, which are restored when writing, see restoreFormat
	format textFormat
}

// NewFileProcessor creates a new file processor
func NewFileProcessor(fileName string, fileType general.FileType, output io.Writer) *FileProcessor {
	if fileType == general.Undefined {
		// strip the leading dot; files without an extension yield an undefined type
		ext := strings.TrimPrefix(path.Ext(fileName), ".")
		fileType = general.FileType(ext)
		logging.Debug("File type not provided, using the file's extension", "file", fileName, "type", fileType)
	}

	return &FileProcessor{
		FileName: fileName,
		FileType: fileType,
		Output:   output,
	}
}

// Process processes the file. The pipeline is the same for every file type:
//  1. getFileProcessor selects the ConfigFileHandler for the file type
//  2. Read parses the file into the handler's document
//  3. Process flattens the document into a list of entries
//  4. every entry's value containing a placeholder is passed to value.ProcessValue, which replaces
//     its placeholders, and the result is set on the entry, updating the document
//  5. Write serializes the updated document to the output
//
// Since only the values of entries are changed, everything else in the document is kept
// as far as the file type's library allows, e.g. comments and key order.
// If any value fails to process, nothing is written.
func (fp *FileProcessor) Process() error {
	handler, entries, err := fp.read()
	if err != nil {
		return err
	}

	for entry := range entries {
		if err := fp.processEntry(entry, true, fp.fileGroup()); err != nil {
			return err
		}
	}

	if err := fp.write(handler, fp.Output); err != nil {
		return fmt.Errorf("failed to write %s: %w", fp.FileName, err)
	}

	return nil
}

// read reads and parses the file with the handler of its file type and returns the handler and its entries
func (fp *FileProcessor) read() (ConfigFileHandler, iter.Seq[ConfigEntry], error) {
	handler, err := fp.getFileProcessor()
	if err != nil {
		return nil, nil, err
	}
	return fp.readWith(handler)
}

// readWith reads and parses the file with the given handler and returns the handler and its entries
func (fp *FileProcessor) readWith(handler ConfigFileHandler) (ConfigFileHandler, iter.Seq[ConfigEntry], error) {
	source, err := fp.open(handler)
	if err != nil {
		return nil, nil, err
	}
	// The file is only read, so an error closing it does not affect the result
	defer func() { _ = source.Close() }()

	if err := handler.Read(source); err != nil {
		return nil, nil, fmt.Errorf("failed to read %s: %w", fp.FileName, err)
	}

	entries, err := handler.Process()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to process %s: %w", fp.FileName, err)
	}

	return handler, entries, nil
}

// open opens the file for reading with the handler. The diff needs the content after parsing, so it is read at once
// and kept then. Otherwise the file is streamed, so handlers reading it line by line don't keep it in memory.
// For handlers that don't keep the BOM and line endings, the format is detected while reading, see formatReader.
func (fp *FileProcessor) open(handler ConfigFileHandler) (io.ReadCloser, error) {
	// The format of a previous read must not apply if the processor is used again, e.g. after the file changed
	fp.format = textFormat{}

	var source io.ReadCloser
	if fp.Diff {
		content, err := os.ReadFile(fp.FileName)
		if err != nil {
			return nil, err
		}
		fp.source = content
		source = io.NopCloser(bytes.NewReader(content))
	} else {
		file, err := os.Open(fp.FileName)
		if err != nil {
			return nil, err
		}
		source = file
	}

	if keeper, ok := handler.(formatKeeper); ok && keeper.keepsFormat() {
		return source, nil
	}
	return newFormatReader(source, &fp.format), nil
}

// write writes the document of the handler to the target, with the BOM and line ending of the file restored
func (fp *FileProcessor) write(handler ConfigFileHandler, target io.Writer) error {
	if fp.format == (textFormat{}) {
		return handler.Write(target)
	}

	var written bytes.Buffer
	if err := handler.Write(&written); err != nil {
		return err
	}
	_, err := target.Write(fp.format.restore(written.Bytes()))
	return err
}

// formatKeeper is implemented by handlers that may write the BOM and line endings of the file as they were read,
// e.g. as they process the file line by line, so FileProcessor doesn't restore them
type formatKeeper interface {
	// keepsFormat returns whether the handler writes the BOM and line endings as read
	keepsFormat() bool
}

// utf8BOM is the byte order mark some editors write at the start of UTF-8 files
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// textFormat is the BOM and line ending of a file. The libraries parsing YAML, JSON, XML and properties write
// LF line endings and drop the BOM, so they are restored after writing to keep the file as it was.
type textFormat struct {
	// bom is set if the file starts with a UTF-8 byte order mark
	bom bool
	// crlf is set if the first line of the file ends with CRLF. Files with mixed line endings get the first one.
	crlf bool
}

// formatReader detects the format of the file it reads into format while it is read. It removes the BOM, which the
// parsers of some file types reject, e.g. JSON, and passes everything else on unchanged.
type formatReader struct {
	io.Closer
	reader *bufio.Reader
	format *textFormat
	// lineEnded is set once the end of the first line was read, which decides the line ending
	lineEnded bool
	// previous is the last byte of the first line read so far, to find the CR of a CRLF split across reads
	previous byte
}

// newFormatReader returns a formatReader reading the source, which records the BOM before the first read
func newFormatReader(source io.ReadCloser, format *textFormat) *formatReader {
	reader := bufio.NewReader(source)
	// Peek fails for files shorter than the BOM, which have none
	if start, _ := reader.Peek(len(utf8BOM)); bytes.Equal(start, utf8BOM) {
		format.bom = true
		_, _ = reader.Discard(len(utf8BOM))
	}
	return &formatReader{Closer: source, reader: reader, format: format}
}

// Read reads from the source and records the line ending of the first line
func (r *formatReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	for i := 0; i < n && !r.lineEnded; i++ {
		if p[i] == '\n' {
			r.format.crlf = r.previous == '\r'
			r.lineEnded = true
		}
		r.previous = p[i]
	}
	return n, err
}

// restore returns the written content with the BOM and line ending of the format.
// Line breaks written as CRLF already are kept, so they are not doubled.
func (f textFormat) restore(written []byte) []byte {
	var restored bytes.Buffer
	restored.Grow(len(written) + len(utf8BOM) + bytes.Count(written, []byte{'\n'}))

	if f.bom && !bytes.HasPrefix(written, utf8BOM) {
		restored.Write(utf8BOM)
	}
	if !f.crlf {
		restored.Write(written)
		return restored.Bytes()
	}

	for i, c := range written {
		if c == '\n' && (i == 0 || written[i-1] != '\r') {
			restored.WriteByte('\r')
		}
		restored.WriteByte(c)
	}
	return restored.Bytes()
}

// fileGroup returns the attributes describing the file in log records
func (fp *FileProcessor) fileGroup() slog.Attr {
	return slog.Group("file", "name", fp.FileName, "type", fp.FileType)
}

// processEntry replaces the placeholders of the entry. If fragments are allowed, an entry may be replaced by
// a fragment, whose entries are processed as well, but must not contain further fragments.
func (fp *FileProcessor) processEntry(entry ConfigEntry, allowFragments bool, fileGroup slog.Attr) error {
	current := entry.GetValue()

	// Most entries do not contain any placeholders, so skip them before doing any further work
	if !strings.Contains(current, "${") {
		return nil
	}

	if fp.report != nil {
		return fp.checkEntry(entry, current, allowFragments)
	}
	return fp.applyEntry(entry, current, allowFragments, fileGroup)
}

// applyEntry replaces the placeholders of the entry's current value and sets the result
func (fp *FileProcessor) applyEntry(entry ConfigEntry, current string, allowFragments bool, fileGroup slog.Attr) error {
	logging.Debug("Processing entry", "entry", entryPath{entry}, fileGroup)

	process := value.ProcessValue
	if fp.masked() {
		process = value.ProcessMaskedValue
	}
	newVal, err := process(current)

	if err != nil {
		return fmt.Errorf("failed to process the value of %s in %s: %w", entry.Path(), fp.FileName, err)
	}

	// Only the type of the value is logged by default, since the value may contain secrets
	if logValues {
		logging.Debug("Setting new value", "entry", entryPath{entry}, "type", valueType{newVal}, "value", newVal, fileGroup)
	} else {
		logging.Debug("Setting new value", "entry", entryPath{entry}, "type", valueType{newVal}, fileGroup)
	}

	if fragment, isFragment := newVal.(filter.Fragment); isFragment {
		return fp.insertFragment(entry, fragment, allowFragments, fileGroup)
	}

	newText, ok := newVal.(string)
	if !ok {
		// Write non-string results (e.g. of to_int) with their type if the file type supports it, otherwise as text
		if typed, ok := entry.(TypedConfigEntry); ok && typed.SetTypedValue(newVal) {
			return nil
		}
		newText = fmt.Sprint(newVal)
	}
	entry.SetValue(newText)

	return nil
}

// Errors of fragments, which are wrapped with the entry and file, so a check can report the reason only
var (
	errNestedFragment       = errors.New("fragments cannot contain further fragments")
	errFragmentNotSupported = errors.New("fragments are not supported here, only for YAML and JSON values and keys and the content of XML elements")
)

// insertFragment inserts the fragment in place of the entry and processes the entries of the fragment.
// It returns an error when allowFragments is false or the entry does not support fragments.
// Placeholders in inserted entries are processed with further fragments disabled.
// Errors of the insertion and of processing placeholders are returned. Processing then stops, so nothing is written.
func (fp *FileProcessor) insertFragment(entry ConfigEntry, fragment filter.Fragment, allowFragments bool, fileGroup slog.Attr) error {
	if !allowFragments {
		return fmt.Errorf("failed to process the value of %s in %s: %w", entry.Path(), fp.FileName, errNestedFragment)
	}

	fragmentEntry, ok := entry.(FragmentConfigEntry)
	if !ok {
		return fmt.Errorf("failed to process the value of %s in %s: %w", entry.Path(), fp.FileName, errFragmentNotSupported)
	}

	inserted, err := fragmentEntry.SetFragment(fragment)
	if err != nil {
		return fmt.Errorf("failed to insert the fragment of %s in %s: %w", entry.Path(), fp.FileName, err)
	}

	for _, insertedEntry := range inserted {
		if err := fp.processEntry(insertedEntry, false, fileGroup); err != nil {
			return err
		}
	}

	// The fragment may contain secrets, so only its structure is shown, e.g. the keys of mappings.
	// Values that failed to process are masked as well, since their text is part of the fragment.
	if fp.masked() {
		for _, insertedEntry := range inserted {
			if maskable, ok := insertedEntry.(maskableConfigEntry); ok {
				maskable.mask()
			}
		}
	}

	return nil
}

// maskableConfigEntry is implemented by entries that can be inserted by fragments, see FileProcessor.masked
type maskableConfigEntry interface {
	ConfigEntry
	// mask replaces a value by value.Mask as a string. Keys are kept, since they are part of the structure.
	mask()
}

// masked returns whether values are masked, which is the case while checking a file unless values may be logged.
// Checking writes nothing but the diff of the report, which shows the masked values.
func (fp *FileProcessor) masked() bool {
	return fp.report != nil && !logValues
}

// entryPath logs the path of an entry. The path is only computed if the log record is written,
// since computing it is expensive for some file types, e.g. XML.
type entryPath struct {
	entry ConfigEntry
}

// LogValue returns the path of the entry
func (p entryPath) LogValue() slog.Value {
	return slog.StringValue(p.entry.Path())
}

// logValues includes processed values in debug logs, see SetLogValues
var logValues bool

// SetLogValues sets whether processed values are included in debug logs.
// Values may contain secrets read from environment variables or files, so this is meant for debugging only.
func SetLogValues(enabled bool) {
	logValues = enabled
}

// LogValues returns whether processed values are included in debug logs
func LogValues() bool {
	return logValues
}

// valueType logs the type of a processed value, e.g. string, int or filter.Fragment.
// The value itself is only logged if enabled by SetLogValues, since it may contain secrets.
type valueType struct {
	value any
}

// LogValue returns the type of the value. It is only computed if the log record is written.
func (t valueType) LogValue() slog.Value {
	return slog.StringValue(fmt.Sprintf("%T", t.value))
}

// appendToPath appends the key to the dot separated path
func appendToPath(path string, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

// getFileProcessor returns the file processor based on the file type
func (fp *FileProcessor) getFileProcessor() (ConfigFileHandler, error) {
	switch fp.FileType {
	case general.YAML:
		fallthrough
	case general.YML:
		return NewYamlConfigFileHandler(), nil
	case general.JSON:
		return NewJsonConfigFileHandler(), nil
	case general.XML:
		return NewXmlConfigFileHandler(), nil
	case general.PROPERTIES:
		return NewPropertiesConfigFileHandler(), nil
	case general.PLAIN:
		return NewPlainFileProcessor(), nil
	case general.Undefined:
		return nil, fmt.Errorf("could not infer the file type of %q; use -t / --file-type to set it explicitly", fp.FileName)
	default:
		return nil, fmt.Errorf("unsupported file type: %v", fp.FileType)
	}
}
