package file

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"slices"
	"strconv"
	"strings"

	"github.com/denglertai/gonfig/internal/filter"
	"github.com/denglertai/gonfig/internal/value"
)

// jsonKind represents the kind of a JSON node
type jsonKind int

const (
	jsonScalar jsonKind = iota
	jsonObject
	jsonArray
)

// jsonNode represents a node of a JSON document while preserving the order of object keys
type jsonNode struct {
	kind jsonKind
	// keys holds the keys of an object, in the same order as children
	keys []string
	// children holds the values of an object or the items of an array
	children []*jsonNode
	// value holds the value of a scalar, which is one of string, json.Number, bool or nil
	value any
}

// JsonConfigEntry represents a single scalar value or a key containing a placeholder of a JSON document
type JsonConfigEntry struct {
	// node is the scalar, or the value of the key for keys
	node   *jsonNode
	key    string
	path   string
	value  string
	edited bool
	// typed holds a number (json.Number) or boolean set by SetTypedValue, which is written instead of value
	typed any
	// handler collects the entries of inserted fragments
	handler *JsonConfigFileHandler
	// isKey, parent and parentPath mark a key of the object parent at parentPath
	isKey      bool
	parent     *jsonNode
	parentPath string
	// superseded is set when the entry or its parent object was replaced by a fragment
	superseded bool
}

// Key returns the key of the configuration entry
func (j *JsonConfigEntry) Key() string {
	return j.key
}

// Path returns the path of the configuration entry
func (j *JsonConfigEntry) Path() string {
	return j.path
}

// GetValue returns the value of the configuration entry
func (j *JsonConfigEntry) GetValue() string {
	return j.value
}

// SetValue sets the value of the configuration entry, or renames the key
func (j *JsonConfigEntry) SetValue(value string) {
	if j.superseded {
		return
	}
	if j.isKey {
		i := slices.Index(j.parent.children, j.node)
		if i < 0 {
			// mergeObjects moves key entries along with the values they name, so this is a bug. Renaming nothing
			// would silently leave the placeholder in the output, and the message names no key, as it may be a secret.
			panic("internal error: a JSON key containing a placeholder is no longer part of its object")
		}
		j.parent.keys[i] = value
		j.value = value
		return
	}

	j.edited = j.edited || j.value != value
	j.value = value
	j.typed = nil
}

// mask replaces a value by value.Mask as a string, so numbers, booleans and nulls are masked as well.
// Keys are kept, since they are part of the structure.
func (j *JsonConfigEntry) mask() {
	if j.isKey || j.superseded {
		return
	}
	// The value is written with the type of the node, which is a string from now on
	j.node.value = value.Mask
	j.SetValue(value.Mask)
}

// SetTypedValue sets a number or boolean, which replaces the original value regardless of its type.
// Keys are always strings, so they are not set.
func (j *JsonConfigEntry) SetTypedValue(value any) bool {
	if j.superseded {
		return false
	}
	tv, ok := newTypedValue(value)
	if !ok || j.isKey {
		return false
	}

	j.typed = json.Number(tv.text)
	if tv.kind == typedBool {
		j.typed = tv.text == "true"
	}
	j.value = tv.text
	j.edited = true
	return true
}

// SetFragment replaces a value by the fragment, or merges the fragment into the object containing a key.
// An empty fragment replaces a value by null, or removes a key.
// f.Text must contain a single JSON value; whitespace-only text is empty.
// A key must have a null value, and a nonempty fragment used as a key must be an object.
// It returns the inserted entries for placeholder processing, or an error for invalid JSON,
// trailing data, or violations of the key requirements.
func (j *JsonConfigEntry) SetFragment(f filter.Fragment) ([]ConfigEntry, error) {
	if j.superseded {
		return nil, nil
	}
	fragment, err := parseJsonFragment(f.Text)
	if err != nil {
		return nil, err
	}

	if j.isKey {
		return j.mergeFragment(fragment)
	}

	if fragment == nil {
		fragment = &jsonNode{kind: jsonScalar}
	}

	// Replace the node in place, so the parent refers to the fragment
	*j.node = *fragment
	j.edited = false
	j.typed = nil
	return j.handler.collectEntries(func() { j.handler.handleNode(j.node, j.path, j.key) }), nil
}

