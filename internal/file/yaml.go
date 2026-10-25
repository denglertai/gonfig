package file

import (
	"cmp"
	"errors"
	"io"
	"iter"
	"regexp"
	"slices"
	"strconv"

	"github.com/denglertai/gonfig/internal/filter"
	"github.com/denglertai/gonfig/internal/value"
	"go.yaml.in/yaml/v3"
)

// YamlConfigEntry represents a single scalar of a YAML document, which is either a value or the key of a mapping
type YamlConfigEntry struct {
	node  *yaml.Node
	key   string
	path  string
	isKey bool
	// handler collects the entries of inserted fragments
	handler *YamlConfigFileHandler
	// parent and parentPath are the mapping containing the key and its path, only set for keys
	parent     *yaml.Node
	parentPath string
	// keyNode is the key of a value within a mapping
	keyNode *yaml.Node
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

// mask replaces a value by value.Mask as a string, so numbers, booleans and nulls are masked as well.
// Keys are kept, since they are part of the structure.
func (y *YamlConfigEntry) mask() {
	if y.isKey {
		return
	}
	y.node.Value = value.Mask
	y.node.Tag = "!!str"
}

// yamlTags are the YAML tags of the kinds of typed values
var yamlTags = map[typedKind]string{
	typedInt:   "!!int",
	typedFloat: "!!float",
	typedBool:  "!!bool",
}

// SetTypedValue sets a number or boolean as a plain scalar with the matching tag, even if the placeholder was quoted.
// Keys of mappings keep their text, so they are not set.
func (y *YamlConfigEntry) SetTypedValue(value any) bool {
	tv, ok := newTypedValue(value)
	if !ok || y.isKey {
		return false
	}

	y.node.Value = tv.text
	y.node.Tag = yamlTags[tv.kind]
	y.node.Style = 0
	return true
}

// SetFragment replaces a value by the fragment, or merges the fragment into the mapping containing a key.
// An empty fragment replaces a value by null, or removes a key.
// Only the first YAML document in f.Text is used. Any JSON is accepted as well, since JSON is valid YAML.
// A key must have a null value, and a nonempty fragment used as a key must be a mapping.
// It returns the inserted entries for placeholder processing, or an error for invalid YAML
// or violations of the key requirements.
func (y *YamlConfigEntry) SetFragment(f filter.Fragment) ([]ConfigEntry, error) {
	fragment, err := parseYamlFragment(f.Text)
	if err != nil {
		return nil, err
	}

	if y.isKey {
		return y.mergeFragment(fragment)
	}

	if fragment == nil {
		fragment = &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null"}
	}

	// A line comment of a value belongs to its key if the value is written as a block below the key. Empty mappings and
	// sequences are written as {} and [] on the line of the key, so their comment stays on them, e.g. "a: [] #c".
	isBlock := (fragment.Kind == yaml.MappingNode || fragment.Kind == yaml.SequenceNode) && len(fragment.Content) > 0
	if y.keyNode != nil && isBlock {
		y.keyNode.LineComment = cmp.Or(y.keyNode.LineComment, y.node.LineComment)
		y.node.LineComment = ""
	}
	keepYamlComments(y.node, fragment)

	// Replace the node in place, so the parent refers to the fragment
	*y.node = *fragment
	return y.handler.collectEntries(func() { y.handler.handleNode(y.node, y.path, y.key) }), nil
}

// mergeFragment merges the fragment into the mapping containing the key and removes the key
func (y *YamlConfigEntry) mergeFragment(fragment *yaml.Node) ([]ConfigEntry, error) {
	i := slices.Index(y.parent.Content, y.node)
	value := y.parent.Content[i+1]
	if value.Kind != yaml.ScalarNode || value.ShortTag() != "!!null" {
		return nil, errors.New("a key with a fragment must not have a value")
	}
	if fragment != nil && fragment.Kind != yaml.MappingNode {
		return nil, errors.New("a fragment used as a key has to be a mapping")
	}

	y.parent.Content = slices.Delete(y.parent.Content, i, i+2)
	if fragment == nil {
		return nil, nil
	}

	return y.handler.collectEntries(func() { y.handler.mergeMappings(y.parent, fragment, y.parentPath) }), nil
}

// yamlErrorLineRe matches the line number at the start of a YAML syntax error, which is the only part of the
// message that never quotes the parsed text
var yamlErrorLineRe = regexp.MustCompile(`^yaml: line (\d+):`)

// parseYamlFragment parses the first document of a fragment, or returns nil for an empty fragment
func parseYamlFragment(text string) (*yaml.Node, error) {
	var document yaml.Node
	if err := yaml.Unmarshal([]byte(text), &document); err != nil {
		location := ""
		if match := yamlErrorLineRe.FindStringSubmatch(err.Error()); match != nil {
			location = "on line " + match[1]
		}
		return nil, fragmentError("YAML", location, err)
	}
	if len(document.Content) == 0 {
		return nil, nil
	}

	fragment := document.Content[0]
	resetYamlStyles(fragment)
	return fragment, nil
}

// keepYamlComments keeps the comments of a replaced node, unless the replacing node has comments of its own
func keepYamlComments(replaced *yaml.Node, replacing *yaml.Node) {
	replacing.HeadComment = cmp.Or(replacing.HeadComment, replaced.HeadComment)
	replacing.LineComment = cmp.Or(replacing.LineComment, replaced.LineComment)
	replacing.FootComment = cmp.Or(replacing.FootComment, replaced.FootComment)
}

// resetYamlStyles writes fragments in block style without unnecessary quotes, e.g. for JSON fragments.
// The encoder quotes strings that would be read as another type otherwise, and literal and folded scalars are kept.
func resetYamlStyles(node *yaml.Node) {
	node.Style &^= yaml.FlowStyle | yaml.DoubleQuotedStyle | yaml.SingleQuotedStyle
	for _, child := range node.Content {
		resetYamlStyles(child)
	}
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
			y.handleMappingPair(node, i, path)
		}
	case yaml.SequenceNode:
		for i, child := range node.Content {
			is := strconv.Itoa(i)
			y.handleNode(child, appendToPath(path, is), is)
		}
	case yaml.ScalarNode:
		y.entries = append(y.entries, &YamlConfigEntry{
			node:    node,
			key:     key,
			path:    path,
			handler: y,
		})
	case yaml.AliasNode:
		// Aliases refer to an anchored node, which is processed where it is defined
	}
}

