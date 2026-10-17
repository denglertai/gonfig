package file

import (
	"bytes"
	"fmt"
	"os"
	"path"
	"slices"
	"strings"
	"testing"

	"github.com/denglertai/gonfig/internal/config"
	"github.com/denglertai/gonfig/internal/general"
	"github.com/denglertai/gonfig/internal/value"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/assert"
)

// behaviorCase is a case of a snapshot test documenting the behavior of gonfig
type behaviorCase struct {
	name     string
	fileType general.FileType
	// gonfigConfig is the content of .gonfig.yaml, e.g. to define templates
	gonfigConfig string
	env          map[string]string
	content      string
}

// snapshotEnv are the environment variables used by the snapshots. Variables not set by a case are cleared,
// so the snapshots do not depend on the environment of the machine running the tests.
var snapshotEnv = []string{"PORT", "RATIO", "WHOLE", "ENABLED", "ZIP", "PASSWORD", "NAME", "DB_HOST", "DB_PORT", "DB_NAME", "DB_USER", "DB_PASSWORD"}

// runBehaviorSnapshots processes the cases and matches the snapshots, which show the .gonfig.yaml,
// the environment variables, the processed file and the output or error, so changes in behavior are easy to review.
// Note that go-snaps separates snapshots by "---" and therefore stores YAML document separators as "/-/-/-/".
func runBehaviorSnapshots(t *testing.T, cases []behaviorCase) {
	t.Helper()

	for _, tC := range cases {
		t.Run(tC.name, func(t *testing.T) {
			for _, name := range snapshotEnv {
				t.Setenv(name, "")
			}
			for name, val := range tC.env {
				t.Setenv(name, val)
			}

			snaps.MatchSnapshot(t, renderBehavior(tC, processBehavior(t, tC)))
		})
	}
}

// processBehavior loads the templates of the case like the CLI does, processes the file and returns the output or error
func processBehavior(t *testing.T, tC behaviorCase) string {
	t.Helper()

	configDir := t.TempDir()
	assert.NoError(t, os.WriteFile(path.Join(configDir, ".gonfig.yaml"), []byte(tC.gonfigConfig), 0o644))
	t.Setenv("GONFIG_CONFIG_PATH", configDir)
	t.Cleanup(func() { assert.NoError(t, value.SetTemplates(config.Templates{})) })

	v, err := config.SetupViper(nil)
	if err != nil {
		return "error: " + err.Error()
	}
	cfg, err := config.LoadAppConfig(v)
	if err != nil {
		return "error: " + err.Error()
	}
	if err := value.SetTemplates(cfg.Templates); err != nil {
		return "error: invalid templates in config file: " + err.Error()
	}

	file := path.Join(t.TempDir(), "config."+string(tC.fileType))
	assert.NoError(t, os.WriteFile(file, []byte(tC.content), 0o644))

	var output bytes.Buffer
	if err := NewFileProcessor(file, tC.fileType, &output).Process(); err != nil {
		// The file is processed in a temporary directory, so only its name is part of the snapshot
		return "error: " + tempFileRe.ReplaceAllString(err.Error(), "$1")
	}
	return output.String()
}

// renderBehavior renders the case and its result as a readable snapshot
func renderBehavior(tC behaviorCase, result string) string {
	var sb strings.Builder

	if tC.gonfigConfig != "" {
		fmt.Fprintf(&sb, ".gonfig.yaml:\n%s\n\n", strings.TrimRight(tC.gonfigConfig, "\n"))
	}

	sb.WriteString("environment:\n")
	names := make([]string, 0, len(tC.env))
	for name := range tC.env {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		fmt.Fprintf(&sb, "%s=%q\n", name, tC.env[name])
	}
	if len(names) == 0 {
		sb.WriteString("(none)\n")
	}

	fmt.Fprintf(&sb, "\nfile (config.%s):\n%s\n\nresult:\n%s", tC.fileType, strings.TrimRight(tC.content, "\n"), strings.TrimRight(result, "\n"))
	return sb.String()
}

