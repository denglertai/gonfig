package cmd

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path"
	"runtime"
	"strings"
	"testing"

	"github.com/denglertai/gonfig/internal/filter"
	"github.com/denglertai/gonfig/internal/general"
	"github.com/denglertai/gonfig/internal/logging"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/assert"
)

// TestStdout processes the test data of each file type to stdout and compares the output with the snapshots
func TestStdout(t *testing.T) {
	wd, err := os.Getwd()
	assert.NoError(t, err)

	testCases := []struct {
		desc     string
		file     string
		fileType general.FileType
		wantErr  bool
	}{
		{
			desc:     "XML AutoDiscover",
			file:     path.Join(wd, "./testdata/xml/customers_param.xml"),
			fileType: general.Undefined,
		},
		{
			desc:     "XML Explicit",
			file:     path.Join(wd, "./testdata/xml/customers_param.xml"),
			fileType: general.XML,
		},
		{
			desc:     "JSON AutoDiscover",
			file:     path.Join(wd, "./testdata/json/quiz_param.json"),
			fileType: general.Undefined,
		},
		{
			desc:     "JSON Explicit",
			file:     path.Join(wd, "./testdata/json/quiz_param.json"),
			fileType: general.JSON,
		},
		{
			desc:     "YAML AutoDiscover",
			file:     path.Join(wd, "./testdata/yaml/deployment_param.yaml"),
			fileType: general.Undefined,
		},
		{
			desc:     "YAML Explicit",
			file:     path.Join(wd, "./testdata/yaml/deployment_param.yaml"),
			fileType: general.YAML,
		},
		{
			desc:     "PROPERTIES AutoDiscover",
			file:     path.Join(wd, "./testdata/properties/props_param.properties"),
			fileType: general.Undefined,
		},
		{
			desc:     "PROPERTIES Explicit",
			file:     path.Join(wd, "./testdata/properties/props_param.properties"),
			fileType: general.PROPERTIES,
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			var out bytes.Buffer
			rootCmd.SetOut(&out)
			t.Cleanup(func() { rootCmd.SetOut(nil) })

			// BLA_BLUB is expected to be present in the environment variables and the value should be YOYOYO after the command is executed
			t.Setenv("BLA_BLUB", "yoyoyo")
			t.Setenv("INT", "123")
			t.Setenv("FLOAT", "123.123")
			t.Setenv("BOOL", "true")
			t.Setenv("STRING", "string")
			t.Setenv("SPECIAL_CHARACTERS", "%^&*()_+")

			// Take the path of the file and pass it as an argument to the command and see if it works
			args := []string{"config", "process", "-f", tC.file, "-o", "-", "-l", "trace", "-s"}

			if tC.fileType != general.Undefined {
				args = append(args, "-t", string(tC.fileType))
			}

			resetFlags(t)
			rootCmd.SetArgs(args)
			assert.NoError(t, rootCmd.Execute())

			snaps.MatchSnapshot(t, out.String())
		})
	}
}

