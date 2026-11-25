package file

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path"
	"strings"
	"testing"

	"github.com/denglertai/gonfig/internal/filter"
	"github.com/denglertai/gonfig/internal/general"
	"github.com/stretchr/testify/assert"
	"go.yaml.in/yaml/v4"
)

// processFragment processes the content as a file of the given type with the environment variable FRAGMENT set to the fragment
func processFragment(t *testing.T, fileType general.FileType, content string, fragment string) (string, error) {
	t.Helper()
	t.Setenv("FRAGMENT", fragment)
	t.Setenv("FRAGMENT_SECRET", "s3cret")
	t.Setenv("FRAGMENT_PORT", "8080")

	file := path.Join(t.TempDir(), "config."+string(fileType))
	assert.NoError(t, os.WriteFile(file, []byte(content), 0o644))

	var output bytes.Buffer
	err := NewFileProcessor(file, fileType, &output).Process()
	return output.String(), err
}

// TestFragments checks inserting and merging fragments for each file type, including lists, empty fragments and errors
func TestFragments(t *testing.T) {
	testCases := []struct {
		desc     string
		fileType general.FileType
		content  string
		fragment string
		want     string
		wantErr  string
	}{
		// YAML values
		{
			desc:     "yaml value replaced by a JSON fragment in block style",
			fileType: general.YAML,
			content:  "logging:\n  level: info\n  appenders: ${FRAGMENT | fragment} # appenders\n",
			fragment: `[{"type": "console"}, {"type": "file", "path": "/var/log/app.log", "port": "123"}]`,
			want:     "logging:\n  level: info\n  appenders: # appenders\n    - type: console\n    - type: file\n      path: /var/log/app.log\n      port: \"123\"\n",
		},
		{
			desc:     "yaml value replaced by an empty sequence keeps its line comment",
			fileType: general.YAML,
			content:  "appenders: ${FRAGMENT | fragment} # appenders\nlevel: info\n",
			fragment: "[]",
			want:     "appenders: [] # appenders\nlevel: info\n",
		},
		{
			desc:     "yaml value replaced by an empty mapping keeps its line comment",
			fileType: general.YAML,
			content:  "pool: ${FRAGMENT | fragment} # pool\nlevel: info\n",
			fragment: "{}",
			want:     "pool: {} # pool\nlevel: info\n",
		},
		{
			desc:     "yaml sequence item replaced by an empty sequence keeps its line comment",
			fileType: general.YAML,
			content:  "- ${FRAGMENT | fragment} # item\n- b\n",
			fragment: "[]",
			want:     "- [] # item\n- b\n",
		},
		{
			desc:     "yaml value replaced by a YAML fragment keeping literal scalars",
			fileType: general.YAML,
			content:  "script: ${FRAGMENT | fragment}\n",
			fragment: "run: |\n  echo a\n  echo b\n",
			want:     "script:\n  run: |\n    echo a\n    echo b\n",
		},
		{
			desc:     "yaml value replaced by a scalar fragment",
			fileType: general.YAML,
			content:  "key: ${FRAGMENT | fragment}\n",
			fragment: "42",
			want:     "key: 42\n",
		},
		{
			desc:     "yaml value replaced by null for an empty fragment",
			fileType: general.YAML,
			content:  "key: ${FRAGMENT | fragment}\n",
			fragment: "",
			want:     "key:\n",
		},
		{
			desc:     "placeholders within yaml fragments",
			fileType: general.YAML,
			content:  "db: ${FRAGMENT | fragment}\n",
			fragment: "password: ${FRAGMENT_SECRET | base64}\nport: ${FRAGMENT_PORT | to_int}\n${FRAGMENT_PORT}: key\n",
			want:     "db:\n  password: czNjcmV0\n  port: 8080\n  \"8080\": key\n",
		},
		{
			desc:     "yaml list from split",
			fileType: general.YAML,
			content:  "a: \"val\"\nb: \"${FRAGMENT | split(,) | fragment}\"\n",
			fragment: "alpha,beta,gamma",
			want:     "a: \"val\"\nb:\n  - alpha\n  - beta\n  - gamma\n",
		},
		{
			desc:     "yaml empty list from split",
			fileType: general.YAML,
			content:  "b: ${FRAGMENT | split | fragment}\n",
			fragment: "",
			want:     "b: []\n",
		},
		{
			desc:     "yaml list elements are strings",
			fileType: general.YAML,
			content:  "b: ${FRAGMENT | split | trim | fragment}\n",
			fragment: "1, true ,null",
			want:     "b:\n  - \"1\"\n  - \"true\"\n  - \"null\"\n",
		},
		// YAML keys
		{
			desc:     "yaml key deep merged into the mapping",
			fileType: general.YAML,
			content:  "database:\n  host: localhost # dev\n  port: 5432\n  pool:\n    timeout: 30\n  hosts: [a, b]\n  ${FRAGMENT | fragment}:\nother: x\n",
			fragment: "host: db.prod\npool:\n  size: 20\nhosts: [c]\nssl: true\n",
			want:     "database:\n  host: db.prod # dev\n  port: 5432\n  pool:\n    timeout: 30\n    size: 20\n  hosts:\n    - c\n  ssl: true\nother: x\n",
		},
		{
			desc:     "yaml key merged into the first of duplicate keys",
			fileType: general.YAML,
			content:  "a:\n  x: 1\na:\n  y: 2\n${FRAGMENT | fragment}:\n",
			fragment: "a:\n  z: 3\nb: 1\nb: 2\n",
			want:     "a:\n  x: 1\n  z: 3\na:\n  y: 2\nb: 2\n",
		},
		{
			desc:     "yaml key removed for an empty fragment",
			fileType: general.YAML,
			content:  "database:\n  host: localhost\n  ${FRAGMENT | fragment}:\n",
			fragment: "",
			want:     "database:\n  host: localhost\n",
		},
		{
			desc:     "yaml key replacing the value of a later placeholder key",
			fileType: general.YAML,
			content:  "${FRAGMENT | fragment}:\n${FRAGMENT_SECRET}: 1\n",
			fragment: `{"${FRAGMENT_SECRET}": {"y": 2}}`,
			want:     "s3cret:\n  y: 2\n",
		},
		{
			desc:     "yaml key replacing the value of a nested placeholder key",
			fileType: general.YAML,
			content:  "${FRAGMENT | fragment}:\no:\n  ${FRAGMENT_SECRET}: 1\n",
			fragment: `{"o": {"${FRAGMENT_SECRET}": 2}}`,
			want:     "o:\n  s3cret: 2\n",
		},
		{
			desc:     "yaml key replacing a later key fragment by a value",
			fileType: general.YAML,
			content:  "${FRAGMENT | fragment}:\n${FRAGMENT_PORT | fragment}:\n",
			fragment: `{"${FRAGMENT_PORT | fragment}": 1}`,
			wantErr:  "a key with a fragment must not have a value",
		},
		{
			desc:     "yaml key with a value",
			fileType: general.YAML,
			content:  "${FRAGMENT | fragment}: value\n",
			fragment: "a: 1",
			wantErr:  "a key with a fragment must not have a value",
		},
		{
			desc:     "yaml key with a fragment other than a mapping",
			fileType: general.YAML,
			content:  "${FRAGMENT | fragment}:\n",
			fragment: "[1, 2]",
			wantErr:  "a fragment used as a key has to be a mapping",
		},
		{
			desc:     "invalid yaml fragment",
			fileType: general.YAML,
			content:  "key: ${FRAGMENT | fragment}\n",
			fragment: "a: [1",
			wantErr:  "invalid YAML fragment",
		},
		{
			desc:     "nested fragments",
			fileType: general.YAML,
			content:  "key: ${FRAGMENT | fragment}\n",
			fragment: "inner: ${FRAGMENT | fragment}",
			wantErr:  "fragments cannot contain further fragments",
		},
		{
			desc:     "embedded fragment",
			fileType: general.YAML,
			content:  "key: x ${FRAGMENT | fragment}\n",
			fragment: "a: 1",
			wantErr:  "has to make up the whole value",
		},
		// JSON values
		{
			desc:     "json value replaced by a fragment",
			fileType: general.JSON,
			content:  `{"appenders": "${FRAGMENT | fragment}", "level": "info"}`,
			fragment: `[{"type": "console", "port": "${FRAGMENT_PORT | to_int}"}]`,
			want:     "{\n  \"appenders\": [\n    {\n      \"type\": \"console\",\n      \"port\": 8080\n    }\n  ],\n  \"level\": \"info\"\n}\n",
		},
		{
			desc:     "json value replaced by null for an empty fragment",
			fileType: general.JSON,
			content:  `{"key": "${FRAGMENT | fragment}"}`,
			fragment: " ",
			want:     "{\n  \"key\": null\n}\n",
		},
		{
			desc:     "invalid json fragment",
			fileType: general.JSON,
			content:  `{"key": "${FRAGMENT | fragment}"}`,
			fragment: `[1,`,
			wantErr:  "invalid JSON fragment",
		},
		{
			desc:     "json fragment with trailing data",
			fileType: general.JSON,
			content:  `{"key": "${FRAGMENT | fragment}"}`,
			fragment: `1 2`,
			wantErr:  "unexpected data after the top-level JSON value",
		},
		{
			desc:     "json list from split",
			fileType: general.JSON,
			content:  `{"b": "${FRAGMENT | split | fragment}"}`,
			fragment: `alpha,"quoted"`,
			want:     "{\n  \"b\": [\n    \"alpha\",\n    \"\\\"quoted\\\"\"\n  ]\n}\n",
		},
		// JSON keys
		{
			desc:     "json key deep merged into the object",
			fileType: general.JSON,
			content:  `{"database": {"host": "localhost", "pool": {"timeout": 30}, "${FRAGMENT | fragment}": null, "port": 5432}}`,
			fragment: `{"host": "db.prod", "pool": {"size": 20}, "${FRAGMENT_PORT}_key": "${FRAGMENT_SECRET}"}`,
			want:     "{\n  \"database\": {\n    \"host\": \"db.prod\",\n    \"pool\": {\n      \"timeout\": 30,\n      \"size\": 20\n    },\n    \"port\": 5432,\n    \"8080_key\": \"s3cret\"\n  }\n}\n",
		},
		{
			desc:     "json key merged into the first of duplicate keys",
			fileType: general.JSON,
			content:  `{"a": {"x": 1}, "a": {"y": 2}, "${FRAGMENT | fragment}": null}`,
			fragment: `{"a": {"z": 3}, "b": 1, "b": 2}`,
			want:     "{\n  \"a\": {\n    \"x\": 1,\n    \"z\": 3\n  },\n  \"a\": {\n    \"y\": 2\n  },\n  \"b\": 2\n}\n",
		},
		{
			desc:     "json key removed for an empty fragment",
			fileType: general.JSON,
			content:  `{"a": 1, "${FRAGMENT | fragment}": null}`,
			fragment: "",
			want:     "{\n  \"a\": 1\n}\n",
		},
		{
			desc:     "json key replacing the value of a later placeholder key",
			fileType: general.JSON,
			content:  `{"${FRAGMENT | fragment}": null, "${FRAGMENT_SECRET}": 1}`,
			fragment: `{"${FRAGMENT_SECRET}": {"y": 2}}`,
			want:     "{\n  \"s3cret\": {\n    \"y\": 2\n  }\n}\n",
		},
		{
			desc:     "json key replacing the scalar of a later placeholder key",
			fileType: general.JSON,
			content:  `{"${FRAGMENT | fragment}": null, "${FRAGMENT_SECRET}": 1}`,
			fragment: `{"${FRAGMENT_SECRET}": 2}`,
			want:     "{\n  \"s3cret\": 2\n}\n",
		},
		{
			desc:     "json key in a nested object replacing the value of a later placeholder key",
			fileType: general.JSON,
			content:  `{"o": {"${FRAGMENT | fragment}": null, "${FRAGMENT_SECRET}": 1}}`,
			fragment: `{"${FRAGMENT_SECRET}": 3}`,
			want:     "{\n  \"o\": {\n    \"s3cret\": 3\n  }\n}\n",
		},
		{
			desc:     "json key replacing the value of a nested placeholder key",
			fileType: general.JSON,
			content:  `{"${FRAGMENT | fragment}": null, "o": {"${FRAGMENT_SECRET}": 1}}`,
			fragment: `{"o": {"${FRAGMENT_SECRET}": 2}}`,
			want:     "{\n  \"o\": {\n    \"s3cret\": 2\n  }\n}\n",
		},
		{
			// Pins the current first-occurrence matching only; how duplicate keys should be handled is tracked separately
			desc:     "json key replacing the first of duplicate placeholder keys",
			fileType: general.JSON,
			content:  `{"${FRAGMENT | fragment}": null, "${FRAGMENT_SECRET}": 1, "${FRAGMENT_SECRET}": 5}`,
			fragment: `{"${FRAGMENT_SECRET}": 2}`,
			want:     "{\n  \"s3cret\": 2,\n  \"s3cret\": 5\n}\n",
		},
		{
			desc:     "json key replacing a later key fragment by a value",
			fileType: general.JSON,
			content:  `{"${FRAGMENT | fragment}": null, "${FRAGMENT_PORT | fragment}": null}`,
			fragment: `{"${FRAGMENT_PORT | fragment}": 1}`,
			wantErr:  "a key with a fragment must have the value null",
		},
		{
			desc:     "json key with a value",
			fileType: general.JSON,
			content:  `{"${FRAGMENT | fragment}": 1}`,
			fragment: `{}`,
			wantErr:  "a key with a fragment must have the value null",
		},
		{
			desc:     "json key with a fragment other than an object",
			fileType: general.JSON,
			content:  `{"${FRAGMENT | fragment}": null}`,
			fragment: `[1]`,
			wantErr:  "a fragment used as a key has to be an object",
		},
		{
			desc:     "json key with a placeholder",
			fileType: general.JSON,
			content:  `{"${FRAGMENT_PORT | to_int}": "${FRAGMENT_PORT | to_int}"}`,
			fragment: "",
			want:     "{\n  \"8080\": 8080\n}\n",
		},
		// XML
		{
			desc:     "xml element content replaced by a fragment",
			fileType: general.XML,
			content:  "<server><resources>${FRAGMENT | fragment}</resources></server>",
			fragment: `<?xml version="1.0"?><datasource url="${FRAGMENT_SECRET}&amp;x"/>text<password>${FRAGMENT_SECRET | upper}</password>`,
			want:     `<server><resources><datasource url="s3cret&amp;x"/>text<password>S3CRET</password></resources></server>`,
		},
		{
			desc:     "xml element content with child elements replaced in the order of the fragment",
			fileType: general.XML,
			content:  "<server><resources>${FRAGMENT | fragment}<a/>text<b/></resources><keep/></server>",
			fragment: "<x/>1<y>2</y><z/>3",
			want:     "<server><resources><x/>1<y>2</y><z/>3</resources><keep/></server>",
		},
		{
			desc:     "xml fragment embedded into text after a child element",
			fileType: general.XML,
			content:  "<server><resources><old/>text ${FRAGMENT | fragment}</resources></server>",
			fragment: "",
			wantErr:  "has to make up the whole value",
		},
		{
			desc:     "xml fragment in a later text node replaces the whole content",
			fileType: general.XML,
			content:  "<server><resources><old/>${FRAGMENT | fragment}<other/></resources><keep/></server>",
			fragment: "<new/>",
			want:     "<server><resources><new/></resources><keep/></server>",
		},
		{
			desc:     "xml element content removed for an empty fragment",
			fileType: general.XML,
			content:  "<server><resources>${FRAGMENT | fragment}</resources></server>",
			fragment: "",
			want:     "<server><resources/></server>",
		},
		{
			desc:     "invalid xml fragment",
			fileType: general.XML,
			content:  "<server><resources>${FRAGMENT | fragment}</resources></server>",
			fragment: "<a>",
			wantErr:  "invalid XML fragment",
		},
		{
			desc:     "xml list from split",
			fileType: general.XML,
			content:  "<r><b>${FRAGMENT | split | fragment}</b></r>",
			fragment: "alpha,beta",
			wantErr:  "lists can only be inserted into YAML, JSON and TOML files",
		},
		{
			desc:     "xml attribute",
			fileType: general.XML,
			content:  `<server><resources name="${FRAGMENT | fragment}"/></server>`,
			fragment: "<a/>",
			wantErr:  "fragments are not supported here",
		},
		// File types without structure
		{
			desc:     "properties",
			fileType: general.PROPERTIES,
			content:  "key=${FRAGMENT | fragment}\n",
			fragment: "a=1",
			wantErr:  "fragments are not supported here",
		},
		{
			desc:     "plain text",
			fileType: general.PLAIN,
			content:  "${FRAGMENT | fragment}\n",
			fragment: "a",
			wantErr:  "fragments are not supported here",
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			output, err := processFragment(t, tC.fileType, tC.content, tC.fragment)
			if tC.wantErr != "" {
				assert.ErrorContains(t, err, tC.wantErr)
				assert.Empty(t, output)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tC.want, output)
		})
	}
}

