package file

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/denglertai/gonfig/internal/filter"
	"github.com/denglertai/gonfig/internal/general"
	"github.com/denglertai/gonfig/internal/schema"
	"github.com/stretchr/testify/assert"
	"go.yaml.in/yaml/v4"
)

// redactedViolations processes the content as a file of the type, validates the result against the schema with the
// keys redacted by the processor and returns the violations as text
func redactedViolations(t *testing.T, fileType general.FileType, content string, schemaText string) []string {
	t.Helper()

	dir := t.TempDir()
	schemaFile := filepath.Join(dir, "schema.json")
	assert.NoError(t, os.WriteFile(schemaFile, []byte(schemaText), 0o644))
	sch, err := schema.Load(schemaFile)
	if !assert.NoError(t, err) {
		return nil
	}
	file := filepath.Join(dir, "config."+string(fileType))
	assert.NoError(t, os.WriteFile(file, []byte(content), 0o644))

	var output bytes.Buffer
	processor := NewFileProcessor(file, general.Undefined, &output)
	processor.Validated = true
	if !assert.NoError(t, processor.Process()) {
		return nil
	}
	violations, err := sch.Validate(processor.FileType, output.Bytes(), processor.RedactKeys)
	assert.NoError(t, err)
	texts := make([]string, 0, len(violations))
	for _, violation := range violations {
		texts = append(texts, violation.String())
	}
	return texts
}

// TestRedactKeysYamlAliases checks that processed values used as keys through aliases are masked at every alias, also
// within fragments, as well as the keys of mappings referred to through aliases and merge keys
func TestRedactKeysYamlAliases(t *testing.T) {
	t.Setenv("KEYS_TEST_PW", "made-up-s3cret")
	t.Setenv("KEYS_TEST_NUM", "1234")
	t.Setenv("KEYS_TEST_FRAGMENT", `{a: &v "${KEYS_TEST_PW}", b: {*v : 1}}`)

	testCases := []struct {
		desc    string
		content string
		schema  string
		want    []string
	}{
		{desc: "alias as key", content: "a: &v ${KEYS_TEST_PW}\n*v : 1\n",
			schema: `{"properties": {"a": {}}, "additionalProperties": false}`,
			want:   []string{"(root): additional properties '***' not allowed"}},
		{desc: "alias as key in path", content: "a: &v pre_${KEYS_TEST_PW}\nb:\n  *v : {c: x}\n",
			schema: `{"properties": {"b": {"additionalProperties": {"additionalProperties": {"type": "integer"}}}}}`,
			want:   []string{"b.pre_***.c: got string, want integer"}},
		{desc: "typed alias as key", content: "a: &v ${KEYS_TEST_NUM | to_int}\n*v : 1\n",
			schema: `{"properties": {"a": {}}, "additionalProperties": false}`,
			want:   []string{"(root): additional properties '***' not allowed"}},
		{desc: "alias within fragment", content: "f: ${KEYS_TEST_FRAGMENT | fragment}\n",
			schema: `{"properties": {"f": {"properties": {"b": {"additionalProperties": false}}}}}`,
			want:   []string{"f.b: additional properties '***' not allowed"}},
		{desc: "keys of an aliased mapping", content: "base: &b {\"${KEYS_TEST_PW}\": x}\nother: *b\n",
			schema: `{"additionalProperties": {"additionalProperties": {"type": "integer"}}}`,
			want:   []string{"base.***: got string, want integer", "other.***: got string, want integer"}},
		{desc: "merge key", content: "base: &b {\"${KEYS_TEST_PW}\": x}\nother:\n  <<: *b\n",
			schema: `{"additionalProperties": {"additionalProperties": {"type": "integer"}}}`,
			want:   []string{"base.***: got string, want integer", "other.***: got string, want integer"}},
		{desc: "merge keys of several mappings", content: "b1: &b1 {\"${KEYS_TEST_PW}\": x}\nb2: &b2 {static: x}\nother:\n  <<: [*b1, *b2]\n",
			schema: `{"properties": {"other": {"additionalProperties": {"type": "integer"}}}}`,
			want:   []string{"other.***: got string, want integer", "other.static: got string, want integer"}},
		{desc: "custom tag", content: "!secret ${KEYS_TEST_PW}: 1\n", schema: `{"additionalProperties": false}`,
			want: []string{"(root): additional properties '***' not allowed"}},
		{desc: "documents", content: "${KEYS_TEST_PW}: x\n---\nmade-up-s3cret: x\n",
			schema: `{"additionalProperties": {"type": "integer"}}`,
			want:   []string{"document 1: ***: got string, want integer", "document 2: made-up-s3cret: got string, want integer"}},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			assert.Equal(t, tC.want, redactedViolations(t, general.YAML, tC.content, tC.schema))
		})
	}
}