// TestFile processes the test data of each file type into a separate output file with -o and -w and compares it with the snapshots
func TestFile(t *testing.T) {
	wd, err := os.Getwd()
	assert.NoError(t, err)

	testCases := []struct {
		desc     string
		file     string
		fileType general.FileType
		wantErr  bool
	}{
		{
			desc:     "XML AutoDiscover",
			file:     path.Join(wd, "./testdata/xml/customers_param.xml"),
			fileType: general.Undefined,
		},
		{
			desc:     "XML Explicit",
			file:     path.Join(wd, "./testdata/xml/customers_param.xml"),
			fileType: general.XML,
		},
		{
			desc:     "JSON AutoDiscover",
			file:     path.Join(wd, "./testdata/json/quiz_param.json"),
			fileType: general.Undefined,
		},
		{
			desc:     "JSON Explicit",
			file:     path.Join(wd, "./testdata/json/quiz_param.json"),
			fileType: general.JSON,
		},
		{
			desc:     "YAML AutoDiscover",
			file:     path.Join(wd, "./testdata/yaml/deployment_param.yaml"),
			fileType: general.Undefined,
		},
		{
			desc:     "YAML Explicit",
			file:     path.Join(wd, "./testdata/yaml/deployment_param.yaml"),
			fileType: general.YAML,
		},
		{
			desc:     "YML AutoDiscover",
			file:     path.Join(wd, "./testdata/yaml/deployment_param.yml"),
			fileType: general.Undefined,
		},
		{
			desc:     "YML Explicit",
			file:     path.Join(wd, "./testdata/yaml/deployment_param.yml"),
			fileType: general.YML,
		},
		{
			desc:     "PROPERTIES AutoDiscover",
			file:     path.Join(wd, "./testdata/properties/props_param.properties"),
			fileType: general.Undefined,
		},
		{
			desc:     "PROPERTIES Explicit",
			file:     path.Join(wd, "./testdata/properties/props_param.properties"),
			fileType: general.PROPERTIES,
		},
		{
			desc:     "PLAIN Shell Script",
			file:     path.Join(wd, "./testdata/plain/test.sh"),
			fileType: general.PLAIN,
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			// BLA_BLUB is expected to be present in the environment variables and the value should be YOYOYO after the command is executed
			t.Setenv("BLA_BLUB", "yoyoyo")
			t.Setenv("INT", "123")
			t.Setenv("FLOAT", "123.123")
			t.Setenv("BOOL", "true")
			t.Setenv("STRING", "string")
			t.Setenv("SPECIAL_CHARACTERS", "%^&*()_+")

			fileName := path.Join(t.TempDir(), "output")

			// Take the path of the file and pass it as an argument to the command and see if it works
			args := []string{"config", "process", "-f", tC.file, "-o", fileName, "-w", "-l", "trace", "-s"}

			if tC.fileType != general.Undefined {
				args = append(args, "-t", string(tC.fileType))
			}

			resetFlags(t)
			rootCmd.SetArgs(args)
			assert.NoError(t, rootCmd.Execute())

			// Read the file and compare it with the snapshot
			snaps.MatchSnapshot(t, readFile(t, fileName))
		})
	}
}

// TestPassingExtension tests the case where the user passes a file with a different extension than the actual file type
func TestPassingExtension(t *testing.T) {
	wd, err := os.Getwd()
	assert.NoError(t, err)

	testCases := []struct {
		desc     string
		file     string
		fileType general.FileType
		wantErr  bool
	}{
		{
			desc:     "XML Explicit",
			file:     path.Join(wd, "./testdata/xml/customers_param.xml"),
			fileType: general.XML,
		},
		{
			desc:     "JSON Explicit",
			file:     path.Join(wd, "./testdata/json/quiz_param.json"),
			fileType: general.JSON,
		},
		{
			desc:     "YAML Explicit",
			file:     path.Join(wd, "./testdata/yaml/deployment_param.yaml"),
			fileType: general.YAML,
		},
		{
			desc:     "PROPERTIES Explicit",
			file:     path.Join(wd, "./testdata/properties/props_param.properties"),
			fileType: general.PROPERTIES,
		},
	}

	t.Setenv("BLA_BLUB", "yoyoyo")
	t.Setenv("INT", "123")
	t.Setenv("FLOAT", "123.123")
	t.Setenv("BOOL", "true")
	t.Setenv("STRING", "string")
	t.Setenv("SPECIAL_CHARACTERS", "%^&*()_+")

	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			// The copy has no extension, so the file type has to be passed with -t
			fileName := copyToTempFile(t, tC.file, false)

			// Take the path of the file and pass it as an argument to the command and see if it works
			args := []string{"config", "process", "-f", fileName, "-i", "-l", "trace", "-s", "-t", string(tC.fileType)}

			resetFlags(t)
			rootCmd.SetArgs(args)
			assert.NoError(t, rootCmd.Execute())

			// Read the file and compare it with the snapshot
			snaps.MatchSnapshot(t, readFile(t, fileName))
		})
	}
}

