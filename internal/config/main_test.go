package config

import (
	"fmt"
	"os"
	"testing"

	"github.com/denglertai/gonfig/internal/testutil"
)

// TestMain runs the tests isolated from the GONFIG_* environment variables and config files of the machine
func TestMain(m *testing.M) {
	restore, err := testutil.IsolateFromEnvironment()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	code := m.Run()
	restore()
	os.Exit(code)
}