// TestRedactKeysYamlTags checks that keys with tags are masked, which the validator gets decoded, e.g. 31 for
// "!!int 0x1F", and that static keys with the same text stay visible
func TestRedactKeysYamlTags(t *testing.T) {
	t.Setenv("KEYS_TEST_FLOAT", "1e3")
	t.Setenv("KEYS_TEST_TIME", "2001-12-14")
	t.Setenv("KEYS_TEST_HEX", "0x1F")
	t.Setenv("KEYS_TEST_PIN", "1234")

	violations := redactedViolations(t, general.YAML,
		"!!float ${KEYS_TEST_FLOAT}: 1\n!!timestamp ${KEYS_TEST_TIME}: 2\n!!int ${KEYS_TEST_HEX}: 3\n!!int ${KEYS_TEST_PIN}: 4\n",
		`{"additionalProperties": false}`)
	assert.Equal(t, []string{"(root): additional properties '***', '***', '***', '***' not allowed"}, violations)

	violations = redactedViolations(t, general.YAML, "!!int ${KEYS_TEST_PIN}: x\nother:\n  1234: x\n",
		`{"properties": {"other": {"additionalProperties": {"type": "integer"}}}, "additionalProperties": {"type": "integer"}}`)
	assert.Equal(t, []string{"***: got string, want integer", "other.1234: got string, want integer"}, violations)
}

// TestRedactKeysInvalidUTF8 checks that keys with invalid UTF-8 are masked, which the JSON and TOML writers write
// with U+FFFD instead
func TestRedactKeysInvalidUTF8(t *testing.T) {
	t.Setenv("KEYS_TEST_PW", "made-up-\xffs3cret")

	for fileType, content := range map[general.FileType]string{
		general.JSON: `{"${KEYS_TEST_PW}": 1}`,
		general.TOML: "\"${KEYS_TEST_PW}\" = 1\n",
	} {
		t.Run(string(fileType), func(t *testing.T) {
			assert.Equal(t, []string{"(root): additional properties '***' not allowed"},
				redactedViolations(t, fileType, content, `{"additionalProperties": false}`))
		})
	}
}

// TestRedactKeysByLocation checks that static keys and array indexes equal to a processed key stay visible, since
// masking them would reveal the equality, for every file type
func TestRedactKeysByLocation(t *testing.T) {
	const schemaText = `{"properties": {"users": {"additionalProperties": {"type": "string"}},
  "list": {"items": {"properties": {"x": {"type": "integer"}}}}}}`
	testCases := []struct {
		fileType general.FileType
		content  string
	}{
		{general.YAML, "${KEYS_TEST_PW}: 1\nusers: {admin: {x: 1}}\nlist: [{x: s}]\n"},
		{general.JSON, `{"${KEYS_TEST_PW}": 1, "users": {"admin": {"x": 1}}, "list": [{"x": "s"}]}`},
		{general.TOML, "\"${KEYS_TEST_PW}\" = 1\nlist = [{ x = \"s\" }]\n[users.admin]\nx = 1\n"},
	}
	for _, tC := range testCases {
		for _, secret := range []string{"admin", "0", "x"} {
			t.Run(string(tC.fileType)+" "+secret, func(t *testing.T) {
				t.Setenv("KEYS_TEST_PW", secret)
				assert.Equal(t, []string{"list.0.x: got string, want integer", "users.admin: got object, want string"},
					redactedViolations(t, tC.fileType, tC.content, schemaText))
			})
		}
	}
}

// TestRedactKeysWithValuesInErrors checks that keys are not masked if values are allowed in errors
func TestRedactKeysWithValuesInErrors(t *testing.T) {
	t.Setenv("KEYS_TEST_PW", "made-up-s3cret")
	filter.SetValuesInErrors(true)
	t.Cleanup(func() { filter.SetValuesInErrors(false) })

	assert.Equal(t, []string{"(root): additional properties 'made-up-s3cret' not allowed"},
		redactedViolations(t, general.YAML, "a: &v ${KEYS_TEST_PW}\n*v : 1\n", `{"properties": {"a": {}}, "additionalProperties": false}`))
}

