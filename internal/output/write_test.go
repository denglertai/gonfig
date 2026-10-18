package output

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
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
			name: "rename fails, e.g. for a bind mounted file",
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
