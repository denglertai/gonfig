package file

import (
	"bytes"
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/denglertai/gonfig/internal/general"
	"github.com/denglertai/gonfig/internal/schema"
	"github.com/denglertai/gonfig/internal/value"
	"github.com/pelletier/go-toml/v2/unstable"
	"go.yaml.in/yaml/v4"
)

// Keys produced by placeholders may contain secrets, so schema violations mask them, see FileProcessor.RedactKeys.
// They are found by their location in the written document rather than their text, so a static key that happens to
// equal a processed one stays visible and doesn't reveal the equality. The locations use the keys as the schema
// validator decodes them from the written document, e.g. 1000 for a YAML key "!!float ${N}" or U+FFFD for invalid
// UTF-8 in a JSON key, see the keyLocator of each file type. The static keys are located as well, so a key is only
// shown where a static key is located: a processed key the locator places wrongly is masked, even if a static key
// elsewhere has the same text.

// keyLocations maps the locations of the keys of the written documents, see locationKey, to the masked text of keys
// produced by placeholders, or to "" for static keys
type keyLocations map[string]string

// add records the key at the location with its masked text, or "" for a static key. A static key doesn't replace a
// processed one at the same location, e.g. a merged key overridden by a processed one, since the validator may
// decode either of them there.
func (k keyLocations) add(location string, masked string) {
	if _, recorded := k[location]; recorded && masked == "" {
		return
	}
	k[location] = masked
}

// locationKey returns the key of a location within a document in keyLocations. Each part is quoted, so the parts
// cannot run into each other whatever they contain.
func locationKey(document int, location []string) string {
	var key strings.Builder
	key.WriteString(strconv.Itoa(document))
	for _, part := range location {
		key.WriteString(strconv.Quote(part))
	}
	return key.String()
}

// keyLocatorSource is implemented by the handlers of file types validated against schemas, whose keys may be produced
// by placeholders. keyLocator is called before Write with the processed entries that are or may be keys, see
// keyCandidate, and returns a keyLocator keeping only what is needed to locate them, e.g. the positions Write writes
// the keys at, but not the entries, so the handler can be freed while writing.
type keyLocatorSource interface {
	keyLocator(candidates map[ConfigEntry]string) keyLocator
}

// keyLocator locates the keys produced by placeholders in a written document. locateKeys gets the number of documents
// the validator decoded from the written content and returns the locations of the keys, or false if it cannot be sure
// to have found all of them, e.g. because the documents don't match, in which case every key is masked.
type keyLocator interface {
	locateKeys(documents int) (keyLocations, bool)
}

// The handlers of the file types with keys as entries
var (
	_ keyLocatorSource = (*YamlConfigFileHandler)(nil)
	_ keyLocatorSource = (*JsonConfigFileHandler)(nil)
	_ keyLocatorSource = (*TomlFileHandler)(nil)
)

// keyCandidate is implemented by entries that may end up as a key of the written document
type keyCandidate interface {
	// mayBeKey returns whether the entry is a key, or a value that may be used as a key, e.g. through a YAML alias
	mayBeKey() bool
}

// mayBeKey returns true for keys and anchored values, which aliases may use as keys
func (y *YamlConfigEntry) mayBeKey() bool {
	return y.isKey || y.node.Anchor != ""
}

// aliasedValue is implemented by entries that are values an alias may use as a key, see recordFragmentValues
type aliasedValue interface {
	// isAliasedValue returns whether the entry is a value an alias may use as a key
	isAliasedValue() bool
}

// isAliasedValue returns true for anchored values, which aliases may use as keys
func (y *YamlConfigEntry) isAliasedValue() bool {
	return !y.isKey && y.node.Anchor != ""
}

// mayBeKey returns whether the entry is a key
func (j *JsonConfigEntry) mayBeKey() bool {
	return j.isKey
}

// mayBeKey returns whether the entry is a key or a part of a dotted key
func (t *TomlConfigEntry) mayBeKey() bool {
	return t.isKey
}