// TestRedactKeysUnchangedOrUndecodable checks that keys whose placeholders give their own text are masked like other
// keys for every file type, although TOML writes them as read, and keys the validator cannot decode, which fail the
// validation before any violation
func TestRedactKeysUnchangedOrUndecodable(t *testing.T) {
	t.Setenv("KEYS_TEST_SAME", "${KEYS_TEST_SAME}")
	for fileType, content := range map[general.FileType]string{
		general.YAML: "\"${KEYS_TEST_SAME}\": x\n",
		general.JSON: `{"${KEYS_TEST_SAME}": "x"}`,
		general.TOML: "\"${KEYS_TEST_SAME}\" = \"x\"\n[t]\n\"${KEYS_TEST_SAME}\" = 2\n",
	} {
		t.Run(string(fileType), func(t *testing.T) {
			violations := redactedViolations(t, fileType, content, `{"additionalProperties": {"type": "object", "additionalProperties": false}}`)
			assert.Contains(t, violations, "***: got string, want object")
		})
	}
	assert.Equal(t, []string{"***: got string, want object", "t: additional properties '***' not allowed"},
		redactedViolations(t, general.TOML, "\"${KEYS_TEST_SAME}\" = \"x\"\n[t]\n\"${KEYS_TEST_SAME}\" = 2\n",
			`{"additionalProperties": {"type": "object", "additionalProperties": false}}`))

	t.Setenv("KEYS_TEST_PW", "made-up-s3cret")
	file := filepath.Join(t.TempDir(), "config.yaml")
	assert.NoError(t, os.WriteFile(file, []byte("!!int ${KEYS_TEST_PW}: 1\n"), 0o644))
	var output bytes.Buffer
	processor := NewFileProcessor(file, general.Undefined, &output)
	processor.Validated = true
	assert.NoError(t, processor.Process())
	assert.Equal(t, []string{"***"}, processor.RedactKeys(0, 1, []string{"made-up-s3cret"}, digitIndexes([]string{"made-up-s3cret"})),
		"no key is located, but the key is no key of the input")
}

// TestRedactKeysEmptyYamlDocuments checks that documents are numbered like the validator numbers the documents of the
// written content, which has no document for a first empty document, also with comments or "...", while an explicit
// null is a document
func TestRedactKeysEmptyYamlDocuments(t *testing.T) {
	t.Setenv("KEYS_TEST_PW", "made-up-s3cret")
	const closed = `{"additionalProperties": false}`

	testCases := []struct {
		content string
		want    []string
	}{
		{"---\n---\n${KEYS_TEST_PW}: 1\n", []string{"(root): additional properties '***' not allowed"}},
		{"---\n# only comment\n---\n${KEYS_TEST_PW}: 1\n", []string{"(root): additional properties '***' not allowed"}},
		{"---\n...\n---\n${KEYS_TEST_PW}: 1\n", []string{"(root): additional properties '***' not allowed"}},
		{"--- \n--- \n--- {\"${KEYS_TEST_PW}\": 1}\n", []string{"document 2: (root): additional properties '***' not allowed"}},
		{"---\n---\n---\n${KEYS_TEST_PW}: 1\n---\n${KEYS_TEST_PW}x: 1\n", []string{
			"document 2: (root): additional properties '***' not allowed",
			"document 3: (root): additional properties '***x' not allowed"}},
		{"---\n~\n---\n${KEYS_TEST_PW}: 1\n", []string{"document 2: (root): additional properties '***' not allowed"}},
		{"a: 1\n---\n---\n${KEYS_TEST_PW}: 1\n", []string{
			"document 1: (root): additional properties 'a' not allowed",
			"document 3: (root): additional properties '***' not allowed"}},
	}
	for i, tC := range testCases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			assert.Equal(t, tC.want, redactedViolations(t, general.YAML, tC.content, closed), tC.content)
		})
	}
}

