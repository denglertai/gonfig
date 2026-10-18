//go:build !unix

package output

import "io/fs"

// keepOwner does nothing, since files on other systems, e.g. Windows, have no Unix owner to keep
func keepOwner(_ string, _ fs.FileInfo) error {
	return nil
}

// syncDir does nothing, since directories cannot be synced on other systems, e.g. Windows
func syncDir(_ string) {}

// isSpecialPath returns false, since other systems, e.g. Windows, have no special files that are links to file descriptors
func isSpecialPath(_ string) bool {
	return false
}