// recordKeyCandidate records an entry with placeholders that may end up as a key, with its text masked like in the
// diff of Check, e.g. "pre_***". The masked text is built from the text before processing, so no filters run again.
// An entry with escape sequences only, e.g. "$${X}", is a key of known text, which is recorded as a static key. The
// values of fragments are masked whatever they contain, so fragment is set for the entries of a fragment, whose values
// are recorded by recordFragmentValues instead. Nothing is recorded unless the content is validated, see
// FileProcessor.Validated.
func (fp *FileProcessor) recordKeyCandidate(entry ConfigEntry, current string, fragment bool) {
	if !fp.Validated || fp.report != nil {
		return
	}
	if candidate, ok := entry.(keyCandidate); !ok || !candidate.mayBeKey() {
		return
	}
	if aliased, ok := entry.(aliasedValue); fragment && ok && aliased.isAliasedValue() {
		return
	}
	if !value.HasPlaceholders(current) {
		fp.addStaticKey(value.MaskPlaceholders(current))
		return
	}
	if fp.keyCandidates == nil {
		fp.keyCandidates = make(map[ConfigEntry]string)
	}
	fp.keyCandidates[entry] = value.MaskPlaceholders(current)
}

// recordFragmentValues records the values of a fragment that aliases may use as keys, e.g. "&a v" of
// "{v: &a v, m: {*a : 1}}", as keys produced by placeholders masked as "***". The values of fragments are masked like
// in the diff of Check, so their text must not be shown as a key, even without placeholders or with escape sequences
// only. Nothing is recorded unless the content is validated, see FileProcessor.Validated.
func (fp *FileProcessor) recordFragmentValues(inserted []ConfigEntry) {
	if !fp.Validated || fp.report != nil {
		return
	}
	for _, entry := range inserted {
		if aliased, ok := entry.(aliasedValue); ok && aliased.isAliasedValue() {
			if fp.keyCandidates == nil {
				fp.keyCandidates = make(map[ConfigEntry]string)
			}
			fp.keyCandidates[entry] = value.Mask
		}
	}
}

// keyLocatorOf returns what is needed to locate the keys produced by placeholders after writing, or nil if there are
// none or the content is not validated. It is called before writing, so the handler, and with it the document as
// read, is not referenced after writing and can be freed while it is written, e.g. while JSON is indented. The
// candidates are not needed afterwards.
func (fp *FileProcessor) keyLocatorOf(handler ConfigFileHandler) keyLocator {
	candidates := fp.keyCandidates
	fp.keyCandidates = nil
	if len(candidates) == 0 {
		return nil
	}
	source, ok := handler.(keyLocatorSource)
	if !ok {
		return nil
	}
	return source.keyLocator(candidates)
}

// RedactKeys returns the location within the document written by the last Process with the keys produced by
// placeholders replaced by their masked text, e.g. "***" or "pre_***" for a key "pre_${VAR}", see schema.KeyRedactor.
// document is the index of the document and documents the number of documents the validator decoded, which differ
// from 0 and 1 for YAML files with several documents only. indexes tells which parts are array indexes.
//
// The keys are located on the first call, the static keys as well. A key is only shown if a static key is located
// at its location, so a key at a location the locator doesn't know, e.g. because it places a processed key wrongly,
// is masked as "***" even if a static key elsewhere has the same text. Besides, as a safety net for processed keys
// the locator takes for static ones, only keys of known static origin are shown: keys of the input and of fragments as
// decoded, see recordStaticKeys. Array indexes are kept. Keys of fragments are part of the structure like in the diff
// of Check, so they are shown. Without keys produced by placeholders nothing is located, and the keys of known static
// origin are shown. If the keys cannot be located for sure, every part of every location is masked, including array
// indexes, since leaking a key is worse than hiding the structure. Without Validated, nothing is recorded, so every
// key is masked.
func (fp *FileProcessor) RedactKeys(document int, documents int, location []string, indexes []bool) []string {
	if fp.keyLocator != nil {
		var sure bool
		fp.keys, sure = fp.keyLocator.locateKeys(documents)
		fp.keysUnsure = !sure
		fp.keyLocator = nil
	}

	redacted := slices.Clone(location)
	for i, part := range location {
		if fp.keysUnsure {
			redacted[i] = value.Mask
			continue
		}
		masked, located := fp.keys[locationKey(document, location[:i+1])]
		switch {
		case masked != "":
			redacted[i] = masked
		case i < len(indexes) && indexes[i]:
		case fp.keys != nil && !located:
			// No key is known at the location, so it may be a processed key the locator placed elsewhere
			redacted[i] = value.Mask
		default:
			if _, static := fp.staticKeys[part]; !static {
				redacted[i] = value.Mask
			}
		}
	}
	return redacted
}

