package cmd

import (
	"bytes"
	"fmt"
	"os"
	"path"
	"slices"
	"strings"
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/assert"
)

// checkSnapshotCase is a case of TestCheckSnapshots
type checkSnapshotCase struct {
	name string
	// fileName is the name of the checked file, which determines its type
	fileName string
	content  string
	// gonfigConfig is the content of .gonfig.yaml, e.g. to define templates
	gonfigConfig string
	env          map[string]string
	args         []string
}

// TestCheckSnapshots documents the reports of config check. Each snapshot shows the .gonfig.yaml, the environment
// variables, the checked file and the output and error of the command, so changes in behavior are easy to review.
// Note that go-snaps separates snapshots by "---" and therefore stores YAML document separators as "/-/-/-/".
func TestCheckSnapshots(t *testing.T) {
	testCases := []checkSnapshotCase{
		{
			name:     "file without problems",
			fileName: "config.yaml",
			content: `database:
  host: ${SNAP_CHECK_HOST}
  port: ${SNAP_CHECK_PORT | to_int}
  name: static
`,
			env: map[string]string{"SNAP_CHECK_HOST": "db.internal", "SNAP_CHECK_PORT": "5432"},
		},
		{
			name:     "missing, empty and default variables with an error",
			fileName: "config.yaml",
			content: `database:
  host: ${SNAP_CHECK_HOST | default(localhost)}
  user: ${SNAP_CHECK_USER}
  password: ${SNAP_CHECK_PASWORD}
  port: ${SNAP_CHECK_PORT | to_int}
  schema: ${SNAP_CHECK_SCHEMA}
  url: postgres://${SNAP_CHECK_USER}@${SNAP_CHECK_HOST | default(localhost)}/${SNAP_CHECK_DB}
`,
			env: map[string]string{"SNAP_CHECK_USER": "app", "SNAP_CHECK_PORT": "eighty", "SNAP_CHECK_SCHEMA": ""},
		},
		{
			name:     "json output",
			fileName: "config.yaml",
			content: `password: ${SNAP_CHECK_PASWORD}
port: ${SNAP_CHECK_PORT | to_int}
host: ${SNAP_CHECK_HOST | default(localhost)}
`,
			env:  map[string]string{"SNAP_CHECK_PORT": "eighty"},
			args: []string{"-o", "json"},
		},
		{
			name:     "templates",
			fileName: "config.yaml",
			gonfigConfig: `templates:
  filters:
    secret:
      filters: trim | base64
  placeholders:
    db_host:
      env: SNAP_CHECK_HOST
      default: localhost
    db_url:
      value: "jdbc:postgresql://${@db_host}:${SNAP_CHECK_PORT}/app"
`,
			content: `datasource:
  url: ${@db_url}
  password: ${SNAP_CHECK_PASSWORD | @secret}
`,
			env: map[string]string{"SNAP_CHECK_PASSWORD": " s3cret "},
		},
		{
			name:     "fragments",
			fileName: "config.yaml",
			content: `logging: ${SNAP_CHECK_LOGGING | fragment}
database:
  host: localhost
  ${SNAP_CHECK_OVERRIDES | fragment}:
`,
			env: map[string]string{
				"SNAP_CHECK_LOGGING":   "level: ${SNAP_CHECK_LEVEL}\nappenders: ${SNAP_CHECK_APPENDERS | fragment}",
				"SNAP_CHECK_OVERRIDES": "port: [1",
			},
		},
		{
			name:     "fragment replacing an anchored value",
			fileName: "config.yaml",
			content:  "base: &base ${SNAP_CHECK_BASE | fragment}\nservice:\n  <<: *base\n  name: app\n",
			env:      map[string]string{"SNAP_CHECK_BASE": "pool: &pool 10\nmax: *pool"},
			args:     []string{"--diff"},
		},
		{
			name:     "scalar fragment used as a merge key",
			fileName: "config.yaml",
			content:  "base: &base ${SNAP_CHECK_BASE | fragment}\nservice:\n  <<: *base\n",
			env:      map[string]string{"SNAP_CHECK_BASE": "made-up"},
			args:     []string{"--diff"},
		},
		{
			name:     "scalar fragment used as a merge key without diff",
			fileName: "config.yaml",
			content:  "base: &base ${SNAP_CHECK_BASE | fragment}\nservice:\n  <<: *base\n",
			env:      map[string]string{"SNAP_CHECK_BASE": "made-up"},
		},
		{
			name:     "toml keys masked alike",
			fileName: "config.toml",
			content:  "\"${SNAP_CHECK_K1}\" = 1\n\"${SNAP_CHECK_K2}\" = 2\n",
			env:      map[string]string{"SNAP_CHECK_K1": "made-up-a", "SNAP_CHECK_K2": "made-up-b"},
		},
		{
			name:     "toml keys masked alike with diff",
			fileName: "config.toml",
			content:  "\"${SNAP_CHECK_K1}\" = 1\n\"${SNAP_CHECK_K2}\" = 2\n",
			env:      map[string]string{"SNAP_CHECK_K1": "made-up-a", "SNAP_CHECK_K2": "made-up-b"},
			args:     []string{"--diff"},
		},
		{
			name:     "missing fragment used by a merge key",
			fileName: "config.yaml",
			content:  "base: &b ${SNAP_CHECK_MISSING | fragment}\nother: ${SNAP_CHECK_OK | fragment}\nx: {<<: *b}\n",
			env:      map[string]string{"SNAP_CHECK_OK": "{k: v}"},
			args:     []string{"--fail-on-missing"},
		},
		{
			name:     "missing fragment used by a merge key with diff",
			fileName: "config.yaml",
			content:  "base: &b ${SNAP_CHECK_MISSING | fragment}\nother: ${SNAP_CHECK_OK | fragment}\nx: {<<: *b}\n",
			env:      map[string]string{"SNAP_CHECK_OK": "{k: v}"},
			args:     []string{"--fail-on-missing", "--diff"},
		},
		{
			name:     "diff without the comments of fragments",
			fileName: "config.yaml",
			content:  "a: &k ${SNAP_CHECK_FR | fragment} # file-comment\nb: *k\n",
			env:      map[string]string{"SNAP_CHECK_FR": "[1, 2] # made-up-fragment-comment"},
			args:     []string{"--diff"},
		},
		{
			name:     "fail on missing",
			fileName: "config.yaml",
			content: `password: ${SNAP_CHECK_PASWORD}
host: ${SNAP_CHECK_HOST | default(localhost)}
`,
			args: []string{"--fail-on-missing"},
		},
		{
			name:     "xml file",
			fileName: "server.xml",
			content: `<server port="${SNAP_CHECK_PORT | to_int}">
  ${SNAP_CHECK_NAME}
  <datasource url="${SNAP_CHECK_URL}">
    <user>${SNAP_CHECK_USER}</user>
    <password><![CDATA[${SNAP_CHECK_PASSWORD}]]></password>
  </datasource>
</server>
`,
			env: map[string]string{"SNAP_CHECK_PORT": "8080", "SNAP_CHECK_USER": "app"},
		},
		{
			name:     "plain text file",
			fileName: "entrypoint.sh",
			content: `#!/bin/sh
export DB_URL="postgres://${SNAP_CHECK_HOST}:5432/app"
exec app --user '${SNAP_CHECK_USER | required}'
`,
			env:  map[string]string{"SNAP_CHECK_HOST": "db"},
			args: []string{"-t", "plain"},
		},
		{
			name:     "file without placeholders",
			fileName: "config.yaml",
			content:  "database:\n  host: localhost\n",
		},
		{
			name:     "diff with masked values",
			fileName: "config.yaml",
			content: `database:
  host: ${SNAP_CHECK_HOST}
  port: ${SNAP_CHECK_PORT | to_int}
  url: postgres://${SNAP_CHECK_USER}@${SNAP_CHECK_HOST}/app
  password: ${SNAP_CHECK_PASWORD}
  name: static
`,
			env:  map[string]string{"SNAP_CHECK_HOST": "db.internal", "SNAP_CHECK_PORT": "eighty", "SNAP_CHECK_USER": "app"},
			args: []string{"--diff"},
		},
		{
			name:     "diff of fragments",
			fileName: "config.yaml",
			content: `logging:
  level: info
  appenders: ${SNAP_CHECK_APPENDERS | fragment}
database:
  host: localhost
  pool:
    max: 5
  ${SNAP_CHECK_OVERRIDES | fragment}:
`,
			env: map[string]string{
				"SNAP_CHECK_APPENDERS": "- name: console\n  level: ${SNAP_CHECK_LEVEL | default(debug)}\n- name: file\n  path: /var/log/app.log",
				"SNAP_CHECK_OVERRIDES": "host: db.internal\npool:\n  max: 20\n  idle: 2\nssl: true",
			},
			args: []string{"--diff"},
		},
		{
			name:     "diff of fragments in a json file",
			fileName: "config.json",
			content: `{
  "pool": "${SNAP_CHECK_POOL | fragment}",
  "${SNAP_CHECK_OVERRIDES | fragment}": null,
  "hosts": "${SNAP_CHECK_HOSTS | split | fragment}"
}
`,
			env: map[string]string{
				"SNAP_CHECK_POOL":      `{"max": 20, "ssl": true, "name": null}`,
				"SNAP_CHECK_OVERRIDES": `{"timeout": 30}`,
				"SNAP_CHECK_HOSTS":     "a,b",
			},
			args: []string{"--diff"},
		},
		{
			name:     "diff of an xml file",
			fileName: "server.xml",
			content: `<server port="${SNAP_CHECK_PORT}">
  <resources>${SNAP_CHECK_RESOURCES | fragment}</resources>
</server>
`,
			env: map[string]string{
				"SNAP_CHECK_PORT":      "8080",
				"SNAP_CHECK_RESOURCES": `<datasource name="main"><user>sa</user></datasource>`,
			},
			args: []string{"--diff"},
		},
		{
			name:     "diff with log-values",
			fileName: "config.yaml",
			content: `host: ${SNAP_CHECK_HOST}
port: ${SNAP_CHECK_PORT | to_int}
appenders: ${SNAP_CHECK_APPENDERS | fragment}
`,
			env:  map[string]string{"SNAP_CHECK_HOST": "db.internal", "SNAP_CHECK_PORT": "5432", "SNAP_CHECK_APPENDERS": "- name: console"},
			args: []string{"--diff", "--log-values"},
		},
		{
			name:     "diff in json output",
			fileName: "config.yaml",
			content: `host: ${SNAP_CHECK_HOST}
port: 5432
`,
			env:  map[string]string{"SNAP_CHECK_HOST": "db.internal"},
			args: []string{"--diff", "-o", "json"},
		},
		{
			name:     "diff without changes",
			fileName: "config.yaml",
			content:  "database:\n  host: localhost\n",
			args:     []string{"--diff", "-o", "json"},
		},
	}

	for _, tC := range testCases {
		t.Run(tC.name, func(t *testing.T) {
			for name, value := range tC.env {
				t.Setenv(name, value)
			}

			args := []string{"config", "check", "-f", tC.fileName}
			if tC.gonfigConfig != "" {
				configDir := t.TempDir()
				assert.NoError(t, os.WriteFile(path.Join(configDir, ".gonfig.yaml"), []byte(tC.gonfigConfig), 0o644))
				args = append([]string{"--config-path", configDir}, args...)
			}

			// The file is checked by a relative name, so the report does not contain a temporary path
			t.Chdir(t.TempDir())
			assert.NoError(t, os.WriteFile(tC.fileName, []byte(tC.content), 0o644))

			var out bytes.Buffer
			rootCmd.SetOut(&out)
			t.Cleanup(func() { rootCmd.SetOut(nil) })

			resetFlags(t)
			rootCmd.SetArgs(append(args, tC.args...))
			err := rootCmd.Execute()

			snaps.MatchSnapshot(t, renderCheckSnapshot(tC, args, out.String(), err))
		})
	}
}

