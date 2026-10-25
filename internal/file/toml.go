package file

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"iter"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/denglertai/gonfig/internal/filter"
	"github.com/pelletier/go-toml/v2"
	"github.com/pelletier/go-toml/v2/unstable"
)

// TomlFileHandler is a handler for TOML files. The parser of go-toml provides the position of every string and key
// in the file, so only the strings and keys whose value changed are replaced and everything else is written as read,
// including comments, whitespace, the quotes of unchanged strings and line endings.
type TomlFileHandler struct {
	// data is the content of the file without the BOM
	data []byte
	// entries are the strings and keys containing placeholders, ordered by their position in data
	entries []*TomlConfigEntry
	// bom is set if the file starts with a UTF-8 byte order mark, which go-toml rejects
	bom bool
}

// TomlConfigEntry is a configuration entry for TOML files: a string value, or a key containing a placeholder
type TomlConfigEntry struct {
	key  string
	path string
	val  string
	// original is the value as read, so strings are only rewritten if their value changed
	original string
	// raw is the string or key as written in the file, including its quotes, and offset its position in the data
	raw    string
	offset int
	// typed is the text of a number or boolean set by SetTypedValue, which is written instead of a string
	typed string
	isKey bool
}

// Key returns the key of the configuration entry, or the index of an array item
func (t *TomlConfigEntry) Key() string {
	return t.key
}

// Path returns the path of the configuration entry, with the keys of tables and the indexes of arrays separated by dots
func (t *TomlConfigEntry) Path() string {
	return t.path
}

// GetValue returns the value of the configuration entry
func (t *TomlConfigEntry) GetValue() string {
	return t.val
}

// SetValue sets the value of the configuration entry, or renames the key
func (t *TomlConfigEntry) SetValue(value string) {
	t.val = value
	t.typed = ""
}

// SetTypedValue sets an integer, float or boolean, which replaces the string. Keys are always strings and TOML
// integers have 64 bits, so keys and larger unsigned integers are not set.
func (t *TomlConfigEntry) SetTypedValue(value any) bool {
	tv, ok := newTypedValue(value)
	if !ok || t.isKey {
		return false
	}
	if _, err := strconv.ParseInt(tv.text, 10, 64); tv.kind == typedInt && err != nil {
		return false
	}

	t.val = tv.text
	t.typed = tv.text
	return true
}

// changed returns whether the entry has to be rewritten
func (t *TomlConfigEntry) changed() bool {
	return t.typed != "" || t.val != t.original
}

// NewTomlConfigFileHandler creates a new TomlFileHandler
func NewTomlConfigFileHandler() *TomlFileHandler {
	return &TomlFileHandler{}
}

// keepsFormat returns true, since everything but changed strings and keys is written as read and the handler keeps the BOM
func (t *TomlFileHandler) keepsFormat() bool {
	return true
}

// Read reads the configuration file and checks that it is valid TOML, e.g. without duplicate keys,
// which the parser providing the positions doesn't check
func (t *TomlFileHandler) Read(source io.Reader) error {
	data, err := io.ReadAll(source)
	if err != nil {
		return err
	}

	t.bom = bytes.HasPrefix(data, utf8BOM)
	t.data = bytes.TrimPrefix(data, utf8BOM)
	t.entries = nil

	var document map[string]any
	if err := toml.Unmarshal(t.data, &document); err != nil {
		return fmt.Errorf("line %d: %w", tomlLine(err), err)
	}

	parser := unstable.Parser{}
	parser.Reset(t.data)
	collector := tomlCollector{parser: &parser, arrayTables: map[string]int{}}
	for parser.NextExpression() {
		collector.expression(parser.Expression())
	}
	// The document was valid for Unmarshal, so the parser using the same grammar doesn't fail
	t.entries = collector.entries
	return nil
}

// Process processes the configuration file and returns an entry for every string value and every key containing a placeholder
func (t *TomlFileHandler) Process() (iter.Seq[ConfigEntry], error) {
	return func(yield func(ConfigEntry) bool) {
		for _, entry := range t.entries {
			if !yield(entry) {
				return
			}
		}
	}, nil
}

// Write writes the configuration file with the changed strings and keys replaced. Renamed keys may collide with
// other keys, so the result is checked if a key changed.
func (t *TomlFileHandler) Write(destination io.Writer) error {
	var result bytes.Buffer
	result.Grow(len(t.data))
	if t.bom {
		result.Write(utf8BOM)
	}

	written := 0
	keyChanged := false
	for _, entry := range t.entries {
		if !entry.changed() {
			continue
		}
		keyChanged = keyChanged || entry.isKey
		result.Write(t.data[written:entry.offset])
		result.WriteString(entry.encode())
		written = entry.offset + len(entry.raw)
	}
	result.Write(t.data[written:])

	if keyChanged {
		if err := checkRenamedKeys(bytes.TrimPrefix(result.Bytes(), utf8BOM)); err != nil {
			return err
		}
	}

	_, err := destination.Write(result.Bytes())
	return err
}

