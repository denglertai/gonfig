package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeFiles writes the files with their content into the directory and returns their paths in the order of names
func writeFiles(t *testing.T, dir string, files map[string]string, names ...string) []string {
	t.Helper()

	for name, content := range files {
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
	}
	paths := make([]string, 0, len(names))
	for _, name := range names {
		paths = append(paths, filepath.Join(dir, name))
	}
	return paths
}

// runGonfig runs gonfig with the arguments and returns its output and error
func runGonfig(t *testing.T, args ...string) (string, error) {
	t.Helper()

	var out bytes.Buffer
	rootCmd.SetOut(&out)
	t.Cleanup(func() { rootCmd.SetOut(nil) })

	resetFlags(t)
	rootCmd.SetArgs(args)
	err := rootCmd.Execute()
	return out.String(), err
}

// TestExpandFiles checks that patterns are expanded into sorted files without directories and duplicates,
// that existing files are used literally and that patterns matching nothing are errors
func TestExpandFiles(t *testing.T) {
	dir := t.TempDir()
	paths := writeFiles(t, dir, map[string]string{
		"b.yaml": "", "a.yaml": "", "c.json": "", "a[1].yaml": "", "sub.yaml/x.yaml": "",
	}, "a.yaml", "b.yaml", "c.json", "a[1].yaml")
	a, b, c, literal := paths[0], paths[1], paths[2], paths[3]
	join := func(name string) string { return filepath.Join(dir, name) }

	tests := []struct {
		name    string
		values  []string
		want    []string
		wantErr string
	}{
		{name: "single file", values: []string{a}, want: []string{a}},
		{name: "missing files are read later", values: []string{join("missing.yaml")}, want: []string{join("missing.yaml")}},
		{name: "matches are sorted", values: []string{join("*.yaml")}, want: []string{a, literal, b}},
		{name: "files keep their order", values: []string{c, join("?.yaml")}, want: []string{c, a, b}},
		{name: "directories are skipped", values: []string{join("*")}, want: []string{a, literal, b, c}},
		{name: "duplicates are removed", values: []string{b, join("*.yaml"), dir + string(os.PathSeparator) + "." + string(os.PathSeparator) + "b.yaml"}, want: []string{b, a, literal}},
		{name: "existing files are used literally", values: []string{literal}, want: []string{literal}},
		{name: "pattern without matches", values: []string{a, join("*.xml")}, wantErr: fmt.Sprintf("no files match the pattern %q of -f / --file", join("*.xml"))},
		{name: "pattern matching directories only", values: []string{join("sub*")}, wantErr: fmt.Sprintf("no files match the pattern %q of -f / --file", join("sub*"))},
		{name: "invalid pattern", values: []string{join("[a.yaml")}, wantErr: fmt.Sprintf("invalid pattern %q for -f / --file: syntax error in pattern", join("[a.yaml"))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := expandFiles(tt.values)
			if tt.wantErr != "" {
				assert.EqualError(t, err, tt.wantErr)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestPositionalArgumentsAreRejected checks that arguments are an error instead of being ignored, e.g. a second file without -f
func TestPositionalArgumentsAreRejected(t *testing.T) {
	dir := t.TempDir()
	paths := writeFiles(t, dir, map[string]string{"a.yaml": "a: 1\n", "b.yaml": "b: 1\n"}, "a.yaml", "b.yaml")

	for _, command := range []string{"process", "check"} {
		_, err := runGonfig(t, "config", command, "-f", paths[0], paths[1])
		assert.EqualError(t, err, fmt.Sprintf("unexpected argument %q; use -f for each file, e.g. -f a.yaml -f b.yaml", paths[1]), command)
	}
}

// TestPatternWithoutMatchesFails checks that process and check fail for a pattern matching no file, so typos don't skip files
func TestPatternWithoutMatchesFails(t *testing.T) {
	pattern := filepath.Join(t.TempDir(), "conf.d", "*.yaml")

	for _, args := range [][]string{{"process", "-i"}, {"check"}} {
		_, err := runGonfig(t, append(append([]string{"config"}, args...), "-f", pattern)...)
		assert.EqualError(t, err, fmt.Sprintf("no files match the pattern %q of -f / --file", pattern), args[0])
	}
}

// TestProcessSeveralFilesInline checks that -i writes every file back to itself, keeping the mode of each file
func TestProcessSeveralFilesInline(t *testing.T) {
	t.Setenv("MULTI_VALUE", "value")
	dir := t.TempDir()
	paths := writeFiles(t, dir, map[string]string{
		"conf/a.yaml": "a: ${MULTI_VALUE}\n", "conf/b.json": `{"b": "${MULTI_VALUE}"}`, "secret.properties": "s=${MULTI_VALUE}\n",
	}, "conf/a.yaml", "conf/b.json", "secret.properties")
	require.NoError(t, os.Chmod(paths[2], 0o600))

	output, err := runGonfig(t, "config", "process", "-i", "-f", filepath.Join(dir, "conf", "*"), "-f", paths[2])
	assert.NoError(t, err)
	assert.Empty(t, output)

	assert.Equal(t, "a: value\n", readFile(t, paths[0]))
	assert.JSONEq(t, `{"b": "value"}`, readFile(t, paths[1]))
	assert.Equal(t, "s = value\n", readFile(t, paths[2]))
	if runtime.GOOS != "windows" {
		assert.Equal(t, os.FileMode(0o644), fileMode(t, paths[0]))
		assert.Equal(t, os.FileMode(0o600), fileMode(t, paths[2]), "each file keeps its mode")
	}
}

// TestProcessSeveralFilesIntoDirectory checks that -o with a directory writes each file into it under its base name,
// creating it if needed, with the mode of each input file unless --mode is given
func TestProcessSeveralFilesIntoDirectory(t *testing.T) {
	t.Setenv("MULTI_VALUE", "value")
	dir := t.TempDir()
	paths := writeFiles(t, dir, map[string]string{
		"templates/a.yaml": "a: ${MULTI_VALUE}\n", "templates/b.yaml": "b: ${MULTI_VALUE}\n",
	}, "templates/a.yaml", "templates/b.yaml")
	require.NoError(t, os.Chmod(paths[1], 0o640))
	out := filepath.Join(dir, "out", "nested") + "/"

	_, err := runGonfig(t, "config", "process", "-f", filepath.Join(dir, "templates", "*.yaml"), "-o", out)
	assert.NoError(t, err)
	assert.Equal(t, "a: value\n", readFile(t, filepath.Join(out, "a.yaml")))
	assert.Equal(t, "b: value\n", readFile(t, filepath.Join(out, "b.yaml")))
	assert.Equal(t, "a: ${MULTI_VALUE}\n", readFile(t, paths[0]), "the templates are unchanged")
	if runtime.GOOS != "windows" {
		assert.Equal(t, os.FileMode(0o644), fileMode(t, filepath.Join(out, "a.yaml")))
		assert.Equal(t, os.FileMode(0o640), fileMode(t, filepath.Join(out, "b.yaml")), "new files get the mode of their input file")
		assert.Equal(t, os.FileMode(0o755)&^umask(t), fileMode(t, out), "directories get 0755 minus the umask, not --mode")
	}

	// The directory exists now, so it is a directory without the trailing slash as well
	_, err = runGonfig(t, "config", "process", "-f", paths[0], "-f", paths[1], "-o", filepath.Clean(out), "-w", "--mode", "0600")
	assert.NoError(t, err)
	if runtime.GOOS != "windows" {
		assert.Equal(t, os.FileMode(0o600), fileMode(t, filepath.Join(out, "a.yaml")), "--mode applies to every file")
		assert.Equal(t, os.FileMode(0o600), fileMode(t, filepath.Join(out, "b.yaml")), "--mode applies to every file")
	}

	// A single file is written into an existing directory as well
	single := filepath.Join(dir, "single")
	require.NoError(t, os.Mkdir(single, 0o755))
	_, err = runGonfig(t, "config", "process", "-f", paths[0], "-o", single)
	assert.NoError(t, err)
	assert.Equal(t, "a: value\n", readFile(t, filepath.Join(single, "a.yaml")))
}

// umask returns the umask of the process, which restricts the mode of new directories
func umask(t *testing.T) os.FileMode {
	t.Helper()

	probe := filepath.Join(t.TempDir(), "probe")
	require.NoError(t, os.Mkdir(probe, 0o777))
	return 0o777 &^ fileMode(t, probe)
}

// TestProcessSeveralFilesErrors checks that invalid outputs for several files, existing output files and errors while
// processing a file are reported before any file is written
func TestProcessSeveralFilesErrors(t *testing.T) {
	t.Setenv("MULTI_VALUE", "value")
	dir := t.TempDir()
	original := map[string]string{
		"a.yaml": "a: ${MULTI_VALUE}\n", "b.yaml": "b: ${MULTI_VALUE}\n", "invalid.yaml": "c: ${MULTI_VALUE | nope}\n",
		"other/a.yaml": "a: other\n", "out/b.yaml": "existing\n",
	}
	paths := writeFiles(t, dir, original, "a.yaml", "b.yaml", "invalid.yaml", "other/a.yaml")
	a, b, invalid, otherA := paths[0], paths[1], paths[2], paths[3]
	out := filepath.Join(dir, "out")
	notADir := filepath.Join(dir, "out.yaml")

	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{name: "stdout", args: []string{"-f", a, "-f", b}, wantErr: "2 files cannot be written to stdout; use -i to write them back or -o <dir>/ to write them into a directory"},
		{name: "output file", args: []string{"-f", a, "-f", b, "-o", notADir}, wantErr: fmt.Sprintf("-o %s is not a directory, which 2 files need; end it with / to create it, or use -i", notADir)},
		{name: "same base name", args: []string{"-f", a, "-f", otherA, "-o", out}, wantErr: fmt.Sprintf("%s and %s would both be written to %s", a, otherA, filepath.Join(out, "a.yaml"))},
		{name: "existing output file", args: []string{"-f", a, "-f", b, "-o", out}, wantErr: fmt.Sprintf("file %s already exists; Use -w / --overwrite if this is intended", filepath.Join(out, "b.yaml"))},
		{name: "processing error", args: []string{"-i", "-f", a, "-f", invalid}, wantErr: fmt.Sprintf(`failed to process the value of c in %s: unknown filter "nope"`, invalid)},
		{name: "output directory below a file", args: []string{"-f", a, "-o", filepath.Join(a, "out") + "/"}, wantErr: "failed to create the output directory: "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := runGonfig(t, append([]string{"config", "process"}, tt.args...)...)
			assert.ErrorContains(t, err, tt.wantErr)

			for name, content := range original {
				assert.Equal(t, content, readFile(t, filepath.Join(dir, name)), "no file may be changed")
			}
			assert.NoFileExists(t, notADir)
			assert.NoFileExists(t, filepath.Join(out, "a.yaml"))
		})
	}
}

// TestCheckSeveralFiles checks that every file is reported, followed by the total, and that the check fails if any file fails
func TestCheckSeveralFiles(t *testing.T) {
	t.Setenv("MULTI_VALUE", "value")
	dir := t.TempDir()
	paths := writeFiles(t, dir, map[string]string{
		"a.yaml": "a: ${MULTI_VALUE}\n", "b.yaml": "b: ${MULTI_MISSING}\n",
	}, "a.yaml", "b.yaml")

	output, err := runGonfig(t, "config", "check", "-f", filepath.Join(dir, "*.yaml"))
	assert.EqualError(t, err, "check failed: 1 missing variable, 0 errors")
	assert.Equal(t, fmt.Sprintf(`%s: 1 entry with placeholders
  a
    MULTI_VALUE              set
0 missing variables, 0 errors
%s: 1 entry with placeholders
  b
    MULTI_MISSING            missing
1 missing variable, 0 errors
2 files: 1 missing variable, 0 errors
`, paths[0], paths[1]), output)

	output, err = runGonfig(t, "config", "check", "-f", paths[0], "-f", paths[1], "-o", "json")
	assert.Error(t, err)
	var report struct {
		Files []struct {
			File             string `json:"file"`
			MissingVariables int    `json:"missingVariables"`
			Failed           bool   `json:"failed"`
		} `json:"files"`
		MissingVariables int  `json:"missingVariables"`
		Errors           int  `json:"errors"`
		Failed           bool `json:"failed"`
	}
	require.NoError(t, json.Unmarshal([]byte(output), &report))
	require.Len(t, report.Files, 2)
	assert.Equal(t, paths[0], report.Files[0].File)
	assert.False(t, report.Files[0].Failed)
	assert.Equal(t, paths[1], report.Files[1].File)
	assert.Equal(t, 1, report.Files[1].MissingVariables)
	assert.True(t, report.Files[1].Failed)
	assert.Equal(t, 1, report.MissingVariables)
	assert.Equal(t, 0, report.Errors)
	assert.True(t, report.Failed)

	// A file that cannot be read is reported as an error, and the files after it are still checked
	missing := filepath.Join(dir, "missing.yaml")
	output, err = runGonfig(t, "config", "check", "-f", missing, "-f", paths[0])
	assert.EqualError(t, err, "check failed: 0 missing variables, 1 error")
	assert.Contains(t, output, missing+": cannot be checked\n  error: open "+missing+": ")
	assert.Contains(t, output, paths[0]+": 1 entry with placeholders\n")
	assert.True(t, strings.HasSuffix(output, "2 files: 0 missing variables, 1 error\n"), output)
}

// TestSeveralFilesThroughLinks checks that a file given by a symbolic link as well is processed once, and that two files
// written to the same file through a link in the output directory are an error before anything is written
func TestSeveralFilesThroughLinks(t *testing.T) {
	t.Setenv("MULTI_VALUE", "value")
	dir := t.TempDir()
	paths := writeFiles(t, dir, map[string]string{
		"a.yaml": "a: ${MULTI_VALUE}\n", "b.yaml": "b: ${MULTI_VALUE}\n", "out/a.yaml": "existing\n",
	}, "a.yaml", "b.yaml", "out/a.yaml")
	link := filepath.Join(dir, "link.yaml")
	if err := os.Symlink(paths[0], link); err != nil {
		t.Skipf("symbolic links are not supported: %v", err)
	}

	files, err := expandFiles([]string{paths[0], link, filepath.Join(dir, "*.yaml")})
	assert.NoError(t, err)
	assert.Equal(t, []string{paths[0], paths[1]}, files, "the link is the same file as a.yaml")

	// out/b.yaml is a link to out/a.yaml, so a.yaml and b.yaml would both be written to out/a.yaml
	out := filepath.Join(dir, "out")
	require.NoError(t, os.Symlink(paths[2], filepath.Join(out, "b.yaml")))
	target, err := filepath.EvalSymlinks(paths[2])
	require.NoError(t, err)

	_, err = runGonfig(t, "config", "process", "-f", paths[0], "-f", paths[1], "-o", out, "-w")
	assert.EqualError(t, err, fmt.Sprintf("%s and %s would both be written to %s", paths[0], paths[1], target))
	assert.Equal(t, "existing\n", readFile(t, paths[2]))
}