// TestRedactKeysUnsure checks that every part of every location is masked if the documents don't match the ones the
// validator decoded, which must not happen, but would number the keys wrongly
func TestRedactKeysUnsure(t *testing.T) {
	t.Setenv("KEYS_TEST_PW", "made-up-s3cret")

	for fileType, content := range map[general.FileType]string{
		general.YAML: "${KEYS_TEST_PW}: [1]\n",
		general.JSON: `{"${KEYS_TEST_PW}": [1]}`,
		general.TOML: "\"${KEYS_TEST_PW}\" = [1]\n",
	} {
		t.Run(string(fileType), func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "config."+string(fileType))
			assert.NoError(t, os.WriteFile(file, []byte(content), 0o644))
			processor := NewFileProcessor(file, general.Undefined, &bytes.Buffer{})
			processor.Validated = true
			assert.NoError(t, processor.Process())
			assert.Equal(t, []string{"***", "***", "***"}, processor.RedactKeys(0, 3, []string{"static", "made-up-s3cret", "0"}, digitIndexes([]string{"static", "made-up-s3cret", "0"})))
			assert.Equal(t, []string{"***"}, processor.RedactKeys(0, 1, []string{"static"}, digitIndexes([]string{"static"})), "the first call decides")
		})
	}
}

// laughsYaml returns a YAML document whose aliases expand to 10^7 items: a7 refers to a6 ten times, and so on, with
// the given first line and a0
func laughsYaml(first string, a0 string) string {
	var content strings.Builder
	content.WriteString(first + "a0: &a0 " + a0 + "\n")
	for i := 1; i <= 7; i++ {
		fmt.Fprintf(&content, "a%d: &a%d [%s]\n", i, i, strings.TrimSuffix(strings.Repeat(fmt.Sprintf("*a%d,", i-1), 10), ","))
	}
	return content.String()
}

// TestRedactKeysAliasExpansion checks that processing a document whose aliases expand exponentially doesn't locate
// keys, and that locating them skips aliases without candidates and stops at its budget for aliases with candidates,
// which is unsure then
func TestRedactKeysAliasExpansion(t *testing.T) {
	t.Setenv("KEYS_TEST_PW", "made-up-s3cret")

	processor := processedKeys(t, general.YAML, laughsYaml("p: &p \"${KEYS_TEST_PW}\"\n\"${KEYS_TEST_PW}\": 1\n", "[x,x,x,x,x,x,x,x,x,x]"))
	assert.NotNil(t, processor.keyLocator, "the keys are located by RedactKeys only")
	assert.Nil(t, processor.keys)
	assert.Equal(t, []string{"***"}, processor.RedactKeys(0, 1, []string{"made-up-s3cret"}, digitIndexes([]string{"made-up-s3cret"})), "aliases without candidates are skipped")
	assert.Equal(t, []string{"a7", "0", "1"}, processor.RedactKeys(0, 1, []string{"a7", "0", "1"}, digitIndexes([]string{"a7", "0", "1"})))

	processor = processedKeys(t, general.YAML, laughsYaml("", "{\"${KEYS_TEST_PW}\": 1}"))
	assert.Equal(t, []string{"***", "***"}, processor.RedactKeys(0, 1, []string{"a0", "made-up-s3cret"}, digitIndexes([]string{"a0", "made-up-s3cret"})), "the budget is exceeded")

	assert.Equal(t, 1000, yamlWalkBudget(0))
	assert.Equal(t, 2000, yamlWalkBudget(10))
}

// TestCheckRecordsNoKeyCandidates checks that a check records no keys, since it validates nothing
func TestCheckRecordsNoKeyCandidates(t *testing.T) {
	t.Setenv("KEYS_TEST_PW", "made-up-s3cret")
	file := filepath.Join(t.TempDir(), "config.yaml")
	assert.NoError(t, os.WriteFile(file, []byte("${KEYS_TEST_PW}: 1\n"), 0o644))
	processor := NewFileProcessor(file, general.Undefined, nil)
	processor.Validated = true
	_, err := processor.Check()
	assert.NoError(t, err)
	assert.Nil(t, processor.keyCandidates)
	assert.Nil(t, processor.keyLocator)
}

// TestLocateKeysWithoutKeys checks the edge cases of locating keys: a YAML document without content, which is empty,
// and a TOML document without written keys, which has nothing to locate
func TestLocateKeysWithoutKeys(t *testing.T) {
	assert.True(t, emptyYamlDocument(&yaml.Node{Kind: yaml.DocumentNode}))
	assert.False(t, emptyYamlDocument(&yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.ScalarNode, Value: "~"}}}))

	locations, sure := (&TomlFileHandler{}).keyLocator(nil).locateKeys(1)
	assert.Empty(t, locations)
	assert.True(t, sure)
}