// addStaticKey records a key of known static origin, which is shown in violations, see RedactKeys
func (fp *FileProcessor) addStaticKey(key string) {
	if fp.staticKeys == nil {
		fp.staticKeys = make(map[string]struct{})
	}
	fp.staticKeys[key] = struct{}{}
}

// recordStaticKeys records the keys of the input or of a fragment as the validator decodes them, which contain no
// results of placeholders, so they are shown in violations, see RedactKeys. Keys with placeholders are recorded with
// their placeholders, which are no secrets. JSON, TOML, dotenv and INI content is decoded like the validator decodes
// it. YAML is parsed into nodes, since decoding fails for placeholders after tags, e.g. "!!int ${PORT}", and keys
// that are aliases of anchored scalars are recorded as well. A fragment of a TOML file is a value or the key/value
// pairs of a fragment used as a key, so both are tried. Content that cannot be decoded records nothing, so its keys
// are masked.
func (fp *FileProcessor) recordStaticKeys(content []byte, fragment bool) {
	switch fp.FileType {
	case general.YAML, general.YML:
		fp.recordYamlStaticKeys(content, fragment)
	case general.TOML:
		if documents, err := schema.Decode(general.TOML, content); err == nil {
			fp.recordDecodedKeys(documents...)
		}
		if fragment {
			if documents, err := schema.Decode(general.TOML, append([]byte(tomlFragmentKey), content...)); err == nil {
				if pair, ok := documents[0].(map[string]any); ok {
					fp.recordDecodedKeys(pair["v"])
				}
			}
		}
	case general.JSON, general.ENV, general.DOTENV, general.INI:
		if documents, err := schema.Decode(fp.FileType, content); err == nil {
			fp.recordDecodedKeys(documents...)
		}
	}
}

// recordDecodedKeys records the keys of the objects within the decoded values
func (fp *FileProcessor) recordDecodedKeys(values ...any) {
	for _, decoded := range values {
		switch v := decoded.(type) {
		case map[string]any:
			for key, item := range v {
				fp.addStaticKey(key)
				fp.recordDecodedKeys(item)
			}
		case []any:
			fp.recordDecodedKeys(v...)
		}
	}
}

// recordYamlStaticKeys records the keys and anchored scalars of every YAML document of the content, both as written
// and as decoded with their tag, e.g. 0x1F and 31 for "!!int 0x1F", since the decoder keeps the text of an integer
// key merged into a mapping with string keys. The values of a fragment are results of placeholders, so anchored
// scalars of fragments are not recorded, which an alias could use as a key.
func (fp *FileProcessor) recordYamlStaticKeys(content []byte, fragment bool) {
	var record func(node *yaml.Node)
	record = func(node *yaml.Node) {
		if node.Kind == yaml.MappingNode {
			for i := 0; i+1 < len(node.Content); i += 2 {
				fp.recordYamlScalar(node.Content[i])
			}
		}
		if node.Anchor != "" && !fragment {
			fp.recordYamlScalar(node)
		}
		for _, child := range node.Content {
			record(child)
		}
	}

	decoder := yaml.NewDecoder(bytes.NewReader(content))
	for {
		var document yaml.Node
		if decoder.Decode(&document) != nil {
			return
		}
		record(&document)
	}
}

// recordYamlScalar records the text of a scalar as written and as decoded, see recordYamlStaticKeys
func (fp *FileProcessor) recordYamlScalar(node *yaml.Node) {
	if node.Kind != yaml.ScalarNode {
		return
	}
	fp.addStaticKey(node.Value)
	if text, ok := yamlKeyText(node); ok {
		fp.addStaticKey(text)
	}
}

// The schema validator gets the redactor of the processor
var _ schema.KeyRedactor = (*FileProcessor)(nil).RedactKeys