// handleMappingPair collects the entries of the key and value at index i of the mapping
func (y *YamlConfigFileHandler) handleMappingPair(mapping *yaml.Node, i int, path string) {
	keyNode, valueNode := mapping.Content[i], mapping.Content[i+1]
	currentPath := appendToPath(path, keyNode.Value)

	// The encoder writes merge keys with an explicit tag ("!!merge <<"), which is
	// valid but noisy; without the tag the key resolves to a merge key again
	if keyNode.Tag == "!!merge" {
		keyNode.Tag = ""
	}

	// Keys may contain placeholders as well
	if keyNode.Kind == yaml.ScalarNode {
		y.entries = append(y.entries, &YamlConfigEntry{
			node:       keyNode,
			key:        keyNode.Value,
			path:       currentPath,
			isKey:      true,
			handler:    y,
			parent:     mapping,
			parentPath: path,
		})
	}

	before := len(y.entries)
	y.handleNode(valueNode, currentPath, keyNode.Value)

	// Remember the key of a scalar value, e.g. to move comments when the value is replaced by a fragment
	if valueNode.Kind == yaml.ScalarNode && len(y.entries) == before+1 {
		y.entries[before].(*YamlConfigEntry).keyNode = keyNode
	}
}

// collectEntries returns the entries added while inserting a fragment
func (y *YamlConfigFileHandler) collectEntries(insert func()) []ConfigEntry {
	before := len(y.entries)
	insert()
	return slices.Clone(y.entries[before:])
}

// mergeMappings deep merges the source mapping into the target mapping: mappings are merged recursively,
// other values of the source replace the ones of the target, and new keys are appended in the order of the source
func (y *YamlConfigFileHandler) mergeMappings(target *yaml.Node, source *yaml.Node, path string) {
	// Index the keys of the target once, so merging takes linear instead of quadratic time for large mappings.
	// Like a lookup in YAML, the first occurrence of a duplicate key is used.
	index := make(map[string]int, len(target.Content)/2)
	for i := 0; i+1 < len(target.Content); i += 2 {
		if _, found := index[target.Content[i].Value]; !found {
			index[target.Content[i].Value] = i
		}
	}

	for i := 0; i+1 < len(source.Content); i += 2 {
		keyNode, valueNode := source.Content[i], source.Content[i+1]
		currentPath := appendToPath(path, keyNode.Value)

		j, found := index[keyNode.Value]
		switch {
		case !found:
			target.Content = append(target.Content, keyNode, valueNode)
			index[keyNode.Value] = len(target.Content) - 2
			y.handleMappingPair(target, len(target.Content)-2, path)
		case target.Content[j+1].Kind == yaml.MappingNode && valueNode.Kind == yaml.MappingNode:
			y.mergeMappings(target.Content[j+1], valueNode, currentPath)
		default:
			keepYamlComments(target.Content[j+1], valueNode)
			target.Content[j+1] = valueNode
			y.handleNode(valueNode, currentPath, keyNode.Value)
		}
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
