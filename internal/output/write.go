// Package output writes processed config files, replacing existing files atomically where possible
package output

import (
	"bytes"
	"errors"
	"fmt"
	"io"
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
	// SkipUnchanged leaves an existing file alone if it is unchanged, i.e. Unchanged is set or the file already contains
	// the data, so files that need no rendering can stay read-only and keep their mtime, e.g. for -i.
	// With ForceMode, only the mode of an unchanged file is changed, if it differs.
	SkipUnchanged bool
	// Unchanged is set by the caller if processing changed no value of the file, see SkipUnchanged
	Unchanged bool
	// CheckWritable opens existing files for writing before WriteAll writes any file, so a read-only file fails
	// before other files have been replaced, e.g. for -i
	CheckWritable bool
	// IgnoreReadOnly skips files that cannot be written because they are read-only, logging a warning instead of
	// failing, see isReadOnlyErr
	IgnoreReadOnly bool
}

// rename, setOwner, writeTemp, writeFile, openWritable and chmod are variables, so tests can simulate failures,
// e.g. of a rename onto a bind-mounted file, of keeping the owner of a file, which requires root, of writing to a full
// disk or of writing to a read-only file system
var (
	rename       = os.Rename
	setOwner     = keepOwner
	writeTemp    = writeTempFile
	writeFile    = os.WriteFile
	openWritable = openWritableFile
	chmod        = os.Chmod
)

// openWritableFile opens the existing file for writing without truncating it, so checking it changes nothing.
// It doesn't block, e.g. if the file has been replaced by a named pipe without a reader since it was checked.
func openWritableFile(name string) (*os.File, error) {
	return os.OpenFile(name, os.O_WRONLY|nonBlock, 0) //nolint:gosec // an output file given by the user
}

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
//
// Write writes a single file, so it ignores opts.SkipUnchanged, opts.CheckWritable and opts.IgnoreReadOnly,
// which only WriteAll handles.
func Write(path string, data []byte, opts Options) error {
	opts.SkipUnchanged, opts.CheckWritable, opts.IgnoreReadOnly = false, false, false
	p, err := prepare(path, data, opts)
	if err != nil {
		return err
	}
	return p.commit()
}

// WriteAll writes several files like Write, in two phases, so a failure leaves as few files changed as possible.
//
// First, the temporary files of all files are written. If one of them fails, e.g. because the disk is full,
// all temporary files are removed and no file is changed. With CheckWritable, existing files are opened for writing
// in this phase as well, so a read-only file fails before any file is changed.
// Then the temporary files are renamed to their files, and finally the files that cannot be replaced atomically
// are written in place. Errors of these phases name the files that have been written already.
//
// With SkipUnchanged, unchanged files are not written, see Options. With IgnoreReadOnly, read-only files are
// skipped with a warning and the other files are still written.
func WriteAll(files []File) error {
	prepared := make([]*pending, 0, len(files))
	for _, f := range files {
		p, err := prepare(f.Path, f.Data, f.Options)
		if err != nil {
			for _, p := range prepared {
				p.discard()
			}
			// The check of an unchanged file fails only for the change of its mode, which isn't written
			action := "write"
			if p != nil && p.unchanged {
				action = "change the mode of"
			}
			return fmt.Errorf("failed to %s %s, no file has been changed: %w", action, f.Path, err)
		}
		prepared = append(prepared, p)
	}

	// All renames happen before any file is written in place, since writing in place may leave partial output if it fails.
	// Files whose rename fails, e.g. bind-mounted files, are written in place as well.
	written := make([]string, 0, len(prepared))
	var skipped []string
	var inPlace []*pending
	for i, p := range prepared {
		switch {
		case p.skip != nil:
			p.warnSkipped(p.skip)
			skipped = append(skipped, p.path)
		case p.unchanged:
			// Changing only the mode doesn't write the file, so it isn't listed as written in errors.
			// Read-only files were found by prepare already, unless they became read-only meanwhile.
			err := p.updateMode()
			if err != nil && !p.ignoresReadOnly(err) {
				for _, rest := range prepared[i+1:] {
					rest.discard()
				}
				return fmt.Errorf("failed to change the mode of %s%s: %w", p.path, writeStatus(written, skipped, notWrittenPaths(prepared[i+1:], inPlace)), err)
			}
			if err != nil {
				p.warnSkipped(err)
				skipped = append(skipped, p.path)
			}
		case p.writesInPlace() || !p.rename():
			inPlace = append(inPlace, p)
		default:
			written = append(written, p.path)
		}
	}

	for i, p := range inPlace {
		err := p.writeInPlace()
		if err != nil && p.ignoresReadOnly(err) {
			// The file was writable when it was checked, but became read-only meanwhile
			p.warnSkipped(err)
			skipped = append(skipped, p.path)
			continue
		}
		if err != nil {
			return fmt.Errorf("failed to write %s%s: %w", p.path, writeStatus(written, skipped, notWrittenPaths(nil, inPlace[i+1:])), err)
		}
		written = append(written, p.path)
	}
	return nil
}

// notWrittenPaths returns the paths of the files of WriteAll that have not been written yet: the remaining prepared
// files, which are written unless unchanged or skipped, and the files deferred to be written in place
func notWrittenPaths(remaining []*pending, inPlace []*pending) []string {
	var paths []string
	for _, p := range inPlace {
		paths = append(paths, p.path)
	}
	for _, p := range remaining {
		if p.skip == nil && !p.unchanged {
			paths = append(paths, p.path)
		}
	}
	return paths
}

