package file

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"iter"
	"regexp"
	"slices"
	"strings"

	"github.com/beevik/etree"
	"github.com/denglertai/gonfig/internal/filter"
	"github.com/denglertai/gonfig/internal/value"
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

// mask replaces the value by value.Mask
func (x *XmlAttributeConfigEntry) mask() {
	x.SetValue(value.Mask)
}

// XmlTextConfigEntry represents a text node of an element, which is either plain text or a CDATA section.
// An element containing child elements may have several text nodes, e.g. before and after a child element.
type XmlTextConfigEntry struct {
	text *etree.CharData
	// element contains the text node
	element *etree.Element
	// handler collects the entries of inserted fragments
	handler *XmlConfigFileHandler
}

// Key returns the key of the configuration entry, which is the tag of the element containing the text
func (x *XmlTextConfigEntry) Key() string {
	return x.element.Tag
}

// Path returns the path of the configuration entry, which is the path of the element containing the text
func (x *XmlTextConfigEntry) Path() string {
	return x.element.GetPath()
}

// GetValue returns the value of the configuration entry
func (x *XmlTextConfigEntry) GetValue() string {
	return x.text.Data
}

// SetValue sets the value of the configuration entry. A CDATA section stays a CDATA section.
func (x *XmlTextConfigEntry) SetValue(value string) {
	x.text.Data = value
}

// mask replaces the text by value.Mask
func (x *XmlTextConfigEntry) mask() {
	x.SetValue(value.Mask)
}

// SetFragment replaces the whole content of the element containing the text by the fragment, which may consist of
// several elements and text. An empty fragment removes the content.
// It returns entries for all inserted elements, including nested ones, and their attributes for placeholder processing.
// It returns an error if f.IsList is set or f.Text cannot be parsed as XML content.
func (x *XmlTextConfigEntry) SetFragment(f filter.Fragment) ([]ConfigEntry, error) {
	// XML has no lists, so a JSON array would only become text
	if f.IsList {
		return nil, errors.New("lists can only be inserted into YAML, JSON and TOML files")
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

	// The attributes of the element already have entries, so only the new content is collected
	return x.handler.collectEntries(func() { x.handler.handleContent(x.element) }), nil
}

// xmlDeclarationRe matches an XML declaration at the start of a fragment, e.g. of a fragment file
var xmlDeclarationRe = regexp.MustCompile(`^\s*<\?xml[^>]*\?>`)

// parseXmlFragment parses a fragment into the children of a wrapping element, so it may contain several elements and text
func parseXmlFragment(text string) (*etree.Element, error) {
	document := etree.NewDocument()
	document.ReadSettings.PreserveCData = true

	wrapped := "<gonfig-fragment>" + xmlDeclarationRe.ReplaceAllString(text, "") + "</gonfig-fragment>"
	if err := document.ReadFromString(wrapped); err != nil {
		// The wrapping element is on the same line, so the line number matches the fragment
		location := ""
		var syntaxError *xml.SyntaxError
		if errors.As(err, &syntaxError) {
			location = fmt.Sprintf("on line %d", syntaxError.Line)
		}
		return nil, fragmentError("XML", location, err)
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
	// Keep CDATA sections as text nodes of their own, so they are written back as CDATA
	x.document.ReadSettings.PreserveCData = true
	if _, err := x.document.ReadFrom(source); err != nil {
		return err
	}

	root := x.document.Root()
	if root == nil {
		return fmt.Errorf("no root element in file found")
	}

	x.handleElement(root)

	return nil
}

// collectEntries returns the entries added while inserting a fragment
func (x *XmlConfigFileHandler) collectEntries(insert func()) []ConfigEntry {
	before := len(x.entries)
	insert()
	return slices.Clone(x.entries[before:])
}

// handleElement collects the entries of the attributes and the content of the element
func (x *XmlConfigFileHandler) handleElement(element *etree.Element) {
	for _, attr := range element.Attr {
		x.entries = append(x.entries, newXmlAttributeConfigEntry(element, attr))
	}
	x.handleContent(element)
}

// handleContent collects the entries of the text nodes and child elements of the element in document order.
// Text nodes consisting of whitespace only, e.g. the indentation between elements, cannot contain placeholders.
func (x *XmlConfigFileHandler) handleContent(element *etree.Element) {
	for _, child := range element.Child {
		switch token := child.(type) {
		case *etree.CharData:
			if strings.TrimSpace(token.Data) != "" {
				x.entries = append(x.entries, &XmlTextConfigEntry{text: token, element: element, handler: x})
			}
		case *etree.Element:
			x.handleElement(token)
		}
	}
}

// Process processes the configuration file and returns the configuration entries
func (x *XmlConfigFileHandler) Process() (iter.Seq[ConfigEntry], error) {
	return slices.Values(x.entries), nil
}

// Write writes the configuration entries to the target
func (x *XmlConfigFileHandler) Write(target io.Writer) error {
	_, err := x.document.WriteTo(target)
	return err
}
