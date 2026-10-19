package output

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// assertMode checks the mode of the file, except on Windows, which only knows read-only files
func assertMode(t *testing.T, path string, want fs.FileMode) {
	t.Helper()
	if runtime.GOOS == "windows" {
		return
	}
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, want, info.Mode().Perm(), path)
}

// assertNoLeftovers checks that no temporary files are left in the directory
func assertNoLeftovers(t *testing.T, dir string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, ".*.gonfig-*"))
	require.NoError(t, err)
	assert.Empty(t, matches)
}

// TestWriteNewFile checks that a new file gets exactly the mode of the options, regardless of the umask
func TestWriteNewFile(t *testing.T) {
	dir := t.TempDir()
	for _, mode := range []fs.FileMode{0o600, 0o640, 0o666} {
		path := filepath.Join(dir, mode.String()+".yaml")
		require.NoError(t, Write(path, []byte("a: 1\n"), Options{Mode: mode}))

		content, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, "a: 1\n", string(content))
		assertMode(t, path, mode)
	}
	assertNoLeftovers(t, dir)
}

// TestWriteReplacesExistingFile checks that an existing file is replaced by a new one, keeping its mode unless it is forced
func TestWriteReplacesExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.yaml")
	require.NoError(t, os.WriteFile(path, []byte("old\n"), 0o600))
	require.NoError(t, os.Chmod(path, 0o604))
	before, err := os.Stat(path)
	require.NoError(t, err)
	// Windows reads the file id lazily by the path, which has to happen before the file is replaced
	require.True(t, os.SameFile(before, before))

	require.NoError(t, Write(path, []byte("new\n"), Options{Mode: 0o644}))
	after, err := os.Stat(path)
	require.NoError(t, err)
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "new\n", string(content))
	assert.False(t, os.SameFile(before, after), "the file has to be replaced, not written in place")
	assertMode(t, path, 0o604)

	require.NoError(t, Write(path, []byte("forced\n"), Options{Mode: 0o600, ForceMode: true}))
	assertMode(t, path, 0o600)
	assertNoLeftovers(t, dir)
}

// TestWriteKeepsSymlinks checks that the file a symbolic link points to is replaced, while the link is kept
func TestWriteKeepsSymlinks(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "data", "app.yaml")
	require.NoError(t, os.Mkdir(filepath.Dir(target), 0o700))
	require.NoError(t, os.WriteFile(target, []byte("old\n"), 0o600))
	link := filepath.Join(dir, "app.yaml")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symbolic links are not supported: %v", err)
	}

	require.NoError(t, Write(link, []byte("new\n"), Options{Mode: 0o644}))

	info, err := os.Lstat(link)
	require.NoError(t, err)
	assert.Equal(t, fs.ModeSymlink, info.Mode().Type())
	content, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, "new\n", string(content))
	assertMode(t, target, 0o600)
	assertNoLeftovers(t, filepath.Dir(target))
}

// TestWriteFallsBackToWritingInPlace checks that a file which cannot be replaced is written in place,
// keeping the file, its mode and, with ForceMode, setting the mode, without leaving temporary files
func TestWriteFallsBackToWritingInPlace(t *testing.T) {
	tests := []struct {
		name   string
		inject func(t *testing.T)
	}{
		{
			name: "rename fails, e.g. for a bind-mounted file",
			inject: func(t *testing.T) {
				rename = func(_, _ string) error { return syscall.EBUSY }
				t.Cleanup(func() { rename = os.Rename })
			},
		},
		{
			name: "owner cannot be kept",
			inject: func(t *testing.T) {
				setOwner = func(_ string, _ fs.FileInfo) error { return errors.New("operation not permitted") }
				t.Cleanup(func() { setOwner = keepOwner })
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "app.yaml")
			require.NoError(t, os.WriteFile(path, []byte("old content\n"), 0o600))
			require.NoError(t, os.Chmod(path, 0o640))
			before, err := os.Stat(path)
			require.NoError(t, err)
			tt.inject(t)

			require.NoError(t, Write(path, []byte("new\n"), Options{Mode: 0o644}))
			after, err := os.Stat(path)
			require.NoError(t, err)
			content, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Equal(t, "new\n", string(content))
			assert.True(t, os.SameFile(before, after), "the file has to be written in place")
			assertMode(t, path, 0o640)

			require.NoError(t, Write(path, []byte("forced\n"), Options{Mode: 0o600, ForceMode: true}))
			assertMode(t, path, 0o600)
			assertNoLeftovers(t, dir)
		})
	}
}

