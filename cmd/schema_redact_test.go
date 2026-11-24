package cmd

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/denglertai/gonfig/internal/file"
	"github.com/denglertai/gonfig/internal/filter"
)

// leafSchema accepts objects, arrays and integers at every level, so every string leaf is a violation at its path
const leafSchema = `{"$id": "leaf", "type": ["object", "array", "integer"], "additionalProperties": {"$ref": "#"}, "items": {"$ref": "#"}}`

// TestSchemaViolationPathsHideKeyEdgeCases checks which keys the paths of violations mask for edge cases of keys, in
// process and check: nested placeholder keys, filters, defaults, escapes, arrays, quoted and dotted TOML keys, flow
// mappings and non-string keys. Keys are masked by their location, so a static key that equals the result of a
// placeholder elsewhere stays visible, since masking it would reveal that the two are equal.
func TestSchemaViolationPathsHideKeyEdgeCases(t *testing.T) {
	const secretA, secretB = "made-up-key-a-s3cret", "made-up-key-b-s3cret"
	t.Setenv("SCHEMA_EDGE_A", secretA)
	t.Setenv("SCHEMA_EDGE_B", secretB)
	t.Setenv("SCHEMA_EDGE_NUM", "42")
	t.Cleanup(func() {
		file.SetLogValues(false)
		filter.SetValuesInErrors(false)
	})

	testCases := []struct {
		name    string
		file    string
		content string
		want    []string // texts that must be reported
		hidden  []string // texts that must not be in the output
	}{
		{name: "two keys", file: "c.yaml", content: "${SCHEMA_EDGE_A}: x\n${SCHEMA_EDGE_B}: x\n",
			want: []string{"***: got string"}, hidden: []string{secretA, secretB}},
		{name: "nested keys", file: "c.yaml", content: "${SCHEMA_EDGE_A}:\n  pre_${SCHEMA_EDGE_B}:\n    static: x\n",
			want: []string{"***.pre_***.static: got string"}, hidden: []string{secretA, secretB}},
		{name: "composed key", file: "c.yaml", content: "${SCHEMA_EDGE_A}.${SCHEMA_EDGE_B}: x\n",
			want: []string{"***.***: got string"}, hidden: []string{secretA, secretB}},
		{name: "filter and default", file: "c.yaml", content: "\"${SCHEMA_EDGE_A | upper}\": x\n\"${SCHEMA_EDGE_MISSING | default(dflt-key)}\": x\n",
			want: []string{"***: got string"}, hidden: []string{secretA, strings.ToUpper(secretA)}},
		{name: "escape stays visible", file: "c.yaml", content: "$${SCHEMA_EDGE_A}: x\n",
			want: []string{"${SCHEMA_EDGE_A}: got string"}},
		{name: "escape and placeholder", file: "c.yaml", content: "\"$${LIT}_${SCHEMA_EDGE_A}\": x\n",
			want: []string{"${LIT}_***: got string"}, hidden: []string{secretA}},
		{name: "array below key", file: "c.yaml", content: "${SCHEMA_EDGE_A}:\n  - x\n  - [y]\n",
			want: []string{"***.0: got string", "***.1.0: got string"}, hidden: []string{secretA}},
		{name: "flow mapping", file: "c.yaml", content: "{\"${SCHEMA_EDGE_A}\": {\"pre_${SCHEMA_EDGE_B}\": x}}\n",
			want: []string{"***.pre_***: got string"}, hidden: []string{secretA, secretB}},
		{name: "complex key", file: "c.yaml", content: "? ${SCHEMA_EDGE_A}\n: x\n",
			want: []string{"***: got string"}, hidden: []string{secretA}},
		{name: "non string key", file: "c.yaml", content: "${SCHEMA_EDGE_NUM | to_int}: x\n",
			want: []string{"***: got string"}, hidden: []string{"42"}},
		{name: "static key equal to a processed key", file: "c.yaml", content: "${SCHEMA_EDGE_NUM}: x\nother:\n  \"42\": x\n",
			want: []string{"***: got string", "other.42: got string"}},
		{name: "json keys", file: "c.json", content: `{"${SCHEMA_EDGE_A}": {"pre_${SCHEMA_EDGE_B}": ["x"]}, "$${X}": "x"}`,
			want:   []string{"***.pre_***.0: got string", "${X}: got string"},
			hidden: []string{secretA, secretB}},
		{name: "toml dotted and quoted keys", file: "c.toml",
			content: "[\"${SCHEMA_EDGE_A}\"]\nstatic.\"pre_${SCHEMA_EDGE_B}\".k = \"x\"\n",
			want:    []string{"***.static.pre_***.k: got string"}, hidden: []string{secretA, secretB}},
		{name: "toml inline table", file: "c.toml", content: "t = { \"${SCHEMA_EDGE_A}\" = \"x\", n = { \"${SCHEMA_EDGE_B}\" = \"x\" } }\n",
			want: []string{"t.***: got string", "t.n.***: got string"}, hidden: []string{secretA, secretB}},
	}
	for _, tC := range testCases {
		t.Run(tC.name, func(t *testing.T) {
			t.Setenv("GONFIG_LOG_VALUES", "")
			writeSchemaFiles(t, map[string]string{tC.file: tC.content, "leaf.json": leafSchema})
			for _, args := range [][]string{
				{"config", "process", "-f", tC.file},
				{"config", "check", "-f", tC.file},
				{"config", "check", "-o", "json", "-f", tC.file},
			} {
				args = append(args, "--schema", "leaf.json")
				output, err := runSchemaCommand(t, args...)
				if !assert.Error(t, err, args) {
					continue
				}
				all := output + err.Error()
				for _, want := range tC.want {
					// The JSON report has the path and the message in separate fields
					path, _, _ := strings.Cut(want, ": got")
					assert.True(t, strings.Contains(all, want) || strings.Contains(all, `"path": "`+path+`"`), "%s in %v", want, args)
				}
				for _, text := range tC.hidden {
					assert.NotContains(t, all, text, args)
				}
			}
		})
	}
}

