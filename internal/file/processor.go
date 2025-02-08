package file

import (
	"fmt"
	"io"
	"iter"
	"log/slog"
	"os"
	"path"

	"github.com/denglertai/gonfig/internal/general"
	"github.com/denglertai/gonfig/internal/value"
	"github.com/denglertai/gonfig/pkg/logging"
)

// ConfigEntry represents a single configuration entry
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

// ConfigFileHandler represents a configuration file handler
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
		ext := path.Ext(fileName)
		// strip the leading dot
		ext = ext[1:]
		fileType = general.FileType(ext)
		logging.Debug("File type not provided, using the file's extension", "file", fileName, "type", fileType)
	}

	return &FileProcessor{
		FileName: fileName,
		FileType: fileType,
		Output:   output,
	}
}

// Process processes the file
func (fp *FileProcessor) Process() error {
	fileGroup := slog.Group("file", "name", fp.FileName, "type", fp.FileType)

	handler, err := fp.getFileProcessor()
	if err != nil {
		logging.Error("Error initializing file processor", "err", err, fileGroup)
		return err
	}

	file, err := os.Open(fp.FileName)
	if err != nil {
		return err
	}
	defer file.Close()

	err = handler.Read(file)
	if err != nil {
		logging.Error("Failed to read the file", "err", err, fileGroup)
		return err
	}

	entries, err := handler.Process()
	if err != nil {
		logging.Error("Failed to process the file", "err", err, fileGroup)
		return err
	}

	for entry := range entries {
		logging.Debug("Processing entry", "entry", entry.Path(), fileGroup)

		newVal, err := value.ProcessValue(entry.GetValue())

		if err != nil {
			logging.Error("Failed to process the value", "err", err, "entry", entry.Path(), fileGroup)
			return err
		}

		logging.Debug("Setting new value", "entry", entry.Path(), "value", newVal, fileGroup)
		entry.SetValue(fmt.Sprintf("%v", newVal))
	}

	return handler.Write(fp.Output)
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
	default:
		return nil, fmt.Errorf("unsupported file type: %v", fp.FileType)
	}
}
