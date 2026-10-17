package file

import (
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
	fileGroup := slog.Group("file", "name", fp.FileName, "type", fp.FileType)

	handler, err := fp.getFileProcessor()
	if err != nil {
		return err
	}

	file, err := os.Open(fp.FileName)
	if err != nil {
		return err
	}
	// The file is only read, so an error closing it does not affect the result
	defer func() { _ = file.Close() }()

	err = handler.Read(file)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", fp.FileName, err)
	}

	entries, err := handler.Process()
	if err != nil {
		return fmt.Errorf("failed to process %s: %w", fp.FileName, err)
	}

	for entry := range entries {
		if err := fp.processEntry(entry, true, fileGroup); err != nil {
			return err
		}
	}

	if err := handler.Write(fp.Output); err != nil {
		return fmt.Errorf("failed to write %s: %w", fp.FileName, err)
	}

	return nil
}

// processEntry replaces the placeholders of the entry. If fragments are allowed, an entry may be replaced by
// a fragment, whose entries are processed as well, but must not contain further fragments.
func (fp *FileProcessor) processEntry(entry ConfigEntry, allowFragments bool, fileGroup slog.Attr) error {
	current := entry.GetValue()

	// Most entries do not contain any placeholders, so skip them before doing any further work
	if !strings.Contains(current, "${") {
		return nil
	}

	logging.Debug("Processing entry", "entry", entryPath{entry}, fileGroup)

	newVal, err := value.ProcessValue(current)

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

// insertFragment inserts the fragment in place of the entry and processes the entries of the fragment.
// It returns an error when allowFragments is false or the entry does not support fragments.
// Placeholders in inserted entries are processed with further fragments disabled.
// Errors of the insertion and of processing placeholders are returned. Processing then stops, so nothing is written.
func (fp *FileProcessor) insertFragment(entry ConfigEntry, fragment filter.Fragment, allowFragments bool, fileGroup slog.Attr) error {
	if !allowFragments {
		return fmt.Errorf("failed to process the value of %s in %s: fragments cannot contain further fragments", entry.Path(), fp.FileName)
	}

	fragmentEntry, ok := entry.(FragmentConfigEntry)
	if !ok {
		return fmt.Errorf("failed to process the value of %s in %s: fragments are not supported here, only for YAML and JSON values and keys and the content of XML elements", entry.Path(), fp.FileName)
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

	return nil
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