// TestSchemaViolationKeysNotLeakedBetweenFiles checks that the keys produced in one file don't mask the same static
// text in another file of the same run, since every file has its own processor
func TestSchemaViolationKeysNotLeakedBetweenFiles(t *testing.T) {
	t.Setenv("SCHEMA_EDGE_A", "shared-key")
	t.Setenv("GONFIG_LOG_VALUES", "")
	t.Cleanup(func() {
		file.SetLogValues(false)
		filter.SetValuesInErrors(false)
	})
	writeSchemaFiles(t, map[string]string{
		"a.yaml":    "${SCHEMA_EDGE_A}: x\n",
		"b.yaml":    "shared-key: x\n",
		"leaf.json": leafSchema,
	})
	// process stops at the first file not matching the schema, so only check reports both
	output, err := runSchemaCommand(t, "config", "check", "-f", "*.yaml", "--schema", "leaf.json")
	assert.Error(t, err)
	assert.Contains(t, output, "schema: ***: got string")
	assert.Contains(t, output, "schema: shared-key: got string")
}

// TestSchemaViolationsHideKeysOfAliasesTagsAndInvalidUTF8 checks that keys produced by placeholders are masked in
// process and check, in text and JSON reports, when they are YAML aliases of processed values, have YAML tags or
// contain invalid UTF-8, follow merge keys in sequences, anchors of fragments renamed since the document has them or
// anchored values of fragments, and that static keys and indexes equal to a processed key stay visible, unless
// --log-values is set
func TestSchemaViolationsHideKeysOfAliasesTagsAndInvalidUTF8(t *testing.T) {
	const closed = `{"properties": {"a": {}}, "additionalProperties": false}`
	testCases := []struct {
		name    string
		env     map[string]string
		file    string
		content string
		schema  string
		want    []string // texts that must be reported, the path and message are separate fields in the JSON report
		hidden  []string // texts that must not be in the output without --log-values
		absent  []string // texts that must not be in the output even with --log-values, e.g. values that are no keys
	}{
		{name: "alias as key", env: map[string]string{"SCHEMA_KEY_PW": "made-up-alias-s3cret"}, file: "c.yaml",
			content: "a: &v ${SCHEMA_KEY_PW}\n*v : 1\n", schema: closed,
			want: []string{"additional properties '***' not allowed"}, hidden: []string{"made-up-alias-s3cret"}},
		{name: "alias in path", env: map[string]string{"SCHEMA_KEY_PW": "made-up-alias-s3cret"}, file: "c.yaml",
			content: "a: &v ${SCHEMA_KEY_PW}\nb:\n  *v : {c: x}\n", schema: leafSchema,
			want: []string{"b.***.c"}, hidden: []string{"made-up-alias-s3cret"}},
		{name: "tagged keys", env: map[string]string{"SCHEMA_KEY_N": "1e3", "SCHEMA_KEY_TM": "2001-12-14", "SCHEMA_KEY_PIN": "1234"},
			file: "c.yaml", content: "!!float ${SCHEMA_KEY_N}: 1\n!!timestamp ${SCHEMA_KEY_TM}: 2\n!!int ${SCHEMA_KEY_PIN}: 3\n",
			schema: closed, want: []string{"additional properties '***', '***', '***' not allowed"},
			hidden: []string{"1000", "2001", "1234"}},
		{name: "invalid UTF-8 in JSON", env: map[string]string{"SCHEMA_KEY_PW": "made-up-\xffs3cret"}, file: "c.json",
			content: `{"${SCHEMA_KEY_PW}": 1}`, schema: closed,
			want: []string{"additional properties '***' not allowed"}, hidden: []string{"s3cret"}},
		{name: "invalid UTF-8 in TOML", env: map[string]string{"SCHEMA_KEY_PW": "made-up-\xffs3cret"}, file: "c.toml",
			content: "\"${SCHEMA_KEY_PW}\" = 1\n", schema: closed,
			want: []string{"additional properties '***' not allowed"}, hidden: []string{"s3cret"}},
		{name: "static key equal to a processed key", env: map[string]string{"SCHEMA_KEY_PW": "admin"}, file: "c.yaml",
			content: "${SCHEMA_KEY_PW}: 1\nusers: {admin: {x: s}}\n", schema: leafSchema,
			want: []string{"users.admin.x"}},
		{name: "dependency", env: map[string]string{"SCHEMA_KEY_PW": "admin"}, file: "c.yaml",
			content: "${SCHEMA_KEY_PW}: 1\n", schema: `{"dependentRequired": {"admin": ["pw"]}}`,
			want: []string{"properties 'pw' required, if '***' exists"}, hidden: []string{"'admin'"}},
		{name: "index equal to a processed key", env: map[string]string{"SCHEMA_KEY_PW": "0"}, file: "c.yaml",
			content: "${SCHEMA_KEY_PW}: 1\nlist: [{x: s}]\n", schema: leafSchema,
			want: []string{"list.0.x"}},
		{name: "merge tagged sequence item", env: map[string]string{"SCHEMA_KEY_PW": "made-up-seq-s3cret"}, file: "c.yaml",
			content: "l:\n  - !!merge x\n  - {\"${SCHEMA_KEY_PW}\": 1}\n",
			schema:  `{"properties": {"l": {"items": {"additionalProperties": false}}}}`,
			want:    []string{"additional properties '***' not allowed"}, hidden: []string{"made-up-seq-s3cret"}},
		// An anchor of a fragment with the name of an anchor of the document is renamed, so the alias still refers to the
		// static anchor of the document, and the value of the fragment is no key
		{name: "anchor of a fragment with the name of an anchor of the document",
			env:     map[string]string{"SCHEMA_KEY_PW": "made-up-anchor-s3cret", "SCHEMA_KEY_FR": `[&k "${SCHEMA_KEY_PW}"]`},
			file:    "c.yaml",
			content: "a: &k static-anchor\nf: ${SCHEMA_KEY_FR | fragment}\nm:\n  *k : {c: x}\n",
			schema:  `{"properties": {"m": {"additionalProperties": {"additionalProperties": {"type": "integer"}}}}}`,
			want:    []string{"m.static-anchor.c"}, absent: []string{"made-up-anchor-s3cret"}},
		{name: "anchored scalar of a fragment with the name of an anchor of the document",
			env:  map[string]string{"SCHEMA_KEY_FR": "&s made-up-fragment-s3cret"},
			file: "c.yaml", content: "x: &s plain\nf: ${SCHEMA_KEY_FR | fragment}\na: {*s : 1}\n",
			schema: `{"additionalProperties": {"additionalProperties": false}}`,
			want:   []string{"additional properties 'plain' not allowed"}, absent: []string{"made-up-fragment-s3cret"}},
		{name: "anchored scalar within a mapping of a fragment with the name of an anchor of the document",
			env:  map[string]string{"SCHEMA_KEY_FR": "{q: &s made-up-fragment-s3cret}"},
			file: "c.yaml", content: "x: &s plain\nf: ${SCHEMA_KEY_FR | fragment}\na: {*s : 1}\n",
			schema: `{"additionalProperties": {"additionalProperties": false}}`,
			want:   []string{"additional properties 'plain' not allowed"}, absent: []string{"made-up-fragment-s3cret"}},
		{name: "alias within a fragment", env: map[string]string{"SCHEMA_KEY_FR": "{v: &a made-up-fragment-s3cret, m: {*a : 1}}"},
			file: "c.yaml", content: "f: ${SCHEMA_KEY_FR | fragment}\n",
			schema: `{"properties": {"f": {"properties": {"m": {"additionalProperties": false}}}}}`,
			want:   []string{"additional properties '***' not allowed"}, hidden: []string{"made-up-fragment-s3cret"}},
		{name: "integer key merged into string keys", env: map[string]string{"SCHEMA_KEY_PW": "0x5EC"}, file: "c.yaml",
			content: "base: &b {!!int \"${SCHEMA_KEY_PW}\": 1}\nd:\n  s: 1\n  <<: *b\n",
			schema:  `{"properties": {"d": {"properties": {"s": {}}, "additionalProperties": false}}}`,
			want:    []string{"additional properties '***' not allowed"}, hidden: []string{"0x5EC"}},
		{name: "merge tagged sequence item before a key equal to a static key",
			env: map[string]string{"SCHEMA_KEY_PW": "made-up-same-key"}, file: "c.yaml",
			content: "public: {made-up-same-key: 1}\nl:\n  - !!merge x\n  - {\"${SCHEMA_KEY_PW}\": 1}\n",
			schema:  `{"properties": {"l": {"items": {"additionalProperties": false}}}}`,
			want:    []string{"additional properties '***' not allowed"}, hidden: []string{"made-up-same-key"}},
		{name: "merge tagged sequence item before a static key and a processed key",
			env: map[string]string{"SCHEMA_KEY_PW": "made-up-same-key"}, file: "c.yaml",
			content: "l:\n  - !!merge x\n  - {made-up-same-key: 1}\n  - {\"${SCHEMA_KEY_PW}\": 1}\n",
			schema:  `{"properties": {"l": {"items": {"additionalProperties": false}}}}`,
			want:    []string{"l.0", "additional properties 'made-up-same-key' not allowed", "l.1", "additional properties '***' not allowed"}},
		{name: "integer key merged into string keys equal to a static key", env: map[string]string{"SCHEMA_KEY_PW": "0x5EC"},
			file:    "c.yaml",
			content: "base: &b {!!int \"${SCHEMA_KEY_PW}\": 1}\nd:\n  s: 1\n  <<: *b\nother: {\"0x5EC\": 1}\n",
			schema:  `{"properties": {"d": {"properties": {"s": {}}, "additionalProperties": false}}}`,
			want:    []string{"additional properties '***' not allowed"}, hidden: []string{"'0x5EC'"}},
		{name: "integer key merged into string keys with a static key of its text merged as well",
			env: map[string]string{"SCHEMA_KEY_PW": "0x5EC"}, file: "c.yaml",
			content: "b: &b {!!int \"${SCHEMA_KEY_PW}\": 1}\nc: &c {\"0x5EC\": 1}\nd:\n  s: 1\n  <<: [*b, *c]\n",
			schema:  `{"properties": {"d": {"properties": {"s": {}}, "additionalProperties": false}}}`,
			want:    []string{"additional properties '***' not allowed"}, hidden: []string{"'0x5EC'"}},
		{name: "escaped anchored scalar of a fragment", env: map[string]string{"SCHEMA_KEY_FR": `{v: &a "made-up-anchored-s3cret-$${ESC}", m: {*a : 1}}`},
			file: "c.yaml", content: "f: ${SCHEMA_KEY_FR | fragment}\n",
			schema: `{"properties": {"f": {"properties": {"m": {"additionalProperties": false}}}}}`,
			want:   []string{"additional properties '***' not allowed"}, hidden: []string{"made-up-anchored-s3cret"}},
		{name: "anchored scalar of a fragment equal to a static key", env: map[string]string{"SCHEMA_KEY_FR": "{v: &a made-up-public-key, m: {*a : 1}}"},
			file: "c.yaml", content: "f: ${SCHEMA_KEY_FR | fragment}\nmade-up-public-key: 1\n",
			schema: `{"properties": {"f": {"properties": {"m": {"additionalProperties": false}}}}}`,
			want:   []string{"additional properties '***' not allowed"}, hidden: []string{"'made-up-public-key'"}},
	}
	for _, tC := range testCases {
		t.Run(tC.name, func(t *testing.T) {
			t.Setenv("GONFIG_LOG_VALUES", "")
			for name, value := range tC.env {
				t.Setenv(name, value)
			}
			t.Cleanup(func() {
				file.SetLogValues(false)
				filter.SetValuesInErrors(false)
			})
			writeSchemaFiles(t, map[string]string{tC.file: tC.content, "schema.json": tC.schema})
			for _, args := range [][]string{
				{"config", "process", "-f", tC.file},
				{"config", "check", "-f", tC.file},
				{"config", "check", "-o", "json", "-f", tC.file},
			} {
				args = append(args, "--schema", "schema.json")
				output, err := runSchemaCommand(t, args...)
				if !assert.Error(t, err, args) {
					continue
				}
				all := output + err.Error()
				for _, want := range tC.want {
					assert.Contains(t, all, want, args)
				}
				for _, text := range append(tC.hidden, tC.absent...) {
					assert.NotContains(t, all, text, args)
				}

				output, err = runSchemaCommand(t, append(args, "--log-values")...)
				assert.Error(t, err, args)
				for _, text := range tC.hidden {
					assert.Contains(t, output+err.Error(), text, args)
				}
				for _, text := range tC.absent {
					assert.NotContains(t, output+err.Error(), text, args)
				}
			}
		})
	}
}

