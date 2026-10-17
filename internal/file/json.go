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

// JsonConfigEntry represents a single scalar value of a JSON document
type JsonConfigEntry struct {
	node   *jsonNode
	key    string
	path   string
	value  string
	edited bool
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

// SetValue sets the value of the configuration entry
func (j *JsonConfigEntry) SetValue(value string) {
	j.edited = j.edited || j.value != value
	j.value = value
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
}

// NewJsonConfigFileHandler creates a new JSON configuration file handler
func NewJsonConfigFileHandler() *JsonConfigFileHandler {
	return &JsonConfigFileHandler{
		entries: make([]ConfigEntry, 0),
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
		for i, child := range node.children {
			j.handleNode(child, appendToPath(path, node.keys[i]), node.keys[i])
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
			node:  node,
			key:   key,
			path:  path,
			value: value,
		})
	}
}

// Process processes the configuration file and returns the configuration entries
func (j *JsonConfigFileHandler) Process() (iter.Seq[ConfigEntry], error) {
	return slices.Values(j.entries), nil
}

// Write writes the configuration entries to the target
func (j *JsonConfigFileHandler) Write(target io.Writer) error {
	for _, entry := range j.entries {
		jce := entry.(*JsonConfigEntry)
		if !jce.edited {
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
