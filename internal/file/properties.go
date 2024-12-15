package file

import (
	"bytes"
	"io"
	"iter"

	"github.com/magiconair/properties"
)

// PropertiesFileHandler is a handler for properties files
type PropertiesFileHandler struct {
	props   *properties.Properties
	wrapped []ConfigEntry
}

// PropertiesConfigEntry is a configuration entry for properties files
type PropertiesConfigEntry struct {
	key string
	val string
}

// Key returns the key of the configuration entry
func (p *PropertiesConfigEntry) Key() string {
	return p.key
}

// Path returns the path of the configuration entry
func (p *PropertiesConfigEntry) Path() string {
	return p.key
}

// GetValue returns the value of the configuration entry
func (p *PropertiesConfigEntry) GetValue() string {
	return p.val
}

func (p *PropertiesConfigEntry) SetValue(value string) {
	p.val = value
}

// NewPropertiesConfigFileHandler creates a new PropertiesFileHandler
func NewPropertiesConfigFileHandler() *PropertiesFileHandler {
	return &PropertiesFileHandler{}
}

// Read reads the configuration file
func (p *PropertiesFileHandler) Read(source io.Reader) (err error) {
	ldr := properties.Loader{
		Encoding:         properties.UTF8,
		DisableExpansion: true,
		IgnoreMissing:    true,
	}

	buf := new(bytes.Buffer)
	buf.ReadFrom(source)

	props, err := ldr.LoadBytes(buf.Bytes())

	if err != nil {
		return err
	}

	p.props = props

	return nil
}

// Process processes the configuration file
func (p *PropertiesFileHandler) Process() (iter.Seq[ConfigEntry], error) {
	p.wrapped = make([]ConfigEntry, 0)

	for _, key := range p.props.Keys() {
		wrapped := PropertiesConfigEntry{
			key: key,
			val: p.props.GetString(key, ""),
		}

		p.wrapped = append(p.wrapped, &wrapped)
	}

	return func(yield func(ConfigEntry) bool) {
		for _, entry := range p.wrapped {
			if !yield(entry) {
				break
			}
		}
	}, nil
}

// Write writes the configuration file
func (p *PropertiesFileHandler) Write(destination io.Writer) (err error) {
	for _, w := range p.wrapped {
		p.props.Set(w.Key(), w.GetValue())
	}

	_, err = p.props.Write(destination, properties.UTF8)

	return err
}