// mergeFragment merges the fragment into the object containing the key and removes the key
func (j *JsonConfigEntry) mergeFragment(fragment *jsonNode) ([]ConfigEntry, error) {
	if j.superseded {
		return nil, nil
	}
	if j.node.kind != jsonScalar || j.node.value != nil {
		return nil, errors.New("a key with a fragment must have the value null")
	}
	if fragment != nil && fragment.kind != jsonObject {
		return nil, errors.New("a fragment used as a key has to be an object")
	}

	i := slices.Index(j.parent.children, j.node)
	if i < 0 {
		// mergeObjects moves key entries along with the values they name, so this is a bug. The caller names the
		// entry and file.
		return nil, errors.New("internal error: the key with the fragment is no longer part of its object")
	}
	j.parent.keys = slices.Delete(j.parent.keys, i, i+1)
	j.parent.children = slices.Delete(j.parent.children, i, i+1)
	if fragment == nil {
		return nil, nil
	}

	return j.handler.collectEntries(func() { j.handler.mergeObjects(j.parent, fragment, j.parentPath) }), nil
}

// parseJsonFragment parses a fragment, or returns nil for an empty fragment
func parseJsonFragment(text string) (*jsonNode, error) {
	if strings.TrimSpace(text) == "" {
		return nil, nil
	}

	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.UseNumber()

	fragment, err := decodeJsonNode(decoder)
	if err != nil {
		location := ""
		var syntaxError *json.SyntaxError
		if errors.As(err, &syntaxError) {
			location = fmt.Sprintf("at offset %d", syntaxError.Offset)
		}
		return nil, fragmentError("JSON", location, err)
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("invalid JSON fragment: unexpected data after the top-level JSON value")
	}

	return fragment, nil
}

// getConvertedValue converts the value to the type of the original value
func (j *JsonConfigEntry) getConvertedValue() (any, error) {
	switch j.node.value.(type) {
	case string:
		return j.value, nil
	case json.Number:
		// ParseFloat rejects anything but numbers, json.Valid rejects NaN, Inf and hex notation
		if _, err := strconv.ParseFloat(j.value, 64); err != nil || !json.Valid([]byte(j.value)) {
			return nil, fmt.Errorf("value %q of %s is not a valid JSON number", j.value, j.path)
		}
		return json.Number(j.value), nil
	case bool:
		return strconv.ParseBool(j.value)
	case nil:
		if j.value == "null" {
			return nil, nil
		}
	}

	return nil, fmt.Errorf("value %q of %s cannot replace %v", j.value, j.path, j.node.value)
}

// JsonConfigFileHandler represents a configuration file handler
type JsonConfigFileHandler struct {
	root    *jsonNode
	entries []ConfigEntry
	// keyEntries maps the value of each key containing a placeholder to the entry of the key, so mergeObjects can move
	// the entry to the value of a fragment replacing it. The entry finds its key by the identity of the value.
	keyEntries map[*jsonNode]*JsonConfigEntry
}

// NewJsonConfigFileHandler creates a new JSON configuration file handler
func NewJsonConfigFileHandler() *JsonConfigFileHandler {
	return &JsonConfigFileHandler{
		entries:    make([]ConfigEntry, 0),
		keyEntries: make(map[*jsonNode]*JsonConfigEntry),
	}
}

// Read reads the configuration file
func (j *JsonConfigFileHandler) Read(source io.Reader) (err error) {
	decoder := json.NewDecoder(source)
	// Keep numbers as written instead of converting them to float64
	decoder.UseNumber()

	j.root, err = decodeJsonNode(decoder)
	if err != nil {
		return err
	}

	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return fmt.Errorf("unexpected data after the top-level JSON value")
	}

	j.handleNode(j.root, "", "")
	return nil
}