// warnSkipped logs that a read-only file is skipped, or only the change of its mode for an unchanged file, so it is
// clear the requested mode hasn't been applied. Only the OS error is logged, e.g. "read-only file system", without
// the path it contains, which is logged on its own, and never the data of the file.
func (p *pending) warnSkipped(err error) {
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) {
		err = pathErr.Err
	}
	if p.unchanged {
		logging.Warn("Skipping mode change of read-only file", "path", p.path, "mode", fmt.Sprintf("%#o", p.opts.Mode), "error", err.Error())
		return
	}
	logging.Warn("Skipping read-only file", "path", p.path, "error", err.Error())
}

// writeStatus lists the files that have been written, skipped and not written,
// e.g. " (written: a.yaml; skipped: b.yaml; not written: c.yaml)"
func writeStatus(written []string, skipped []string, notWritten []string) string {
	var parts []string
	if len(written) > 0 {
		parts = append(parts, "written: "+strings.Join(written, ", "))
	}
	if len(skipped) > 0 {
		parts = append(parts, "skipped: "+strings.Join(skipped, ", "))
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
	// unchanged is set if the file is not written, since it is unchanged, see Options.SkipUnchanged
	unchanged bool
	// skip is the error of a read-only file that is not written, see Options.IgnoreReadOnly
	skip error
}

// prepare writes the temporary file of the file at path, or prepares writing it in place, see Write.
// If checking whether the file can be written fails, the file is returned with the error, so the error can name
// what was going to be changed.
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
		if opts.SkipUnchanged && (opts.Unchanged || sameContent(p.target, existing, data)) {
			logging.Debug("File unchanged, not writing it", "file", path)
			p.unchanged = true
			// The mode of an unchanged file is still changed, so a read-only file fails before any file is changed as well
			if opts.CheckWritable && p.changesMode() {
				return p.checked(checkModeChangeable(p.target, existing), existing)
			}
			return p, nil
		}
		if !existing.Mode().IsRegular() {
			return p, nil
		}
		if opts.CheckWritable {
			checked, err := p.checked(checkWritable(p.target), existing)
			if err != nil || checked.skip != nil {
				return checked, err
			}
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

// checked returns the result of checking whether the file can be written: the error of a read-only file, or the file
// marked as skipped with IgnoreReadOnly. Other errors are ignored, since writing the file may still succeed,
// e.g. replacing a file without write permission by a rename.
func (p *pending) checked(err error, existing fs.FileInfo) (*pending, error) {
	if err == nil || !isReadOnlyErr(err, existing) {
		return p, nil
	}
	if !p.opts.IgnoreReadOnly {
		// The file is returned with the error, so the error can tell whether it was going to be written
		return p, err
	}
	p.skip = err
	return p, nil
}

// checkModeChangeable sets the mode the existing file has already, which changes nothing, but fails like changing it
// would, e.g. on a read-only file system. Unlike opening the file for writing, it succeeds for a file with the
// read-only attribute on Windows, whose mode can still be changed.
func checkModeChangeable(target string, existing fs.FileInfo) error {
	return chmod(target, existing.Mode()&(fs.ModePerm|fs.ModeSetuid|fs.ModeSetgid|fs.ModeSticky))
}

// sameContent returns whether the existing regular file contains exactly the data.
// Files of another size differ, so they are not read. A file that cannot be read is written as if it differed.
// The file is opened without blocking and checked again, so a named pipe swapped in since it was checked isn't read.
func sameContent(target string, existing fs.FileInfo, data []byte) bool {
	if !existing.Mode().IsRegular() || existing.Size() != int64(len(data)) {
		return false
	}
	f, err := os.OpenFile(target, os.O_RDONLY|nonBlock, 0) //nolint:gosec // an output file given by the user
	if err != nil {
		return false
	}
	// The file is only read, so an error closing it does not affect the result
	defer func() { _ = f.Close() }()
	if info, err := f.Stat(); err != nil || !info.Mode().IsRegular() {
		return false
	}
	content, err := io.ReadAll(f)
	return err == nil && bytes.Equal(content, data)
}

// checkWritable opens the existing file for writing and closes it again, without changing it
func checkWritable(target string) error {
	f, err := openWritable(target)
	if err != nil {
		return err
	}
	return f.Close()
}

// ignoresReadOnly returns whether the error is a read-only error that is ignored, see Options.IgnoreReadOnly
func (p *pending) ignoresReadOnly(err error) bool {
	return p.opts.IgnoreReadOnly && !p.special && isReadOnlyErr(err, p.existing)
}

// changesMode returns whether the mode of an unchanged file is changed, i.e. ForceMode sets another mode of a regular file.
// Only the bits the system applies are compared, see modeBits, so e.g. a read-only file on Windows stays untouched
// with --mode 0444.
func (p *pending) changesMode() bool {
	return p.opts.ForceMode && p.existing.Mode().IsRegular() && (p.existing.Mode().Perm()^p.opts.Mode)&modeBits != 0
}

// updateMode sets the mode of an unchanged regular file to the mode of ForceMode if it differs, without writing it
func (p *pending) updateMode() error {
	if !p.changesMode() {
		return nil
	}
	return chmod(p.target, p.opts.Mode)
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
		return writeFile(p.target, p.data, p.opts.Mode)
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
	if err := writeFile(target, data, opts.Mode); err != nil {
		return err
	}

	// A new file has been created as a regular file by WriteFile. Files that are not regular, e.g. named pipes, keep their mode.
	if existing == nil || (opts.ForceMode && existing.Mode().IsRegular()) {
		return chmod(target, opts.Mode)
	}
	return nil
}
