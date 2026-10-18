// Package output writes processed config files, replacing existing files atomically where possible
package output

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/denglertai/gonfig/pkg/logging"
)

// Options controls the mode of written files
type Options struct {
	// Mode is the mode of a new file, e.g. the mode of the processed input file
	Mode fs.FileMode
	// ForceMode sets Mode for existing files as well, which keep their mode otherwise, e.g. for an explicit --mode
	ForceMode bool
}

// rename, setOwner and writeTemp are variables, so tests can simulate failures, e.g. of a rename onto a bind-mounted file,
// of keeping the owner of a file, which requires root, or of writing to a full disk
var (
	rename    = os.Rename
	setOwner  = keepOwner
	writeTemp = writeTempFile
)

// errInPlace marks failures of an atomic write after which the file is written in place instead,
// e.g. if the directory is not writable or the file is a bind mount, which cannot be replaced by a rename
type errInPlace struct {
	err error
}

// Error returns the message of the wrapped error
func (e errInPlace) Error() string {
	return e.err.Error()
}

// Write writes the data to the file at path.
//
// An existing regular file is replaced atomically: the data is written to a temporary file in the same directory,
// which then replaces the file by a rename, so the file contains either the old or the complete new data,
// e.g. if writing fails because the disk is full or the process is killed.
// Symbolic links are resolved, so the file they point to is replaced and the link is kept.
// The file of a link pointing to a file that does not exist yet is created in place.
// If the temporary file cannot be created, its owner cannot be set to the owner of the existing file,
// or the rename fails, the file is written in place instead, which works for bind-mounted files as well.
// Files that are not regular and paths of special files like /dev/stdout are always written in place.
//
// New files get opts.Mode. Existing files keep their mode, unless opts.ForceMode is set.
func Write(path string, data []byte, opts Options) error {
	// Special files like /dev/stdout are links to the file descriptors of the process, e.g. /proc/self/fd/1,
	// which resolve to the file the output is redirected to. Replacing that file would drop its other content.
	// Their mode is kept as well, as before atomic writes.
	if isSpecialPath(path) {
		return os.WriteFile(path, data, opts.Mode)
	}

	target := path
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		target = resolved
	} else if info, err := os.Lstat(path); err == nil && info.Mode()&fs.ModeSymlink != 0 {
		// The link points to a file that does not exist yet, which writing in place creates, keeping the link
		return writeInPlace(path, data, nil, opts)
	}

	existing, err := os.Stat(target)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if existing != nil && !existing.Mode().IsRegular() {
		return writeInPlace(target, data, existing, opts)
	}

	mode := opts.Mode
	if existing != nil && !opts.ForceMode {
		mode = existing.Mode().Perm()
	}

	err = writeAtomic(target, data, mode, existing)
	var inPlace errInPlace
	if errors.As(err, &inPlace) {
		logging.Debug("Writing the file in place, since it cannot be replaced atomically", "file", target, "reason", inPlace.err)
		return writeInPlace(target, data, existing, opts)
	}
	return err
}

// writeAtomic writes the data to a temporary file next to target and renames it to target.
// Failures after which writing in place may still succeed are returned as errInPlace.
// Other failures, e.g. a full disk while writing, are returned as they are, since writing in place would fail as well.
func writeAtomic(target string, data []byte, mode fs.FileMode, existing fs.FileInfo) (err error) {
	// The temporary file is hidden and named after the target, so leftovers are recognizable
	tmp, err := os.CreateTemp(filepath.Dir(target), "."+filepath.Base(target)+".gonfig-*")
	if err != nil {
		return errInPlace{err}
	}
	renamed := false
	defer func() {
		if !renamed {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()

	if err := writeTemp(tmp, data, mode); err != nil {
		return err
	}
	if existing != nil {
		if err := setOwner(tmp.Name(), existing); err != nil {
			return errInPlace{fmt.Errorf("cannot keep the owner of the file: %w", err)}
		}
	}

	if err := rename(tmp.Name(), target); err != nil {
		return errInPlace{err}
	}
	renamed = true

	syncDir(filepath.Dir(target))
	return nil
}

// writeTempFile writes the data to the temporary file, syncs and closes it and sets its mode
func writeTempFile(tmp *os.File, data []byte, mode fs.FileMode) error {
	_, err := tmp.Write(data)
	// The data has to be on disk before the rename, otherwise a crash may leave an empty file.
	// Close reports errors of writing as well, so it is checked like Sync.
	if err := errors.Join(err, tmp.Sync(), tmp.Close()); err != nil {
		return err
	}
	// Unlike creating a file, Chmod is not restricted by the umask, so the file gets exactly the mode
	return os.Chmod(tmp.Name(), mode)
}

// writeInPlace truncates and writes the file, which keeps the owner and mode of an existing file.
// New files and, with opts.ForceMode, existing regular files get exactly opts.Mode, regardless of the umask.
func writeInPlace(target string, data []byte, existing fs.FileInfo, opts Options) error {
	// os.WriteFile also reports errors on closing the file, unlike a deferred Close
	if err := os.WriteFile(target, data, opts.Mode); err != nil {
		return err
	}

	// A new file has been created as a regular file by WriteFile. Files that are not regular, e.g. named pipes, keep their mode.
	if existing == nil || (opts.ForceMode && existing.Mode().IsRegular()) {
		return os.Chmod(target, opts.Mode)
	}
	return nil
}
