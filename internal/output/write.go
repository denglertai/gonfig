// Package output writes processed config files, replacing existing files atomically where possible
package output

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

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

// File is a file written by WriteAll
type File struct {
	// Path is the path of the file, which may be a symbolic link
	Path string
	// Data is the new content of the file
	Data []byte
	// Options controls the mode of the file
	Options Options
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
	p, err := prepare(path, data, opts)
	if err != nil {
		return err
	}
	return p.commit()
}

// WriteAll writes several files like Write, in two phases, so a failure leaves as few files changed as possible.
//
// First, the temporary files of all files are written. If one of them fails, e.g. because the disk is full,
// all temporary files are removed and no file is changed.
// Then the temporary files are renamed to their files, and finally the files that cannot be replaced atomically
// are written in place. Errors of these phases name the files that have been written already.
func WriteAll(files []File) error {
	prepared := make([]*pending, 0, len(files))
	for _, f := range files {
		p, err := prepare(f.Path, f.Data, f.Options)
		if err != nil {
			for _, p := range prepared {
				p.discard()
			}
			return fmt.Errorf("failed to write %s, no file has been changed: %w", f.Path, err)
		}
		prepared = append(prepared, p)
	}

	// All renames happen before any file is written in place, since writing in place may leave partial output if it fails.
	// Files whose rename fails, e.g. bind-mounted files, are written in place as well.
	written := make([]string, 0, len(prepared))
	var inPlace []*pending
	for _, p := range prepared {
		if p.writesInPlace() || !p.rename() {
			inPlace = append(inPlace, p)
			continue
		}
		written = append(written, p.path)
	}

	for i, p := range inPlace {
		if err := p.writeInPlace(); err != nil {
			notWritten := make([]string, 0, len(inPlace)-i-1)
			for _, rest := range inPlace[i+1:] {
				notWritten = append(notWritten, rest.path)
			}
			return fmt.Errorf("failed to write %s%s: %w", p.path, writeStatus(written, notWritten), err)
		}
		written = append(written, p.path)
	}
	return nil
}

// writeStatus lists the files that have and have not been written, e.g. " (written: a.yaml; not written: c.yaml)"
func writeStatus(written []string, notWritten []string) string {
	var parts []string
	if len(written) > 0 {
		parts = append(parts, "written: "+strings.Join(written, ", "))
	}
	if len(notWritten) > 0 {
		parts = append(parts, "not written: "+strings.Join(notWritten, ", "))
	}
	if len(parts) == 0 {
		return ""
	}
	return " (" + strings.Join(parts, "; ") + ")"
}

// pending is a file prepared for writing: either a temporary file next to its target, which commit renames to it,
// or a file that commit writes in place, since it cannot be replaced atomically
type pending struct {
	// path is the path the file was given by, which is used in errors
	path string
	// target is the file written, i.e. path with symbolic links resolved
	target   string
	data     []byte
	existing fs.FileInfo
	opts     Options
	// special is set for special files like /dev/stdout, which are written in place and keep their mode
	special bool
	// tmp is the temporary file, which is empty for files written in place
	tmp string
}

// prepare writes the temporary file of the file at path, or prepares writing it in place, see Write
func prepare(path string, data []byte, opts Options) (*pending, error) {
	p := &pending{path: path, target: path, data: data, opts: opts}

	// Special files like /dev/stdout are links to the file descriptors of the process, e.g. /proc/self/fd/1,
	// which resolve to the file the output is redirected to. Replacing that file would drop its other content.
	// Their mode is kept as well, as before atomic writes.
	if isSpecialPath(path) {
		p.special = true
		return p, nil
	}

	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		p.target = resolved
	} else if info, err := os.Lstat(path); err == nil && info.Mode()&fs.ModeSymlink != 0 {
		// The link points to a file that does not exist yet, which writing in place creates, keeping the link
		return p, nil
	}

	existing, err := os.Stat(p.target)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	if existing != nil {
		p.existing = existing
		if !existing.Mode().IsRegular() {
			return p, nil
		}
	}

	mode := opts.Mode
	if existing != nil && !opts.ForceMode {
		mode = existing.Mode().Perm()
	}

	p.tmp, err = writeAtomicTemp(p.target, data, mode, p.existing)
	var inPlace errInPlace
	if errors.As(err, &inPlace) {
		logging.Debug("Writing the file in place, since it cannot be replaced atomically", "file", p.target, "reason", inPlace.err)
		return p, nil
	}
	if err != nil {
		return nil, err
	}
	return p, nil
}

// writesInPlace returns whether commit writes the file in place instead of renaming a temporary file
func (p *pending) writesInPlace() bool {
	return p.tmp == ""
}

// commit renames the temporary file to the target, or writes the target in place if there is no temporary file
// or the rename fails, e.g. for a bind-mounted file
func (p *pending) commit() error {
	if !p.writesInPlace() && p.rename() {
		return nil
	}
	return p.writeInPlace()
}

// rename renames the temporary file to the target and returns whether it succeeded.
// If it fails, e.g. for a bind-mounted file, the temporary file is removed, so the file is written in place instead.
func (p *pending) rename() bool {
	if err := rename(p.tmp, p.target); err != nil {
		p.discard()
		logging.Debug("Writing the file in place, since it cannot be replaced atomically", "file", p.target, "reason", err)
		return false
	}
	p.tmp = ""

	syncDir(filepath.Dir(p.target))
	return true
}

// writeInPlace writes the target in place, keeping the mode of special files like /dev/stdout
func (p *pending) writeInPlace() error {
	if p.special {
		return os.WriteFile(p.target, p.data, p.opts.Mode)
	}
	return writeInPlace(p.target, p.data, p.existing, p.opts)
}

// discard removes the temporary file, if any
func (p *pending) discard() {
	if p.tmp != "" {
		_ = os.Remove(p.tmp)
		p.tmp = ""
	}
}

// writeAtomicTemp writes the data to a temporary file next to target and returns its name, so it can replace target by a rename.
// Failures after which writing in place may still succeed are returned as errInPlace.
// Other failures, e.g. a full disk while writing, are returned as they are, since writing in place would fail as well.
func writeAtomicTemp(target string, data []byte, mode fs.FileMode, existing fs.FileInfo) (name string, err error) {
	// The temporary file is hidden and named after the target, so leftovers are recognizable
	tmp, err := os.CreateTemp(filepath.Dir(target), "."+filepath.Base(target)+".gonfig-*")
	if err != nil {
		return "", errInPlace{err}
	}
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()

	if err := writeTemp(tmp, data, mode); err != nil {
		return "", err
	}
	if existing != nil {
		if err := setOwner(tmp.Name(), existing); err != nil {
			return "", errInPlace{fmt.Errorf("cannot keep the owner of the file: %w", err)}
		}
	}
	return tmp.Name(), nil
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