// decodeJsonNode decodes the next JSON value of the decoder into a node
func decodeJsonNode(decoder *json.Decoder) (*jsonNode, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}

	delim, isDelim := token.(json.Delim)
	if !isDelim {
		return &jsonNode{kind: jsonScalar, value: token}, nil
	}

	node := &jsonNode{kind: jsonArray}
	if delim == '{' {
		node.kind = jsonObject
	}

	for decoder.More() {
		if node.kind == jsonObject {
			key, err := decoder.Token()
			if err != nil {
				return nil, err
			}
			node.keys = append(node.keys, key.(string))
		}

		child, err := decodeJsonNode(decoder)
		if err != nil {
			return nil, err
		}
		node.children = append(node.children, child)
	}

	// Consume the closing delimiter
	if _, err := decoder.Token(); err != nil {
		return nil, err
	}

	return node, nil
}

// handleNode recursively collects all scalars of the given node as entries
func (j *JsonConfigFileHandler) handleNode(node *jsonNode, path string, key string) {
	switch node.kind {
	case jsonObject:
		for i := range node.children {
			j.handleObjectChild(node, i, path)
		}
	case jsonArray:
		for i, child := range node.children {
			is := strconv.Itoa(i)
			j.handleNode(child, appendToPath(path, is), is)
		}
	case jsonScalar:
		value := "null"
		if node.value != nil {
			value = fmt.Sprint(node.value)
		}

		j.entries = append(j.entries, &JsonConfigEntry{
			node:    node,
			key:     key,
			path:    path,
			value:   value,
			handler: j,
		})
	}
}

// handleObjectChild collects the entries of the key and value at index i of the object.
// Keys containing placeholders are recorded in keyEntries as well.
func (j *JsonConfigFileHandler) handleObjectChild(object *jsonNode, i int, path string) {
	key, child := object.keys[i], object.children[i]
	currentPath := appendToPath(path, key)

	// Keys may contain placeholders as well
	if strings.Contains(key, "${") {
		entry := &JsonConfigEntry{
			node:       child,
			key:        key,
			path:       currentPath,
			value:      key,
			handler:    j,
			isKey:      true,
			parent:     object,
			parentPath: path,
		}
		j.entries = append(j.entries, entry)
		j.keyEntries[child] = entry
	}

	j.handleNode(child, currentPath, key)
}

// collectEntries returns the entries added while inserting a fragment
func (j *JsonConfigFileHandler) collectEntries(insert func()) []ConfigEntry {
	before := len(j.entries)
	insert()
	return slices.Clone(j.entries[before:])
}

// collectDescendantNodes recursively collects all jsonNode pointers in the subtree of node
func collectDescendantNodes(node *jsonNode, nodes map[*jsonNode]struct{}) {
	if node == nil {
		return
	}
	nodes[node] = struct{}{}
	for _, child := range node.children {
		collectDescendantNodes(child, nodes)
	}
}

// invalidateSuperseded invalidates value-entry bookkeeping and marks entries in the subtree of the child
// at index k as superseded before replacing it in target, while preserving placeholder-key entries on target
func (j *JsonConfigFileHandler) invalidateSuperseded(target *jsonNode, k int) {
	oldNode := target.children[k]
	subtree := make(map[*jsonNode]struct{})
	collectDescendantNodes(oldNode, subtree)

	for _, entry := range j.entries {
		jce := entry.(*JsonConfigEntry)
		if jce.superseded {
			continue
		}
		// Preserve placeholder-key entries directly on target for this member
		if jce.isKey && jce.parent == target && jce.node == oldNode {
			continue
		}
		_, nodeInSubtree := subtree[jce.node]
		_, parentInSubtree := subtree[jce.parent]
		if nodeInSubtree || parentInSubtree {
			jce.edited = false
			jce.typed = nil
			jce.superseded = true
		}
	}
}