// yamlKeyLocator locates the keys produced by placeholders and the static keys in the node tree of a YAML document.
// The tree is what is written, and the keys are decoded from it like the validator decodes them from the written
// document.
type yamlKeyLocator struct {
	// candidates are the masked texts of the processed scalars that may be keys
	candidates map[*yaml.Node]string
	locations  keyLocations
	document   int
	// aliases are the nodes the aliases refer to in the written documents, see yamlAliasTargets
	aliases map[*yaml.Node]*yaml.Node
	// active are the anchored nodes being walked through aliases, which stop aliases referring to their own ancestors
	active map[*yaml.Node]bool
	// contains memoizes whether a node contains candidates, following aliases, see containsCandidates
	contains map[*yaml.Node]bool
	// budget is the number of nodes with candidates the walk may still visit. Aliases may expand a small document
	// exponentially, e.g. "a1: &a1 [*a0, *a0, ...]", so the walk stops once it is used up, see yamlWalkBudget.
	budget int
	// spare is the number of nodes without candidates the walk may still visit. Once it is used up, such nodes are
	// skipped, so their keys are not located and masked, but the keys produced by placeholders are still located.
	spare int
}

// yamlWalkBudget returns the number of nodes the walk of documents with the number of nodes may visit: every node
// a hundred times, which is plenty for keys within mappings merged or referred to through aliases
func yamlWalkBudget(nodes int) int {
	return 100*nodes + 1000
}

// yamlKeys keeps the documents of a YAML file and the processed scalars that may be keys, see yamlKeys.locateKeys
type yamlKeys struct {
	documents []*yaml.Node
	// candidates are the masked texts of the processed scalars that may be keys
	candidates map[*yaml.Node]string
}

// keyLocator returns the documents with the nodes of the candidates, without the entries of the handler
func (y *YamlConfigFileHandler) keyLocator(candidates map[ConfigEntry]string) keyLocator {
	keys := &yamlKeys{documents: y.documents, candidates: make(map[*yaml.Node]string, len(candidates))}
	for entry, masked := range candidates {
		if yamlEntry, ok := entry.(*YamlConfigEntry); ok {
			keys.candidates[yamlEntry.node] = masked
		}
	}
	return keys
}

// locateKeys returns the locations of the keys produced by placeholders and of the static keys in every document,
// including keys that are aliases of processed values and keys of mappings merged or referred to through aliases. It
// returns false if the documents don't match the ones the validator decoded or the walk exceeds its budget for nodes
// with candidates.
func (y *yamlKeys) locateKeys(documents int) (keyLocations, bool) {
	written, ok := writtenYamlDocuments(y.documents, documents)
	if !ok {
		return nil, false
	}
	locator := yamlKeyLocator{candidates: y.candidates, locations: keyLocations{}, aliases: yamlAliasTargets(written),
		active: map[*yaml.Node]bool{}, contains: map[*yaml.Node]bool{}}
	for _, document := range written {
		locator.containsCandidates(document)
	}
	locator.budget = yamlWalkBudget(len(locator.contains))
	locator.spare = locator.budget
	for i, document := range written {
		locator.document = i
		locator.walk(document, nil)
	}
	return locator.locations, locator.budget >= 0
}

// writtenYamlDocuments returns the documents that are documents of the written content, which the validator decoded
// the number of documents from. The encoder writes the first document without "---", so a first empty document, e.g.
// of "---\n---\na: 1", is no document of the written content. It returns false if the documents still don't match.
func writtenYamlDocuments(documents []*yaml.Node, decoded int) ([]*yaml.Node, bool) {
	if len(documents) == decoded+1 && emptyYamlDocument(documents[0]) {
		documents = documents[1:]
	}
	return documents, len(documents) == decoded
}

// emptyYamlDocument returns whether the document is empty, i.e. has no content or an implicit null only, unlike an
// explicit "~", "!!null" or "". The encoder removes the tag of an implicit null when writing it.
func emptyYamlDocument(document *yaml.Node) bool {
	if len(document.Content) == 0 {
		return true
	}
	content := document.Content[0]
	return content.Kind == yaml.ScalarNode && content.Value == "" && content.Style == 0 &&
		(content.Tag == "" || content.ShortTag() == "!!null")
}

