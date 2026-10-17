package cmd

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path"
	"testing"

	"github.com/denglertai/gonfig/internal/filter"
	"github.com/denglertai/gonfig/internal/general"
	"github.com/denglertai/gonfig/internal/logging"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/assert"
)

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
			old := os.Stdout // keep backup of the real stdout
			r, w, _ := os.Pipe()
			os.Stdout = w

			outC := make(chan string)
			// copy the output in a separate goroutine so printing can't block indefinitely
			go func() {
				var buf bytes.Buffer
				io.Copy(&buf, r)
				outC <- buf.String()
			}()

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
			err = rootCmd.Execute()
			assert.NoError(t, err)

			// back to normal state
			w.Close()
			os.Stdout = old // restoring the real stdout
			out := <-outC

			snaps.MatchSnapshot(t, out)
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

			tmp := t.TempDir()
			file, err := os.CreateTemp(tmp, "gonfig-test-*")
			assert.NoError(t, err)
			fileName := file.Name()

			defer func() {
				file.Close()
				os.Remove(fileName)
			}()

			// Take the path of the file and pass it as an argument to the command and see if it works
			args := []string{"config", "process", "-f", tC.file, "-o", fileName, "-w", "-l", "trace", "-s"}

			if tC.fileType != general.Undefined {
				args = append(args, "-t", string(tC.fileType))
			}

			resetFlags(t)
			rootCmd.SetArgs(args)
			err = rootCmd.Execute()
			assert.NoError(t, err)

			// Read the file and compare it with the snapshot
			file, err = os.Open(fileName)
			assert.NoError(t, err)
			stat, err := file.Stat()
			assert.NoError(t, err)
			res := make([]byte, stat.Size())
			file.Read(res)

			snaps.MatchSnapshot(t, string(res))
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
			tmp := t.TempDir()
			file, err := os.CreateTemp(tmp, "gonfig-test-*")
			assert.NoError(t, err)
			fileName := file.Name()

			c, err := os.ReadFile(tC.file)
			assert.NoError(t, err)

			_, err = file.Write(c)
			assert.NoError(t, err)

			file.Close()
			defer func() {
				os.Remove(fileName)
			}()

			// Take the path of the file and pass it as an argument to the command and see if it works
			args := []string{"config", "process", "-f", fileName, "-i", "-l", "trace", "-s", "-t", string(tC.fileType)}

			resetFlags(t)
			rootCmd.SetArgs(args)
			err = rootCmd.Execute()
			assert.NoError(t, err)

			// Read the file and compare it with the snapshot
			file, err = os.Open(fileName)
			assert.NoError(t, err)
			stat, err := file.Stat()
			assert.NoError(t, err)
			res := make([]byte, stat.Size())
			file.Read(res)

			snaps.MatchSnapshot(t, string(res))
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

			tmp := t.TempDir()
			file, err := os.CreateTemp(tmp, fmt.Sprintf("gonfig-test-*%s", path.Ext(tC.file)))
			assert.NoError(t, err)
			fileName := file.Name()

			// Copy the file to the temp file
			source, err := os.Open(tC.file) //open the source file
			assert.NoError(t, err)
			defer source.Close()

			defer func() {
				file.Close()
				os.Remove(fileName)
			}()

			_, err = io.Copy(file, source) //copy the contents of source to destination file
			assert.NoError(t, err)
			file.Close()

			// Take the path of the file and pass it as an argument to the command and see if it works
			args := []string{"config", "process", "-f", fileName, "-i", "-l", "trace", "-s"}

			if tC.fileType != general.Undefined {
				args = append(args, "-t", string(tC.fileType))
			}

			resetFlags(t)
			rootCmd.SetArgs(args)
			err = rootCmd.Execute()
			assert.NoError(t, err)

			// Read the file and compare it with the snapshot
			file, err = os.Open(fileName)
			assert.NoError(t, err)
			stat, err := file.Stat()
			assert.NoError(t, err)
			res := make([]byte, stat.Size())
			file.Read(res)

			snaps.MatchSnapshot(t, string(res))
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

			tmp := t.TempDir()
			file, err := os.CreateTemp(tmp, fmt.Sprintf("gonfig-test-*%s", path.Ext(tC.file)))
			assert.NoError(t, err)
			fileName := file.Name()

			// Copy the file to the temp file
			source, err := os.Open(tC.file) //open the source file
			assert.NoError(t, err)
			defer source.Close()

			defer func() {
				file.Close()
				os.Remove(fileName)
			}()

			_, err = io.Copy(file, source) //copy the contents of source to destination file
			assert.NoError(t, err)
			file.Close()

			// Take the path of the file and pass it as an argument to the command and see if it works
			args := []string{"config", "process", "-f", fileName, "-i", "-l", "trace", "-s"}

			if tC.fileType != general.Undefined {
				args = append(args, "-t", string(tC.fileType))
			}

			resetFlags(t)
			rootCmd.SetArgs(args)
			err = rootCmd.Execute()
			assert.NoError(t, err)

			// Read the file and compare it with the snapshot
			file, err = os.Open(fileName)
			assert.NoError(t, err)
			stat, err := file.Stat()
			assert.NoError(t, err)
			res := make([]byte, stat.Size())
			file.Read(res)

			snaps.MatchSnapshot(t, string(res))
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

func TestInvalidConfigFile(t *testing.T) {
	testCases := []struct {
		desc    string
		content string
		wantErr string
	}{
		{desc: "invalid YAML", content: "templates:\n  filters:\n    a:\n      filters: @b\n", wantErr: "failed to read config file"},
		{desc: "invalid templates", content: "templates:\n  filters: not a map\n", wantErr: "invalid templates in config file"},
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

func TestFragmentFromTemplate(t *testing.T) {
	t.Setenv("FRAGMENT_LOGGING", "")
	t.Setenv("FRAGMENT_LEVEL", "debug")

	dir := t.TempDir()
	fragmentFile := path.Join(dir, "logging.yaml")
	assert.NoError(t, os.WriteFile(fragmentFile, []byte("level: ${FRAGMENT_LEVEL}\nappenders: [console]\n"), 0o644))

	// The template reads the fragment from a file by default
	configDir := t.TempDir()
	config := "templates:\n  placeholders:\n    logging:\n      env: FRAGMENT_LOGGING\n      default: \"@" + fragmentFile + "\"\n"
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
	// Options like log-values log warnings, so neither the environment nor a config file of the machine may enable them
	t.Setenv("HOME", t.TempDir())
	t.Chdir(t.TempDir())
	for _, key := range []string{"GONFIG_LOG_LEVEL", "GONFIG_LOG_SOURCE", "GONFIG_CONFIG_PATH", "GONFIG_PLUGIN_PATH", "GONFIG_ALLOW_UNKNOWN_FILTERS", "GONFIG_LOG_VALUES", "GONFIG_FAIL_ON_MISSING"} {
		t.Setenv(key, "")
	}
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
