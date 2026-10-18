// Package testutil provides helpers for the tests of gonfig.
package testutil

import (
	"os"
	"runtime"
	"strings"
	"testing"
)

// envPrefix is the prefix of the environment variables configuring gonfig
const envPrefix = "GONFIG_"

// homeVariables returns the environment variables viper reads the home directory from for "$HOME" in config paths:
// HOME, and on Windows HOMEDRIVE and HOMEPATH, or USERPROFILE if they are empty
func homeVariables() []string {
	if runtime.GOOS == "windows" {
		return []string{"HOME", "USERPROFILE", "HOMEDRIVE", "HOMEPATH"}
	}
	return []string{"HOME"}
}

// homeValue returns the value of the home variable for the home directory dir.
// HOMEDRIVE and HOMEPATH are cleared, so viper uses USERPROFILE on Windows.
func homeValue(key, dir string) string {
	if key == "HOMEDRIVE" || key == "HOMEPATH" {
		return ""
	}
	return dir
}

// SetHome sets the home directory for the test on every system, see homeVariables, and restores it after the test
func SetHome(t testing.TB, dir string) {
	t.Helper()
	for _, key := range homeVariables() {
		t.Setenv(key, homeValue(key, dir))
	}
}

// IsolateFromEnvironment prepares the process for tests that must not depend on the machine running them.
// It is meant to be called once from TestMain, before the tests run:
//   - all GONFIG_* environment variables are removed, including ones of options added later
//   - the home directory (HOME, and on Windows USERPROFILE) and GONFIG_CONFIG_PATH point to the same empty directory,
//     so neither a config file in the home directory nor one in the working directory or /etc/gonfig is used
//
// Tests may still set environment variables with t.Setenv. The returned function restores the environment
// and removes the directory.
func IsolateFromEnvironment() (restore func(), err error) {
	dir, err := os.MkdirTemp("", "gonfig-test-")
	if err != nil {
		return nil, err
	}

	saved := map[string]string{}
	save := func(key string) {
		if value, found := os.LookupEnv(key); found {
			saved[key] = value
		}
	}

	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, envPrefix) {
			save(key)
			// Setting and removing variables with valid names cannot fail
			_ = os.Unsetenv(key)
		}
	}

	for _, key := range homeVariables() {
		save(key)
		_ = os.Setenv(key, homeValue(key, dir))
	}
	_ = os.Setenv(envPrefix+"CONFIG_PATH", dir)

	return func() {
		for _, key := range homeVariables() {
			_ = os.Unsetenv(key)
		}
		for _, entry := range os.Environ() {
			key, _, _ := strings.Cut(entry, "=")
			if strings.HasPrefix(key, envPrefix) {
				_ = os.Unsetenv(key)
			}
		}
		for key, value := range saved {
			_ = os.Setenv(key, value)
		}
		// A directory left behind in the temporary directory does not affect the tests
		_ = os.RemoveAll(dir)
	}, nil
}