// TestSchemaViolationsHideKeysAfterEmptyDocuments checks that keys of YAML files with empty documents are masked in
// process and check, in text and JSON reports, since the written content has no document for a first empty document
func TestSchemaViolationsHideKeysAfterEmptyDocuments(t *testing.T) {
	const secret = "made-up-doc-s3cret"
	t.Setenv("SCHEMA_DOC_PW", secret)
	t.Setenv("GONFIG_LOG_VALUES", "")
	t.Cleanup(func() {
		file.SetLogValues(false)
		filter.SetValuesInErrors(false)
	})

	for _, content := range []string{
		"---\n---\n${SCHEMA_DOC_PW}: 1\n",
		"---\n# only comment\n---\n${SCHEMA_DOC_PW}: 1\n",
		"---\n...\n---\n${SCHEMA_DOC_PW}: 1\n",
		"--- \n--- \n--- {\"${SCHEMA_DOC_PW}\": 1}\n",
		"---\n---\n---\n${SCHEMA_DOC_PW}: 1\n---\n${SCHEMA_DOC_PW}x: 1\n",
		"---\n~\n---\n${SCHEMA_DOC_PW}: 1\n",
	} {
		writeSchemaFiles(t, map[string]string{"c.yaml": content, "schema.json": `{"additionalProperties": false}`})
		for _, args := range [][]string{
			{"config", "process", "-f", "c.yaml"},
			{"config", "check", "-f", "c.yaml"},
			{"config", "check", "-o", "json", "-f", "c.yaml"},
		} {
			args = append(args, "--schema", "schema.json")
			output, err := runSchemaCommand(t, args...)
			if !assert.Error(t, err, args) {
				continue
			}
			all := output + err.Error()
			assert.Contains(t, all, "additional properties '***' not allowed", content, args)
			assert.NotContains(t, all, secret, content, args)
		}
	}
}