// checkRenamedKeys returns an error if the document with renamed keys is invalid, e.g. because a key is defined twice.
// The keys may contain values of placeholders, so the message of go-toml naming the key is only included if values
// are allowed in errors, see filter.SetValuesInErrors.
func checkRenamedKeys(data []byte) error {
	var document map[string]any
	err := toml.Unmarshal(data, &document)
	if err == nil {
		return nil
	}

	if filter.ValuesInErrors() {
		return fmt.Errorf("the renamed keys make the TOML document invalid: %w", err)
	}
	return fmt.Errorf("the renamed keys make the TOML document invalid on line %d, e.g. because a key is defined twice", tomlLine(err))
}

// tomlLine returns the line of an error of go-toml, which returns a DecodeError for every invalid document
// when unmarshalling into a map, or 0 for other errors
func tomlLine(err error) int {
	row := 0
	if decodeError := (*toml.DecodeError)(nil); errors.As(err, &decodeError) {
		row, _ = decodeError.Position()
	}
	return row
}

// tomlCollector collects the entries of a TOML document while walking the expressions of the parser
type tomlCollector struct {
	parser  *unstable.Parser
	entries []*TomlConfigEntry
	// table is the path of the current table, set by the last table header
	table string
	// arrayTables counts the tables of each array of tables by its path, e.g. 2 for "servers" after two [[servers]],
	// so the path of the current table contains the index of the last one, e.g. "servers.1"
	arrayTables map[string]int
}

// expression collects the entries of a top-level expression: a table header, an array of tables header or a key/value pair
func (c *tomlCollector) expression(node *unstable.Node) {
	switch node.Kind {
	case unstable.Table:
		c.table = c.header(node.Key(), false)
	case unstable.ArrayTable:
		c.table = c.header(node.Key(), true)
	case unstable.KeyValue:
		c.keyValue(node, c.table)
	}
}

// header returns the path of the table of a header and collects its keys. Tables within an array of tables refer to
// its last table, so the index of the last table is added after the key of an array of tables.
// For the header of an array of tables, a new table is added to the array.
func (c *tomlCollector) header(keys unstable.Iterator, arrayTable bool) string {
	path := ""
	for keys.Next() {
		path = c.key(keys.Node(), path)
		if arrayTable && keys.IsLast() {
			c.arrayTables[path]++
		}
		if count, ok := c.arrayTables[path]; ok {
			path = appendToPath(path, strconv.Itoa(count-1))
		}
	}
	return path
}

// keyValue collects the entries of a key/value pair within the table at path.
// Dotted keys like a.b define nested tables, so each part of the key is collected.
func (c *tomlCollector) keyValue(node *unstable.Node, path string) {
	key := ""
	for keys := node.Key(); keys.Next(); {
		key = string(keys.Node().Data)
		path = c.key(keys.Node(), path)
	}
	c.value(node.Value(), path, key)
}

// key collects the key node if it contains a placeholder and returns the path of the key within the parent path
func (c *tomlCollector) key(node *unstable.Node, parent string) string {
	key := string(node.Data)
	path := appendToPath(parent, key)
	if strings.Contains(key, "${") {
		c.add(node, key, path, true)
	}
	return path
}

// value collects the strings of a value: a string, or the strings within an array or inline table
func (c *tomlCollector) value(node *unstable.Node, path string, key string) {
	switch node.Kind {
	case unstable.String:
		c.add(node, key, path, false)
	case unstable.Array:
		index := 0
		for items := node.Children(); items.Next(); index++ {
			is := strconv.Itoa(index)
			c.value(items.Node(), appendToPath(path, is), is)
		}
	case unstable.InlineTable:
		for keyValues := node.Children(); keyValues.Next(); {
			c.keyValue(keyValues.Node(), path)
		}
	}
}

// add adds an entry for the string or key node
func (c *tomlCollector) add(node *unstable.Node, key string, path string, isKey bool) {
	text := string(node.Data)
	c.entries = append(c.entries, &TomlConfigEntry{
		key:      key,
		path:     path,
		val:      text,
		original: text,
		raw:      string(c.parser.Raw(node.Raw)),
		offset:   int(node.Raw.Offset),
		isKey:    isKey,
	})
}