// TestFragmentErrorsWithoutValues checks that errors of invalid fragments only name the format and location, since the
// messages of the parsers may quote parts of the fragment, unless values are allowed in errors
func TestFragmentErrorsWithoutValues(t *testing.T) {
	testCases := []struct {
		desc     string
		fileType general.FileType
		content  string
		fragment string
		want     string
		// wantWithValues is the error if values are allowed in errors
		wantWithValues string
	}{
		{
			desc:           "unknown YAML anchor",
			fileType:       general.YAML,
			content:        "a: ${FRAGMENT | fragment}\n",
			fragment:       "pw: *s3cret",
			want:           "invalid YAML fragment on line 1",
			wantWithValues: "invalid YAML fragment: yaml: line 1: unknown anchor 's3cret' referenced",
		},
		{
			desc:           "YAML syntax error",
			fileType:       general.YAML,
			content:        "a: ${FRAGMENT | fragment}\n",
			fragment:       "a: 1\nb: 2\n  pw: s3cret",
			want:           "invalid YAML fragment on line 3",
			wantWithValues: "invalid YAML fragment: yaml: line 3: mapping values are not allowed in this context",
		},
		{
			desc:           "JSON syntax error",
			fileType:       general.JSON,
			content:        `{"a": "${FRAGMENT | fragment}"}`,
			fragment:       `{"pw": s3cret}`,
			want:           "invalid JSON fragment at offset 8",
			wantWithValues: "invalid JSON fragment: invalid character 's' looking for beginning of value",
		},
		{
			desc:           "JSON key replacing a later key fragment by a value",
			fileType:       general.JSON,
			content:        `{"${FRAGMENT | fragment}": null, "${FRAGMENT_PORT | fragment}": null}`,
			fragment:       `{"${FRAGMENT_PORT | fragment}": "s3cret"}`,
			want:           "config.json: a key with a fragment must have the value null",
			wantWithValues: "a key with a fragment must have the value null",
		},
		{
			desc:           "unknown XML entity",
			fileType:       general.XML,
			content:        "<r><a>${FRAGMENT | fragment}</a></r>",
			fragment:       "<pw>\n&s3cret;</pw>",
			want:           "invalid XML fragment on line 2",
			wantWithValues: "invalid XML fragment: XML syntax error on line 2: invalid character entity &s3cret;",
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			_, err := processFragment(t, tC.fileType, tC.content, tC.fragment)
			assert.ErrorContains(t, err, tC.want)
			assert.NotContains(t, err.Error(), "s3cret")
			assert.True(t, strings.HasSuffix(err.Error(), tC.want), err.Error())

			filter.SetValuesInErrors(true)
			t.Cleanup(func() { filter.SetValuesInErrors(false) })

			_, err = processFragment(t, tC.fileType, tC.content, tC.fragment)
			assert.ErrorContains(t, err, tC.wantWithValues)
		})
	}
}