// yamlAliasTargets returns the node each alias of the documents refers to when the written content is decoded: the
// last node before it with its anchor, or nil if there is none. Anchors of fragments with the name of an anchor of the
// document are renamed when they are inserted, see YamlConfigFileHandler.keepYamlAnchors, so an alias of the input
// refers to the node it referred to when it was read, or to the fragment that replaced it, but resolving the written
// names keeps the walk independent of how the tree was built. The nodes are visited in the order they are written.
// Anchors are reset per document like the decoder does, so an alias referring to an anchor of an earlier document gets
// nil.
func yamlAliasTargets(documents []*yaml.Node) map[*yaml.Node]*yaml.Node {
	targets := map[*yaml.Node]*yaml.Node{}
	var anchors map[string]*yaml.Node
	var visit func(node *yaml.Node)
	visit = func(node *yaml.Node) {
		if node.Kind == yaml.AliasNode {
			targets[node] = anchors[node.Value]
			return
		}
		if node.Anchor != "" {
			anchors[node.Anchor] = node
		}
		for _, child := range node.Content {
			visit(child)
		}
	}
	for _, document := range documents {
		anchors = map[string]*yaml.Node{}
		visit(document)
	}
	return targets
}

// resolve returns the node an alias refers to in the written document, see yamlAliasTargets, or the node itself if it
// is no alias. It returns nil for an alias referring to no node, which the validator cannot decode.
func (l *yamlKeyLocator) resolve(node *yaml.Node) *yaml.Node {
	if node.Kind == yaml.AliasNode {
		return l.aliases[node]
	}
	return node
}

// containsCandidates returns whether the node or a node within it, following aliases, is a candidate. The result is
// memoized for every node, so an alias costs a lookup only. A node referred to by an alias within itself counts as
// containing candidates while it is computed, since its result is not known yet: a wrong true only costs walking
// the node, which the active aliases and the budget bound, while a wrong false would skip keys.
func (l *yamlKeyLocator) containsCandidates(node *yaml.Node) bool {
	if node.Kind == yaml.AliasNode {
		target := l.resolve(node)
		return target != nil && l.containsCandidates(target)
	}
	if found, ok := l.contains[node]; ok {
		return found
	}
	l.contains[node] = true
	_, found := l.candidates[node]
	for _, child := range node.Content {
		found = l.containsCandidates(child) || found
	}
	l.contains[node] = found
	return found
}

// visit counts a visit of the node and returns whether the walk may visit it: nodes with candidates while the budget
// lasts and nodes without candidates while the spare lasts
func (l *yamlKeyLocator) visit(node *yaml.Node) bool {
	if l.containsCandidates(node) {
		l.budget--
		return l.budget >= 0
	}
	l.spare--
	return l.spare >= 0
}

