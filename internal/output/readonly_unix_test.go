//go:build unix

package output

import (
	"bytes"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// inlineOptions are the options of files written back with -i
var inlineOptions = Options{Mode: 0o600, SkipUnchanged: true, CheckWritable: true}

// captureLogs collects the log records written while the test runs, so warnings can be checked
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var logs bytes.Buffer
	defaultLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(defaultLogger) })
	return &logs
}

// readOnlyFile simulates a file on a read-only file system: opening it for writing fails with EROFS
func readOnlyFile(t *testing.T, readOnly string) {
	t.Helper()
	openWritable = func(name string) (*os.File, error) {
		if name == readOnly {
			return nil, &fs.PathError{Op: "open", Path: name, Err: syscall.EROFS}
		}
		return openWritableFile(name)
	}
	t.Cleanup(func() { openWritable = openWritableFile })
}

// resolvedTempDir returns a temporary directory with symbolic links resolved. Files are opened, written and their modes set
// by their resolved paths, so tests replacing these functions compare against resolved paths, e.g. on macOS, where
// temporary directories are below /var, a symbolic link to /private/var
func resolvedTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	return dir
}

// writeOld writes the old content of a file with an mtime in the past, so a rewrite would change it
func writeOld(t *testing.T, path string, content string) fs.FileInfo {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o640))
	past := time.Now().Add(-time.Hour)
	require.NoError(t, os.Chtimes(path, past, past))
	info, err := os.Stat(path)
	require.NoError(t, err)
	return info
}

// assertUntouched checks that the file still has its content, inode and mtime
func assertUntouched(t *testing.T, path string, content string, before fs.FileInfo) {
	t.Helper()
	after, err := os.Stat(path)
	require.NoError(t, err)
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, content, string(got), path)
	assert.True(t, os.SameFile(before, after), "%s must not be replaced", path)
	assert.Equal(t, before.ModTime(), after.ModTime(), "%s must keep its mtime", path)
}

// TestWriteAllSkipsUnchangedFiles checks that files without changed values or with equal content are not written,
// while changed files are
func TestWriteAllSkipsUnchangedFiles(t *testing.T) {
	dir := resolvedTempDir(t)
	noValues := filepath.Join(dir, "compact.json")
	equal := filepath.Join(dir, "equal.yaml")
	changed := filepath.Join(dir, "changed.yaml")
	noValuesInfo := writeOld(t, noValues, `{"a":1}`)
	equalInfo := writeOld(t, equal, "a: 1\n")
	changedInfo := writeOld(t, changed, "a: ${A}\n")

	unchanged := inlineOptions
	unchanged.Unchanged = true
	require.NoError(t, WriteAll([]File{
		{Path: noValues, Data: []byte("{\n  \"a\": 1\n}\n"), Options: unchanged},
		{Path: equal, Data: []byte("a: 1\n"), Options: inlineOptions},
		{Path: changed, Data: []byte("a: 1\n"), Options: inlineOptions},
	}))

	assertUntouched(t, noValues, `{"a":1}`, noValuesInfo)
	assertUntouched(t, equal, "a: 1\n", equalInfo)
	content, err := os.ReadFile(changed)
	require.NoError(t, err)
	assert.Equal(t, "a: 1\n", string(content))
	assertMode(t, changed, changedInfo.Mode().Perm())
	assertNoLeftovers(t, dir)
}

// TestWriteAllChangesModeOfUnchangedFiles checks that --mode changes the mode of an unchanged file without writing it
func TestWriteAllChangesModeOfUnchangedFiles(t *testing.T) {
	dir := resolvedTempDir(t)
	path := filepath.Join(dir, "app.yaml")
	before := writeOld(t, path, "a: 1\n")

	opts := inlineOptions
	opts.Mode, opts.ForceMode = 0o600, true
	require.NoError(t, WriteAll([]File{{Path: path, Data: []byte("a: 1\n"), Options: opts}}))
	assertUntouched(t, path, "a: 1\n", before)
	assertMode(t, path, 0o600)
}

