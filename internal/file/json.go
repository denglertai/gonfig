package file

import (
	"io"
	"iter"
)

// JsonConfigEntry represents a single configuration entry
type JsonConfigEntry struct {
	value  string
	edited bool
}

// Key returns the key of the configuration entry
func (j *JsonConfigEntry) Key() string {
	return "x.attribute.Key"
}

// GetValue returns the value of the configuration entry
func (j *JsonConfigEntry) GetValue() string {
	return "x.attribute.Value"
}

// SetValue sets the value of the configuration entry
func (j *JsonConfigEntry) SetValue(value string) {

}

// JsonConfigFileHandler represents a configuration file handler
type JsonConfigFileHandler struct {
}

// NewXmlConfigFileHandler creates a new XML configuration file handler
func NewJsonConfigFileHandler() *JsonConfigFileHandler {
	return &JsonConfigFileHandler{}
}

// Read reads the configuration file
func (j *JsonConfigFileHandler) Read(source io.Reader) (err error) {
	return err
}

// Process processes the configuration file and returns the configuration entries
func (j *JsonConfigFileHandler) Process() (iter.Seq[ConfigEntry], error) {
	return func(yield func(ConfigEntry) bool) {

	}, nil
}

// Write writes the configuration entries to the target
func (j *JsonConfigFileHandler) Write(target io.Writer) error {
	return nil
}