// TestFileOverwrite processes a copy of the test data of each file type in place with -i and compares it with the snapshots
func TestFileOverwrite(t *testing.T) {
	wd, err := os.Getwd()
	assert.NoError(t, err)

	testCases := []struct {
		desc     string
		file     string
		fileType general.FileType
		wantErr  bool
	}{
		{
			desc:     "XML AutoDiscover",
			file:     path.Join(wd, "./testdata/xml/customers_param.xml"),
			fileType: general.Undefined,
		},
		{
			desc:     "XML Explicit",
			file:     path.Join(wd, "./testdata/xml/customers_param.xml"),
			fileType: general.XML,
		},
		{
			desc:     "JSON AutoDiscover",
			file:     path.Join(wd, "./testdata/json/quiz_param.json"),
			fileType: general.Undefined,
		},
		{
			desc:     "JSON Explicit",
			file:     path.Join(wd, "./testdata/json/quiz_param.json"),
			fileType: general.JSON,
		},
		{
			desc:     "YAML AutoDiscover",
			file:     path.Join(wd, "./testdata/yaml/deployment_param.yaml"),
			fileType: general.Undefined,
		},
		{
			desc:     "YAML Explicit",
			file:     path.Join(wd, "./testdata/yaml/deployment_param.yaml"),
			fileType: general.YAML,
		},
		{
			desc:     "PROPERTIES AutoDiscover",
			file:     path.Join(wd, "./testdata/properties/props_param.properties"),
			fileType: general.Undefined,
		},
		{
			desc:     "PROPERTIES Explicit",
			file:     path.Join(wd, "./testdata/properties/props_param.properties"),
			fileType: general.PROPERTIES,
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			// BLA_BLUB is expected to be present in the environment variables and the value should be YOYOYO after the command is executed
			t.Setenv("BLA_BLUB", "yoyoyo")
			t.Setenv("INT", "123")
			t.Setenv("FLOAT", "123.123")
			t.Setenv("BOOL", "true")
			t.Setenv("STRING", "string")
			t.Setenv("SPECIAL_CHARACTERS", "%^&*()_+")

			fileName := copyToTempFile(t, tC.file, true)

			// Take the path of the file and pass it as an argument to the command and see if it works
			args := []string{"config", "process", "-f", fileName, "-i", "-l", "trace", "-s"}

			if tC.fileType != general.Undefined {
				args = append(args, "-t", string(tC.fileType))
			}

			resetFlags(t)
			rootCmd.SetArgs(args)
			assert.NoError(t, rootCmd.Execute())

			// Read the file and compare it with the snapshot
			snaps.MatchSnapshot(t, readFile(t, fileName))
		})
	}
}

// TestYamlMove processes a copy of the YAML test data with placeholders in keys in place with -i and compares it with the snapshots
func TestYamlMove(t *testing.T) {
	wd, err := os.Getwd()
	assert.NoError(t, err)

	testCases := []struct {
		desc     string
		file     string
		fileType general.FileType
		wantErr  bool
	}{
		{
			desc:     "YAML AutoDiscover",
			file:     path.Join(wd, "./testdata/yaml/key.yaml"),
			fileType: general.Undefined,
		},
		{
			desc:     "YAML Explicit",
			file:     path.Join(wd, "./testdata/yaml/key.yaml"),
			fileType: general.YAML,
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			t.Setenv("PASSWORD", "adjkgadjgjagdjdag")
			t.Setenv("USERNAME", "Random User with spaces")

			fileName := copyToTempFile(t, tC.file, true)

			// Take the path of the file and pass it as an argument to the command and see if it works
			args := []string{"config", "process", "-f", fileName, "-i", "-l", "trace", "-s"}

			if tC.fileType != general.Undefined {
				args = append(args, "-t", string(tC.fileType))
			}

			resetFlags(t)
			rootCmd.SetArgs(args)
			assert.NoError(t, rootCmd.Execute())

			// Read the file and compare it with the snapshot
			snaps.MatchSnapshot(t, readFile(t, fileName))
		})
	}
}