// TestProcessAliasExpansionWithoutSchema checks that processing a YAML file whose aliases expand exponentially and
// that has keys with placeholders takes no time without a schema, which locating the keys would take
func TestProcessAliasExpansionWithoutSchema(t *testing.T) {
	t.Setenv("SCHEMA_DOC_PW", "made-up-doc-s3cret")
	t.Setenv("GONFIG_LOG_VALUES", "")
	content := "p: &p \"${SCHEMA_DOC_PW}\"\n\"${SCHEMA_DOC_PW}\": 1\na0: &a0 {\"${SCHEMA_DOC_PW}\": [x,x,x,x,x,x,x,x,x,x]}\n"
	for i := 1; i <= 7; i++ {
		item := "*a" + strconv.Itoa(i-1)
		content += "a" + strconv.Itoa(i) + ": &a" + strconv.Itoa(i) + " [" + strings.TrimSuffix(strings.Repeat(item+",", 10), ",") + "]\n"
	}
	writeSchemaFiles(t, map[string]string{"c.yaml": content, "schema.json": `{}`})

	output, err := runSchemaCommand(t, "config", "process", "-f", "c.yaml")
	assert.NoError(t, err)
	assert.Contains(t, output, "a7: &a7 [*a6")

	// The validator rejects the file, since it expands too much, so the keys are never located
	_, err = runSchemaCommand(t, "config", "process", "-f", "c.yaml", "--schema", "schema.json")
	assert.ErrorContains(t, err, "invalid YAML")
}