// TestTypedValueSnapshots documents how typed filter results are written by each file type
func TestTypedValueSnapshots(t *testing.T) {
	env := map[string]string{"PORT": "8080", "RATIO": "0.75", "WHOLE": "2", "ENABLED": "1", "ZIP": "01701"}

	runBehaviorSnapshots(t, []behaviorCase{
		{
			name:     "yaml",
			fileType: general.YAML,
			env:      env,
			content: `port: ${PORT | to_int}
quoted_port: "${PORT | to_int}" # to_int wins over quotes
ratio: ${RATIO | to_float}
whole: ${WHOLE | to_float}
enabled: ${ENABLED | to_bool}
zip: ${ZIP} # strings stay strings
url: http://localhost:${PORT | to_int}/api
${PORT | to_int}: keys are always text
list: ["${PORT | to_int}", "${ENABLED | to_bool}"]
`,
		},
		{
			name:     "json",
			fileType: general.JSON,
			env:      env,
			content: `{
  "port": "${PORT | to_int}",
  "ratio": "${RATIO | to_float}",
  "whole": "${WHOLE | to_float}",
  "enabled": "${ENABLED | to_bool}",
  "zip": "${ZIP}",
  "url": "http://localhost:${PORT | to_int}/api",
  "${PORT | to_int}": "keys are always text"
}`,
		},
		{
			name:     "xml",
			fileType: general.XML,
			env:      env,
			content:  `<server port="${PORT | to_int}"><ratio>${RATIO | to_float}</ratio><enabled>${ENABLED | to_bool}</enabled></server>`,
		},
		{
			name:     "properties",
			fileType: general.PROPERTIES,
			env:      env,
			content:  "port=${PORT | to_int}\nwhole=${WHOLE | to_float}\nenabled=${ENABLED | to_bool}\n",
		},
		{
			name:     "error for an invalid number",
			fileType: general.YAML,
			env:      map[string]string{"PORT": "eighty"},
			content:  "port: ${PORT | to_int}\n",
		},
	})
}

// templatesConfig defines the templates used by the template snapshots
const templatesConfig = `templates:
  filters:
    clean:
      description: Trims and lowers a value
      filters: trim | lower
    secret:
      description: Trims a secret and encodes it to base64
      filters: trim | base64
  placeholders:
    db_host:
      env: DB_HOST
      default: localhost
    db_port:
      env: DB_PORT
      default: "5432"
    db_name:
      env: DB_NAME
      default: app
      filters: "@clean"
    db_url:
      description: JDBC URL of the database
      value: "jdbc:postgresql://${@db_host}:${@db_port}/${@db_name}"
    db_url_with_credentials:
      value: "${@db_url}?user=${DB_USER | url_encode}&password=${DB_PASSWORD | url_encode}"
`

// TestTemplateSnapshots documents how templates are expanded and validated
func TestTemplateSnapshots(t *testing.T) {
	runBehaviorSnapshots(t, []behaviorCase{
		{
			name:         "defaults",
			fileType:     general.YAML,
			gonfigConfig: templatesConfig,
			content: `datasource:
  host: ${@db_host}
  url: ${@db_url}
  url_ssl: ${@db_url}?ssl=true
  port: ${@db_port | to_int}
`,
		},
		{
			name:         "environment variables",
			fileType:     general.YAML,
			gonfigConfig: templatesConfig,
			env:          map[string]string{"DB_HOST": "db.prod", "DB_NAME": " Orders ", "DB_USER": "app", "DB_PASSWORD": "p@ss/w rd&"},
			content: `datasource:
  url: ${@db_url_with_credentials}
  port: ${@db_port | to_int}
`,
		},
		{
			name:         "filter templates",
			fileType:     general.YAML,
			gonfigConfig: templatesConfig,
			env:          map[string]string{"PASSWORD": " s3cret ", "NAME": " Gonfig "},
			content: `password: ${PASSWORD | @secret}
name: ${NAME | @clean}
chained: ${NAME | @clean | upper | @secret}
`,
		},
		{
			name:         "json",
			fileType:     general.JSON,
			gonfigConfig: templatesConfig,
			env:          map[string]string{"DB_PORT": "6543"},
			content:      `{"url": "${@db_url}", "port": "${@db_port | to_int}"}`,
		},
		{
			name:         "error for an unknown template in the file",
			fileType:     general.YAML,
			gonfigConfig: templatesConfig,
			content:      "url: ${@db_uri}\n",
		},
		{
			name:     "error for invalid templates",
			fileType: general.YAML,
			gonfigConfig: `templates:
  filters:
    secret:
      filters: trim | bas64
    loop_a:
      filters: "@loop_b"
    loop_b:
      filters: "@loop_a"
  placeholders:
    db_host:
      env: DB_HOST
      value: localhost
    db_url:
      value: "jdbc:postgresql://${@db_hots}/app"
`,
			content: "url: ${@db_url}\n",
		},
	})
}