// TestWriteAllModeOfReadOnlyUnchangedFiles checks that changing the mode of an unchanged read-only file after a changed
// file fails before any file is changed, or skips only the mode change with a warning with IgnoreReadOnly
func TestWriteAllModeOfReadOnlyUnchangedFiles(t *testing.T) {
	for _, ignore := range []bool{true, false} {
		t.Run(map[bool]string{true: "ignored", false: "failing"}[ignore], func(t *testing.T) {
			logs := captureLogs(t)
			dir := resolvedTempDir(t)
			changed := filepath.Join(dir, "a.yaml")
			unchanged := filepath.Join(dir, "c.yaml")
			changedInfo := writeOld(t, changed, "a: ${A}\n")
			unchangedInfo := writeOld(t, unchanged, "c: 1\n")
			chmod = func(name string, mode fs.FileMode) error {
				if name == unchanged {
					return &fs.PathError{Op: "chmod", Path: name, Err: syscall.EROFS}
				}
				return os.Chmod(name, mode)
			}
			t.Cleanup(func() { chmod = os.Chmod })

			opts := inlineOptions
			opts.ForceMode, opts.IgnoreReadOnly = true, ignore
			err := WriteAll([]File{
				{Path: changed, Data: []byte("a: made-up-secret\n"), Options: opts},
				{Path: unchanged, Data: []byte("c: 1\n"), Options: opts},
			})
			if ignore {
				require.NoError(t, err)
				content, err := os.ReadFile(changed)
				require.NoError(t, err)
				assert.Equal(t, "a: made-up-secret\n", string(content))
				assert.Contains(t, logs.String(), `"level":"WARN","msg":"Skipping mode change of read-only file","path":"`+unchanged+`","mode":"0600","error":"read-only file system"`)
				assert.NotContains(t, logs.String(), "made-up-secret")
			} else {
				assert.ErrorIs(t, err, syscall.EROFS)
				assert.Contains(t, err.Error(), "failed to change the mode of "+unchanged+", no file has been changed: chmod ")
				assertUntouched(t, changed, "a: ${A}\n", changedInfo)
			}
			assertUntouched(t, unchanged, "c: 1\n", unchangedInfo)
			assertMode(t, unchanged, 0o640)
			assertNoLeftovers(t, dir)
		})
	}
}

// TestWriteAllModeOfUnchangedFilesBecomingReadOnly checks that an unchanged file that becomes read-only only after it
// was checked is skipped with IgnoreReadOnly as well, and fails without it, naming the files not written
func TestWriteAllModeOfUnchangedFilesBecomingReadOnly(t *testing.T) {
	for _, ignore := range []bool{true, false} {
		t.Run(map[bool]string{true: "ignored", false: "failing"}[ignore], func(t *testing.T) {
			logs := captureLogs(t)
			dir := resolvedTempDir(t)
			unchanged := filepath.Join(dir, "a.yaml")
			other := filepath.Join(dir, "b.yaml")
			writeOld(t, unchanged, "a: 1\n")
			otherInfo := writeOld(t, other, "b: ${B}\n")
			// Setting the current mode while checking succeeds, setting the requested mode fails
			chmod = func(name string, mode fs.FileMode) error {
				if name == unchanged && mode == 0o600 {
					return &fs.PathError{Op: "chmod", Path: name, Err: syscall.EROFS}
				}
				return os.Chmod(name, mode)
			}
			t.Cleanup(func() { chmod = os.Chmod })

			opts := inlineOptions
			opts.ForceMode, opts.IgnoreReadOnly = true, ignore
			err := WriteAll([]File{
				{Path: unchanged, Data: []byte("a: 1\n"), Options: opts},
				{Path: other, Data: []byte("b: 2\n"), Options: Options{Mode: 0o640, SkipUnchanged: true, CheckWritable: true, IgnoreReadOnly: ignore}},
			})
			if ignore {
				require.NoError(t, err)
				content, err := os.ReadFile(other)
				require.NoError(t, err)
				assert.Equal(t, "b: 2\n", string(content))
				assert.Contains(t, logs.String(), `"msg":"Skipping mode change of read-only file","path":"`+unchanged+`"`)
			} else {
				assert.ErrorIs(t, err, syscall.EROFS)
				assert.Contains(t, err.Error(), "failed to change the mode of "+unchanged+" (not written: "+other+")")
				assertUntouched(t, other, "b: ${B}\n", otherInfo)
			}
			assertNoLeftovers(t, dir)
		})
	}
}

// TestSameContentSkipsNamedPipes checks that a named pipe swapped in for a regular file since it was checked is not
// read, which would block without a writer
func TestSameContentSkipsNamedPipes(t *testing.T) {
	dir := resolvedTempDir(t)
	regular := filepath.Join(dir, "a.yaml")
	require.NoError(t, os.WriteFile(regular, []byte("a: 1\n"), 0o600))
	info, err := os.Stat(regular)
	require.NoError(t, err)
	pipe := filepath.Join(dir, "pipe")
	require.NoError(t, mkfifo(pipe))

	assert.True(t, sameContent(regular, info, []byte("a: 1\n")))
	assert.False(t, sameContent(pipe, info, []byte("a: 1\n")), "the regular file's info must not make the pipe readable")
	assert.False(t, sameContent(filepath.Join(dir, "missing"), info, []byte("a: 1\n")))
	// Without O_NONBLOCK, opening a pipe without a reader for writing blocks; with it, it fails with ENXIO
	err = checkWritable(pipe)
	assert.ErrorIs(t, err, syscall.ENXIO)
	assert.False(t, isReadOnlyErr(err, info), "the pipe is written as before")
}