// renderCheckSnapshot renders the case and the result of config check as a readable snapshot
func renderCheckSnapshot(tC checkSnapshotCase, args []string, output string, err error) string {
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

	// The temporary config path is replaced, since it differs on every run
	command := strings.Join(append(args, tC.args...), " ")
	if tC.gonfigConfig != "" {
		command = strings.Replace(command, args[1], "<dir of .gonfig.yaml>", 1)
	}

	fmt.Fprintf(&sb, "\n%s:\n%s\n\n$ gonfig %s\n%s", tC.fileName, strings.TrimRight(tC.content, "\n"), command, output)
	if err != nil {
		fmt.Fprintf(&sb, "Error: %s\n", err)
	} else {
		sb.WriteString("(exit code 0)\n")
	}
	return sb.String()
}

// TestCheckMasksFragmentComments checks that check shows no comment of a fragment, since the text of fragments is
// masked, while the comments of the file are kept, with and without a diff, and that --log-values shows both
func TestCheckMasksFragmentComments(t *testing.T) {
	const secret = "made-up-fragment-comment"
	testCases := []struct {
		name     string
		fileName string
		content  string
		fragment string
		// keptInDiff are the file comments the diff shows
		keptInDiff string
	}{
		{
			name:       "yaml value",
			fileName:   "config.yaml",
			content:    "# file-head\na: ${FRAG | fragment} # file-line\nb: 1\n",
			fragment:   "# " + secret + "\n[1, 2] # " + secret,
			keptInDiff: "file-line",
		},
		{
			name:       "yaml key",
			fileName:   "config.yaml",
			content:    "t:\n  ${FRAG | fragment}: # file-line\n  z: 1\n",
			fragment:   "# " + secret + "\nx: 1 # " + secret + "\ny: '#" + secret + "-in-string'",
			keptInDiff: "file-line",
		},
		{
			name:       "toml value",
			fileName:   "config.toml",
			content:    "# file-head\na = \"${FRAG | fragment}\" # file-line\nb = 1\n",
			fragment:   "[\n  1, # " + secret + "\n  \"#" + secret + "-in-string\", #" + secret + "\n] # " + secret,
			keptInDiff: "file-line",
		},
		{
			name:       "toml key",
			fileName:   "config.toml",
			content:    "[t]\n\"${FRAG | fragment}\" = \"\" # file-line\nz = 1\n",
			fragment:   "# " + secret + "\nx = 1 # " + secret + "\ny = '#" + secret + "-in-string'#" + secret + "\r\n",
			keptInDiff: "file-line",
		},
		{
			name:       "xml",
			fileName:   "config.xml",
			content:    "<a>\n  <!-- file-line -->\n  <b>${FRAG | fragment}</b>\n</a>\n",
			fragment:   "<!-- " + secret + " --><c>1</c><!-- " + secret + " -->",
			keptInDiff: "file-line",
		},
		{
			name:       "xml processing instruction and directive",
			fileName:   "config.xml",
			content:    "<a>\n  <!-- file-line -->\n  <b>${FRAG | fragment}</b>\n</a>\n",
			fragment:   "<c><?pi " + secret + "?>1</c><!DOCTYPE " + secret + ">",
			keptInDiff: "file-line",
		},
	}
	for _, tC := range testCases {
		for _, args := range [][]string{{}, {"--diff"}, {"--diff", "--log-values"}} {
			t.Run(tC.name+" "+strings.Join(args, " "), func(t *testing.T) {
				t.Setenv("FRAG", tC.fragment)
				t.Chdir(t.TempDir())
				assert.NoError(t, os.WriteFile(tC.fileName, []byte(tC.content), 0o644))

				var out bytes.Buffer
				rootCmd.SetOut(&out)
				t.Cleanup(func() { rootCmd.SetOut(nil) })
				resetFlags(t)
				rootCmd.SetArgs(append([]string{"config", "check", "-f", tC.fileName}, args...))
				assert.NoError(t, rootCmd.Execute())

				if slices.Contains(args, "--log-values") {
					assert.Contains(t, out.String(), secret)
					return
				}
				assert.NotContains(t, out.String(), secret)
				if slices.Contains(args, "--diff") {
					assert.Contains(t, out.String(), tC.keptInDiff)
					assert.Contains(t, out.String(), "***")
				}
			})
		}
	}
}