func TestUnknownFilterKeepsFileUnchanged(t *testing.T) {
	t.Setenv("PASSWORD", "s3cret")

	content := "password: ${PASSWORD | not_a_filter}\n"
	fileName := path.Join(t.TempDir(), "config.yaml")
	assert.NoError(t, os.WriteFile(fileName, []byte(content), 0o644))

	resetFlags(t)
	rootCmd.SetArgs([]string{"config", "process", "-f", fileName, "-i"})
	err := rootCmd.Execute()

	assert.ErrorContains(t, err, `unknown filter "not_a_filter"`)

	// The secret must not be written in place of the placeholder
	result, err := os.ReadFile(fileName)
	assert.NoError(t, err)
	assert.Equal(t, content, string(result))
}

func TestUnknownFilterAllowed(t *testing.T) {
	t.Setenv("VALUE", "value")

	testCases := []struct {
		desc string
		args []string
		env  string
	}{
		{desc: "flag", args: []string{"--allow-unknown-filters"}},
		{desc: "env var", env: "true"},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			if tC.env != "" {
				t.Setenv("GONFIG_ALLOW_UNKNOWN_FILTERS", tC.env)
			}

			fileName := path.Join(t.TempDir(), "config.yaml")
			assert.NoError(t, os.WriteFile(fileName, []byte("key: ${VALUE | not_a_filter}\n"), 0o644))

			resetFlags(t)
			rootCmd.SetArgs(append([]string{"config", "process", "-f", fileName, "-i"}, tC.args...))
			assert.NoError(t, rootCmd.Execute())

			result, err := os.ReadFile(fileName)
			assert.NoError(t, err)
			assert.Equal(t, "key: value\n", string(result))
		})
	}
}

// templatesConfig is a .gonfig.yaml defining the templates of the documentation's JDBC example
const templatesConfig = `templates:
  filters:
    secret:
      filters: trim | base64
  placeholders:
    db_host:
      env: TPL_DB_HOST
      default: localhost
    db_port:
      env: TPL_DB_PORT
      default: "5432"
    db_url:
      value: "jdbc:postgresql://${@db_host}:${@db_port}/app"
`

func TestTemplates(t *testing.T) {
	t.Setenv("TPL_DB_HOST", "")
	t.Setenv("TPL_DB_PORT", "6543")
	t.Setenv("TPL_DB_PASSWORD", " s3cret ")

	configDir := t.TempDir()
	assert.NoError(t, os.WriteFile(path.Join(configDir, ".gonfig.yaml"), []byte(templatesConfig), 0o644))

	fileName := path.Join(t.TempDir(), "config.yaml")
	content := "url: ${@db_url}?ssl=true\nport: ${@db_port | to_int}\npassword: ${TPL_DB_PASSWORD | @secret}\n"
	assert.NoError(t, os.WriteFile(fileName, []byte(content), 0o644))

	resetFlags(t)
	rootCmd.SetArgs([]string{"--config-path", configDir, "config", "process", "-f", fileName, "-i"})
	assert.NoError(t, rootCmd.Execute())

	result, err := os.ReadFile(fileName)
	assert.NoError(t, err)
	assert.Equal(t, "url: jdbc:postgresql://localhost:6543/app?ssl=true\nport: 6543\npassword: czNjcmV0\n", string(result))
}

func TestInvalidTemplatesKeepFileUnchanged(t *testing.T) {
	configDir := t.TempDir()
	assert.NoError(t, os.WriteFile(path.Join(configDir, ".gonfig.yaml"), []byte("templates:\n  filters:\n    secret:\n      filters: trim | bas64\n"), 0o644))

	content := "key: value\n"
	fileName := path.Join(t.TempDir(), "config.yaml")
	assert.NoError(t, os.WriteFile(fileName, []byte(content), 0o644))

	resetFlags(t)
	rootCmd.SetArgs([]string{"--config-path", configDir, "config", "process", "-f", fileName, "-i"})
	err := rootCmd.Execute()

	assert.ErrorContains(t, err, `filter template "secret": unknown filter "bas64"`)
	result, readErr := os.ReadFile(fileName)
	assert.NoError(t, readErr)
	assert.Equal(t, content, string(result))
}

