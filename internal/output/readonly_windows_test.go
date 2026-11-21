//go:build windows

package output

import (
	"bytes"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// windowsReadOnlyFile creates a file with the read-only attribute, which os.Chmod sets by removing the write bits
func windowsReadOnlyFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	require.NoError(t, os.Chmod(path, 0o444))
	// The attribute is removed again, so the directory of the test can be cleaned up
	t.Cleanup(func() { _ = os.Chmod(path, 0o666) })
}

// windowsWriteOptions are the options of files written back with -i
func windowsWriteOptions(ignoreReadOnly bool) Options {
	return Options{Mode: 0o600, SkipUnchanged: true, CheckWritable: true, IgnoreReadOnly: ignoreReadOnly}
}

// TestIsReadOnlyErrWindows checks that a permission error of a file with the read-only attribute is a read-only error,
// but not one of a writable file, of a missing file or of another kind
func TestIsReadOnlyErrWindows(t *testing.T) {
	dir := t.TempDir()
	readOnly := filepath.Join(dir, "ro.yaml")
	windowsReadOnlyFile(t, readOnly, "a: 1\n")
	writable := filepath.Join(dir, "rw.yaml")
	require.NoError(t, os.WriteFile(writable, []byte("a: 1\n"), 0o644))

	roInfo, err := os.Stat(readOnly)
	require.NoError(t, err)
	rwInfo, err := os.Stat(writable)
	require.NoError(t, err)
	assert.Zero(t, roInfo.Mode().Perm()&0o200, "the read-only attribute has no write bit")

	// Opening the file for writing fails like the writable check of WriteAll
	_, openErr := os.OpenFile(readOnly, os.O_WRONLY, 0)
	require.Error(t, openErr)
	assert.True(t, isReadOnlyErr(openErr, roInfo))
	assert.False(t, isReadOnlyErr(openErr, rwInfo), "a writable file has no read-only error")
	assert.False(t, isReadOnlyErr(openErr, nil), "a new file has no read-only error")
	assert.False(t, isReadOnlyErr(&fs.PathError{Op: "open", Path: readOnly, Err: fs.ErrNotExist}, roInfo))
	assert.False(t, isReadOnlyErr(nil, roInfo))
}

// TestWriteAllWindowsReadOnlyAttribute checks that WriteAll skips a file with the read-only attribute with a warning
// if IgnoreReadOnly is set, still writing the other file, and fails before writing anything otherwise
func TestWriteAllWindowsReadOnlyAttribute(t *testing.T) {
	var logs bytes.Buffer
	defaultLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(defaultLogger) })

	dir := t.TempDir()
	writable := filepath.Join(dir, "a.yaml")
	readOnly := filepath.Join(dir, "b.yaml")
	require.NoError(t, os.WriteFile(writable, []byte("a: ${A}\n"), 0o644))
	windowsReadOnlyFile(t, readOnly, "b: ${B}\n")

	files := func(ignoreReadOnly bool) []File {
		return []File{
			{Path: writable, Data: []byte("a: made-up-secret\n"), Options: windowsWriteOptions(ignoreReadOnly)},
			{Path: readOnly, Data: []byte("b: made-up-secret\n"), Options: windowsWriteOptions(ignoreReadOnly)},
		}
	}

	// Without IgnoreReadOnly, nothing is written
	err := WriteAll(files(false))
	require.Error(t, err)
	assert.Contains(t, err.Error(), readOnly)
	content, err := os.ReadFile(writable)
	require.NoError(t, err)
	assert.Equal(t, "a: ${A}\n", string(content), "the writable file is untouched if another file fails")
	content, err = os.ReadFile(readOnly)
	require.NoError(t, err)
	assert.Equal(t, "b: ${B}\n", string(content))
	assert.NotContains(t, logs.String(), "Skipping read-only file")

	// With IgnoreReadOnly, the read-only file is skipped and the other one written
	require.NoError(t, WriteAll(files(true)))
	content, err = os.ReadFile(writable)
	require.NoError(t, err)
	assert.Equal(t, "a: made-up-secret\n", string(content))
	content, err = os.ReadFile(readOnly)
	require.NoError(t, err)
	assert.Equal(t, "b: ${B}\n", string(content))
	info, err := os.Stat(readOnly)
	require.NoError(t, err)
	assert.Zero(t, info.Mode().Perm()&0o200, "the skipped file keeps its read-only attribute")

	assert.Contains(t, logs.String(), `"level":"WARN","msg":"Skipping read-only file","path":`)
	assert.Contains(t, logs.String(), `b.yaml`)
	assert.NotContains(t, logs.String(), "made-up-secret")

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Len(t, entries, 2, "no temporary files are left")
}

// TestWriteAllWindowsUnchangedReadOnlyFile checks that an unchanged file with the read-only attribute is neither
// written nor reported
func TestWriteAllWindowsUnchangedReadOnlyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.yaml")
	windowsReadOnlyFile(t, path, "a: 1\n")

	opts := windowsWriteOptions(false)
	opts.Unchanged = true
	require.NoError(t, WriteAll([]File{{Path: path, Data: []byte("a: 1\n"), Options: opts}}))

	// The same content is unchanged without the caller saying so
	require.NoError(t, WriteAll([]File{{Path: path, Data: []byte("a: 1\n"), Options: windowsWriteOptions(false)}}))
}

// TestWriteAllWindowsModeOfUnchangedFiles checks that --mode changes an unchanged file only if the owner's write bit
// differs, the only bit Windows applies, so a read-only file keeps its attribute with 0444 and loses it with 0600,
// while a writable file needs no change for 0600
func TestWriteAllWindowsModeOfUnchangedFiles(t *testing.T) {
	tests := []struct {
		name         string
		readOnly     bool
		mode         fs.FileMode
		wantChmod    bool
		wantReadOnly bool
	}{
		{name: "read-only file with 0444", readOnly: true, mode: 0o444, wantChmod: false, wantReadOnly: true},
		{name: "read-only file with 0600", readOnly: true, mode: 0o600, wantChmod: true, wantReadOnly: false},
		{name: "writable file with 0600", readOnly: false, mode: 0o600, wantChmod: false, wantReadOnly: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "a.yaml")
			if tt.readOnly {
				windowsReadOnlyFile(t, path, "a: 1\n")
			} else {
				require.NoError(t, os.WriteFile(path, []byte("a: 1\n"), 0o666))
			}
			// Only changing the mode counts, not setting the mode the file has while checking it
			var changed []fs.FileMode
			chmod = func(name string, mode fs.FileMode) error {
				if mode == tt.mode {
					changed = append(changed, mode)
				}
				return os.Chmod(name, mode)
			}
			t.Cleanup(func() { chmod = os.Chmod })

			opts := windowsWriteOptions(false)
			opts.Unchanged, opts.Mode, opts.ForceMode = true, tt.mode, true
			require.NoError(t, WriteAll([]File{{Path: path, Data: []byte("a: 1\n"), Options: opts}}))

			assert.Equal(t, tt.wantChmod, len(changed) > 0)
			info, err := os.Stat(path)
			require.NoError(t, err)
			assert.Equal(t, tt.wantReadOnly, info.Mode().Perm()&0o200 == 0)
		})
	}
}
