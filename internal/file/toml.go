package file

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"io"
	"iter"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/denglertai/gonfig/internal/filter"
	"github.com/denglertai/gonfig/internal/value"
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

// TomlConfigEntry is a configuration entry for TOML files: a string value, or a key containing a placeholder.
// Entries inserted by fragments exist for every value, so all of them can be masked, see FileProcessor.masked.
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
	// pair is the position of the key/value pair of a string that is the value of a key/value pair outside arrays and
	// inline tables, which an empty fragment removes, or of the pair of a key a fragment can be merged into, see
	// tomlKeyPair. It is empty otherwise, since a pair is never empty.
	// It is no pointer, since most strings have one and allocating each of them takes noticeable time.
	pair tomlRange
	// keyPair is set for a key that is the only key of a key/value pair with an empty string as its value outside
	// inline tables, e.g. "${X | fragment}" = "", which a fragment can be merged into the table containing it
	keyPair *tomlKeyPair
	// fragment is the fragment set by SetFragment, which replaces the string or removes the pair, or nil
	fragment *tomlFragment
}

// tomlRange is a range of bytes within a TOML document or fragment
type tomlRange struct {
	start int
	end   int
}

// tomlFragment is a fragment replacing a string, or the key/value pair of a key, see TomlConfigEntry.SetFragment
type tomlFragment struct {
	// text is the TOML value of the fragment, or the key/value pairs a fragment used as a key adds to the table,
	// or empty if the fragment removes the key/value pair
	text []byte
	// entries are the values and keys containing placeholders within text, with their positions relative to text
	entries []*TomlConfigEntry
	// overrides are the values of the table's key/value pairs a fragment used as a key replaces
	overrides []tomlOverride
}

// tomlOverride replaces the value of a key/value pair of the table by the value of the pair with the same key in
// a fragment used as a key
type tomlOverride struct {
	// span is the range of the replaced value in the document
	span tomlRange
	// value is the value of the fragment's pair, with the entries of its strings
	value *tomlFragment
}

// tomlKeyPair is the context of a key a fragment can be merged into, see TomlConfigEntry.keyPair
type tomlKeyPair struct {
	// data is the document, which is parsed again to find the key/value pairs of the table when a fragment is merged
	data []byte
	// table is the path of the table containing the pair
	table string
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
	t.fragment = nil
}

// mask replaces a value by value.Mask as a string, so numbers, booleans and dates are masked as well.
// Keys are kept, since they are part of the structure.
func (t *TomlConfigEntry) mask() {
	if !t.isKey {
		t.SetValue(value.Mask)
	}
}

// errTomlKeyFragment is the error of a fragment used as a key that cannot be merged into a table, e.g. a key of a
// table header or inline table
var errTomlKeyFragment = errors.New(`a fragment used as a key in a TOML file must be the only key of a key/value pair ` +
	`outside inline tables, with an empty string as its value, e.g. "${X | fragment}" = ""`)

// SetFragment replaces the string by the fragment, which must be a single TOML value, e.g. an array or an inline table.
// An empty fragment removes the key/value pair, which is only possible outside arrays and inline tables, since TOML
// has no null. A fragment used as a key is merged into the table, see setKeyFragment.
// It returns the entries of the values and keys of the fragment, so their placeholders can be processed.
func (t *TomlConfigEntry) SetFragment(f filter.Fragment) ([]ConfigEntry, error) {
	if t.isKey {
		if t.keyPair == nil {
			return nil, errTomlKeyFragment
		}
		return t.setKeyFragment(strings.TrimSpace(f.Text))
	}

	text := strings.TrimSpace(f.Text)
	if text == "" {
		if t.pair.end == 0 {
			return nil, errors.New("an empty fragment can only remove a key/value pair, not a value within an array or inline table")
		}
		t.fragment = &tomlFragment{}
		return nil, nil
	}

	fragment, err := parseTomlFragment(text, t.path, t.key, t.pair.end != 0)
	if err != nil {
		return nil, err
	}
	t.fragment = fragment
	t.typed = ""

	inserted := make([]ConfigEntry, 0, len(fragment.entries))
	for _, entry := range fragment.entries {
		inserted = append(inserted, entry)
	}
	return inserted, nil
}

