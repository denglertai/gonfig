package file

import (
	"bytes"
	"fmt"
	"io"
	"iter"

	"github.com/beevik/etree"
	"github.com/denglertai/gonfig/internal/value"
)

// XmlConfigEntry represents a single configuration entry for an attribute
type XmlAttributeConfigEntry struct {
	attribute *etree.Attr
	value     string
	edited    bool
	pathBuilt bool
	path      string
}

// Key returns the key of the configuration entry
func (x *XmlAttributeConfigEntry) Key() string {
	return x.attribute.Key
}

// Path returns the path of the configuration entry
func (x *XmlAttributeConfigEntry) Path() string {
	if !x.pathBuilt {
		x.path = fmt.Sprintf("%s@%s", x.attribute.Element().GetPath(), x.attribute.Key)
		x.pathBuilt = true
	}

	return x.path
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
	element   *etree.Element
	pathBuilt bool
	path      string
	fromCData bool
}

// Key returns the key of the configuration entry
func (x *XmlElementConfigEntry) Key() string {
	return x.element.Tag
}

// Path returns the path of the configuration entry
func (x *XmlElementConfigEntry) Path() string {
	if !x.pathBuilt {
		x.path = x.element.GetPath()
		x.pathBuilt = true
	}

	return x.element.GetPath()
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

	// Enable PreserveCData to keep CDATA sections intact
	x.document.ReadSettings.PreserveCData = true
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
		fromCData := false
		for _, child := range element.Child {
			if cdata, ok := child.(*etree.CharData); ok && cdata.IsCData() {
				processed := cdata.Data
				if val, err := value.ProcessValue(cdata.Data); err == nil {
					if str, ok := val.(string); ok {
						processed = str
					}
				}
				element.SetText(processed)
				fromCData = true
				break
			}
		}

		x.entries = append(x.entries, &XmlElementConfigEntry{
			element:   element,
			fromCData: fromCData,
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
		if attr, ok := entry.(*XmlAttributeConfigEntry); ok {
			if attr.edited {
				attr.attribute.Element().CreateAttr(attr.attribute.Key, attr.value)
			}
		}
		if elem, ok := entry.(*XmlElementConfigEntry); ok {
			val := elem.element.Text()
			if elem.fromCData {
				elem.element.SetCData(val)
			} else {
				elem.element.SetText(val)
			}
		}
	}
	_, err := x.document.WriteTo(target)
	return err
}