// TestInvalidConfigFile checks that invalid config files fail before anything is processed
func TestInvalidConfigFile(t *testing.T) {
	testCases := []struct {
		desc    string
		content string
		wantErr string
	}{
		{desc: "invalid YAML", content: "templates:\n  filters:\n    a:\n      filters: @b\n", wantErr: "failed to read config file"},
		{desc: "invalid templates", content: "templates:\n  filters: not a map\n", wantErr: "invalid templates in config file"},
		{desc: "unknown template key", content: "templates:\n  filter: {}\n", wantErr: `unknown key "templates.filter"`},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			configDir := t.TempDir()
			assert.NoError(t, os.WriteFile(path.Join(configDir, ".gonfig.yaml"), []byte(tC.content), 0o644))

			resetFlags(t)
			rootCmd.SetArgs([]string{"--config-path", configDir, "value", "x"})
			assert.ErrorContains(t, rootCmd.Execute(), tC.wantErr)
		})
	}
}

// TestUnknownConfigKeysWarn checks that unknown top level keys of the config file are logged as warnings and ignored
func TestUnknownConfigKeysWarn(t *testing.T) {
	configDir := t.TempDir()
	assert.NoError(t, os.WriteFile(path.Join(configDir, ".gonfig.yaml"), []byte("log-levl: debug\n"), 0o644))

	stderr := captureStderr(t, func() {
		resetFlags(t)
		rootCmd.SetArgs([]string{"--config-path", configDir, "value", "x"})
		assert.NoError(t, rootCmd.Execute())
	})
	assert.Contains(t, stderr, `"level":"WARN","msg":"Unknown key in the config file, ignoring it","key":"log-levl","file":"`+
		path.Join(configDir, ".gonfig.yaml")+`"`)
}

// TestFragmentFromTemplate checks that a placeholder template can read a fragment from a file referenced by its default
func TestFragmentFromTemplate(t *testing.T) {
	t.Setenv("FRAGMENT_LOGGING", "")
	t.Setenv("FRAGMENT_LEVEL", "debug")

	dir := t.TempDir()
	fragmentFile := path.Join(dir, "logging.yaml")
	assert.NoError(t, os.WriteFile(fragmentFile, []byte("level: ${FRAGMENT_LEVEL}\nappenders: [console]\n"), 0o644))

	// The template reads the fragment from a file by default
	configDir := t.TempDir()
	// Single quotes keep the backslashes of Windows paths, which start escape sequences within double quotes.
	// Within single quotes, apostrophes are doubled, e.g. for a user directory like C:\Users\O'Brien.
	config := "templates:\n  placeholders:\n    logging:\n      env: FRAGMENT_LOGGING\n      default: '@" + strings.ReplaceAll(fragmentFile, "'", "''") + "'\n"
	assert.NoError(t, os.WriteFile(path.Join(configDir, ".gonfig.yaml"), []byte(config), 0o644))

	fileName := path.Join(dir, "config.yaml")
	assert.NoError(t, os.WriteFile(fileName, []byte("logging: ${@logging | fragment}\n"), 0o644))

	resetFlags(t)
	rootCmd.SetArgs([]string{"--config-path", configDir, "config", "process", "-f", fileName, "-i"})
	assert.NoError(t, rootCmd.Execute())

	result, err := os.ReadFile(fileName)
	assert.NoError(t, err)
	assert.Equal(t, "logging:\n  level: debug\n  appenders:\n    - console\n", string(result))
}