// mergeObjects deep merges the source object into the target object: objects are merged recursively,
// other values of the source replace the ones of the target, and new keys are appended in the order of the source.
// Keys are matched as they are now, so a key containing a placeholder that is not processed yet is matched as written.
func (j *JsonConfigFileHandler) mergeObjects(target *jsonNode, source *jsonNode, path string) {
	// Index the keys of the target once, so merging takes linear instead of quadratic time for large objects.
	// The first occurrence of a duplicate key is used.
	index := make(map[string]int, len(target.keys))
	for i, key := range target.keys {
		if _, found := index[key]; !found {
			index[key] = i
		}
	}

	for i, key := range source.keys {
		child := source.children[i]
		currentPath := appendToPath(path, key)

		k, found := index[key]
		switch {
		case !found:
			target.keys = append(target.keys, key)
			target.children = append(target.children, child)
			index[key] = len(target.children) - 1
			j.handleObjectChild(target, len(target.children)-1, path)
		case target.children[k].kind == jsonObject && child.kind == jsonObject:
			j.mergeObjects(target.children[k], child, currentPath)
		default:
			j.invalidateSuperseded(target, k)
			// The entry of a key containing a placeholder finds its key by the value, so it moves to the new value
			if entry, isKey := j.keyEntries[target.children[k]]; isKey {
				delete(j.keyEntries, target.children[k])
				entry.node = child
				j.keyEntries[child] = entry
			}
			target.children[k] = child
			j.handleNode(child, currentPath, key)
		}
	}
}

// Process processes the configuration file and returns the configuration entries
func (j *JsonConfigFileHandler) Process() (iter.Seq[ConfigEntry], error) {
	initialEntries := slices.Clone(j.entries)
	return func(yield func(ConfigEntry) bool) {
		for _, entry := range initialEntries {
			jce := entry.(*JsonConfigEntry)
			if jce.superseded {
				continue
			}
			if jce.isKey && slices.Index(jce.parent.children, jce.node) < 0 {
				continue
			}
			if !yield(entry) {
				return
			}
		}
	}, nil
}

// Write writes the configuration entries to the target
func (j *JsonConfigFileHandler) Write(target io.Writer) error {
	for _, entry := range j.entries {
		jce := entry.(*JsonConfigEntry)
		if jce.superseded || !jce.edited {
			continue
		}

		if jce.typed != nil {
			jce.node.value = jce.typed
			continue
		}

		val, err := jce.getConvertedValue()
		if err != nil {
			return err
		}
		jce.node.value = val
	}

	compact := new(bytes.Buffer)
	if err := writeJsonNode(compact, j.root); err != nil {
		return err
	}

	indented := new(bytes.Buffer)
	if err := json.Indent(indented, compact.Bytes(), "", "  "); err != nil {
		return err
	}

	// Text files end with a line break, which Indent doesn't add after the top-level value
	indented.WriteByte('\n')

	_, err := target.Write(indented.Bytes())
	return err
}

// writeJsonNode writes the node as compact JSON
func writeJsonNode(buf *bytes.Buffer, node *jsonNode) error {
	if node.kind == jsonScalar {
		data, err := json.Marshal(node.value)
		if err != nil {
			return err
		}
		buf.Write(data)
		return nil
	}

	open, closing := byte('['), byte(']')
	if node.kind == jsonObject {
		open, closing = '{', '}'
	}

	buf.WriteByte(open)
	for i, child := range node.children {
		if i > 0 {
			buf.WriteByte(',')
		}

		if node.kind == jsonObject {
			key, err := json.Marshal(node.keys[i])
			if err != nil {
				return err
			}
			buf.Write(key)
			buf.WriteByte(':')
		}

		if err := writeJsonNode(buf, child); err != nil {
			return err
		}
	}
	buf.WriteByte(closing)

	return nil
}
