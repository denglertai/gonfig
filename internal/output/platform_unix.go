//go:build unix

package output

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// modeBits are the permission bits os.Chmod applies, which are all of them on Unix
const modeBits = fs.ModePerm

// nonBlock opens files without blocking, e.g. a named pipe without a reader or writer, which has no effect on regular files
const nonBlock = syscall.O_NONBLOCK

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
	if d, err := os.Open(dir); err == nil { //nolint:gosec // the directory of an output file given by the user
		_ = d.Sync()
		_ = d.Close()
	}
}

// isReadOnlyErr returns whether the error is caused by a read-only file system, e.g. a read-only bind mount or a
// Kubernetes ConfigMap. Files without write permission are no read-only error, since they can still be replaced by a
// rename if their directory is writable.
func isReadOnlyErr(err error, _ fs.FileInfo) bool {
	return errors.Is(err, syscall.EROFS)
}