// tomlTablePair is a key/value pair of a table, see tablePairs
type tomlTablePair struct {
	// keys are the parts of the key, e.g. pool and size for pool.size
	keys []string
	// pair is the range of the key/value pair and value the range of its value
	pair  tomlRange
	value tomlRange
}

// setKeyFragment merges the key/value pairs of the fragment into the table containing the key, which is a shallow
// override within the table's section: a pair of the fragment replaces the value of the pair with the same key in the
// section, other pairs replace the pair of the key in their order. Keys defined elsewhere, e.g. by another header like
// [database.pool], are detected when the document is written. The fragment must consist of key/value pairs only.
// An empty fragment removes the pair of the key.
func (t *TomlConfigEntry) setKeyFragment(text string) ([]ConfigEntry, error) {
	fragment := &tomlFragment{}
	if text == "" {
		t.fragment = fragment
		return nil, nil
	}

	var decoded map[string]any
	if err := toml.Unmarshal([]byte(text), &decoded); err != nil {
		return nil, fragmentError("TOML", fmt.Sprintf("on line %d", tomlLine(err)), err)
	}

	source := t.keyPair.data
	pairs := tablePairs(source, t.pair.start)
	lineBreak, indent := pairLayout(source, t.pair.start)

	var added bytes.Buffer
	var inserted []ConfigEntry
	parser := unstable.Parser{}
	parser.Reset([]byte(text))
	for parser.NextExpression() {
		node := parser.Expression()
		if node.Kind != unstable.KeyValue {
			return nil, errors.New("invalid TOML fragment: a fragment used as a key must consist of key/value pairs, without table headers")
		}
		keys := tomlKeys(node)
		pairRange, valueRange := pairRanges(node, []byte(text))

		target, err := overriddenPair(pairs, keys)
		if err != nil {
			return nil, err
		}
		collector := tomlCollector{parser: &parser, arrayTables: map[string]int{}, fragment: true}
		if target != nil {
			// Only the value is written, so the key of the table is kept as written
			collector.base = valueRange.start
			collector.value(node.Value(), appendToPath(t.keyPair.table, strings.Join(keys, ".")), keys[len(keys)-1])
			override := &tomlFragment{text: []byte(text[valueRange.start:valueRange.end]), entries: collector.entries}
			fragment.overrides = append(fragment.overrides, tomlOverride{span: target.value, value: override})
		} else {
			if added.Len() > 0 {
				added.WriteString(lineBreak)
				added.WriteString(indent)
			}
			collector.base = pairRange.start - added.Len()
			collector.keyValue(node, t.keyPair.table)
			added.WriteString(text[pairRange.start:pairRange.end])
			fragment.entries = append(fragment.entries, collector.entries...)
		}
		for _, entry := range collector.entries {
			inserted = append(inserted, entry)
		}
	}

	// Without added pairs, the pair of the key is removed, see span
	if added.Len() > 0 {
		fragment.text = added.Bytes()
	}
	t.fragment = fragment
	return inserted, nil
}

// tablePairs returns the key/value pairs of the section of the document containing the pair starting at offset: the
// pairs between the table header before it and the next one
func tablePairs(source []byte, offset int) []tomlTablePair {
	var pairs []tomlTablePair
	found := false
	parser := unstable.Parser{}
	parser.Reset(source)
	for parser.NextExpression() {
		node := parser.Expression()
		if node.Kind != unstable.KeyValue {
			if found {
				break
			}
			pairs = pairs[:0]
			continue
		}
		pairRange, valueRange := pairRanges(node, source)
		if pairRange.start == offset {
			found = true
			continue
		}
		pairs = append(pairs, tomlTablePair{keys: tomlKeys(node), pair: pairRange, value: valueRange})
	}
	return pairs
}