// TestFailOnMissingKeepsFileUnchanged checks that --fail-on-missing and GONFIG_FAIL_ON_MISSING fail for missing variables without writing
func TestFailOnMissingKeepsFileUnchanged(t *testing.T) {
	t.Setenv("GONFIG_FAIL_ON_MISSING", "")
	t.Setenv("FAIL_ON_MISSING_PORT", "")
	t.Cleanup(func() { filter.SetFailOnMissing(false) })

	content := "password: ${FAIL_ON_MISSING_PASWORD}\nport: ${FAIL_ON_MISSING_PORT | default(5432)}\n"
	for _, tC := range []struct {
		desc string
		args []string
		env  string
	}{
		{desc: "flag", args: []string{"--fail-on-missing"}},
		{desc: "env var", env: "true"},
	} {
		t.Run(tC.desc, func(t *testing.T) {
			t.Setenv("GONFIG_FAIL_ON_MISSING", tC.env)

			fileName := path.Join(t.TempDir(), "config.yaml")
			assert.NoError(t, os.WriteFile(fileName, []byte(content), 0o644))

			resetFlags(t)
			rootCmd.SetArgs(append(tC.args, "config", "process", "-f", fileName, "-i"))
			assert.ErrorContains(t, rootCmd.Execute(), `environment variable "FAIL_ON_MISSING_PASWORD" is not set`)

			result, err := os.ReadFile(fileName)
			assert.NoError(t, err)
			assert.Equal(t, content, string(result))
		})
	}

	// Without the option, missing variables are empty
	fileName := path.Join(t.TempDir(), "config.yaml")
	assert.NoError(t, os.WriteFile(fileName, []byte(content), 0o644))
	resetFlags(t)
	rootCmd.SetArgs([]string{"config", "process", "-f", fileName, "-i"})
	assert.NoError(t, rootCmd.Execute())
	result, err := os.ReadFile(fileName)
	assert.NoError(t, err)
	assert.Equal(t, "password: \"\"\nport: \"5432\"\n", string(result))
}

// captureStderr returns everything written to stderr while run is executed, including logs
func captureStderr(t *testing.T, run func()) string {
	t.Helper()

	reader, writer, err := os.Pipe()
	assert.NoError(t, err)

	stderr := os.Stderr
	os.Stderr = writer
	t.Cleanup(func() {
		os.Stderr = stderr
		// Logging was initialized with the pipe, so initialize it with the real stderr again
		assert.NoError(t, logging.InitLogging("info", false))
	})

	run()

	assert.NoError(t, writer.Close())
	output, err := io.ReadAll(reader)
	assert.NoError(t, err)
	return string(output)
}

// TestSuccessfulRunsAreSilent checks that processing a file writes nothing to stderr at the default log level
func TestSuccessfulRunsAreSilent(t *testing.T) {
	// TestMain isolates the tests from the environment and config files of the machine, which could enable
	// options logging warnings like log-values
	t.Setenv("SILENT_TEST_VALUE", "value")

	dir := t.TempDir()
	input := path.Join(dir, "config.yaml")
	assert.NoError(t, os.WriteFile(input, []byte("key: ${SILENT_TEST_VALUE}\n"), 0o644))

	for _, args := range [][]string{
		{"config", "process", "-f", input},
		{"config", "process", "-f", input, "-o", path.Join(dir, "output.yaml")},
		{"config", "process", "-f", input, "-i"},
		{"value", "${SILENT_TEST_VALUE}"},
	} {
		stderr := captureStderr(t, func() {
			resetFlags(t)
			rootCmd.SetArgs(args)
			assert.NoError(t, rootCmd.Execute())
		})
		assert.Empty(t, stderr, args)
	}

	// The details are still available at debug level
	stderr := captureStderr(t, func() {
		resetFlags(t)
		rootCmd.SetArgs([]string{"-l", "debug", "config", "process", "-f", input})
		assert.NoError(t, rootCmd.Execute())
	})
	assert.Contains(t, stderr, `"msg":"Running command"`)
	assert.Contains(t, stderr, `"msg":"Processing file"`)
}

// copyToTempFile copies the file into a temporary directory and returns the path of the copy.
// The copy keeps the extension of the file if keepExtension is set, so its type can be inferred.
func copyToTempFile(t *testing.T, source string, keepExtension bool) string {
	t.Helper()

	content, err := os.ReadFile(source)
	assert.NoError(t, err)

	name := "config"
	if keepExtension {
		name += path.Ext(source)
	}

	target := path.Join(t.TempDir(), name)
	assert.NoError(t, os.WriteFile(target, content, 0o644))
	return target
}