// TestKeyFragmentReplacingKeyFragment checks that a key fragment whose fragment replaces a later key fragment by null
// keeps the later one a key fragment, which is merged afterwards, for YAML and JSON
func TestKeyFragmentReplacingKeyFragment(t *testing.T) {
	t.Setenv("FRAGMENT_SECOND", `{"q": 1}`)
	for fileType, testCase := range map[general.FileType]struct{ content, want string }{
		general.YAML: {"${FRAGMENT | fragment}:\n${FRAGMENT_SECOND | fragment}:\n", "q: 1\n"},
		general.JSON: {`{"${FRAGMENT | fragment}": null, "${FRAGMENT_SECOND | fragment}": null}`, "{\n  \"q\": 1\n}\n"},
	} {
		output, err := processFragment(t, fileType, testCase.content, `{"${FRAGMENT_SECOND | fragment}": null}`)
		assert.NoError(t, err, fileType)
		assert.Equal(t, testCase.want, output, fileType)
	}
}

// TestJsonKeyFragmentChain checks that the rename of a placeholder key survives several key fragments replacing its
// value one after the other, so the key is renamed once, to its processed name, and the last value stays
func TestJsonKeyFragmentChain(t *testing.T) {
	t.Setenv("FRAGMENT_FIRST", `{"${FRAGMENT_SECRET}": {"a": "${FRAGMENT_PORT}"}, "${FRAGMENT_PORT}": 1}`)
	t.Setenv("FRAGMENT_SECOND", `{"${FRAGMENT_SECRET}": "${FRAGMENT_PORT}"}`)
	t.Setenv("FRAGMENT_THIRD", `{"${FRAGMENT_SECRET}": 3}`)
	content := `{"${FRAGMENT_FIRST | fragment}": null, "${FRAGMENT_SECOND | fragment}": null, "${FRAGMENT_THIRD | fragment}": null, "${FRAGMENT_SECRET}": 0}`

	output, err := processFragment(t, general.JSON, content, "")
	assert.NoError(t, err)
	assert.Equal(t, "{\n  \"s3cret\": 3,\n  \"8080\": 1\n}\n", output)
}