// TestRedactKeysUnsureDependency checks that the property named by dependentRequired is masked if the keys cannot be
// located for sure, here because the number of documents differs
func TestRedactKeysUnsureDependency(t *testing.T) {
	t.Setenv("KEYS_TEST_PW", "admin")
	dir := t.TempDir()
	schemaFile := filepath.Join(dir, "schema.json")
	assert.NoError(t, os.WriteFile(schemaFile, []byte(`{"dependentRequired": {"admin": ["pw"]}}`), 0o644))
	sch, err := schema.Load(schemaFile)
	assert.NoError(t, err)
	file := filepath.Join(dir, "config.yaml")
	assert.NoError(t, os.WriteFile(file, []byte("${KEYS_TEST_PW}: 1\n"), 0o644))

	var output bytes.Buffer
	processor := NewFileProcessor(file, general.Undefined, &output)
	processor.Validated = true
	assert.NoError(t, processor.Process())
	wrongCount := func(document int, documents int, location []string, indexes []bool) []string {
		return processor.RedactKeys(document, documents+1, location, indexes)
	}
	violations, err := sch.Validate(general.YAML, output.Bytes(), wrongCount)
	assert.NoError(t, err)
	if assert.Len(t, violations, 1) {
		assert.Equal(t, "(root): properties 'pw' required, if '***' exists", violations[0].String())
	}
}

// TestKeyLocatorOfOtherHandlers checks that handlers that cannot locate keys get no locator, and that the candidates
// are not kept in any case
func TestKeyLocatorOfOtherHandlers(t *testing.T) {
	processor := NewFileProcessor("config.xml", general.Undefined, nil)
	processor.Validated = true
	processor.keyCandidates = map[ConfigEntry]string{&XmlTextConfigEntry{}: "***"}
	assert.Nil(t, processor.keyLocatorOf(NewXmlConfigFileHandler()))
	assert.Nil(t, processor.keyCandidates)
}

// TestRedactKeysSafetyNet checks that keys the locator misses are masked since they are no keys of the input: a
// sequence item tagged as a merge key, which the decoder skips, and an integer key merged into a mapping with string
// keys, which the decoder keeps as written. An anchor of a fragment with the name of an anchor of the document is
// renamed, so the alias still refers to the static anchor of the document, which is shown.
func TestRedactKeysSafetyNet(t *testing.T) {
	t.Setenv("KEYS_TEST_PW", "made-up-s3cret")
	t.Setenv("KEYS_TEST_FRAGMENT", `[&k "${KEYS_TEST_PW}"]`)
	t.Setenv("KEYS_TEST_HEX", "0x5EC")

	testCases := []struct {
		desc    string
		content string
		schema  string
		want    []string
	}{
		{desc: "merge tagged sequence item", content: "l:\n  - !!merge x\n  - {\"${KEYS_TEST_PW}\": 1}\n",
			schema: `{"properties": {"l": {"items": {"additionalProperties": false}}}}`,
			want:   []string{"l.0: additional properties '***' not allowed"}},
		{desc: "anchor of a fragment with the name of an anchor of the document", content: "a: &k static-anchor\nf: ${KEYS_TEST_FRAGMENT | fragment}\nm:\n  *k : {c: x}\n",
			schema: `{"properties": {"m": {"additionalProperties": {"additionalProperties": {"type": "integer"}}}}}`,
			want:   []string{"m.static-anchor.c: got string, want integer"}},
		{desc: "integer key merged into string keys", content: "base: &b {!!int \"${KEYS_TEST_HEX}\": 1}\nd:\n  s: 1\n  <<: *b\n",
			schema: `{"properties": {"d": {"properties": {"s": {}}, "additionalProperties": false}}}`,
			want:   []string{"d: additional properties '***' not allowed"}},
		{desc: "static keys stay visible", content: "\"${KEYS_TEST_PW}\": 1\nstatic: {\"quoted.key\": x, !!int 0x1F: x}\n",
			schema: `{"properties": {"static": {"additionalProperties": false}}}`,
			want:   []string{"static: additional properties '31', 'quoted.key' not allowed"}},
		{desc: "escaped placeholder stays visible", content: "$${ESC}: 1\n", schema: `{"additionalProperties": false}`,
			want: []string{"(root): additional properties '${ESC}' not allowed"}},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			assert.Equal(t, tC.want, redactedViolations(t, general.YAML, tC.content, tC.schema))
		})
	}
}

