package file

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"strings"
	"testing"

	"github.com/denglertai/gonfig/internal/general"
	"github.com/denglertai/gonfig/internal/value"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/assert"
)

// TestXmlProcessor reads an XML file, counts its elements and attributes and writes it back unchanged
func TestXmlProcessor(t *testing.T) {
	wd, err := os.Getwd()
	assert.NoError(t, err)

	file := path.Join(wd, "/testdata/xml/customers.xml")

	input, err := os.Open(file)
	assert.NoError(t, err)

	defer func() { assert.NoError(t, input.Close()) }()

	handler := NewXmlConfigFileHandler()

	err = handler.Read(input)
	assert.NoError(t, err)

	entries, err := handler.Process()
	assert.NoError(t, err)
	assert.NotNil(t, entries)

	count := 0
	for entry := range entries {
		count++
		assert.NotNil(t, entry)
	}

	// One entry per attribute and per text node that is not whitespace only
	assert.Equal(t, 13, count)

	output := new(bytes.Buffer)
	assert.NoError(t, handler.Write(output))

	snaps.MatchSnapshot(t, output.String())
}

// TestXmlProcessorEdit sets values of elements and attributes, including characters that need escaping, and compares the output with the snapshot
func TestXmlProcessorEdit(t *testing.T) {
	wd, err := os.Getwd()
	assert.NoError(t, err)

	file := path.Join(wd, "/testdata/xml/customers.xml")

	input, err := os.Open(file)
	assert.NoError(t, err)

	defer func() { assert.NoError(t, input.Close()) }()

	handler := NewXmlConfigFileHandler()

	err = handler.Read(input)
	assert.NoError(t, err)

	entries, err := handler.Process()
	assert.NoError(t, err)
	assert.NotNil(t, entries)

	count := 0
	for entry := range entries {
		count++
		if entry.Key() == "id" {
			entry.SetValue("123")
		}

		if entry.Key() == "name" {
			entry.SetValue("John & Doe")
		}

		assert.NotNil(t, entry)
	}

	// One entry per attribute and per text node that is not whitespace only
	assert.Equal(t, 13, count)

	output := new(bytes.Buffer)
	assert.NoError(t, handler.Write(output))

	snaps.MatchSnapshot(t, output.String())
}

// TestXmlCDataWithJsonNotParsed checks that JSON within a CDATA section is processed as text and written back as CDATA
func TestXmlCDataWithJsonNotParsed(t *testing.T) {
	t.Setenv("SERVICE_NAME", "Service")

	wd, err := os.Getwd()
	assert.NoError(t, err)

	file := path.Join(wd, "/testdata/xml/cdata.xml")
	input, err := os.Open(file)
	assert.NoError(t, err)
	defer func() { assert.NoError(t, input.Close()) }()

	handler := NewXmlConfigFileHandler()
	err = handler.Read(input)
	assert.NoError(t, err)

	entries, err := handler.Process()
	assert.NoError(t, err)
	assert.NotNil(t, entries)

	var cdataText string
	var foundPattern bool
	for entry := range entries {
		// Process the values the same way the FileProcessor does
		newVal, err := value.ProcessValue(entry.GetValue())
		assert.NoError(t, err)
		entry.SetValue(fmt.Sprintf("%v", newVal))

		if entry.Key() == "pattern" {
			foundPattern = true
			cdataText = entry.GetValue()
		}
	}
	assert.True(t, foundPattern, "Element <pattern> should be found")
	expected := `
						       {
							 "timestamp": "%date{ISO8601}",
							 "logger": "%logger{0}",
							 "service_name": "Service",
							 "component": "Component",
							 "level": "%level",
							 "thread": "%thread",
							 "ndc": "%X{NDC}",
							 "message": "%message",
							 "traceId": "%mdc{traceId}"
						       }`
	normalize := func(s string) string {
		return strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(s), "\t", ""), " ", "")
	}
	assert.Equal(t, normalize(expected), normalize(cdataText), "CDATA JSON content should be present as text, not parsed")

	output := new(bytes.Buffer)
	assert.NoError(t, handler.Write(output))

	snaps.MatchSnapshot(t, output.String())
}