// TestJsonKeyFragmentDetachedEntries checks that entries beneath a member replaced by a fragment, whose parent is no
// longer part of the document, neither fail nor write into the output
func TestJsonKeyFragmentDetachedEntries(t *testing.T) {
	for desc, testCase := range map[string]struct{ content, fragment, want string }{
		"key beneath a replaced object": {
			`{"${FRAGMENT | fragment}": null, "o": {"${FRAGMENT_SECRET}": 1}}`, `{"o": 5}`, "{\n  \"o\": 5\n}\n"},
		"value beneath a replaced object": {
			`{"${FRAGMENT | fragment}": null, "o": {"k": "${FRAGMENT_SECRET}"}}`, `{"o": "x"}`, "{\n  \"o\": \"x\"\n}\n"},
		"value beneath a replaced list": {
			`{"${FRAGMENT | fragment}": null, "o": ["${FRAGMENT_SECRET}", {"${FRAGMENT_PORT}": 1}]}`, `{"o": null}`, "{\n  \"o\": null\n}\n"},
		"key and value beneath a replaced placeholder key": {
			`{"${FRAGMENT | fragment}": null, "${FRAGMENT_PORT}": {"${FRAGMENT_SECRET}": "${FRAGMENT_SECRET}"}}`, `{"${FRAGMENT_PORT}": true}`, "{\n  \"8080\": true\n}\n"},
	} {
		output, err := processFragment(t, general.JSON, testCase.content, testCase.fragment)
		assert.NoError(t, err, desc)
		assert.Equal(t, testCase.want, output, desc)
		assert.NotContains(t, output, "s3cret", desc)
	}
}