// readFile returns the content of the file
func readFile(t *testing.T, name string) string {
	t.Helper()

	content, err := os.ReadFile(name)
	assert.NoError(t, err)
	return string(content)
}

// fileMode returns the permissions of the file
func fileMode(t *testing.T, name string) os.FileMode {
	t.Helper()

	info, err := os.Stat(name)
	assert.NoError(t, err)
	return info.Mode().Perm()
}

// TestOutputFileMode checks that new output files get the mode of the input file or of --mode,
// and that existing output files keep their mode unless --mode is given
func TestOutputFileMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows only knows read-only files")
	}
	t.Setenv("MODE_TEST_VALUE", "value")

	dir := t.TempDir()
	input := path.Join(dir, "template.yaml")
	assert.NoError(t, os.WriteFile(input, []byte("key: ${MODE_TEST_VALUE}\n"), 0o600))
	assert.NoError(t, os.Chmod(input, 0o640))
	output := path.Join(dir, "config.yaml")

	run := func(args ...string) error {
		resetFlags(t)
		rootCmd.SetArgs(append([]string{"config", "process", "-f", input}, args...))
		return rootCmd.Execute()
	}

	assert.NoError(t, run("-o", output))
	assert.Equal(t, os.FileMode(0o640), fileMode(t, output), "new files get the mode of the input file")
	assert.Equal(t, "key: value\n", readFile(t, output))

	assert.NoError(t, os.Chmod(output, 0o604))
	assert.NoError(t, run("-o", output, "-w"))
	assert.Equal(t, os.FileMode(0o604), fileMode(t, output), "existing files keep their mode")

	assert.NoError(t, run("-o", output, "-w", "--mode", "600"))
	assert.Equal(t, os.FileMode(0o600), fileMode(t, output), "--mode applies to existing files")

	assert.NoError(t, run("-o", path.Join(dir, "other.yaml"), "--mode", "0644"))
	assert.Equal(t, os.FileMode(0o644), fileMode(t, path.Join(dir, "other.yaml")), "--mode applies to new files")

	assert.NoError(t, run("-i"))
	assert.Equal(t, os.FileMode(0o640), fileMode(t, input), "-i keeps the mode of the input file")
	assert.Equal(t, "key: value\n", readFile(t, input))

	assert.NoError(t, run("-i", "--mode", "0600"))
	assert.Equal(t, os.FileMode(0o600), fileMode(t, input), "--mode applies to -i")
}

// TestInvalidOutputFileMode checks that an invalid --mode fails before anything is written
func TestInvalidOutputFileMode(t *testing.T) {
	dir := t.TempDir()
	input := path.Join(dir, "template.yaml")
	assert.NoError(t, os.WriteFile(input, []byte("key: value\n"), 0o644))
	output := path.Join(dir, "config.yaml")

	for _, mode := range []string{"999", "0o600", "rw-r--r--", "1777", "-1"} {
		resetFlags(t)
		rootCmd.SetArgs([]string{"config", "process", "-f", input, "-o", output, "--mode", mode})
		assert.EqualError(t, rootCmd.Execute(), fmt.Sprintf("invalid file mode %q for --mode, use octal permissions like 0600 or 0644", mode), mode)
		assert.NoFileExists(t, output, mode)
	}
}

// TestOutputFileExists checks that an existing output file is only overwritten with -w and kept unchanged otherwise
func TestOutputFileExists(t *testing.T) {
	dir := t.TempDir()
	input := path.Join(dir, "template.yaml")
	assert.NoError(t, os.WriteFile(input, []byte("key: value\n"), 0o644))
	output := path.Join(dir, "config.yaml")
	assert.NoError(t, os.WriteFile(output, []byte("existing\n"), 0o644))

	resetFlags(t)
	rootCmd.SetArgs([]string{"config", "process", "-f", input, "-o", output})
	err := rootCmd.Execute()
	assert.ErrorIs(t, err, ErrFileExists)
	assert.EqualError(t, err, fmt.Sprintf("file already exists: %s; use -w / --overwrite if this is intended", output))
	assert.Equal(t, "existing\n", readFile(t, output))
}
