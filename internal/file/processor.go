package file

import (
	"fmt"
	"io"
	"iter"
	"os"

	"github.com/denglertai/gonfig/internal/general"
)

// ConfigEntry represents a single configuration entry
type ConfigEntry interface {
	// Key returns the key of the configuration entry
	Key() string
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
	return &FileProcessor{
		FileName: fileName,
		FileType: fileType,
		Output:   output,
	}
}

// Process processes the file
func (fp *FileProcessor) Process() error {
	var handler ConfigFileHandler

	file, err := os.Open(fp.FileName)
	if err != nil {
		return err
	}
	defer file.Close()

	err = handler.Read(file)
	if err != nil {
		return err
	}

	entries, err := handler.Process()
	if err != nil {
		return err
	}

	fmt.Print(entries)

	for entry := range entries {
		fmt.Print(entry)
	}

	err = handler.Write(fp.Output)

	return nil
}