// TestWriteNewFileInPlace checks that a new file written in place, e.g. if the rename fails, gets exactly the mode as well
func TestWriteNewFileInPlace(t *testing.T) {
	rename = func(_, _ string) error { return syscall.EXDEV }
	t.Cleanup(func() { rename = os.Rename })

	dir := t.TempDir()
	path := filepath.Join(dir, "app.yaml")
	require.NoError(t, Write(path, []byte("new\n"), Options{Mode: 0o666}))
	assertMode(t, path, 0o666)
	assertNoLeftovers(t, dir)
}

// TestWriteReadOnlyDirectory checks that a writable file in a directory that is not writable is written in place
func TestWriteReadOnlyDirectory(t *testing.T) {
	if runtime.GOOS == "windows" || os.Getuid() == 0 {
		t.Skip("directory permissions do not restrict writing on Windows and for root")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "app.yaml")
	require.NoError(t, os.WriteFile(path, []byte("old\n"), 0o600))
	require.NoError(t, os.Chmod(dir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	require.NoError(t, Write(path, []byte("new\n"), Options{Mode: 0o644}))
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "new\n", string(content))
}

// TestWriteErrors checks that failures of writing in place are returned
func TestWriteErrors(t *testing.T) {
	dir := t.TempDir()
	notADir := filepath.Join(dir, "file")
	require.NoError(t, os.WriteFile(notADir, nil, 0o600))

	// The directory does not exist, so neither a temporary file nor the file itself can be created
	assert.Error(t, Write(filepath.Join(dir, "missing", "app.yaml"), []byte("a"), Options{Mode: 0o600}))
	// A path below a file cannot be checked
	assert.Error(t, Write(filepath.Join(notADir, "app.yaml"), []byte("a"), Options{Mode: 0o600}))
}

// TestWriteSpecialFiles checks that special files are written in place without resolving their links or changing them
func TestWriteSpecialFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no special files like /dev/null on Windows")
	}
	// Only /dev/null is written, since writing e.g. /proc/self/fd/1 would truncate the output of the tests if it is redirected
	before, err := os.Stat("/dev/null")
	require.NoError(t, err)

	assert.NoError(t, Write("/dev/null", []byte("discarded"), Options{Mode: 0o600, ForceMode: true}))

	after, err := os.Stat("/dev/null")
	require.NoError(t, err)
	assert.True(t, os.SameFile(before, after))
	assert.Equal(t, before.Mode(), after.Mode())

	assert.True(t, isSpecialPath("/dev/stdout"))
	assert.True(t, isSpecialPath("/proc/self/fd/1"))
	assert.False(t, isSpecialPath("/etc/app.yaml"))
	assert.False(t, isSpecialPath("devices/app.yaml"))
}

// TestWriteNonRegularFile checks that a file that is not regular, e.g. a named pipe, is written in place
func TestWriteNonRegularFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no named pipes in the file system on Windows")
	}
	pipe := filepath.Join(t.TempDir(), "pipe")
	require.NoError(t, mkfifo(pipe))

	read := make(chan string)
	go func() {
		content, _ := os.ReadFile(pipe)
		read <- string(content)
	}()

	require.NoError(t, os.Chmod(pipe, 0o620))
	require.NoError(t, Write(pipe, []byte("through the pipe"), Options{Mode: 0o600}))
	assert.Equal(t, "through the pipe", <-read)
	info, err := os.Lstat(pipe)
	require.NoError(t, err)
	assert.Equal(t, fs.ModeNamedPipe, info.Mode().Type())
	assert.Equal(t, fs.FileMode(0o620), info.Mode().Perm(), "files that are not regular keep their mode")
}

