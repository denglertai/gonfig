package cmd

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path"
	"testing"

	"github.com/denglertai/gonfig/internal/general"
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

			tmp := os.TempDir()
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
			tmp := os.TempDir()
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

			tmp := os.TempDir()
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

			tmp := os.TempDir()
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
