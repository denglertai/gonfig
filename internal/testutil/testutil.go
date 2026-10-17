// Package testutil provides helpers for the tests of gonfig.
package testutil

import (
	"os"
	"strings"
)

// envPrefix is the prefix of the environment variables configuring gonfig
const envPrefix = "GONFIG_"

// IsolateFromEnvironment prepares the process for tests that must not depend on the machine running them.
// It is meant to be called once from TestMain, before the tests run:
//   - all GONFIG_* environment variables are removed, including ones of options added later
//   - HOME and GONFIG_CONFIG_PATH point to the same empty directory, so neither a config file in the home directory
//     nor one in the working directory or /etc/gonfig is used
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
			os.Unsetenv(key)
		}
	}

	save("HOME")
	os.Setenv("HOME", dir)
	os.Setenv(envPrefix+"CONFIG_PATH", dir)

	return func() {
		os.Unsetenv("HOME")
		for _, entry := range os.Environ() {
			key, _, _ := strings.Cut(entry, "=")
			if strings.HasPrefix(key, envPrefix) {
				os.Unsetenv(key)
			}
		}
		for key, value := range saved {
			os.Setenv(key, value)
		}
		os.RemoveAll(dir)
	}, nil
}
