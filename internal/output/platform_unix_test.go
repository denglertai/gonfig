//go:build unix

package output

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mkfifo creates a named pipe
func mkfifo(path string) error {
	return syscall.Mkfifo(path, 0o600)
}

// TestKeepOwner checks that the owner of a file owned by the current user is kept without changing it,
// and that another owner fails without root
func TestKeepOwner(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.yaml")
	require.NoError(t, os.WriteFile(path, nil, 0o600))
	info, err := os.Stat(path)
	require.NoError(t, err)

	assert.NoError(t, keepOwner(path, info))

	if os.Getuid() == 0 {
		t.Skip("root may change the owner of any file")
	}
	other := &fakeInfo{FileInfo: info, sys: &syscall.Stat_t{Uid: uint32(os.Getuid() + 1), Gid: uint32(os.Getgid())}}
	assert.Error(t, keepOwner(path, other))
	assert.NoError(t, keepOwner(path, &fakeInfo{FileInfo: info, sys: nil}), "files without Unix owner are kept as they are")
}

// fakeInfo is file info with another owner
type fakeInfo struct {
	os.FileInfo
	sys any
}

// Sys returns the fake system specific info
func (f *fakeInfo) Sys() any {
	return f.sys
}
