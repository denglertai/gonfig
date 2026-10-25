package plugin

import (
	"os"
	"path/filepath"
	"testing"
)

// TestInitPluginsMissingPath checks that a plugin path that does not exist is skipped
func TestInitPluginsMissingPath(t *testing.T) {
	t.Cleanup(ClosePlugins)
	// Must not panic or fail when the plugin path does not exist
	InitPlugins(filepath.Join(t.TempDir(), "missing"))
}

// TestInitPluginsInvalidPlugins checks that invalid Go plugins are logged and skipped
func TestInitPluginsInvalidPlugins(t *testing.T) {
	t.Cleanup(ClosePlugins)
	dir := t.TempDir()
	for _, name := range []string{"a.so", "b.so", "readme.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("not a plugin"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Invalid plugins are logged and skipped
	InitPlugins(dir)
}