// TestJsonKeyEntryGuards checks the internal errors for a key whose value is no longer part of its object, which
// mergeObjects prevents by moving the key entries
func TestJsonKeyEntryGuards(t *testing.T) {
	newEntry := func(value *jsonNode) *JsonConfigEntry {
		parent := &jsonNode{kind: jsonObject, keys: []string{"${X}"}, children: []*jsonNode{{kind: jsonScalar}}}
		return &JsonConfigEntry{node: value, key: "${X}", isKey: true, parent: parent, handler: NewJsonConfigFileHandler()}
	}

	assert.PanicsWithValue(t, "internal error: a JSON key containing a placeholder is no longer part of its object",
		func() { newEntry(&jsonNode{kind: jsonScalar}).SetValue("made-up") })

	entry := newEntry(&jsonNode{kind: jsonScalar})
	_, err := entry.SetFragment(filter.Fragment{Text: `{"a": 1}`})
	assert.EqualError(t, err, "internal error: the key with the fragment is no longer part of its object")
	assert.Len(t, entry.parent.keys, 1, "the object must stay unchanged")

	// Entries of the object are found by the identity of the value
	entry = newEntry(nil)
	entry.node = entry.parent.children[0]
	entry.SetValue("made-up")
	assert.Equal(t, []string{"made-up"}, entry.parent.keys)
}

// TestListFragmentWithDel checks that a list element containing DEL, which YAML and TOML only allow escaped, is inserted
// into every file type supporting lists
func TestListFragmentWithDel(t *testing.T) {
	for fileType, testCase := range map[general.FileType]struct{ content, want string }{
		general.YAML: {"b: ${FRAGMENT | split | fragment}\n", "b:\n  - \"a\\x7Fb\"\n  - c\n"},
		general.JSON: {`{"b": "${FRAGMENT | split | fragment}"}`, "{\n  \"b\": [\n    \"a\x7fb\",\n    \"c\"\n  ]\n}\n"},
		general.TOML: {"b = \"${FRAGMENT | split | fragment}\"\n", "b = [\"a\\u007fb\",\"c\"]\n"},
	} {
		output, err := processFragment(t, fileType, testCase.content, "a\x7fb,c")
		assert.NoError(t, err, fileType)
		assert.Equal(t, testCase.want, output, fileType)
	}
}

