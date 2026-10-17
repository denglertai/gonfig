package file

import (
	"bytes"
	"os"
	"path"
	"testing"

	"github.com/denglertai/gonfig/internal/general"
	"github.com/stretchr/testify/assert"
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
			want:     "{\n  \"appenders\": [\n    {\n      \"type\": \"console\",\n      \"port\": 8080\n    }\n  ],\n  \"level\": \"info\"\n}",
		},
		{
			desc:     "json value replaced by null for an empty fragment",
			fileType: general.JSON,
			content:  `{"key": "${FRAGMENT | fragment}"}`,
			fragment: " ",
			want:     "{\n  \"key\": null\n}",
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
			want:     "{\n  \"b\": [\n    \"alpha\",\n    \"\\\"quoted\\\"\"\n  ]\n}",
		},
		// JSON keys
		{
			desc:     "json key deep merged into the object",
			fileType: general.JSON,
			content:  `{"database": {"host": "localhost", "pool": {"timeout": 30}, "${FRAGMENT | fragment}": null, "port": 5432}}`,
			fragment: `{"host": "db.prod", "pool": {"size": 20}, "${FRAGMENT_PORT}_key": "${FRAGMENT_SECRET}"}`,
			want:     "{\n  \"database\": {\n    \"host\": \"db.prod\",\n    \"pool\": {\n      \"timeout\": 30,\n      \"size\": 20\n    },\n    \"port\": 5432,\n    \"8080_key\": \"s3cret\"\n  }\n}",
		},
		{
			desc:     "json key merged into the first of duplicate keys",
			fileType: general.JSON,
			content:  `{"a": {"x": 1}, "a": {"y": 2}, "${FRAGMENT | fragment}": null}`,
			fragment: `{"a": {"z": 3}, "b": 1, "b": 2}`,
			want:     "{\n  \"a\": {\n    \"x\": 1,\n    \"z\": 3\n  },\n  \"a\": {\n    \"y\": 2\n  },\n  \"b\": 2\n}",
		},
		{
			desc:     "json key removed for an empty fragment",
			fileType: general.JSON,
			content:  `{"a": 1, "${FRAGMENT | fragment}": null}`,
			fragment: "",
			want:     "{\n  \"a\": 1\n}",
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
			want:     "{\n  \"8080\": 8080\n}",
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
			wantErr:  "lists can only be inserted into YAML and JSON files",
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
