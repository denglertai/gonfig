package file

import (
	"errors"
	"fmt"
	"io"
	"iter"
	"regexp"
	"slices"

	"github.com/beevik/etree"
	"github.com/denglertai/gonfig/internal/filter"
)

// XmlAttributeConfigEntry represents a single configuration entry for an attribute
type XmlAttributeConfigEntry struct {
	element *etree.Element
	// key is the local name of the attribute
	key string
	// fullKey includes the namespace prefix of the attribute, if any
	fullKey string
}

// newXmlAttributeConfigEntry creates a configuration entry for the attribute of the element
func newXmlAttributeConfigEntry(element *etree.Element, attr etree.Attr) *XmlAttributeConfigEntry {
	return &XmlAttributeConfigEntry{
		element: element,
		key:     attr.Key,
		fullKey: attr.FullKey(),
	}
}

// Key returns the key of the configuration entry
func (x *XmlAttributeConfigEntry) Key() string {
	return x.key
}

// Path returns the path of the configuration entry
func (x *XmlAttributeConfigEntry) Path() string {
	return fmt.Sprintf("%s@%s", x.element.GetPath(), x.fullKey)
}

// GetValue returns the value of the configuration entry
func (x *XmlAttributeConfigEntry) GetValue() string {
	return x.element.SelectAttrValue(x.fullKey, "")
}

// SetValue sets the value of the configuration entry
func (x *XmlAttributeConfigEntry) SetValue(value string) {
	// Replaces the value of the existing attribute, including its namespace prefix
	x.element.CreateAttr(x.fullKey, value)
}

// XmlElementConfigEntry represents a single configuration entry for an element
type XmlElementConfigEntry struct {
	element   *etree.Element
	fromCData bool
	// handler collects the entries of inserted fragments
	handler *XmlConfigFileHandler
}

// Key returns the key of the configuration entry
func (x *XmlElementConfigEntry) Key() string {
	return x.element.Tag
}

// Path returns the path of the configuration entry
func (x *XmlElementConfigEntry) Path() string {
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

// SetFragment replaces the content of the element by the fragment, which may consist of several elements and text.
// An empty fragment removes the content.
// It returns entries for all inserted elements, including nested ones, and their attributes for placeholder processing.
// It returns an error if f.IsList is set or f.Text cannot be parsed as XML content.
func (x *XmlElementConfigEntry) SetFragment(f filter.Fragment) ([]ConfigEntry, error) {
	// XML has no lists, so a JSON array would only become text
	if f.IsList {
		return nil, errors.New("lists can only be inserted into YAML and JSON files")
	}

	fragment, err := parseXmlFragment(f.Text)
	if err != nil {
		return nil, err
	}

	// Children are removed from the end, since removing a child shifts all following children,
	// which would take quadratic time for fragments with many elements
	for i := len(x.element.Child) - 1; i >= 0; i-- {
		x.element.RemoveChildAt(i)
	}
	children := make([]etree.Token, len(fragment.Child))
	for i := len(fragment.Child) - 1; i >= 0; i-- {
		children[i] = fragment.RemoveChildAt(i)
	}
	for _, child := range children {
		x.element.AddChild(child)
	}
	x.fromCData = false

	return x.handler.collectEntries(func() { x.handler.handleChildElements(x.element.ChildElements()) }), nil
}

// xmlDeclarationRe matches an XML declaration at the start of a fragment, e.g. of a fragment file
var xmlDeclarationRe = regexp.MustCompile(`^\s*<\?xml[^>]*\?>`)

// parseXmlFragment parses a fragment into the children of a wrapping element, so it may contain several elements and text
func parseXmlFragment(text string) (*etree.Element, error) {
	document := etree.NewDocument()
	document.ReadSettings.PreserveCData = true

	wrapped := "<gonfig-fragment>" + xmlDeclarationRe.ReplaceAllString(text, "") + "</gonfig-fragment>"
	if err := document.ReadFromString(wrapped); err != nil {
		return nil, fmt.Errorf("invalid XML fragment: %w", err)
	}

	return document.Root(), nil
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
	// Enable PreserveCData to keep CDATA sections intact
	x.document.ReadSettings.PreserveCData = true
	if _, err := x.document.ReadFrom(source); err != nil {
		return err
	}

	root := x.document.Root()
	if root == nil {
		return fmt.Errorf("no root element in file found")
	}

	for _, attr := range root.Attr {
		x.entries = append(x.entries, newXmlAttributeConfigEntry(root, attr))
	}

	x.handleChildElements(root.ChildElements())

	return nil
}

// collectEntries returns the entries added while inserting a fragment
func (x *XmlConfigFileHandler) collectEntries(insert func()) []ConfigEntry {
	before := len(x.entries)
	insert()
	return slices.Clone(x.entries[before:])
}

// handleChildElements recursively processes the child elements and their attributes
func (x *XmlConfigFileHandler) handleChildElements(elements []*etree.Element) {
	for _, element := range elements {
		fromCData := false
		for _, child := range element.Child {
			if cdata, ok := child.(*etree.CharData); ok && cdata.IsCData() {
				// Expose the CDATA content as the element's text; it is processed
				// together with all other entries and written back as CDATA
				element.SetText(cdata.Data)
				fromCData = true
				break
			}
		}

		x.entries = append(x.entries, &XmlElementConfigEntry{
			element:   element,
			fromCData: fromCData,
			handler:   x,
		})

		for _, attr := range element.Attr {
			x.entries = append(x.entries, newXmlAttributeConfigEntry(element, attr))
		}

		x.handleChildElements(element.ChildElements())
	}
}

// Process processes the configuration file and returns the configuration entries
func (x *XmlConfigFileHandler) Process() (iter.Seq[ConfigEntry], error) {
	return slices.Values(x.entries), nil
}

// Write writes the configuration entries to the target
func (x *XmlConfigFileHandler) Write(target io.Writer) error {
	// Setting the text replaces CDATA sections with plain text, so restore them
	for _, entry := range x.entries {
		if elem, ok := entry.(*XmlElementConfigEntry); ok && elem.fromCData {
			elem.element.SetCData(elem.element.Text())
		}
	}
	_, err := x.document.WriteTo(target)
	return err
}