// TestWriteAllFailsBeforeWritingReadOnlyFiles checks that a read-only file fails before any file is changed
func TestWriteAllFailsBeforeWritingReadOnlyFiles(t *testing.T) {
	dir := resolvedTempDir(t)
	writable := filepath.Join(dir, "a.yaml")
	readOnly := filepath.Join(dir, "b.yaml")
	writableInfo := writeOld(t, writable, "a: ${A}\n")
	readOnlyInfo := writeOld(t, readOnly, "b: ${B}\n")
	readOnlyFile(t, readOnly)

	err := WriteAll([]File{
		{Path: writable, Data: []byte("a: 1\n"), Options: inlineOptions},
		{Path: readOnly, Data: []byte("b: 2\n"), Options: inlineOptions},
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, syscall.EROFS)
	assert.Contains(t, err.Error(), "failed to write "+readOnly+", no file has been changed")
	assertUntouched(t, writable, "a: ${A}\n", writableInfo)
	assertUntouched(t, readOnly, "b: ${B}\n", readOnlyInfo)
	assertNoLeftovers(t, dir)
}

// TestWriteAllSkipsReadOnlyFiles checks that IgnoreReadOnly skips a read-only file with a warning naming only the path
// and the OS error, and writes the other files
func TestWriteAllSkipsReadOnlyFiles(t *testing.T) {
	logs := captureLogs(t)
	dir := resolvedTempDir(t)
	writable := filepath.Join(dir, "a.yaml")
	readOnly := filepath.Join(dir, "b.yaml")
	writeOld(t, writable, "a: ${A}\n")
	readOnlyInfo := writeOld(t, readOnly, "b: ${B}\n")
	readOnlyFile(t, readOnly)

	opts := inlineOptions
	opts.IgnoreReadOnly = true
	require.NoError(t, WriteAll([]File{
		{Path: writable, Data: []byte("a: made-up-secret\n"), Options: opts},
		{Path: readOnly, Data: []byte("b: made-up-secret\n"), Options: opts},
	}))

	content, err := os.ReadFile(writable)
	require.NoError(t, err)
	assert.Equal(t, "a: made-up-secret\n", string(content))
	assertUntouched(t, readOnly, "b: ${B}\n", readOnlyInfo)
	assert.Contains(t, logs.String(), `"level":"WARN","msg":"Skipping read-only file","path":"`+readOnly+`","error":"read-only file system"`)
	assert.NotContains(t, logs.String(), "made-up-secret")
	assertNoLeftovers(t, dir)
}

// TestWriteAllSkipsFilesBecomingReadOnly checks that a file that turns out to be read-only only while it is written
// in place is skipped with IgnoreReadOnly as well, and fails without it, naming the files written and skipped
func TestWriteAllSkipsFilesBecomingReadOnly(t *testing.T) {
	for _, ignore := range []bool{true, false} {
		t.Run(map[bool]string{true: "ignored", false: "failing"}[ignore], func(t *testing.T) {
			dir := resolvedTempDir(t)
			atomic := filepath.Join(dir, "a.yaml")
			inPlace := filepath.Join(dir, "b.yaml")
			writeOld(t, atomic, "a: ${A}\n")
			writeOld(t, inPlace, "b: ${B}\n")
			rename = func(from, to string) error {
				if to == inPlace {
					return syscall.EBUSY
				}
				return os.Rename(from, to)
			}
			writeFile = func(name string, data []byte, perm fs.FileMode) error {
				if name == inPlace {
					return &fs.PathError{Op: "open", Path: name, Err: syscall.EROFS}
				}
				return os.WriteFile(name, data, perm)
			}
			t.Cleanup(func() { rename, writeFile = os.Rename, os.WriteFile })

			opts := inlineOptions
			opts.IgnoreReadOnly = ignore
			err := WriteAll([]File{
				{Path: atomic, Data: []byte("a: 1\n"), Options: opts},
				{Path: inPlace, Data: []byte("b: 2\n"), Options: opts},
			})
			if ignore {
				assert.NoError(t, err)
			} else {
				assert.ErrorIs(t, err, syscall.EROFS)
				assert.Contains(t, err.Error(), "failed to write "+inPlace+" (written: "+atomic+")")
			}
			content, err := os.ReadFile(atomic)
			require.NoError(t, err)
			assert.Equal(t, "a: 1\n", string(content))
			assertNoLeftovers(t, dir)
		})
	}
}

// TestWriteAllKeepsReplacingFilesWithoutWritePermission checks that a file without write permission in a writable
// directory is still replaced, since that is no read-only error
func TestWriteAllKeepsReplacingFilesWithoutWritePermission(t *testing.T) {
	dir := resolvedTempDir(t)
	path := filepath.Join(dir, "app.yaml")
	require.NoError(t, os.WriteFile(path, []byte("a: ${A}\n"), 0o444))

	require.NoError(t, WriteAll([]File{{Path: path, Data: []byte("a: 1\n"), Options: inlineOptions}}))
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "a: 1\n", string(content))
	assertMode(t, path, 0o444)
}

// TestIsReadOnlyErr checks that only EROFS is a read-only error on Unix, not a missing permission
func TestIsReadOnlyErr(t *testing.T) {
	assert.True(t, isReadOnlyErr(&fs.PathError{Op: "open", Path: "a", Err: syscall.EROFS}, nil))
	assert.False(t, isReadOnlyErr(&fs.PathError{Op: "open", Path: "a", Err: syscall.EACCES}, nil))
	assert.False(t, isReadOnlyErr(nil, nil))
}
