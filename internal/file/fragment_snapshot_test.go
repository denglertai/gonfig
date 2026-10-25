package file

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/denglertai/gonfig/internal/general"
	"github.com/gkampitakis/go-snaps/snaps"
)

// tempFileRe matches the temporary path of the processed file within error messages
var tempFileRe = regexp.MustCompile(`\S*/(config\.\w+)`)

// TestFragmentSnapshots documents the behavior of fragments. Each snapshot shows the processed file, the fragment
// (the environment variable FRAGMENT) and the output or error, so changes in behavior are easy to review.
// Note that go-snaps separates snapshots by "---" and therefore stores YAML document separators as "/-/-/-/".
func TestFragmentSnapshots(t *testing.T) {
	testCases := []struct {
		name     string
		fileType general.FileType
		content  string
		fragment string
	}{
		{
			name:     "yaml value replaced by a JSON list",
			fileType: general.YAML,
			content: `# Logging configuration
logging:
  level: info # default level
  appenders: ${FRAGMENT | fragment} # set per environment
`,
			fragment: `[{"type": "console"}, {"type": "file", "path": "/var/log/app.log", "maxSize": "100"}]`,
		},
		{
			name:     "yaml value replaced by a YAML mapping with a literal block",
			fileType: general.YAML,
			content: `job:
  name: backup
  steps: ${FRAGMENT | fragment}
`,
			fragment: `prepare: mkdir -p /backup
run: |
  tar czf /backup/data.tgz /data
  echo done
retries: 3
`,
		},
		{
			name:     "yaml key merging environment overrides",
			fileType: general.YAML,
			content: `database:
  host: localhost # development
  port: 5432
  pool:
    timeout: 30
    size: 5
  replicas: [db-1, db-2]
  ${FRAGMENT | fragment}:
cache:
  enabled: false
`,
			fragment: `host: db.prod
pool:
  size: 20
replicas: [db-prod-1]
ssl:
  mode: require
`,
		},
		{
			name:     "yaml fragment with placeholders",
			fileType: general.YAML,
			content: `service:
  name: api
  ${FRAGMENT | fragment}:
`,
			fragment: `port: ${FRAGMENT_PORT | to_int}
password: ${FRAGMENT_SECRET | base64}
url: http://localhost:${FRAGMENT_PORT}/health
`,
		},
		{
			name:     "yaml empty fragments",
			fileType: general.YAML,
			content: `optional: ${FRAGMENT | fragment}
settings:
  a: 1
  ${FRAGMENT | fragment}:
`,
			fragment: "",
		},
		{
			name:     "yaml multiple documents",
			fileType: general.YAML,
			content: `kind: Deployment
metadata:
  name: api
---
kind: ConfigMap
data: ${FRAGMENT | fragment}
`,
			fragment: `{"LOG_LEVEL": "debug", "WORKERS": "4"}`,
		},
		{
			name:     "json value replaced by a list",
			fileType: general.JSON,
			content: `{
  "logging": {
    "level": "info",
    "appenders": "${FRAGMENT | fragment}"
  }
}`,
			fragment: `[{"type": "console"}, {"type": "file", "path": "/var/log/app.log"}]`,
		},
		{
			name:     "json key merging environment overrides",
			fileType: general.JSON,
			content: `{
  "database": {
    "host": "localhost",
    "port": 5432,
    "pool": {"timeout": 30, "size": 5},
    "${FRAGMENT | fragment}": null
  }
}`,
			fragment: `{"host": "db.prod", "pool": {"size": 20}, "ssl": true, "${FRAGMENT_PORT}_port": "${FRAGMENT_PORT | to_int}"}`,
		},
		{
			name:     "xml element content replaced by elements",
			fileType: general.XML,
			content: `<?xml version="1.0"?>
<server>
  <!-- data sources are set per environment -->
  <resources>${FRAGMENT | fragment}</resources>
</server>
`,
			fragment: `<datasource name="main" url="jdbc:postgresql://db/app?ssl=true&amp;user=app"/>
<datasource name="readonly"><password>${FRAGMENT_SECRET}</password></datasource>`,
		},
		{
			name:     "yaml list from split",
			fileType: general.YAML,
			content: `a: "val"
b: "${FRAGMENT | split(,) | fragment}"
trimmed: ${FRAGMENT | split | trim | fragment}
`,
			fragment: "alpha, beta ,gamma",
		},
		{
			name:     "json list from split",
			fileType: general.JSON,
			content:  `{"hosts": "${FRAGMENT | split | trim | fragment}"}`,
			fragment: "db-1, db-2, db-3",
		},
		{
			name:     "toml value replaced by an array of inline tables",
			fileType: general.TOML,
			content: `# Logging configuration
[logging]
level = "info" # default level
appenders = "${FRAGMENT | fragment}" # set per environment
`,
			fragment: `[
  { type = "console" },
  { type = "file", path = "/var/log/app.log", maxSize = 100 }, # rotated daily
]`,
		},
		{
			name:     "toml fragment with placeholders",
			fileType: general.TOML,
			content: `[service]
name = "api"
settings = "${FRAGMENT | fragment}"
`,
			fragment: `{ port = "${FRAGMENT_PORT | to_int}", password = "${FRAGMENT_SECRET | base64}", url = "http://localhost:${FRAGMENT_PORT}/health" }`,
		},
		{
			name:     "toml empty fragments",
			fileType: general.TOML,
			content: `optional = "${FRAGMENT | fragment}"
[settings]
a = 1
  b = '${FRAGMENT | fragment}'
c = """${FRAGMENT | fragment}""" # the comment is kept
`,
			fragment: "",
		},
		{
			name:     "toml list from split",
			fileType: general.TOML,
			content:  "hosts = \"${FRAGMENT | split | trim | fragment}\"\nnested = { hosts = \"${FRAGMENT | split | fragment}\" }\n",
			fragment: "db-1, db-2",
		},
		{
			name:     "error for a toml key",
			fileType: general.TOML,
			content:  "[database]\n\"${FRAGMENT | fragment}\" = \"\"\n",
			fragment: "host = \"db.prod\"\n",
		},
		{
			name:     "error for an empty toml fragment in an array",
			fileType: general.TOML,
			content:  "hosts = [\"db-1\", \"${FRAGMENT | fragment}\"]\n",
			fragment: "",
		},
		{
			name:     "error for a toml fragment with several values",
			fileType: general.TOML,
			content:  "settings = \"${FRAGMENT | fragment}\"\n",
			fragment: "1\nother = 2\n",
		},
		{
			name:     "error for an invalid toml fragment",
			fileType: general.TOML,
			content:  "settings = \"${FRAGMENT | fragment}\"\n",
			fragment: "{ a = 1, a = 2 }",
		},
		{
			name:     "error for a list in an xml file",
			fileType: general.XML,
			content:  "<server><hosts>${FRAGMENT | split | fragment}</hosts></server>\n",
			fragment: "db-1,db-2",
		},
		{
			name:     "error for a nested fragment",
			fileType: general.YAML,
			content:  "outer: ${FRAGMENT | fragment}\n",
			fragment: "inner: ${FRAGMENT | fragment}\n",
		},
		{
			name:     "error for a fragment embedded into text",
			fileType: general.YAML,
			content:  "key: prefix ${FRAGMENT | fragment}\n",
			fragment: "a: 1\n",
		},
		{
			name:     "error for a yaml key with a value",
			fileType: general.YAML,
			content:  "${FRAGMENT | fragment}: value\n",
			fragment: "a: 1\n",
		},
		{
			name:     "error for a properties file",
			fileType: general.PROPERTIES,
			content:  "key=${FRAGMENT | fragment}\n",
			fragment: "a=1\n",
		},
	}
	for _, tC := range testCases {
		t.Run(tC.name, func(t *testing.T) {
			output, err := processFragment(t, tC.fileType, tC.content, tC.fragment)

			result := output
			if err != nil {
				// The file is processed in a temporary directory, so only its name is part of the snapshot
				result = "error: " + tempFileRe.ReplaceAllString(err.Error(), "$1")
			}

			fragment := strings.TrimRight(tC.fragment, "\n")
			if fragment == "" {
				fragment = "(empty)"
			}

			snaps.MatchSnapshot(t, fmt.Sprintf("file (config.%s):\n%s\n\nFRAGMENT:\n%s\n\nresult:\n%s",
				tC.fileType, strings.TrimRight(tC.content, "\n"), fragment, strings.TrimRight(result, "\n")))
		})
	}
}
