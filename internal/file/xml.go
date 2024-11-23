package file

import (
	"bytes"
	"fmt"
	"io"
	"iter"

	"github.com/beevik/etree"
)

// XmlConfigEntry represents a single configuration entry for an attribute
type XmlAttributeConfigEntry struct {
	attribute *etree.Attr
	value     string
	edited    bool
}

// Key returns the key of the configuration entry
func (x *XmlAttributeConfigEntry) Key() string {
	return x.attribute.Key
}

// GetValue returns the value of the configuration entry
func (x *XmlAttributeConfigEntry) GetValue() string {
	return x.attribute.Value
}

// SetValue sets the value of the configuration entry
func (x *XmlAttributeConfigEntry) SetValue(value string) {
	x.value = value
	x.edited = true
}

// XmlConfigEntry represents a single configuration entry for an element
type XmlElementConfigEntry struct {
	element *etree.Element
}

// Key returns the key of the configuration entry
func (x *XmlElementConfigEntry) Key() string {
	return x.element.Tag
}

// GetValue returns the value of the configuration entry
func (x *XmlElementConfigEntry) GetValue() string {
	return x.element.Text()
}

// SetValue sets the value of the configuration entry
func (x *XmlElementConfigEntry) SetValue(value string) {
	x.element.SetText(value)
}

// XmlConfigFileHandler represents a configuration file handler
type XmlConfigFileHandler struct {
	document *etree.Document
	entries  []ConfigEntry
}

// NewXmlConfigFileHandler creates a new XML configuration file handler
func NewXmlConfigFileHandler() *XmlConfigFileHandler {
	return &XmlConfigFileHandler{
		document: etree.NewDocument(),
		entries:  make([]ConfigEntry, 0),
	}
}

// Read reads the configuration file
func (x *XmlConfigFileHandler) Read(source io.Reader) error {
	buf := new(bytes.Buffer)
	buf.ReadFrom(source)

	err := x.document.ReadFromBytes(buf.Bytes())

	if err != nil {
		return err
	}

	root := x.document.Root()
	if root == nil {
		return fmt.Errorf("no root element in file found")
	}

	for _, attr := range root.Attr {
		x.entries = append(x.entries, &XmlAttributeConfigEntry{
			attribute: &attr,
		})
	}

	x.handleChildElements(root.ChildElements())

	return nil
}

// handleChildElements recursively processes the child elements and their attributes
func (x *XmlConfigFileHandler) handleChildElements(elements []*etree.Element) {
	for _, element := range elements {
		x.entries = append(x.entries, &XmlElementConfigEntry{
			element: element,
		})

		for _, attr := range element.Attr {
			x.entries = append(x.entries, &XmlAttributeConfigEntry{
				attribute: &attr,
			})
		}

		x.handleChildElements(element.ChildElements())
	}
}

// Process processes the configuration file and returns the configuration entries
func (x *XmlConfigFileHandler) Process() (iter.Seq[ConfigEntry], error) {
	return func(yield func(ConfigEntry) bool) {
		for _, entry := range x.entries {
			if !yield(entry) {
				return
			}
		}
	}, nil
}

// Write writes the configuration entries to the target
func (x *XmlConfigFileHandler) Write(target io.Writer) error {
	// Need to re-process all Attribute entries to ensure they are written to the document
	for _, entry := range x.entries {
		if attr, ok := entry.(*XmlAttributeConfigEntry); !ok {
			continue
		} else if attr.edited {
			attr.attribute.Element().CreateAttr(attr.attribute.Key, attr.value)
		}
	}

	_, err := x.document.WriteTo(target)

	return err
}