// overriddenPair returns the pair of the table with the keys, or nil if the table has none. Pairs with dotted keys,
// e.g. pool.size, and inline tables define subtables, which a fragment used as a key cannot be merged into, so a pair
// whose keys start with the other's keys is an error. The common keys are part of the file, so the error names them.
func overriddenPair(pairs []tomlTablePair, keys []string) (*tomlTablePair, error) {
	for i, pair := range pairs {
		if slices.Equal(pair.keys, keys) {
			return &pairs[i], nil
		}
		shorter := min(len(pair.keys), len(keys))
		if slices.Equal(pair.keys[:shorter], keys[:shorter]) {
			return nil, fmt.Errorf("the fragment and the table both define %s, one of them with dotted keys within it; "+
				"a fragment used as a key in a TOML file only replaces the values of keys written the same way",
				strings.Join(keys[:shorter], "."))
		}
	}
	return nil, nil
}

// singleKey returns whether the key of the key/value pair has one part only, i.e. is no dotted key
func singleKey(node *unstable.Node) bool {
	keys := node.Key()
	return keys.Next() && keys.IsLast()
}

// tomlKeys returns the parts of the key of a key/value pair, e.g. pool and size for pool.size
func tomlKeys(node *unstable.Node) []string {
	var keys []string
	for it := node.Key(); it.Next(); {
		keys = append(keys, string(it.Node().Data))
	}
	return keys
}

// pairRanges returns the range of the key/value pair and of its value. Arrays and inline tables have no range,
// so the value starts after the = following the last key and ends with the pair.
func pairRanges(node *unstable.Node, source []byte) (pair tomlRange, value tomlRange) {
	pair = tomlRange{start: int(node.Raw.Offset), end: int(node.Raw.Offset + node.Raw.Length)}
	var last *unstable.Node
	for it := node.Key(); it.Next(); {
		last = it.Node()
	}
	// The raw range of a quoted key includes its quotes
	start := int(last.Raw.Offset + last.Raw.Length)
	for start < pair.end && (source[start] == ' ' || source[start] == '\t' || source[start] == '=') {
		start++
	}
	return pair, tomlRange{start: start, end: pair.end}
}

// pairLayout returns the line break of the document and the indentation of the pair starting at offset, which
// separate the key/value pairs a fragment used as a key adds
func pairLayout(source []byte, offset int) (lineBreak string, indent string) {
	lineStart := bytes.LastIndexByte(source[:offset], '\n') + 1
	lineBreak = "\n"
	if i := bytes.IndexByte(source, '\n'); i > 0 && source[i-1] == '\r' {
		lineBreak = "\r\n"
	}
	return lineBreak, string(source[lineStart:offset])
}

// tomlFragmentKey is the key a fragment is parsed with, since a TOML document consists of key/value pairs
const tomlFragmentKey = "v = "

// parseTomlFragment parses the text of a fragment as a single TOML value at the path and key of the replaced string.
// Comments after the value are only allowed for the value of a pair, since the rest of its line may be a comment as
// well, while within arrays and inline tables they would comment out the closing bracket.
func parseTomlFragment(text string, path string, key string, pairValue bool) (*tomlFragment, error) {
	document := []byte(tomlFragmentKey + text + "\n")
	var decoded map[string]any
	if err := toml.Unmarshal(document, &decoded); err != nil {
		return nil, fragmentError("TOML", fmt.Sprintf("on line %d", tomlLine(err)), err)
	}

	// Further key/value pairs would be inserted into the document as well.
	// The parser sets no range for arrays and inline tables, so the end of the value is the end of the pair.
	// The nodes of an expression are only valid until the next one is parsed, so the entries are collected first.
	parser := unstable.Parser{}
	parser.Reset(document)
	parser.NextExpression()
	pair := parser.Expression()
	end := int(pair.Raw.Offset + pair.Raw.Length)
	collector := tomlCollector{parser: &parser, arrayTables: map[string]int{}, fragment: true, base: len(tomlFragmentKey)}
	collector.value(pair.Value(), path, key)

	if parser.NextExpression() {
		return nil, errors.New("invalid TOML fragment: a fragment used as a value must consist of a single value")
	}
	// The document is valid and has no further expression, so only comments and whitespace follow the value
	if len(bytes.TrimSpace(document[end:])) > 0 && !pairValue {
		return nil, errors.New("invalid TOML fragment: a fragment within an array or inline table cannot end with a comment")
	}
	return &tomlFragment{text: []byte(text), entries: collector.entries}, nil
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
	return t.fragment != nil || t.typed != "" || t.val != t.original
}

