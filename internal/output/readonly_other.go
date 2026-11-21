//go:build !unix && !windows

package output

import "io/fs"

// modeBits are the permission bits compared before changing the mode of a file, which are all of them on other systems
const modeBits = fs.ModePerm

// isReadOnlyErr returns false, since other systems, e.g. WebAssembly, report no read-only errors gonfig can detect
func isReadOnlyErr(_ error, _ fs.FileInfo) bool {
	return false
}