// TestYamlFragmentAnchors checks that a value replaced by a fragment keeps its anchor, so aliases of it still refer to
// the fragment, that anchors of the fragment with the kept name are renamed, that anchored keys with fragments are
// rejected and that aliases left without their anchor fail writing instead of producing invalid YAML
func TestYamlFragmentAnchors(t *testing.T) {
	testCases := []struct {
		desc     string
		content  string
		fragment string
		want     string
		wantErr  string
	}{
		{
			desc:     "scalar fragment with an alias as value",
			content:  "a: &k ${FRAGMENT | fragment}\nb: *k\n",
			fragment: "made-up-frag",
			want:     "a: &k made-up-frag\nb: *k\n",
		},
		{
			desc:     "scalar fragment with an alias as key",
			content:  "a: &k ${FRAGMENT | fragment}\nm: {*k : 1}\n",
			fragment: "made-up-frag",
			want:     "a: &k made-up-frag\nm: {*k: 1}\n",
		},
		{
			desc:     "mapping fragment with an alias as value",
			content:  "a: &k ${FRAGMENT | fragment}\nb: *k\n",
			fragment: "x: made-up-x",
			want:     "a: &k\n  x: made-up-x\nb: *k\n",
		},
		{
			desc:     "sequence fragment with an alias as value",
			content:  "a: &k ${FRAGMENT | fragment}\nb: *k\n",
			fragment: "[made-up-1, made-up-2]",
			want:     "a: &k\n  - made-up-1\n  - made-up-2\nb: *k\n",
		},
		{
			desc:     "mapping fragment with a merge key",
			content:  "a: &k ${FRAGMENT | fragment}\nb:\n  <<: *k\n  y: 2\n",
			fragment: "x: made-up-x",
			want:     "a: &k\n  x: made-up-x\nb:\n  <<: *k\n  y: 2\n",
		},
		{
			desc:     "empty fragment",
			content:  "a: &k ${FRAGMENT | fragment}\nb: *k\n",
			fragment: "",
			want:     "a: &k\nb: *k\n",
		},
		{
			desc:     "fragment with an anchor of its own gets the outer one",
			content:  "a: &k ${FRAGMENT | fragment}\nb: *k\n",
			fragment: "&r {x: made-up-x}",
			want:     "a: &k\n  x: made-up-x\nb: *k\n",
		},
		{
			desc:     "anchor of the fragment with the kept name is renamed",
			content:  "a: &k ${FRAGMENT | fragment}\nb: *k\n",
			fragment: "x: &k made-up-x\ny: *k",
			want:     "a: &k\n  x: &k_1 made-up-x\n  y: *k_1\nb: *k\n",
		},
		{
			desc:     "renamed anchor skips names of the document",
			content:  "a: &k ${FRAGMENT | fragment}\nc: &k_1 made-up-c\nb: *k\n",
			fragment: "x: &k made-up-x\ny: *k",
			want:     "a: &k\n  x: &k_2 made-up-x\n  y: *k_2\nc: &k_1 made-up-c\nb: *k\n",
		},
		{
			desc:     "renamed anchors are free per document",
			content:  "a: &k ${FRAGMENT | fragment}\nb: *k\n---\na: &k ${FRAGMENT | fragment}\nb: *k\n",
			fragment: "x: &k made-up-x\ny: *k",
			want:     "a: &k\n  x: &k_1 made-up-x\n  y: *k_1\nb: *k\n---\na: &k\n  x: &k_1 made-up-x\n  y: *k_1\nb: *k\n",
		},
		{
			desc:     "anchor redefined in a later document",
			content:  "a: &k ${FRAGMENT | fragment}\nb: *k\n---\nc: &k made-up-c\nd: *k\n",
			fragment: "made-up-frag",
			want:     "a: &k made-up-frag\nb: *k\n---\nc: &k made-up-c\nd: *k\n",
		},
		{
			desc:     "key fragment overriding an anchored value",
			content:  "a: &k made-up-old\n${FRAGMENT | fragment}:\nb: *k\n",
			fragment: "a: made-up-new",
			want:     "a: &k made-up-new\nb: *k\n",
		},
		{
			desc:     "key fragment overriding an anchored value renames aliases of its own anchor",
			content:  "a: &k made-up-old\n${FRAGMENT | fragment}:\nb: *k\n",
			fragment: "a: &v made-up-new\nc: *v",
			want:     "a: &k made-up-new\nb: *k\nc: *k\n",
		},
		{
			desc:     "anchor of a fragment with the name of an earlier anchor of the document is renamed",
			content:  "b: &k made-up-b\na: ${FRAGMENT | fragment}\nc: *k\n",
			fragment: "{x: &k made-up-x}",
			want:     "b: &k made-up-b\na:\n  x: &k_1 made-up-x\nc: *k\n",
		},
		{
			desc:     "anchor dropped for an alias stays reserved",
			content:  "m: {z: 0, a: &k 1}\nn: {*k : 1}\n${FRAGMENT | fragment}:\n",
			fragment: "{m: {z: &other {x: &k made-up-x}, a: *other}}",
			wantErr:  "config.yaml: the alias *k on line 2 refers to an anchor that is not defined before it",
		},
		{
			desc:     "scalar fragment as a merge value within a fragment",
			content:  "b: ${FRAGMENT | fragment}\n",
			fragment: "<<: made-up-frag",
			wantErr:  "config.yaml: the value of a merge key within a fragment is neither a mapping nor a list of mappings",
		},
		{
			desc:     "anchor removed by a fragment without anchors stays reserved for later fragments",
			content:  "a: {nested: &k old}\nc: old-c\n${FRAGMENT | fragment}:\nb: *k\n",
			fragment: "a: made-up-plain\nc: &k made-up-other",
			wantErr:  "config.yaml: the alias *k on line 4 refers to an anchor that is not defined before it",
		},
		{
			desc:     "merge alias of a scalar fragment followed by a mapping with the same anchor",
			content:  "a: &k ${FRAGMENT | fragment}\nx:\n  <<: [*k, &k {m: 1}]\n",
			fragment: "made-up-frag",
			wantErr:  "config.yaml: the alias *k on line 3 is used as the value of a merge key, but does not refer to a mapping",
		},
		{
			desc:     "merge alias of a mapping followed by a scalar fragment with the same anchor",
			content:  "a: &k {p: 1}\nx:\n  <<: [*k, {n: &k \"${FRAGMENT | fragment}\"}]\n",
			fragment: "made-up-frag",
			want:     "a: &k {p: 1}\nx:\n  <<: [*k, {n: &k made-up-frag}]\n",
		},
		{
			desc:     "alias of a fragment whose kept anchor the input defines again before it",
			content:  "a: &k made-up-old\nb: &k made-up-other\n${FRAGMENT | fragment}:\n",
			fragment: "a: &v made-up-new\nc: *v",
			wantErr: "config.yaml: an alias within a fragment would refer to another value, " +
				"since an anchor of the same name is defined between them",
		},
		{
			desc:     "alias of a fragment with a kept anchor",
			content:  "a: &k made-up-old\nb: made-up-other\n${FRAGMENT | fragment}:\n",
			fragment: "a: &v made-up-new\nc: *v",
			want:     "a: &k made-up-new\nb: made-up-other\nc: *k\n",
		},
		{
			desc:     "anchored key with a fragment",
			content:  "&k ${FRAGMENT | fragment}:\n",
			fragment: "a: made-up-a",
			wantErr:  "a key with a fragment must not have an anchor",
		},
		{
			desc:     "key with a fragment and an anchored null value",
			content:  "${FRAGMENT | fragment}: &k\n",
			fragment: "a: made-up-a",
			wantErr:  "a key with a fragment must not have an anchor",
		},
		{
			desc:     "scalar fragment used by a merge key",
			content:  "a: &k ${FRAGMENT | fragment}\nb:\n  <<: *k\n",
			fragment: "made-up-frag",
			wantErr:  "config.yaml: the alias *k on line 3 is used as the value of a merge key, but does not refer to a mapping",
		},
		{
			desc:     "scalar fragment as the value of a merge key",
			content:  "b:\n  <<: ${FRAGMENT | fragment}\n",
			fragment: "made-up-frag",
			wantErr:  "the value of the merge key on line 2 is neither a mapping nor a list of mappings",
		},
		{
			desc:     "alias of the input replaced by an alias of the fragment",
			content:  "v: 0\na: &k made-up-old\n${FRAGMENT | fragment}:\nb: *k\n",
			fragment: "v: &v made-up-v\na: *v",
			wantErr:  "config.yaml: the alias *k on line 4 refers to an anchor that is not defined before it",
		},
		{
			desc:     "mapping fragment with a list of merge values",
			content:  "m: &m {p: 1}\na: &k ${FRAGMENT | fragment}\nb:\n  <<: [*m, *k]\n",
			fragment: "x: made-up-x",
			want:     "m: &m {p: 1}\na: &k\n  x: made-up-x\nb:\n  <<: [*m, *k]\n",
		},
		{
			desc:     "scalar fragment in a list of merge values",
			content:  "m: &m {p: 1}\na: &k ${FRAGMENT | fragment}\nb:\n  <<: [*m, *k]\n",
			fragment: "made-up-frag",
			wantErr:  "config.yaml: the alias *k on line 4 is used as the value of a merge key, but does not refer to a mapping",
		},
		{
			desc:     "scalar fragment as an item of a list of merge values",
			content:  "m: &m {p: 1}\nb:\n  <<:\n    - *m\n    - ${FRAGMENT | fragment}\n",
			fragment: "made-up-frag",
			wantErr:  "the value of the merge key on line 3 is neither a mapping nor a list of mappings",
		},
		{
			desc:     "anchor names of the fragment and the document are all skipped",
			content:  "a: &k ${FRAGMENT | fragment}\nc: &k_1 made-up-c\nb: *k\n",
			fragment: "x: &k made-up-x\ny: &k_1 made-up-y\nz: *k\nw: *k_1",
			want:     "a: &k\n  x: &k_2 made-up-x\n  y: &k_1_1 made-up-y\n  z: *k_2\n  w: *k_1_1\nc: &k_1 made-up-c\nb: *k\n",
		},
		{
			desc:     "several fragments clashing with the same name get different names",
			content:  "a: &k ${FRAGMENT | fragment}\nc: &k ${FRAGMENT | fragment}\nb: *k\n",
			fragment: "x: &k made-up-x\ny: *k",
			want:     "a: &k\n  x: &k_1 made-up-x\n  y: *k_1\nc: &k\n  x: &k_2 made-up-x\n  y: *k_2\nb: *k\n",
		},
		{
			desc:     "anchor of the fragment is renamed once and also used by later aliases",
			content:  "a: &k ${FRAGMENT | fragment}\nb: *k\n",
			fragment: "x: &k 1\ny: *k\nz: &k 2\nw: *k",
			want:     "a: &k\n  x: &k_1 1\n  y: *k_1\n  z: &k_1 2\n  w: *k_1\nb: *k\n",
		},
		{
			desc:     "fragment with several documents uses the first",
			content:  "a: &k ${FRAGMENT | fragment}\nb: *k\n",
			fragment: "&k [1]\n---\nx: &k 2\n",
			want:     "a: &k\n  - 1\nb: *k\n",
		},
		{
			desc:     "alias of the fragment whose anchor is dropped by merging",
			content:  "a: {x: 1}\n${FRAGMENT | fragment}:\n",
			fragment: "a: &m {y: made-up-y}\nb: *m",
			wantErr:  "an alias within a fragment refers to an anchor that is not defined before it",
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			output, err := processFragment(t, general.YAML, tC.content, tC.fragment)
			if tC.wantErr != "" {
				assert.ErrorContains(t, err, tC.wantErr)
				assert.NotContains(t, err.Error(), "made-up")
				assert.Empty(t, output)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tC.want, output)
			assertDecodableYaml(t, output)
		})
	}
}

