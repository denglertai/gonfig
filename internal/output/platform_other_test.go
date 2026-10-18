//go:build !unix

package output

import "errors"

// mkfifo is not supported on other systems, whose tests using it are skipped
func mkfifo(_ string) error {
	return errors.New("named pipes are not supported")
}
