//go:build unix

package output

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// isSpecialPath returns whether the path refers to a special file like /dev/stdout or /proc/self/fd/1,
// which are written in place without resolving their links
func isSpecialPath(path string) bool {
	// Abs only fails if the working directory is unknown, so the path is relative and not special then
	abs, _ := filepath.Abs(path)
	return strings.HasPrefix(abs, "/dev/") || strings.HasPrefix(abs, "/proc/")
}

// keepOwner sets the owner and group of the file to those of the existing file, so replacing a file does not change them,
// e.g. if root renders the config of an application running as another user. It fails if this is not permitted.
func keepOwner(name string, existing fs.FileInfo) error {
	stat, ok := existing.Sys().(*syscall.Stat_t)
	if !ok {
		return nil
	}
	if int(stat.Uid) == os.Getuid() && int(stat.Gid) == os.Getgid() {
		// The new file belongs to the current user and group already
		return nil
	}
	return os.Chown(name, int(stat.Uid), int(stat.Gid))
}

// syncDir syncs the directory, so the rename of a file within it is on disk as well.
// Errors are ignored, since the file has been replaced already and not all file systems support it.
func syncDir(dir string) {
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
}
