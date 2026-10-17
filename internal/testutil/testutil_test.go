package testutil

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestIsolateFromEnvironment checks that GONFIG_* variables are removed, HOME and the config path point to
// empty directories, and that everything is restored afterwards
func TestIsolateFromEnvironment(t *testing.T) {
	t.Setenv("GONFIG_LOG_LEVEL", "debug")
	t.Setenv("GONFIG_SOME_FUTURE_OPTION", "true")
	t.Setenv("HOME", "/original/home")
	// t.Setenv restores the variable after the test, which os.Unsetenv alone would not
	t.Setenv("GONFIG_CONFIG_PATH", "")
	assert.NoError(t, os.Unsetenv("GONFIG_CONFIG_PATH"))

	restore, err := IsolateFromEnvironment()
	assert.NoError(t, err)

	_, found := os.LookupEnv("GONFIG_LOG_LEVEL")
	assert.False(t, found)
	_, found = os.LookupEnv("GONFIG_SOME_FUTURE_OPTION")
	assert.False(t, found)

	for _, key := range []string{"HOME", "GONFIG_CONFIG_PATH"} {
		dir := os.Getenv(key)
		entries, err := os.ReadDir(dir)
		assert.NoError(t, err, key)
		assert.Empty(t, entries, key)
	}
	home := os.Getenv("HOME")

	restore()

	assert.Equal(t, "debug", os.Getenv("GONFIG_LOG_LEVEL"))
	assert.Equal(t, "true", os.Getenv("GONFIG_SOME_FUTURE_OPTION"))
	assert.Equal(t, "/original/home", os.Getenv("HOME"))
	_, found = os.LookupEnv("GONFIG_CONFIG_PATH")
	assert.False(t, found)
	_, err = os.Stat(home)
	assert.True(t, os.IsNotExist(err))
}

// TestIsolateFromEnvironmentError checks that an error creating the directory is returned
func TestIsolateFromEnvironmentError(t *testing.T) {
	t.Setenv("TMPDIR", "/nonexistent/gonfig-test")

	_, err := IsolateFromEnvironment()
	assert.Error(t, err)
}