// TestProcessWithoutValidationRecordsNothing checks that Process records no keys unless the content is validated,
// so processing costs nothing extra, and that RedactKeys masks every key but array indexes then
func TestProcessWithoutValidationRecordsNothing(t *testing.T) {
	t.Setenv("KEYS_TEST_PW", "made-up-s3cret")
	for fileType, content := range map[general.FileType]string{
		general.YAML: "${KEYS_TEST_PW}: 1\nstatic: [1]\n",
		general.JSON: `{"${KEYS_TEST_PW}": 1, "static": [1]}`,
		general.TOML: "\"${KEYS_TEST_PW}\" = 1\nstatic = [1]\n",
	} {
		t.Run(string(fileType), func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "config."+string(fileType))
			assert.NoError(t, os.WriteFile(file, []byte(content), 0o644))
			processor := NewFileProcessor(file, general.Undefined, &bytes.Buffer{})
			assert.NoError(t, processor.Process())
			assert.Nil(t, processor.keyCandidates)
			assert.Nil(t, processor.keyLocator)
			assert.Nil(t, processor.staticKeys)
			assert.Nil(t, processor.source)
			assert.Equal(t, []string{"***", "0"}, processor.RedactKeys(0, 1, []string{"static", "0"}, []bool{false, true}))
		})
	}
}

// TestRecordStaticKeysOfFragments checks the keys recorded for fragments of TOML files, which are values or the
// key/value pairs of a fragment used as a key, and that content that cannot be decoded records nothing
func TestRecordStaticKeysOfFragments(t *testing.T) {
	processor := NewFileProcessor("config.toml", general.Undefined, nil)
	processor.recordStaticKeys([]byte(`{ a = { b = 1 } }`), true)
	processor.recordStaticKeys([]byte("c = 1\nd.e = [{ f = 1 }]"), true)
	processor.recordStaticKeys([]byte("[invalid"), true)
	assert.Equal(t, map[string]struct{}{"a": {}, "b": {}, "c": {}, "d": {}, "e": {}, "f": {}}, processor.staticKeys)
}

// TestRedactKeysAnchoredScalarsOfFragments checks that anchored scalars of fragments, which are results of
// placeholders, are not shown when an alias within the fragment uses them as a key, while anchored scalars of the input
// are. An anchor of a fragment with the name of an anchor of the document is renamed, so an alias of the document
// still refers to the anchored scalar of the input, which is shown.
func TestRedactKeysAnchoredScalarsOfFragments(t *testing.T) {
	const schemaText = `{"additionalProperties": {"additionalProperties": false}}`
	// The keys of a fragment are shown, like the key q
	for fragment, want := range map[string][]string{
		"&s made-up-anchored-s3cret":      {"a: additional properties 'plain' not allowed"},
		"{q: &s made-up-anchored-s3cret}": {"a: additional properties 'plain' not allowed", "f: additional properties 'q' not allowed"},
	} {
		t.Setenv("KEYS_TEST_FRAGMENT", fragment)
		assert.Equal(t, want,
			redactedViolations(t, general.YAML, "x: &s plain\nf: ${KEYS_TEST_FRAGMENT | fragment}\na: {*s : 1}\n", schemaText), fragment)
	}

	t.Setenv("KEYS_TEST_FRAGMENT", "{v: &a made-up-anchored-s3cret, m: {*a : 1}}")
	assert.Equal(t, []string{"f.m: additional properties '***' not allowed"},
		redactedViolations(t, general.YAML, "f: ${KEYS_TEST_FRAGMENT | fragment}\n",
			`{"properties": {"f": {"properties": {"m": {"additionalProperties": false}}}}}`))

	assert.Equal(t, []string{"a: additional properties 'plain' not allowed"},
		redactedViolations(t, general.YAML, "x: &s plain\na: {*s : 1}\n", schemaText))
}

// TestRedactKeysKeptAnchorOfFragment checks that a fragment replacing an anchored value, which keeps the anchor, is
// masked when an alias uses it as a key, also if the fragment had an anchor of its own
func TestRedactKeysKeptAnchorOfFragment(t *testing.T) {
	for _, fragment := range []string{"made-up-anchored-s3cret", "&s made-up-anchored-s3cret"} {
		t.Setenv("KEYS_TEST_FRAGMENT", fragment)
		assert.Equal(t, []string{"m: additional properties '***' not allowed"},
			redactedViolations(t, general.YAML, "a: &k ${KEYS_TEST_FRAGMENT | fragment}\nm: {*k : 1}\n",
				`{"properties": {"m": {"additionalProperties": false}}}`), fragment)
	}
}