// span returns the range of the source that is replaced by the entry: the string or key, the whole key/value pair
// for an empty fragment, see removedPair, or the key/value pair of a key replaced by the pairs a fragment adds
func (t *TomlConfigEntry) span(source []byte) tomlRange {
	switch {
	case t.fragment != nil && t.fragment.text == nil:
		return removedPair(source, t.pair)
	case t.fragment != nil && t.isKey:
		return t.pair
	}
	return tomlRange{start: t.offset, end: t.offset + len(t.raw)}
}

// removedPair returns the range removed with the key/value pair: its whole line if it contains nothing else,
// otherwise the pair and the whitespace after it, keeping the indentation and a comment after it
func removedPair(source []byte, pair tomlRange) tomlRange {
	start := pair.start
	for start > 0 && (source[start-1] == ' ' || source[start-1] == '\t') {
		start--
	}
	end := pair.end
	for end < len(source) && (source[end] == ' ' || source[end] == '\t') {
		end++
	}

	// A top-level key/value pair is the only expression on its line, so only whitespace precedes it
	switch {
	case end == len(source):
		return tomlRange{start: start, end: end}
	case source[end] == '\n':
		return tomlRange{start: start, end: end + 1}
	case source[end] == '\r' && end+1 < len(source) && source[end+1] == '\n':
		return tomlRange{start: start, end: end + 2}
	default:
		return tomlRange{start: pair.start, end: end}
	}
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
	collector := tomlCollector{parser: &parser, arrayTables: map[string]int{}, data: t.data}
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
// other keys, so the result is checked if a key changed. A fragment is a valid value on its own, see parseTomlFragment,
// which is valid wherever a string is.
func (t *TomlFileHandler) Write(destination io.Writer) error {
	var result bytes.Buffer
	result.Grow(len(t.data))
	if t.bom {
		result.Write(utf8BOM)
	}

	keyFragments, err := t.checkOverrides()
	if err != nil {
		return err
	}
	if keyChanged := spliceToml(&result, t.data, t.entries); keyChanged {
		if err := checkRenamedKeys(bytes.TrimPrefix(result.Bytes(), utf8BOM), keyFragments); err != nil {
			return err
		}
	}

	_, err = destination.Write(result.Bytes())
	return err
}

// checkOverrides returns whether fragments are merged into tables, and an error if two of them replace the same value,
// since only one of them could be written
func (t *TomlFileHandler) checkOverrides() (bool, error) {
	keyFragments := false
	overridden := map[int]bool{}
	for _, entry := range t.entries {
		if entry.fragment == nil || !entry.isKey {
			continue
		}
		keyFragments = true
		for _, override := range entry.fragment.overrides {
			if overridden[override.span.start] {
				return true, fmt.Errorf("two fragments used as keys replace the value on line %d", bytes.Count(t.data[:override.span.start], []byte("\n"))+1)
			}
			overridden[override.span.start] = true
		}
	}
	return keyFragments, nil
}

// spliceToml writes the source with the changed entries replaced to the result and returns whether a key changed,
// including the keys within fragments. The entries are ordered by their position. A pair removed by an empty fragment
// contains the entry of a renamed key before it, so ranges within a range replaced before are skipped, with ranges starting at the same position ordered
// by their length, the longest first.
func spliceToml(result *bytes.Buffer, source []byte, entries []*TomlConfigEntry) bool {
	type replacement struct {
		entry *TomlConfigEntry
		span  tomlRange
		// override is the value replacing the span for the overrides of a fragment used as a key, see tomlOverride
		override *tomlFragment
	}
	var replacements []replacement
	for _, entry := range entries {
		if entry.changed() {
			replacements = append(replacements, replacement{entry: entry, span: entry.span(source)})
			if entry.fragment != nil {
				for _, override := range entry.fragment.overrides {
					replacements = append(replacements, replacement{entry: entry, span: override.span, override: override.value})
				}
			}
		}
	}
	// A replaced value contains the entry of its string, which has the same span, so overrides come first
	slices.SortStableFunc(replacements, func(a, b replacement) int {
		return cmp.Or(cmp.Compare(a.span.start, b.span.start), cmp.Compare(b.span.end, a.span.end),
			cmp.Compare(boolRank(a.override == nil), boolRank(b.override == nil)))
	})

	keyChanged := false
	written := 0
	for _, r := range replacements {
		if r.span.start < written {
			continue
		}
		result.Write(source[written:r.span.start])
		if r.override != nil {
			keyChanged = spliceToml(result, r.override.text, r.override.entries) || keyChanged
		} else {
			keyChanged = r.entry.write(result) || r.entry.isKey || keyChanged
		}
		written = r.span.end
	}
	result.Write(source[written:])
	return keyChanged
}

// boolRank returns 0 for false and 1 for true, to sort by a condition
func boolRank(b bool) int {
	if b {
		return 1
	}
	return 0
}

// checkRenamedKeys returns an error if the document with renamed keys or with fragments merged into tables is invalid,
// e.g. because a key is defined twice. The keys may contain values of placeholders, so the message of go-toml naming
// the key is only included if values are allowed in errors, see filter.SetValuesInErrors.
func checkRenamedKeys(data []byte, keyFragments bool) error {
	var document map[string]any
	err := toml.Unmarshal(data, &document)
	if err == nil {
		return nil
	}

	subject, reason := "the renamed keys", "a key is defined twice"
	if keyFragments {
		subject, reason = "the renamed keys or the fragments used as keys", "a key of a fragment is also defined by a table header"
	}
	if filter.ValuesInErrors() {
		return fmt.Errorf("%s make the TOML document invalid: %w", subject, err)
	}
	return fmt.Errorf("%s make the TOML document invalid on line %d, e.g. because %s", subject, tomlLine(err), reason)
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
	// fragment is set while collecting the entries of a fragment, which are collected for every value and positioned
	// relative to the fragment's text, see parseTomlFragment
	fragment bool
	// base is subtracted from the offsets of the entries, so the entries of fragments are relative to their text
	base int
	// data is the document, which keys fragments can be merged into refer to, or nil for fragments
	data []byte
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
		// The value is collected after the keys, so the last entry is the string, which an empty fragment may remove with its pair
		if node.Value().Kind == unstable.String {
			pair := tomlRange{start: int(node.Raw.Offset), end: int(node.Raw.Offset + node.Raw.Length)}
			c.entries[len(c.entries)-1].pair = pair
			// A key a fragment can be merged into is the only key of its pair, so its entry directly precedes the value's
			if len(node.Value().Data) == 0 && c.data != nil && len(c.entries) > 1 {
				if key := c.entries[len(c.entries)-2]; key.isKey && key.offset == int(node.Raw.Offset) && singleKey(node) {
					key.pair = pair
					key.keyPair = &tomlKeyPair{data: c.data, table: c.table}
				}
			}
		}
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
	default:
		// Numbers, booleans and dates contain no placeholders, but the values of fragments are masked
		if c.fragment {
			c.add(node, key, path, false)
		}
	}
}

// add adds an entry for the string, key or other value node. Values other than strings are entries of fragments only,
// whose value is their text, e.g. 1979-05-27. Entries of fragments are positioned relative to the fragment's text.
func (c *tomlCollector) add(node *unstable.Node, key string, path string, isKey bool) {
	raw := string(c.parser.Raw(node.Raw))
	text := raw
	if node.Kind == unstable.String || isKey {
		text = string(node.Data)
	}
	offset := int(node.Raw.Offset) - c.base
	c.entries = append(c.entries, &TomlConfigEntry{
		key:      key,
		path:     path,
		val:      text,
		original: text,
		raw:      raw,
		offset:   offset,
		isKey:    isKey,
	})
}

// write writes the TOML text replacing the entry: the fragment with its changed entries, nothing for an empty fragment,
// or the encoded value. It returns whether a key within the fragment changed.
func (t *TomlConfigEntry) write(result *bytes.Buffer) bool {
	if t.fragment != nil {
		return spliceToml(result, t.fragment.text, t.fragment.entries)
	}
	result.WriteString(t.encode())
	return false
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