// TestSchemaViolationsShowStaticKeys checks that keys of the input and of fragments stay visible in violations in every
// file type, also when the file has keys produced by placeholders, which are masked: quoted keys, keys with dots and
// spaces, TOML dotted and quoted keys, YAML keys of merged anchors and aliases of static scalars, escaped placeholders
// and keys of dotenv and INI files
func TestSchemaViolationsShowStaticKeys(t *testing.T) {
	t.Setenv("SCHEMA_STATIC_PW", "made-up-static-s3cret")
	t.Setenv("SCHEMA_STATIC_FR", `{"frag key": {"x": "s"}}`)
	t.Setenv("GONFIG_LOG_VALUES", "")
	t.Cleanup(func() {
		file.SetLogValues(false)
		filter.SetValuesInErrors(false)
	})

	testCases := []struct {
		file    string
		content string
		want    []string
	}{
		{"c.yaml", "${SCHEMA_STATIC_PW}: {x: s}\n\"quoted key\": {x: s}\na.b c: {x: s}\nbase: &b {merged: {x: s}}\n" +
			"m:\n  <<: *b\nv: &v aliased\nal:\n  *v : {x: s}\n$${ESC}: {x: s}\nf: ${SCHEMA_STATIC_FR | fragment}\n",
			[]string{"***.x", "quoted key.x", "a.b c.x", "base.merged.x", "m.merged.x", "al.aliased.x", "${ESC}.x", "f.frag key.x"}},
		{"c.json", `{"${SCHEMA_STATIC_PW}": {"x": "s"}, "quoted key": {"x": "s"}, "a.b": {"x": "s"}, "$${ESC}": {"x": "s"}, ` +
			`"f": "${SCHEMA_STATIC_FR | fragment}"}`,
			[]string{"***.x", "quoted key.x", "a.b.x", "${ESC}.x", "f.frag key.x"}},
		{"c.toml", "\"${SCHEMA_STATIC_PW}\" = { x = \"s\" }\n\"quoted key\" = { x = \"s\" }\na.\"b c\".x = \"s\"\n" +
			"[t.\"d.e\"]\nx = \"s\"\n",
			[]string{"***.x", "quoted key.x", "a.b c.x", "t.d.e.x"}},
	}
	for _, tC := range testCases {
		t.Run(tC.file, func(t *testing.T) {
			writeSchemaFiles(t, map[string]string{tC.file: tC.content, "leaf.json": leafSchema})
			for _, args := range [][]string{
				{"config", "process", "-f", tC.file},
				{"config", "check", "-f", tC.file},
				{"config", "check", "-o", "json", "-f", tC.file},
			} {
				args = append(args, "--schema", "leaf.json")
				output, err := runSchemaCommand(t, args...)
				if !assert.Error(t, err, args) {
					continue
				}
				all := output + err.Error()
				for _, want := range tC.want {
					assert.True(t, strings.Contains(all, want+": got string") || strings.Contains(all, `"path": "`+want+`"`),
						"%s in %v", want, args)
				}
				assert.NotContains(t, all, "made-up-static-s3cret", args)
			}
		})
	}

	writeSchemaFiles(t, map[string]string{"c.env": "PLAIN_KEY=x\nOTHER=${SCHEMA_STATIC_PW}\n", "c.ini": "top=x\n[section]\nkey=x\n",
		"strict.json": `{"additionalProperties": {"type": "object"}}`})
	output, err := runSchemaCommand(t, "config", "check", "-f", "c.env", "-f", "c.ini", "--schema", "strict.json")
	assert.Error(t, err)
	assert.Contains(t, output, "schema: PLAIN_KEY: got string")
	assert.Contains(t, output, "schema: OTHER: got string")
	assert.Contains(t, output, "schema: top: got string")
}
