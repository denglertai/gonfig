package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/denglertai/gonfig/internal/file"
	"github.com/stretchr/testify/assert"
)

// runWithStdin runs gonfig with the arguments and the input as stdin and returns its output
func runWithStdin(t *testing.T, input string, args ...string) (string, error) {
	t.Helper()
	resetFlags(t)

	var out bytes.Buffer
	rootCmd.SetIn(strings.NewReader(input))
	rootCmd.SetOut(&out)
	t.Cleanup(func() {
		rootCmd.SetIn(nil)
		rootCmd.SetOut(nil)
	})
	rootCmd.SetArgs(args)
	err := rootCmd.Execute()
	return out.String(), err
}

// TestProcessStdin checks that -f - processes stdin to stdout
func TestProcessStdin(t *testing.T) {
	t.Setenv("STDIN_TEST_VALUE", "value")

	out, err := runWithStdin(t, "key: ${STDIN_TEST_VALUE}\n", "config", "process", "-f", "-", "-t", "yaml")
	assert.NoError(t, err)
	assert.Equal(t, "key: value\n", out)
}

// TestProcessStdinErrorsNameStdin checks that errors of stdin name it <stdin>
func TestProcessStdinErrorsNameStdin(t *testing.T) {
	_, err := runWithStdin(t, "key: [\n", "config", "process", "-f", "-", "-t", "yaml")
	assert.ErrorContains(t, err, "failed to read <stdin>: ")
}

// TestProcessStdinToFile checks that the output of stdin is written to -o with mode 0600, as stdin has no mode to take
func TestProcessStdinToFile(t *testing.T) {
	t.Setenv("STDIN_TEST_VALUE", "value")
	dir := t.TempDir()
	output := path.Join(dir, "config.yaml")
	// A file named - must not be read, nor provide the mode of the output
	t.Chdir(dir)
	assert.NoError(t, os.WriteFile("-", []byte("other: file\n"), 0o644))

	out, err := runWithStdin(t, "key: ${STDIN_TEST_VALUE}\n", "config", "process", "-f", "-", "-t", "yaml", "-o", output)
	assert.NoError(t, err)
	assert.Empty(t, out)
	assert.Equal(t, "key: value\n", readFile(t, output))
	if runtime.GOOS != "windows" {
		assert.Equal(t, os.FileMode(0o600), fileMode(t, output))
	}

	_, err = runWithStdin(t, "key: ${STDIN_TEST_VALUE}\n", "config", "process", "-f", "-", "-t", "yaml", "-o", output, "-w", "--mode", "0644")
	assert.NoError(t, err)
	if runtime.GOOS != "windows" {
		assert.Equal(t, os.FileMode(0o644), fileMode(t, output))
	}
}

// TestFileNamedDash checks that a file named - is still processed with -f ./-
func TestFileNamedDash(t *testing.T) {
	t.Setenv("STDIN_TEST_VALUE", "value")
	t.Chdir(t.TempDir())
	assert.NoError(t, os.WriteFile("-", []byte("key: ${STDIN_TEST_VALUE}\n"), 0o644))

	out, err := runWithStdin(t, "", "config", "process", "-f", "./-", "-t", "yaml")
	assert.NoError(t, err)
	assert.Equal(t, "key: value\n", out)
}

// TestGlobMatchNamedDash checks that a pattern matching a file named - processes the file instead of reading stdin
func TestGlobMatchNamedDash(t *testing.T) {
	t.Setenv("STDIN_TEST_VALUE", "value")
	t.Chdir(t.TempDir())
	assert.NoError(t, os.WriteFile("-", []byte("key: ${STDIN_TEST_VALUE}\n"), 0o644))

	out, err := runWithStdin(t, "key: stdin\n", "config", "process", "-f", "*", "-t", "yaml")
	assert.NoError(t, err)
	assert.Equal(t, "key: value\n", out)

	files, err := expandFiles([]string{"?"})
	assert.NoError(t, err)
	assert.Equal(t, []string{"." + string(filepath.Separator) + "-"}, files)
}

// TestStdinUsageErrors checks the combinations stdin doesn't support, which fail before stdin is read
func TestStdinUsageErrors(t *testing.T) {
	dir := t.TempDir()
	other := path.Join(dir, "other.yaml")
	assert.NoError(t, os.WriteFile(other, []byte("a: 1\n"), 0o644))

	tests := []struct {
		name string
		args []string
		want string
	}{
		{"without type", []string{"config", "process", "-f", "-"}, "-f - needs -t / --file-type, since the file type of stdin cannot be inferred"},
		{"twice", []string{"config", "process", "-f", "-", "-f", "-", "-t", "yaml"}, "-f - reads the input from stdin, which cannot be combined with other files"},
		{"with a file", []string{"config", "process", "-f", other, "-f", "-", "-t", "yaml"}, "-f - reads the input from stdin, which cannot be combined with other files"},
		{"inline", []string{"config", "process", "-f", "-", "-t", "yaml", "-i"}, "-i cannot write back to stdin; the output goes to stdout, or to a file with -o <file>"},
		{"output directory", []string{"config", "process", "-f", "-", "-t", "yaml", "-o", dir + "/"}, "-o " + dir + "/ is a directory, which needs the name of an input file; use -o <file> for stdin"},
		{"check without type", []string{"config", "check", "-f", "-"}, "-f - needs -t / --file-type, since the file type of stdin cannot be inferred"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := runWithStdin(t, "a: 1\n", tt.args...)
			assert.EqualError(t, err, tt.want)
			assert.NoFileExists(t, path.Join(dir, "-"))
		})
	}
}

// TestCheckStdin checks that -f - is checked and reported as <stdin>, with the diff read from stdin as well
func TestCheckStdin(t *testing.T) {
	t.Setenv("STDIN_TEST_VALUE", "value")

	out, err := runWithStdin(t, "key: ${STDIN_TEST_VALUE}\n", "config", "check", "-f", "-", "-t", "yaml", "-o", "json", "--diff")
	assert.NoError(t, err)

	var result struct {
		Files []file.CheckReport `json:"files"`
	}
	assert.NoError(t, json.Unmarshal([]byte(out), &result), out)
	if assert.Len(t, result.Files, 1) {
		assert.Equal(t, "<stdin>", result.Files[0].File)
		if assert.NotNil(t, result.Files[0].Diff) {
			assert.Contains(t, *result.Files[0].Diff, "+key: '***'")
		}
	}

	_, err = runWithStdin(t, "key: [\n", "config", "check", "-f", "-", "-t", "yaml")
	assert.EqualError(t, err, "check failed: 0 missing variables, 1 error")
}
