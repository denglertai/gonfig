package file

import (
	"errors"
	"io"
	"iter"
	"slices"
	"strconv"

	"go.yaml.in/yaml/v3"
)

// YamlConfigEntry represents a single scalar of a YAML document, which is either a value or the key of a mapping
type YamlConfigEntry struct {
	node  *yaml.Node
	key   string
	path  string
	isKey bool
}

// Key returns the key of the configuration entry
func (y *YamlConfigEntry) Key() string {
	return y.key
}

// Path returns the path of the configuration entry
func (y *YamlConfigEntry) Path() string {
	return y.path
}

// GetValue returns the value of the configuration entry
func (y *YamlConfigEntry) GetValue() string {
	return y.node.Value
}

// SetValue sets the value of the configuration entry
func (y *YamlConfigEntry) SetValue(value string) {
	y.node.Value = value
}

// IsKey returns whether the configuration entry represents the key of a mapping rather than a value
func (y *YamlConfigEntry) IsKey() bool {
	return y.isKey
}

// YamlConfigFileHandler represents a configuration file handler.
// It operates on the YAML node tree, so comments, key order and styles are preserved.
type YamlConfigFileHandler struct {
	documents []*yaml.Node
	entries   []ConfigEntry
}

// NewYamlConfigFileHandler creates a new YAML configuration file handler
func NewYamlConfigFileHandler() *YamlConfigFileHandler {
	return &YamlConfigFileHandler{
		documents: make([]*yaml.Node, 0),
		entries:   make([]ConfigEntry, 0),
	}
}

// Read reads the configuration file, including all documents of a multi-document file
func (y *YamlConfigFileHandler) Read(source io.Reader) error {
	decoder := yaml.NewDecoder(source)
	for {
		document := &yaml.Node{}
		err := decoder.Decode(document)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}

		y.documents = append(y.documents, document)
		y.handleNode(document, "", "")
	}
}

// handleNode recursively collects all scalars of the given node as entries
func (y *YamlConfigFileHandler) handleNode(node *yaml.Node, path string, key string) {
	switch node.Kind {
	case yaml.DocumentNode:
		for _, child := range node.Content {
			y.handleNode(child, path, key)
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(node.Content); i += 2 {
			keyNode, valueNode := node.Content[i], node.Content[i+1]
			currentPath := appendToPath(path, keyNode.Value)

			// The encoder writes merge keys with an explicit tag ("!!merge <<"), which is
			// valid but noisy; without the tag the key resolves to a merge key again
			if keyNode.Tag == "!!merge" {
				keyNode.Tag = ""
			}

			// Keys may contain placeholders as well
			if keyNode.Kind == yaml.ScalarNode {
				y.entries = append(y.entries, &YamlConfigEntry{
					node:  keyNode,
					key:   keyNode.Value,
					path:  currentPath,
					isKey: true,
				})
			}

			y.handleNode(valueNode, currentPath, keyNode.Value)
		}
	case yaml.SequenceNode:
		for i, child := range node.Content {
			is := strconv.Itoa(i)
			y.handleNode(child, appendToPath(path, is), is)
		}
	case yaml.ScalarNode:
		y.entries = append(y.entries, &YamlConfigEntry{
			node: node,
			key:  key,
			path: path,
		})
	case yaml.AliasNode:
		// Aliases refer to an anchored node, which is processed where it is defined
	}
}

// Process processes the configuration file and returns the configuration entries
func (y *YamlConfigFileHandler) Process() (iter.Seq[ConfigEntry], error) {
	return slices.Values(y.entries), nil
}

// Write writes the configuration entries to the target
func (y *YamlConfigFileHandler) Write(target io.Writer) error {
	enc := yaml.NewEncoder(target)
	enc.SetIndent(2)

	for _, document := range y.documents {
		if err := enc.Encode(document); err != nil {
			return err
		}
	}

	return enc.Close()
}