// TestWriteDanglingSymlink checks that a link to a file that does not exist yet is kept and the file it points to is created
func TestWriteDanglingSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "data", "app.yaml")
	require.NoError(t, os.Mkdir(filepath.Dir(target), 0o700))
	link := filepath.Join(dir, "app.yaml")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symbolic links are not supported: %v", err)
	}

	require.NoError(t, Write(link, []byte("new\n"), Options{Mode: 0o640}))

	info, err := os.Lstat(link)
	require.NoError(t, err)
	assert.Equal(t, fs.ModeSymlink, info.Mode().Type(), "the link has to be kept")
	content, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, "new\n", string(content))
	assertMode(t, target, 0o640)
}

// TestWriteFailureKeepsFile checks that a failure while writing the temporary file, e.g. because the disk is full,
// keeps the existing file unchanged, removes the temporary file and is returned instead of writing in place
func TestWriteFailureKeepsFile(t *testing.T) {
	writeTemp = func(tmp *os.File, _ []byte, _ fs.FileMode) error {
		_ = tmp.Close()
		return syscall.ENOSPC
	}
	t.Cleanup(func() { writeTemp = writeTempFile })

	dir := t.TempDir()
	path := filepath.Join(dir, "app.yaml")
	require.NoError(t, os.WriteFile(path, []byte("old\n"), 0o600))

	assert.ErrorIs(t, Write(path, []byte("new\n"), Options{Mode: 0o600}), syscall.ENOSPC)
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "old\n", string(content))
	assertNoLeftovers(t, dir)
}

// TestWriteTempFileErrors checks that failures of writing and of setting the mode of the temporary file are returned
func TestWriteTempFileErrors(t *testing.T) {
	dir := t.TempDir()

	closed, err := os.CreateTemp(dir, "closed")
	require.NoError(t, err)
	require.NoError(t, closed.Close())
	assert.Error(t, writeTempFile(closed, []byte("a"), 0o600), "writing a closed file fails")

	removed, err := os.CreateTemp(dir, "removed")
	require.NoError(t, err)
	// writeTempFile closes the file, but not if the test is skipped, and Windows cannot remove open files
	t.Cleanup(func() { _ = removed.Close() })
	if runtime.GOOS != "windows" {
		// An open file cannot be removed on Windows
		require.NoError(t, os.Remove(removed.Name()))
		assert.Error(t, writeTempFile(removed, []byte("a"), 0o600), "setting the mode of a removed file fails")
	}
}

// TestErrInPlace checks that the message of an errInPlace is the one of its error
func TestErrInPlace(t *testing.T) {
	assert.EqualError(t, errInPlace{syscall.EBUSY}, syscall.EBUSY.Error())
}

// TestWriteAll checks that WriteAll writes new and existing files with the mode of their options and leaves no temporary files
func TestWriteAll(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "existing.yaml")
	require.NoError(t, os.WriteFile(existing, []byte("old\n"), 0o600))
	require.NoError(t, os.Chmod(existing, 0o604))
	created := filepath.Join(dir, "new.yaml")

	require.NoError(t, WriteAll([]File{
		{Path: existing, Data: []byte("a\n"), Options: Options{Mode: 0o644}},
		{Path: created, Data: []byte("b\n"), Options: Options{Mode: 0o640}},
	}))

	for path, want := range map[string]string{existing: "a\n", created: "b\n"} {
		content, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, want, string(content))
	}
	assertMode(t, existing, 0o604)
	assertMode(t, created, 0o640)
	assertNoLeftovers(t, dir)
	assert.NoError(t, WriteAll(nil), "writing no files does nothing")
}

// TestWriteAllFailureChangesNoFile checks that a failure while writing the temporary file of one file,
// e.g. because the disk is full, removes the temporary files of the others and changes no file
func TestWriteAllFailureChangesNoFile(t *testing.T) {
	writeTemp = func(tmp *os.File, data []byte, mode fs.FileMode) error {
		if strings.Contains(filepath.Base(tmp.Name()), "b.yaml") {
			_ = tmp.Close()
			return syscall.ENOSPC
		}
		return writeTempFile(tmp, data, mode)
	}
	t.Cleanup(func() { writeTemp = writeTempFile })

	dir := t.TempDir()
	var files []File
	for _, name := range []string{"a.yaml", "b.yaml", "c.yaml"} {
		path := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(path, []byte("old\n"), 0o600))
		files = append(files, File{Path: path, Data: []byte("new\n"), Options: Options{Mode: 0o600}})
	}

	err := WriteAll(files)
	assert.ErrorIs(t, err, syscall.ENOSPC)
	assert.ErrorContains(t, err, "failed to write "+files[1].Path+", no file has been changed: ")
	for _, f := range files {
		content, err := os.ReadFile(f.Path)
		require.NoError(t, err)
		assert.Equal(t, "old\n", string(content), f.Path)
	}
	assertNoLeftovers(t, dir)
}

