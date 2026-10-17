package file

import (
	"fmt"
	"io"
	"iter"
	"log/slog"
	"os"
	"path"
	"strings"

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
	defer file.Close()

	err = handler.Read(file)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", fp.FileName, err)
	}

	entries, err := handler.Process()
	if err != nil {
		return fmt.Errorf("failed to process %s: %w", fp.FileName, err)
	}

	for entry := range entries {
		current := entry.GetValue()

		// Most entries do not contain any placeholders, so skip them before doing any further work
		if !strings.Contains(current, "${") {
			continue
		}

		logging.Debug("Processing entry", "entry", entryPath{entry}, fileGroup)

		newVal, err := value.ProcessValue(current)

		if err != nil {
			return fmt.Errorf("failed to process the value of %s in %s: %w", entry.Path(), fp.FileName, err)
		}

		// Entries store text, so non-string results (e.g. of to_int) are formatted.
		// Handlers may convert the text back to the original type, e.g. JSON numbers.
		newText, ok := newVal.(string)
		if !ok {
			newText = fmt.Sprint(newVal)
		}

		logging.Debug("Setting new value", "entry", entryPath{entry}, "value", newVal, fileGroup)
		entry.SetValue(newText)
	}

	if err := handler.Write(fp.Output); err != nil {
		return fmt.Errorf("failed to write %s: %w", fp.FileName, err)
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
