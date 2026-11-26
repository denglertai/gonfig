package file

import (
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
	"go.yaml.in/yaml/v4"
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
	// document is the index of the document containing the entry, whose anchor names a fragment must not reuse
	document int
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
// A key must have a null value and no anchor, and a nonempty fragment used as a key must be a mapping.
// A replaced value keeps its anchor, so aliases of it still refer to the fragment, and anchors of the fragment that the
// document has already are renamed, see keepYamlAnchors.
// It returns the inserted entries for placeholder processing, or an error for invalid YAML
// or violations of the key requirements.
func (y *YamlConfigEntry) SetFragment(f filter.Fragment) ([]ConfigEntry, error) {
	fragment, err := parseYamlFragment(f.Text)
	if err != nil {
		return nil, err
	}

	// The anchor names of the document are collected before the first fragment changes it, even one without anchors,
	// since it may remove an anchored node, whose name must stay reserved, so its aliases fail writing rather than
	// referring to an anchor of a later fragment. A fragment without anchors has no aliases either, so there is nothing
	// to rename in it.
	var names map[string]int
	hasAnchors := y.handler.registerFragment(fragment)
	documentNames := y.handler.documentAnchors(y.document)
	if hasAnchors {
		names = documentNames
	}
	y.handler.document = y.document

	if y.isKey {
		return y.mergeFragment(fragment, names)
	}

	if fragment == nil {
		fragment = &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null"}
	}
	// Check doesn't show the comments of fragments, since the text of fragments is masked; the comments of the file
	// stay, see keepYamlComments
	if y.handler.masked {
		clearYamlComments(fragment)
	}

	// A line comment of a value belongs to its key if the value is written as a block below the key. Empty mappings and
	// sequences are written as {} and [] on the line of the key, so their comment stays on them, e.g. "a: [] #c".
	isBlock := (fragment.Kind == yaml.MappingNode || fragment.Kind == yaml.SequenceNode) && len(fragment.Content) > 0
	if y.keyNode != nil && isBlock {
		y.keyNode.LineComment = cmp.Or(y.keyNode.LineComment, y.node.LineComment)
		y.node.LineComment = ""
	}
	keepYamlComments(y.node, fragment)
	// The anchor is set before the entries are collected, so they are known as values an alias may use as a key
	y.handler.keepYamlAnchors(fragment, map[*yaml.Node]string{fragment: y.node.Anchor}, names)

	// Replace the node in place, so the parent refers to the fragment
	*y.node = *fragment
	y.handler.addFragmentNode(y.node)
	if y.handler.fragmentCopies == nil {
		y.handler.fragmentCopies = map[*yaml.Node]*yaml.Node{}
	}
	y.handler.fragmentCopies[fragment] = y.node
	return y.handler.collectEntries(func() { y.handler.handleNode(y.node, y.path, y.key) }), nil
}

// mergeFragment merges the fragment into the mapping containing the key and removes the key. An anchor on the key or
// its null value is rejected, even if no alias uses it, since the key and value are removed and aliases of them
// would refer to nothing. names are the anchor names of the document, see keepYamlAnchors.
func (y *YamlConfigEntry) mergeFragment(fragment *yaml.Node, names map[string]int) ([]ConfigEntry, error) {
	i := slices.Index(y.parent.Content, y.node)
	value := y.parent.Content[i+1]
	if value.Kind != yaml.ScalarNode || value.ShortTag() != "!!null" {
		return nil, errors.New("a key with a fragment must not have a value")
	}
	if y.node.Anchor != "" || value.Anchor != "" {
		return nil, errors.New("a key with a fragment must not have an anchor")
	}
	if fragment != nil && fragment.Kind != yaml.MappingNode {
		return nil, errors.New("a fragment used as a key has to be a mapping")
	}

	y.parent.Content = slices.Delete(y.parent.Content, i, i+2)
	if fragment == nil {
		return nil, nil
	}

	if y.handler.masked {
		clearYamlComments(fragment)
	}
	kept := map[*yaml.Node]string{}
	entries := y.handler.collectEntries(func() { y.handler.mergeMappings(y.parent, fragment, y.parentPath, kept) })
	y.handler.keepYamlAnchors(fragment, kept, names)
	return entries, nil
}

// parseYamlFragment parses the first document of a fragment, or returns nil for an empty fragment. Only the
// line of a load error is reported without values in errors, because the message may quote the parsed text
func parseYamlFragment(text string) (*yaml.Node, error) {
	var document yaml.Node
	if err := yaml.Unmarshal([]byte(text), &document); err != nil {
		location := ""
		var loadErr *yaml.LoadError
		if errors.As(err, &loadErr) && loadErr.Mark.Line > 0 {
			location = "on line " + strconv.Itoa(loadErr.Mark.Line)
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

// clearYamlComments removes the comments of the node and all nodes within it
func clearYamlComments(node *yaml.Node) {
	node.HeadComment, node.LineComment, node.FootComment = "", "", ""
	for _, child := range node.Content {
		clearYamlComments(child)
	}
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
	// document is the index of the document whose entries are collected, see YamlConfigEntry.document
	document int
	// anchors are the anchor names of each document and the fragments inserted into it, with the next suffix to try
	// for a free name based on them, collected on first use, see documentAnchors
	anchors []map[string]int
	// fragmentNodes are the nodes of inserted fragments, including the nodes values are replaced in, see checkAliases.
	// Their anchor names and lines come from the fragment and are not named in errors.
	fragmentNodes map[*yaml.Node]struct{}
	// fragmentCopies are the nodes fragment roots were copied into, by the roots, since the aliases of a fragment refer
	// to the root it was parsed with, see checkAliases
	fragmentCopies map[*yaml.Node]*yaml.Node
	// insertedFragment is set once a fragment was inserted, which is the only way an alias may lose its anchor
	insertedFragment bool
	// masked is set while values are masked, so the comments of fragments are not written, see maskingHandler
	masked bool
}

// setMasked removes the comments of the fragments inserted from now on, since their text is masked
func (y *YamlConfigFileHandler) setMasked() {
	y.masked = true
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
		y.document = len(y.documents) - 1
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
			node:     node,
			key:      key,
			path:     path,
			handler:  y,
			document: y.document,
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
			document:   y.document,
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
// other values of the source replace the ones of the target, and new keys are appended in the order of the source.
// A replacing value gets the anchor of the replaced one, unless it is an alias, which cannot have an anchor; the
// nodes are added to kept with their anchors, see keepYamlAnchors.
func (y *YamlConfigFileHandler) mergeMappings(target *yaml.Node, source *yaml.Node, path string, kept map[*yaml.Node]string) {
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
			y.mergeMappings(target.Content[j+1], valueNode, currentPath, kept)
		default:
			keepYamlComments(target.Content[j+1], valueNode)
			// The anchor is set before the entries are collected, so they are known as values an alias may use as a key
			if anchor := target.Content[j+1].Anchor; anchor != "" && valueNode.Kind != yaml.AliasNode {
				valueNode.Anchor = anchor
				kept[valueNode] = anchor
			}
			target.Content[j+1] = valueNode
			y.handleNode(valueNode, currentPath, keyNode.Value)
		}
	}
}

// Process processes the configuration file and returns the configuration entries
func (y *YamlConfigFileHandler) Process() (iter.Seq[ConfigEntry], error) {
	return slices.Values(y.entries), nil
}

// Write writes the configuration entries to the target. It keeps the yaml v3 output, including its unlimited line
// width, so that long lines are never wrapped
func (y *YamlConfigFileHandler) Write(target io.Writer) error {
	if err := y.checkUTF8(); err != nil {
		return err
	}
	if err := y.checkAliases(); err != nil {
		return err
	}
	for _, document := range y.documents {
		moveYamlBlockComments(document)
	}

	// NewDumper only fails for invalid options, and these are constant
	dumper, _ := yaml.NewDumper(target, yaml.WithV3Defaults(), yaml.WithIndent(2), yaml.WithLineWidth(-1))

	for _, document := range y.documents {
		if err := dumper.Dump(document); err != nil {
			return err
		}
	}

	return dumper.Close()
}

// checkUTF8 rejects entries whose text is not valid UTF-8, naming the entry but not the text, which may be a
// secret. yaml v4 would silently write such text base64-encoded as !!binary, while v3 failed
func (y *YamlConfigFileHandler) checkUTF8() error {
	for _, entry := range y.entries {
		yamlEntry := entry.(*YamlConfigEntry)
		if utf8.ValidString(yamlEntry.node.Value) {
			continue
		}
		if yamlEntry.isKey {
			return fmt.Errorf("the key %s is not valid UTF-8", yamlEntry.path)
		}
		return fmt.Errorf("the value of %s is not valid UTF-8", yamlEntry.path)
	}
	return nil
}

// registerFragment remembers the nodes of a fragment being inserted, see checkAliases, and returns whether the
// fragment has anchors
func (y *YamlConfigFileHandler) registerFragment(fragment *yaml.Node) bool {
	y.insertedFragment = true
	if fragment == nil {
		return false
	}
	hasAnchors := false
	var visit func(node *yaml.Node)
	visit = func(node *yaml.Node) {
		y.addFragmentNode(node)
		hasAnchors = hasAnchors || node.Anchor != ""
		for _, child := range node.Content {
			visit(child)
		}
	}
	visit(fragment)
	return hasAnchors
}

// addFragmentNode remembers a node of a fragment, see fragmentNodes
func (y *YamlConfigFileHandler) addFragmentNode(node *yaml.Node) {
	if y.fragmentNodes == nil {
		y.fragmentNodes = map[*yaml.Node]struct{}{}
	}
	y.fragmentNodes[node] = struct{}{}
}

// isFragmentNode returns whether the node belongs to an inserted fragment, see fragmentNodes
func (y *YamlConfigFileHandler) isFragmentNode(node *yaml.Node) bool {
	_, found := y.fragmentNodes[node]
	return found
}

// documentAnchors returns the anchor names of the document, including the ones of fragments inserted into it so far.
// They are collected once per document rather than per fragment, and only for documents fragments with anchors are
// inserted into, so renaming stays linear.
func (y *YamlConfigFileHandler) documentAnchors(document int) map[string]int {
	if y.anchors == nil {
		y.anchors = make([]map[string]int, len(y.documents))
	}
	if y.anchors[document] == nil {
		names := map[string]int{}
		var visit func(node *yaml.Node)
		visit = func(node *yaml.Node) {
			if node.Anchor != "" {
				names[node.Anchor] = 1
			}
			for _, child := range node.Content {
				visit(child)
			}
		}
		visit(y.documents[document])
		y.anchors[document] = names
	}
	return y.anchors[document]
}

// keepYamlAnchors gives the nodes of the fragment that replace anchored nodes the anchors of the replaced nodes, so
// aliases of them, which refer to anchors by name, still refer to the fragment. The outer anchor wins over an anchor
// of the node itself, and aliases within the fragment referring to the node get the outer name. Every other anchor of
// the fragment whose name is one of the names, the anchor names of the document collected before the fragment was
// inserted, is renamed to a free name, e.g. k_1, together with the aliases referring to it. So aliases of the document
// keep referring to its nodes rather than to nodes of the fragment, and the name of an anchor a fragment dropped stays
// reserved, so its aliases fail writing, see checkAliases. The names of the fragment are added to the names afterwards.
// names is nil for a fragment without anchors, which has no aliases either.
func (y *YamlConfigFileHandler) keepYamlAnchors(fragment *yaml.Node, kept map[*yaml.Node]string, names map[string]int) {
	for node, anchor := range kept {
		if anchor == "" {
			delete(kept, node)
			continue
		}
		node.Anchor = anchor
	}
	if names == nil {
		return
	}

	// own are the anchor names of the fragment, which a new name must not clash with either
	own := map[string]struct{}{}
	var collect func(node *yaml.Node)
	collect = func(node *yaml.Node) {
		if _, isKept := kept[node]; !isKept && node.Anchor != "" {
			own[node.Anchor] = struct{}{}
		}
		for _, child := range node.Content {
			collect(child)
		}
	}
	collect(fragment)

	// The anchors are renamed before the aliases after them in the fragment, so an alias gets the current name of
	// the node it refers to
	renamed := map[string]string{}
	var visit func(node *yaml.Node)
	visit = func(node *yaml.Node) {
		if node.Kind == yaml.AliasNode && node.Alias != nil && node.Alias.Anchor != "" {
			node.Value = node.Alias.Anchor
		}
		if _, isKept := kept[node]; !isKept && node.Anchor != "" {
			if _, clashes := names[node.Anchor]; clashes {
				name, found := renamed[node.Anchor]
				if !found {
					name = freeYamlAnchor(names, own, node.Anchor)
					renamed[node.Anchor] = name
				}
				node.Anchor = name
			}
		}
		for _, child := range node.Content {
			visit(child)
		}
	}
	visit(fragment)

	for name := range own {
		if _, wasRenamed := renamed[name]; !wasRenamed {
			names[name] = max(names[name], 1)
		}
	}
}

// freeYamlAnchor returns a name based on the anchor that is neither one of the names nor of own, e.g. k_1, and adds
// it to the names. The next suffix to try is kept per name, so renaming many anchors of the same name stays linear.
func freeYamlAnchor(names map[string]int, own map[string]struct{}, anchor string) string {
	for suffix := max(names[anchor], 1); ; suffix++ {
		candidate := anchor + "_" + strconv.Itoa(suffix)
		_, used := names[candidate]
		_, usedByFragment := own[candidate]
		if !used && !usedByFragment {
			names[anchor] = suffix + 1
			names[candidate] = 1
			return candidate
		}
	}
}

// moveYamlBlockComments moves line comments the encoder cannot write where they were to the first entry of a block
// mapping or sequence: the line comment of a key whose value is a block with an anchor or tag, which the encoder would
// write before the anchor, e.g. "a: # c\n&k\n  x: 1", and the line comment of a block itself, which the encoder
// defers to the next line it writes, e.g. after the key of the next value and so before its anchor, and the line
// comment of a key whose value is an alias, written inline or a scalar with a comment of its own, or of a key that is
// a mapping or sequence, e.g. "{}: v", which is deferred or written before the colon as well. Such comments come
// from fragments, e.g. "[1] # c" written as a block, or from the parser, which reads the comment after an anchor, e.g.
// "a: &k # c\n  x: 1", as the comment of the first entry as well.
func moveYamlBlockComments(node *yaml.Node) {
	if node.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(node.Content); i += 2 {
			key, value := node.Content[i], node.Content[i+1]
			if key.LineComment == "" {
				continue
			}
			switch {
			case hasYamlBlockProperties(value), key.Kind != yaml.ScalarNode && isYamlBlock(value):
				moveYamlCommentToFirstEntry(value, key.LineComment)
				key.LineComment = ""
			case value.Kind == yaml.AliasNode || (value.Kind == yaml.MappingNode || value.Kind == yaml.SequenceNode) && !isYamlBlock(value),
				value.Kind == yaml.ScalarNode && value.LineComment != "", key.Kind != yaml.ScalarNode:
				// The encoder defers the comment of a key whose value is an alias or written inline, e.g. "a: []", or a
				// scalar with a comment of its own, since a line has one comment, but writes the comment of the value on
				// its line
				value.LineComment = joinYamlComments(key.LineComment, value.LineComment)
				key.LineComment = ""
			}
		}
	}
	if node.LineComment != "" && isYamlBlock(node) {
		moveYamlCommentToFirstEntry(node, node.LineComment)
		node.LineComment = ""
	}
	for _, child := range node.Content {
		moveYamlBlockComments(child)
	}
}

// isYamlBlock returns whether the node is a nonempty mapping or sequence the encoder writes in block style. Empty ones
// are written as {} and [].
func isYamlBlock(node *yaml.Node) bool {
	return (node.Kind == yaml.MappingNode || node.Kind == yaml.SequenceNode) && node.Style&yaml.FlowStyle == 0 &&
		len(node.Content) > 0
}

// hasYamlBlockProperties returns whether the node is a block mapping or sequence written with an anchor or a tag on
// the line of its key. The encoder omits the default tags !!map and !!seq unless they were written explicitly.
func hasYamlBlockProperties(node *yaml.Node) bool {
	if !isYamlBlock(node) {
		return false
	}
	if node.Anchor != "" || node.Style&yaml.TaggedStyle != 0 {
		return true
	}
	tag := node.ShortTag()
	return tag != "!!map" && tag != "!!seq"
}

// moveYamlCommentToFirstEntry makes the comment the line comment of the first entry of the block if it is a scalar
// without one, or of a first key whose value is a scalar without one either, since the encoder writes only one of
// them. Otherwise it puts the comment before the first entry, after the head comment of the block, so no comment is
// lost.
func moveYamlCommentToFirstEntry(block *yaml.Node, comment string) {
	first := block.Content[0]
	free := first.Kind == yaml.ScalarNode && first.LineComment == ""
	if block.Kind == yaml.MappingNode && len(block.Content) > 1 {
		value := block.Content[1]
		free = free && value.Kind == yaml.ScalarNode && value.LineComment == ""
	}
	if free {
		first.LineComment = comment
		return
	}
	// The encoder writes only one of the head comments of the block and its first entry, so they are joined in the
	// order they are written
	first.HeadComment = strings.Join(slices.DeleteFunc([]string{block.HeadComment, comment, first.HeadComment},
		func(c string) bool { return c == "" }), "\n")
	block.HeadComment = ""
}

// joinYamlComments returns the line comments as one, e.g. "# a # b", since a node has only one
func joinYamlComments(first string, second string) string {
	if first == "" || second == "" {
		return first + second
	}
	return first + " " + second
}

// checkAliases rejects aliases whose anchor is not defined before them in their document, e.g. an alias of a value
// whose anchor a fragment dropped, and merge keys whose value is a fragment, or an alias of one, other than a mapping
// or a list of mappings, e.g. an alias of a scalar fragment used as a merge key, which the decoder would reject. Only
// fragments can cause undefined aliases, since the input was decoded, so nothing is checked without them. An alias of a
// fragment must refer to the node it referred to in the fragment, see keepsFragmentBinding. An invalid
// merge key of the input is written as read, like without fragments, since the decoder of the input doesn't check
// them either. An alias of the input is named by its anchor and line, while an alias of a fragment isn't, see
// aliasError.
func (y *YamlConfigFileHandler) checkAliases() error {
	if !y.insertedFragment {
		return nil
	}
	var defined map[string]*yaml.Node
	// targets are the nodes the aliases refer to where they are written, since a later anchor of the same name, e.g.
	// within the same list of merge values, doesn't change them
	targets := map[*yaml.Node]*yaml.Node{}
	var visit func(node *yaml.Node) error
	visit = func(node *yaml.Node) error {
		if node.Kind == yaml.AliasNode {
			target, found := defined[node.Value]
			if !found {
				return y.aliasError(node, "refers to an anchor that is not defined before it")
			}
			if !y.keepsFragmentBinding(node, target) {
				return errors.New("an alias within a fragment would refer to another value, " +
					"since an anchor of the same name is defined between them")
			}
			targets[node] = target
			return nil
		}
		if node.Anchor != "" {
			defined[node.Anchor] = node
		}
		for i, child := range node.Content {
			if err := visit(child); err != nil {
				return err
			}
			if node.Kind == yaml.MappingNode && i%2 == 1 && isYamlMergeKey(node.Content[i-1]) {
				if err := y.checkMergeValue(node.Content[i-1], child, targets); err != nil {
					return err
				}
			}
		}
		return nil
	}
	for _, document := range y.documents {
		defined = map[string]*yaml.Node{}
		if err := visit(document); err != nil {
			return err
		}
	}
	return nil
}

// keepsFragmentBinding returns whether an alias of a fragment refers to the node it referred to in the fragment when it
// resolves to the target by name. The anchor of a fragment's node may get the name of the anchor of the value it
// replaces, which the input may define again between the node and the alias, e.g. "a: &k old", "b: &k other" and a
// fragment "a: &v new\nc: *v" used as a key. Aliases of the input keep referring to their nodes, since anchors of
// fragments with names of the document are renamed and kept anchors stay at the position of the replaced value.
func (y *YamlConfigFileHandler) keepsFragmentBinding(alias *yaml.Node, target *yaml.Node) bool {
	if !y.isFragmentNode(alias) || alias.Alias == nil {
		return true
	}
	intended := alias.Alias
	if copied, found := y.fragmentCopies[intended]; found {
		intended = copied
	}
	return target == intended
}

// checkMergeValue rejects the value of the merge key if it is a node of a fragment, or a list containing one, or an
// alias referring to one, other than a mapping. The aliases are resolved through targets, the nodes they refer to where
// they are written, like the decoder does when it reads the output; they are all recorded, since checkAliases visits
// them first. Values of the input are not checked, see checkAliases. The error names the line of a merge key of the
// input.
func (y *YamlConfigFileHandler) checkMergeValue(key *yaml.Node, value *yaml.Node, targets map[*yaml.Node]*yaml.Node) error {
	items := []*yaml.Node{value}
	if value.Kind == yaml.SequenceNode {
		items = value.Content
	}
	for _, item := range items {
		target := item
		if item.Kind == yaml.AliasNode {
			target = targets[item]
		}
		if !y.isFragmentNode(target) || target.Kind == yaml.MappingNode {
			continue
		}
		switch {
		case item.Kind == yaml.AliasNode:
			return y.aliasError(item, "is used as the value of a merge key, but does not refer to a mapping")
		case y.isFragmentNode(key):
			return errors.New("the value of a merge key within a fragment is neither a mapping nor a list of mappings")
		default:
			return fmt.Errorf("the value of the merge key on line %d is neither a mapping nor a list of mappings", key.Line)
		}
	}
	return nil
}

// aliasError returns the error of an alias, which names the anchor and line of an alias of the input, but not of an
// alias of a fragment. Anchor names are structure rather than values, e.g. a diff of check shows them, but the names
// and lines of a fragment would only point into the fragment text, so errors stay conservative and leave them out.
func (y *YamlConfigFileHandler) aliasError(alias *yaml.Node, reason string) error {
	if y.isFragmentNode(alias) {
		return errors.New("an alias within a fragment " + reason)
	}
	return fmt.Errorf("the alias *%s on line %d %s", alias.Value, alias.Line, reason)
}