// TestCDataJsonSyntaxValid checks that the JSON within the CDATA section of the test data is valid JSON
func TestCDataJsonSyntaxValid(t *testing.T) {
	wd, err := os.Getwd()
	assert.NoError(t, err)

	file := path.Join(wd, "/testdata/xml/cdata.xml")
	input, err := os.Open(file)
	assert.NoError(t, err)
	defer func() { assert.NoError(t, input.Close()) }()

	handler := NewXmlConfigFileHandler()
	err = handler.Read(input)
	assert.NoError(t, err)

	entries, err := handler.Process()
	assert.NoError(t, err)
	assert.NotNil(t, entries)

	var cdataJson string
	var foundPattern bool
	for entry := range entries {
		if entry.Key() == "pattern" {
			cdataJson = entry.GetValue()
			foundPattern = true
		}
	}
	assert.True(t, foundPattern, "Element <pattern> should be found")

	// Check if CDATA content is valid JSON
	var js map[string]interface{}
	err = json.Unmarshal([]byte(strings.TrimSpace(cdataJson)), &js)
	assert.NoError(t, err, "CDATA content should be valid JSON")
}

func TestXmlCDataProcessedOnce(t *testing.T) {
	// The resolved value contains a placeholder itself, which must not be expanded again
	t.Setenv("CDATA_OUTER", "${CDATA_INNER}")
	t.Setenv("CDATA_INNER", "expanded twice")

	file := path.Join(t.TempDir(), "cdata.xml")
	assert.NoError(t, os.WriteFile(file, []byte(`<r><a><![CDATA[${CDATA_OUTER}]]></a></r>`), 0o644))

	output := new(bytes.Buffer)
	assert.NoError(t, NewFileProcessor(file, general.Undefined, output).Process())
	assert.Equal(t, `<r><a><![CDATA[${CDATA_INNER}]]></a></r>`, output.String())
}

func TestXmlNamespacedAttributes(t *testing.T) {
	handler := NewXmlConfigFileHandler()
	assert.NoError(t, handler.Read(strings.NewReader(`<r xmlns:x="urn:x"><a x:loc="old" plain="old"/></r>`)))

	entries, err := handler.Process()
	assert.NoError(t, err)
	for entry := range entries {
		if entry.GetValue() == "old" {
			entry.SetValue("new")
			assert.Equal(t, "new", entry.GetValue())
		}
	}

	output := new(bytes.Buffer)
	assert.NoError(t, handler.Write(output))
	assert.Equal(t, `<r xmlns:x="urn:x"><a x:loc="new" plain="new"/></r>`, output.String())
}

// TestXmlTextNodes checks that placeholders in every text node are processed, including the root element's text,
// text after child elements and comments, and text around CDATA sections, which used to be lost
func TestXmlTextNodes(t *testing.T) {
	t.Setenv("XML_TEXT", "v")
	t.Setenv("XML_SPECIAL", "a<b&c")

	testCases := []struct {
		desc    string
		content string
		want    string
	}{
		{desc: "root element text", content: `<r>${XML_TEXT}<a/></r>`, want: `<r>v<a/></r>`},
		{desc: "text after a child element", content: `<r><e>old<a/>${XML_TEXT}</e></r>`, want: `<r><e>old<a/>v</e></r>`},
		{desc: "several text nodes", content: `<r><e>${XML_TEXT}<a/>${XML_SPECIAL}<!-- c -->${XML_TEXT}</e></r>`, want: `<r><e>v<a/>a&lt;b&amp;c<!-- c -->v</e></r>`},
		{desc: "text around a CDATA section", content: `<r><e>lead ${XML_TEXT}<![CDATA[${XML_SPECIAL} <raw>]]>tail</e></r>`, want: `<r><e>lead v<![CDATA[a<b&c <raw>]]>tail</e></r>`},
		{desc: "whitespace around a CDATA section", content: "<r>\n  <e>\n    <![CDATA[${XML_TEXT}]]>\n  </e>\n</r>", want: "<r>\n  <e>\n    <![CDATA[v]]>\n  </e>\n</r>"},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			file := path.Join(t.TempDir(), "config.xml")
			assert.NoError(t, os.WriteFile(file, []byte(tC.content), 0o644))

			var output bytes.Buffer
			assert.NoError(t, NewFileProcessor(file, general.XML, &output).Process())
			assert.Equal(t, tC.want, output.String())
		})
	}
}

// TestXmlWhitespaceTextNodesHaveNoEntries checks that only attributes and text nodes that are not whitespace only are entries
func TestXmlWhitespaceTextNodesHaveNoEntries(t *testing.T) {
	handler := NewXmlConfigFileHandler()
	assert.NoError(t, handler.Read(strings.NewReader("<r a=\"1\">\n  <e>text</e>\n  <f/>\n</r>")))

	entries, err := handler.Process()
	assert.NoError(t, err)

	var values []string
	for entry := range entries {
		values = append(values, entry.Path()+"="+entry.GetValue())
	}
	assert.Equal(t, []string{"/r@a=1", "/r/e=text"}, values)
}