// encode returns the TOML text of the entry's value: the typed value, or the string with the quotes it was written with.
// Strings that cannot be written with their quotes, e.g. a literal string containing a ', are written as basic strings.
func (t *TomlConfigEntry) encode() string {
	if t.typed != "" {
		return t.typed
	}

	switch {
	case strings.HasPrefix(t.raw, `'''`):
		if literal, ok := encodeTomlLiteral(t.val, `'''`, t.raw); ok {
			return literal
		}
		return encodeTomlMultiLine(t.val, t.raw)
	case strings.HasPrefix(t.raw, `"""`):
		return encodeTomlMultiLine(t.val, t.raw)
	case strings.HasPrefix(t.raw, `'`):
		if literal, ok := encodeTomlLiteral(t.val, `'`, t.raw); ok {
			return literal
		}
	}
	return encodeTomlBasic(t.val)
}

// encodeTomlBasic returns the value as a basic string, e.g. "a\"b"
func encodeTomlBasic(value string) string {
	var encoded strings.Builder
	encoded.Grow(len(value) + 2)
	encoded.WriteByte('"')
	for _, r := range value {
		switch r {
		case '"':
			encoded.WriteString(`\"`)
		case '\n':
			encoded.WriteString(`\n`)
		default:
			writeTomlRune(&encoded, r)
		}
	}
	encoded.WriteByte('"')
	return encoded.String()
}

// encodeTomlMultiLine returns the value as a multi-line basic string. The line break after the opening quotes is
// kept if the original string has one. A line break directly after the quotes is not part of the value,
// so it is added as well if the value starts with a line break.
// Quotes are only escaped where they would end the string: as the third quote in a row and at the end of the value.
func encodeTomlMultiLine(value string, original string) string {
	var encoded strings.Builder
	encoded.Grow(len(value) + 7)
	encoded.WriteString(`"""`)
	if lineBreakAfterQuotes(original) || strings.HasPrefix(value, "\n") {
		encoded.WriteString(lineBreakOf(original))
	}

	quotes := 0
	trailingQuotes := len(value) - len(strings.TrimRight(value, `"`))
	for i, r := range value {
		if r != '"' {
			quotes = 0
		}
		switch r {
		case '"':
			quotes++
			if quotes == 3 || i >= len(value)-trailingQuotes {
				encoded.WriteString(`\"`)
				quotes = 0
				continue
			}
			encoded.WriteByte('"')
		case '\n', '\t':
			encoded.WriteRune(r)
		default:
			writeTomlRune(&encoded, r)
		}
	}
	encoded.WriteString(`"""`)
	return encoded.String()
}

// encodeTomlLiteral returns the value as a literal string with the given quotes, which have no escape sequences.
// It returns false if the value cannot be written as a literal string, e.g. because it contains the quotes
// or control characters other than tabs, and line breaks for multi-line literal strings.
func encodeTomlLiteral(value string, quotes string, original string) (string, bool) {
	multiLine := len(quotes) == 3
	if strings.Contains(value, quotes) || (multiLine && strings.HasSuffix(value, `'`)) || !utf8.ValidString(value) {
		return "", false
	}
	for _, r := range value {
		if (r < 0x20 && r != '\t' && (r != '\n' || !multiLine)) || r == 0x7f {
			return "", false
		}
	}

	if multiLine && (lineBreakAfterQuotes(original) || strings.HasPrefix(value, "\n")) {
		return quotes + lineBreakOf(original) + value + quotes, true
	}
	return quotes + value + quotes, true
}

// writeTomlRune writes the rune of a basic string, escaping backslashes and control characters
func writeTomlRune(encoded *strings.Builder, r rune) {
	switch {
	case r == '\\':
		encoded.WriteString(`\\`)
	case r == '\t':
		encoded.WriteString(`\t`)
	case r == '\r':
		encoded.WriteString(`\r`)
	case r == '\b':
		encoded.WriteString(`\b`)
	case r == '\f':
		encoded.WriteString(`\f`)
	case r < 0x20 || r == 0x7f:
		fmt.Fprintf(encoded, `\u%04X`, r)
	default:
		// Invalid UTF-8 is not allowed in TOML files, ranging over it yields U+FFFD, which replaces it like encoding/json does
		encoded.WriteRune(r)
	}
}

// lineBreakAfterQuotes returns whether the multi-line string starts with a line break after its opening quotes
func lineBreakAfterQuotes(original string) bool {
	rest := original[min(3, len(original)):]
	return strings.HasPrefix(rest, "\n") || strings.HasPrefix(rest, "\r\n")
}

// lineBreakOf returns the first line break of the multi-line string, or LF if it has none
func lineBreakOf(original string) string {
	if i := strings.IndexByte(original, '\n'); i > 0 && original[i-1] == '\r' {
		return "\r\n"
	}
	return "\n"
}
