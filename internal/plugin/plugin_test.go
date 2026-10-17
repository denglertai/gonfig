package plugin

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInitPluginsMissingPath(t *testing.T) {
	// Must not panic or fail when the plugin path does not exist
	InitPlugins(filepath.Join(t.TempDir(), "missing"))
}

func TestInitPluginsInvalidPlugins(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.so", "b.so", "readme.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("not a plugin"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Invalid plugins are logged and skipped
	InitPlugins(dir)
}