// assertDecodableYaml checks that every document of the output decodes, which fails for aliases without an anchor
// and merge keys of values other than mappings
func assertDecodableYaml(t *testing.T, output string) {
	t.Helper()
	decoder := yaml.NewDecoder(strings.NewReader(output))
	for {
		var document any
		err := decoder.Decode(&document)
		if errors.Is(err, io.EOF) {
			return
		}
		if !assert.NoError(t, err) {
			return
		}
	}
}

// TestYamlAnchorWithoutFragment checks that an anchored value with a placeholder other than a fragment keeps its
// anchor and aliases
func TestYamlAnchorWithoutFragment(t *testing.T) {
	output, err := processFragment(t, general.YAML, "a: &k ${FRAGMENT_SECRET}\nb: *k\nm: {*k : 1}\n", "")
	assert.NoError(t, err)
	assert.Equal(t, "a: &k s3cret\nb: *k\nm: {*k: 1}\n", output)
	assertDecodableYaml(t, output)
}

// TestYamlFragmentAnchorWithLineComment documents a known bug: a block fragment with an anchor, e.g. the anchor kept
// from the replaced value, is written after the line comment of its placeholder, which cannot be read as YAML:
// "a: # c\n&k\n  x: 1". The bug exists for fragments with an anchor of their own without aliases as well.
func TestYamlFragmentAnchorWithLineComment(t *testing.T) {
	output, err := processFragment(t, general.YAML, "a: &k ${FRAGMENT | fragment} # c\nb: *k\n", "x: made-up-x")
	assert.NoError(t, err)
	assertDecodableYaml(t, output)
}

