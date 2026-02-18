package file

import (
	"bytes"
	"fmt"
	"io"
	"iter"
)

// PlainFileProcessor processes a file in plain text format without any specific structure or format.
type PlainFileProcessor struct {
	lines []*plainFileLine
}

// plainFileLine represents a single line in a plain text file, treated as a configuration entry with the line content as the key and value.
type plainFileLine struct {
	value      string
	eol        string
	lineNumber uint32
}

// Key returns the key of the configuration entry, which is the line number in this case.
func (p *plainFileLine) Key() string {
	return fmt.Sprint(p.lineNumber)
}

// Path returns the path of the configuration entry, which is the line number in this case.
func (p *plainFileLine) Path() string {
	return fmt.Sprint(p.lineNumber)
}

// GetValue returns the value of the configuration entry, which is the line content in this case.
func (p *plainFileLine) GetValue() string {
	return p.value
}

// SetValue sets the value of the configuration entry, which is the line content in this case.
func (p *plainFileLine) SetValue(value string) {
	p.value = value
}

func NewPlainFileProcessor() *PlainFileProcessor {
	return &PlainFileProcessor{}
}

// Read reads the content of the plain text file and stores it in the processor, treating each line as a separate entry with the line content as the key and value.
func (p *PlainFileProcessor) Read(source io.Reader) error {
	buf := new(bytes.Buffer)
	_, err := buf.ReadFrom(source)
	if err != nil {
		return err
	}
	// Split the content into lines and create a plainFileLine for each line with the line content as the value and the end of line character as the eol
	lines := bytes.Split(buf.Bytes(), []byte{'\n'})
	p.lines = make([]*plainFileLine, len(lines))
	for i, line := range lines {
		p.lines[i] = &plainFileLine{
			value:      string(line),
			eol:        "\n",
			lineNumber: uint32(i),
		}
	}
	return nil
}

// Process processes the configuration file and returns the configuration entries, treating each line as a separate entry with the line content as the key and value.
func (p *PlainFileProcessor) Process() (iter.Seq[ConfigEntry], error) {
	// Return a config entry line by line, treating each line as a separate entry with the line content as the key and value
	return func(yield func(ConfigEntry) bool) {
		for _, line := range p.lines {
			if !yield(line) {
				break
			}
		}
	}, nil
}

// Write writes the configuration entries back to the target, preserving the original line content and end of line characters.
func (p *PlainFileProcessor) Write(target io.Writer) error {
	for _, line := range p.lines {
		_, err := target.Write([]byte(line.value + line.eol))
		if err != nil {
			return err
		}
	}
	return nil
}