// TestWriteAllWritesInPlaceLast checks that files written in place are written after the renames,
// and that an error names the files that have and have not been written
func TestWriteAllWritesInPlaceLast(t *testing.T) {
	dir := t.TempDir()
	// Links to files in a missing directory are written in place, which fails
	failing := filepath.Join(dir, "failing.yaml")
	if err := os.Symlink(filepath.Join(dir, "missing", "failing.yaml"), failing); err != nil {
		t.Skipf("symbolic links are not supported: %v", err)
	}
	skipped := filepath.Join(dir, "skipped.yaml")
	require.NoError(t, os.Symlink(filepath.Join(dir, "missing", "skipped.yaml"), skipped))
	first := filepath.Join(dir, "first.yaml")
	last := filepath.Join(dir, "last.yaml")

	opts := Options{Mode: 0o600}
	err := WriteAll([]File{
		{Path: first, Data: []byte("first\n"), Options: opts},
		{Path: failing, Data: []byte("failing\n"), Options: opts},
		{Path: skipped, Data: []byte("skipped\n"), Options: opts},
		{Path: last, Data: []byte("last\n"), Options: opts},
	})

	assert.ErrorIs(t, err, fs.ErrNotExist)
	assert.ErrorContains(t, err, fmt.Sprintf("failed to write %s (written: %s, %s; not written: %s): ", failing, first, last, skipped))
	assert.FileExists(t, first)
	assert.FileExists(t, last, "files replaced atomically are written before the files written in place")
	assertNoLeftovers(t, dir)
}

// TestWriteStatus checks the list of written and not written files in errors of WriteAll
func TestWriteStatus(t *testing.T) {
	assert.Equal(t, "", writeStatus(nil, nil))
	assert.Equal(t, " (written: a, b)", writeStatus([]string{"a", "b"}, nil))
	assert.Equal(t, " (not written: c)", writeStatus(nil, []string{"c"}))
	assert.Equal(t, " (written: a; not written: c)", writeStatus([]string{"a"}, []string{"c"}))
}

// TestWriteAllDefersFailedRenames checks that a file whose rename fails, e.g. a bind-mounted file, is written in place
// after all other files are renamed, so a failure while writing it in place cannot keep other files from being replaced
func TestWriteAllDefersFailedRenames(t *testing.T) {
	dir := t.TempDir()
	bindMounted := filepath.Join(dir, "bind-mounted.yaml")
	require.NoError(t, os.WriteFile(bindMounted, []byte("old\n"), 0o600))
	rename = func(oldpath, newpath string) error {
		if newpath == bindMounted {
			return syscall.EBUSY
		}
		return os.Rename(oldpath, newpath)
	}
	t.Cleanup(func() { rename = os.Rename })

	// A link to a file in a missing directory is written in place, which fails
	failing := filepath.Join(dir, "failing.yaml")
	if err := os.Symlink(filepath.Join(dir, "missing", "failing.yaml"), failing); err != nil {
		t.Skipf("symbolic links are not supported: %v", err)
	}
	last := filepath.Join(dir, "last.yaml")

	opts := Options{Mode: 0o600}
	err := WriteAll([]File{
		{Path: bindMounted, Data: []byte("new\n"), Options: opts},
		{Path: failing, Data: []byte("failing\n"), Options: opts},
		{Path: last, Data: []byte("last\n"), Options: opts},
	})

	// The bind-mounted file comes before the failing file in the in-place phase, as it comes first in the files
	assert.ErrorContains(t, err, fmt.Sprintf("failed to write %s (written: %s, %s): ", failing, last, bindMounted))
	for path, want := range map[string]string{bindMounted: "new\n", last: "last\n"} {
		content, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, want, string(content), path)
	}
	assertNoLeftovers(t, dir)
}