// walk walks the node at the location, following aliases, whose content is part of the decoded document where they
// are. Nodes without candidates are walked as well to locate the static keys, as long as the spare lasts.
func (l *yamlKeyLocator) walk(node *yaml.Node, location []string) {
	if !l.visit(node) {
		return
	}
	switch node.Kind {
	case yaml.DocumentNode:
		for _, child := range node.Content {
			l.walk(child, location)
		}
	case yaml.AliasNode:
		if target := l.resolve(node); target != nil && !l.active[target] {
			l.active[target] = true
			l.walk(target, location)
			delete(l.active, target)
		}
	case yaml.SequenceNode:
		index := 0
		for _, child := range node.Content {
			// The decoder drops merge keys from sequences, e.g. "- !!merge x", so the items after them move up
			if target := l.resolve(child); target == nil || isYamlMergeScalar(target) {
				continue
			}
			l.walk(child, append(slices.Clip(location), strconv.Itoa(index)))
			index++
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(node.Content); i += 2 {
			l.pair(node.Content[i], node.Content[i+1], location)
		}
	}
}

// pair records the key and walks the value of a mapping at the location. The keys of merged mappings
// ("<<: *base") are keys of the mapping itself. A processed key other than a string is recorded at its decoded text
// and at its text as written, since the decoder keeps the latter when the key is merged into a mapping with string
// keys, so a static key of that text merged there as well doesn't show it.
func (l *yamlKeyLocator) pair(key *yaml.Node, value *yaml.Node, location []string) {
	if isYamlMergeKey(key) {
		l.merge(value, location)
		return
	}

	// A key that is an alias decodes to the anchored node, e.g. a processed value
	resolved := l.resolve(key)
	if resolved == nil {
		return
	}
	text, ok := yamlKeyText(resolved)
	if !ok {
		// The document cannot be decoded, so the validator fails before reporting any violation
		return
	}
	masked := l.candidates[resolved]
	if masked != "" && resolved.Kind == yaml.ScalarNode && resolved.ShortTag() != "!!str" && resolved.Value != text {
		// The decoder keeps the text of a key other than a string merged into a mapping with string keys, e.g. 0x5EC
		// for "!!int 0x5EC", so the processed key is masked there as well, also if a static key of that text is merged
		l.locations.add(locationKey(l.document, append(slices.Clip(location), resolved.Value)), masked)
	}
	location = append(slices.Clip(location), text)
	l.locations.add(locationKey(l.document, location), masked)
	l.walk(value, location)
}

// isYamlMergeScalar returns whether the decoder takes the node for a merge key like it does in a sequence, where it
// drops it: a scalar tagged "!!merge" or an untagged plain "<<", see isYamlMergeKey
func isYamlMergeScalar(node *yaml.Node) bool {
	if node.Kind != yaml.ScalarNode {
		return false
	}
	if node.Tag == "" {
		quoted := yaml.DoubleQuotedStyle | yaml.SingleQuotedStyle | yaml.LiteralStyle | yaml.FoldedStyle
		return node.Value == "<<" && node.Style&quoted == 0
	}
	return node.ShortTag() == "!!merge"
}

// isYamlMergeKey returns whether the key is a merge key: a plain "<<", whose tag the handler removes, see
// handleMappingPair, or one tagged as such
func isYamlMergeKey(key *yaml.Node) bool {
	if key.Kind != yaml.ScalarNode || key.Value != "<<" {
		return false
	}
	return key.ShortTag() == "!!merge" || (key.Tag == "" && key.Style&(yaml.DoubleQuotedStyle|yaml.SingleQuotedStyle) == 0)
}

// merge walks the mappings merged into the mapping at the location: a mapping, an alias of one or a sequence of them
func (l *yamlKeyLocator) merge(node *yaml.Node, location []string) {
	if !l.visit(node) {
		return
	}
	switch node.Kind {
	case yaml.AliasNode:
		if target := l.resolve(node); target != nil && !l.active[target] {
			l.active[target] = true
			l.merge(target, location)
			delete(l.active, target)
		}
	case yaml.SequenceNode:
		for _, item := range node.Content {
			l.merge(item, location)
		}
	case yaml.MappingNode:
		l.walk(node, location)
	}
}

// yamlKeyText returns the key as the validator gets it: decoded with its tag, e.g. 31 for "!!int 0x1F", with keys
// other than strings converted like schema.KeyText. It returns false if the key cannot be decoded.
func yamlKeyText(key *yaml.Node) (string, bool) {
	if key.Kind == yaml.ScalarNode && key.ShortTag() == "!!str" {
		return key.Value, true
	}
	var decoded any
	if err := key.Decode(&decoded); err != nil {
		return "", false
	}
	if text, ok := decoded.(string); ok {
		return text, true
	}
	return schema.KeyText(decoded), true
}

// jsonKeys are the locations of the keys produced by placeholders and of the static keys in a JSON file, see
// JsonConfigFileHandler.keyLocator
type jsonKeys struct {
	locations keyLocations
}

// keyLocator locates the keys produced by placeholders and the static keys in the object tree, which is what is
// written. A JSON document has no aliases, so a single walk over the tree finds them, and the tree can be freed while
// it is written.
func (j *JsonConfigFileHandler) keyLocator(candidates map[ConfigEntry]string) keyLocator {
	// Keys are identified by their value node, which is kept when the value is replaced by a fragment
	keys := make(map[*jsonNode]string, len(candidates))
	for entry, masked := range candidates {
		if jsonEntry, ok := entry.(*JsonConfigEntry); ok && jsonEntry.isKey {
			keys[jsonEntry.node] = masked
		}
	}
	located := &jsonKeys{locations: keyLocations{}}
	// Every key is located before writing, so the walk extends the location key of the parent, see locationKey,
	// rather than building it from the parts for every key
	var walk func(node *jsonNode, parent string)
	walk = func(node *jsonNode, parent string) {
		for i, child := range node.children {
			if node.kind != jsonObject {
				walk(child, parent+strconv.Quote(strconv.Itoa(i)))
				continue
			}
			location := parent + strconv.Quote(jsonKeyText(node.keys[i]))
			located.locations.add(location, keys[child])
			walk(child, location)
		}
	}
	walk(j.root, locationKey(0, nil))
	return located
}

// locateKeys returns the keys located before writing. A JSON file is a single document, so it returns false for any
// other number of documents.
func (j *jsonKeys) locateKeys(documents int) (keyLocations, bool) {
	if documents != 1 {
		return nil, false
	}
	return j.locations, true
}

// jsonKeyText returns the key as the validator decodes it from the written document. The encoder writes invalid
// UTF-8 as U+FFFD, so such keys are encoded and decoded like when writing and validating.
func jsonKeyText(key string) string {
	if utf8.ValidString(key) {
		return key
	}
	// Marshalling a string and unmarshalling the result never fails
	encoded, _ := json.Marshal(key)
	var decoded string
	_ = json.Unmarshal(encoded, &decoded)
	return decoded
}

// keyLocator returns the positions Write writes the keys produced by placeholders at, which it records, see
// tomlWrittenKeys. The handler changes the text of the document only, so the written document is parsed again to
// locate the keys.
func (t *TomlFileHandler) keyLocator(candidates map[ConfigEntry]string) keyLocator {
	keys := &tomlWrittenKeys{masks: make(map[*TomlConfigEntry]string, len(candidates))}
	for entry, masked := range candidates {
		if tomlEntry, ok := entry.(*TomlConfigEntry); ok && tomlEntry.isKey {
			keys.masks[tomlEntry] = masked
		}
	}
	t.writtenKeys = keys
	return keys
}

// locateKeys returns the locations of the keys produced by placeholders and of the static keys in the written
// document. A TOML file is a single document, so it returns false for any other number of documents.
func (k *tomlWrittenKeys) locateKeys(documents int) (keyLocations, bool) {
	if documents != 1 {
		return nil, false
	}
	locations := keyLocations{}
	walker := tomlKeyWalker{written: k.positions, locations: locations, arrayTables: map[string]int{}}
	parser := unstable.Parser{}
	parser.Reset(k.written)
	for parser.NextExpression() {
		walker.expression(parser.Expression())
	}
	return locations, true
}

// tomlKeyWalker walks the expressions of a written TOML document and records the locations of the keys written by
// the processed entries and of the static keys. It follows the tables like tomlCollector, but with the parts of the
// keys as written.
type tomlKeyWalker struct {
	// written are the masked texts of the keys produced by placeholders by their position in the written document
	written   map[int]string
	locations keyLocations
	// table is the location of the current table
	table []string
	// arrayTables counts the tables of each array of tables by its location, see tomlCollector.arrayTables
	arrayTables map[string]int
}

// expression walks a top-level expression: a table header, an array of tables header or a key/value pair
func (w *tomlKeyWalker) expression(node *unstable.Node) {
	switch node.Kind {
	case unstable.Table, unstable.ArrayTable:
		var table []string
		for keys := node.Key(); keys.Next(); {
			table = w.key(keys.Node(), table)
			if node.Kind == unstable.ArrayTable && keys.IsLast() {
				w.arrayTables[locationKey(0, table)]++
			}
			if count, ok := w.arrayTables[locationKey(0, table)]; ok {
				table = append(table, strconv.Itoa(count-1))
			}
		}
		w.table = table
	case unstable.KeyValue:
		w.keyValue(node, w.table)
	}
}

// keyValue walks a key/value pair within the table at the location. Each part of a dotted key is a key.
func (w *tomlKeyWalker) keyValue(node *unstable.Node, location []string) {
	for keys := node.Key(); keys.Next(); {
		location = w.key(keys.Node(), location)
	}
	w.value(node.Value(), location)
}

// key records the key within the location, with its masked text if a processed entry wrote it, and returns the
// location of the key
func (w *tomlKeyWalker) key(node *unstable.Node, parent []string) []string {
	location := append(slices.Clip(parent), string(node.Data))
	w.locations.add(locationKey(0, location), w.written[int(node.Raw.Offset)])
	return location
}

// value walks the keys within a value: the items of an array and the pairs of an inline table
func (w *tomlKeyWalker) value(node *unstable.Node, location []string) {
	switch node.Kind {
	case unstable.Array:
		index := 0
		for items := node.Children(); items.Next(); index++ {
			w.value(items.Node(), append(slices.Clip(location), strconv.Itoa(index)))
		}
	case unstable.InlineTable:
		for pairs := node.Children(); pairs.Next(); {
			w.keyValue(pairs.Node(), location)
		}
	}
}