// TestYamlFragmentAnchorShadowsAnchor documents a known bug: an anchor of a fragment that has the name of an anchor
// elsewhere in the document, which is not the one of the replaced value, is not renamed, so aliases after the fragment
// refer to the fragment instead of the anchor of the document: "c: *k" refers to the 1 of "x: &k 1".
func TestYamlFragmentAnchorShadowsAnchor(t *testing.T) {
	output, err := processFragment(t, general.YAML, "a: &k {p: 1}\nb: &k2 ${FRAGMENT | fragment}\nc: *k\n", "x: &k 1")
	assert.NoError(t, err)
	assert.NotContains(t, output, "x: &k 1")
}

// TestYamlFragmentCommentsKept checks that process keeps the head, line and foot comments of a fragment replacing an
// anchored value with a comment of its own, which are moved where the encoder can write them
func TestYamlFragmentCommentsKept(t *testing.T) {
	output, err := processFragment(t, general.YAML, "a: &k ${FRAGMENT | fragment} # file-c\nb: *k\n",
		"# head-c\n{x: made-up-x} # line-c\n# foot-c")
	assert.NoError(t, err)
	assert.Equal(t, "a: &k\n  # head-c\n  # line-c\n  x: made-up-x # file-c\nb: *k\n# foot-c\n", output)
	assertDecodableYaml(t, output)
}

// TestYamlInvalidMergeKeyOfInputWithFragment checks that a merge key of the input whose value is no mapping is written
// as read, like without fragments, when an unrelated fragment is inserted, since only what fragments cause is checked
func TestYamlInvalidMergeKeyOfInputWithFragment(t *testing.T) {
	const input = "base: &b plain\nx:\n  <<: *b\n"
	withoutFragment, err := processFragment(t, general.YAML, input+"other: ${FRAGMENT_PORT}\n", "")
	assert.NoError(t, err)
	assert.Equal(t, input+"other: \"8080\"\n", withoutFragment)

	output, err := processFragment(t, general.YAML, input+"other: ${FRAGMENT | fragment}\n", "{k: v}")
	assert.NoError(t, err)
	assert.Equal(t, input+"other:\n  k: v\n", output)
}

// TestYamlAnchorReservedBeforeFragmentsWithoutAnchors checks that the anchor names of a document are reserved before a
// first fragment without anchors removes an anchored node, so the anchor of a later fragment with that name is renamed
// and the alias of the removed node fails writing instead of referring to the later fragment
func TestYamlAnchorReservedBeforeFragmentsWithoutAnchors(t *testing.T) {
	t.Setenv("FIRST", "a: made-up-plain")
	t.Setenv("SECOND", "c: &k made-up-other")
	_, err := processFragment(t, general.YAML,
		"a: {nested: &k old}\nc: old-c\n${FIRST | fragment}:\n${SECOND | fragment}:\nb: *k\n", "")
	assert.ErrorContains(t, err, "the alias *k on line 5 refers to an anchor that is not defined before it")
	assert.NotContains(t, err.Error(), "made-up")
}

// TestYamlFragmentRootAliasKeepsBinding checks that an alias within a fragment referring to the fragment's root, which
// is copied into the replaced node, is not reported as referring to another value. The alias contains itself, which
// the parser accepts and the decoder rejects, like without fragments, so the output is only compared.
func TestYamlFragmentRootAliasKeepsBinding(t *testing.T) {
	output, err := processFragment(t, general.YAML, "a: &k ${FRAGMENT | fragment}\nb: &k made-up-other\n",
		"&r [made-up-1, *r]")
	assert.NoError(t, err)
	assert.Equal(t, "a: &k\n  - made-up-1\n  - *k\nb: &k made-up-other\n", output)
}