// TestCheckAgreesWithProcessOnTomlKeyFragmentConflicts checks that a key of a fragment that is also defined by a table
// header fails process and check alike, with and without --diff and --schema, although check masks the keys produced
// by placeholders, and that keys masked alike don't fail either of them
func TestCheckAgreesWithProcessOnTomlKeyFragmentConflicts(t *testing.T) {
	const content = "[t]\na = \"str\"\n\"${FRAG | fragment}\" = \"\"\n[t.sub]\nz = 1\n\"${KEY_1}\" = 1\n\"${KEY_2}\" = 2\n"
	t.Setenv("KEY_1", "made-up-a")
	t.Setenv("KEY_2", "made-up-b")
	for fragment, wantErr := range map[string]bool{"sub = 1": true, "sub.z = 2": true, "a = 1\nsub.y = 3": true, "ok = 1": false} {
		for _, args := range [][]string{{"process"}, {"check"}, {"check", "--diff"}, {"check", "--schema", "schema.json"}} {
			t.Run(fragment+" "+strings.Join(args, " "), func(t *testing.T) {
				t.Setenv("FRAG", fragment)
				t.Chdir(t.TempDir())
				assert.NoError(t, os.WriteFile("config.toml", []byte(content), 0o644))
				assert.NoError(t, os.WriteFile("schema.json", []byte("{}"), 0o644))

				var out bytes.Buffer
				rootCmd.SetOut(&out)
				t.Cleanup(func() { rootCmd.SetOut(nil) })
				resetFlags(t)
				rootCmd.SetArgs(append([]string{"config", args[0], "-f", "config.toml"}, args[1:]...))
				err := rootCmd.Execute()
				assert.Equal(t, wantErr, err != nil, "%v: %s", err, out.String())
				assert.NotContains(t, out.String(), "gonfig-masked-key")
			})
		}
	}
}
