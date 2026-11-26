package plugin

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/denglertai/gonfig/internal/filter"
	"github.com/stretchr/testify/assert"
)

// TestInitPluginsMissingPath checks that a plugin path that does not exist is skipped
func TestInitPluginsMissingPath(t *testing.T) {
	t.Cleanup(ClosePlugins)
	// Must not panic or fail when the plugin path does not exist
	InitPlugins(filepath.Join(t.TempDir(), "missing"))
}

// captureLogs collects the log records written while the test runs, so warnings can be checked
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var logs bytes.Buffer
	defaultLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(defaultLogger) })
	return &logs
}

// TestInitPluginsIgnoresGoPlugins checks that leftover Go plugins are not loaded, but logged as a warning naming only
// their path and listed with the reason, while other files are skipped silently
func TestInitPluginsIgnoresGoPlugins(t *testing.T) {
	t.Cleanup(ClosePlugins)
	logs := captureLogs(t)
	dir := t.TempDir()
	// Made-up content, which must never be read or logged
	for _, name := range []string{"a.so", "readme.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("s3cret-content"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	InitPlugins(dir)

	goPlugin := filepath.Join(dir, "a.so")
	assert.Equal(t, []Info{{Path: goPlugin, Type: TypeGo, Filters: []FilterInfo{}, Error: goPluginError}}, Plugins())
	assert.Contains(t, logs.String(), `"level":"WARN"`)
	assert.Contains(t, logs.String(), `"msg":"Ignoring Go plugin (.so): Go plugins are no longer supported`)
	pathJSON, _ := json.Marshal(goPlugin)
	assert.Contains(t, logs.String(), `"path":`+string(pathJSON))
	assert.Contains(t, logs.String(), "PLUGINS.md#migrating-from-go-plugins")
	assert.NotContains(t, logs.String(), "s3cret-content")
	assert.NotContains(t, logs.String(), "readme.txt")
}

// ignoredGoPlugin is the info of an ignored Go plugin at the path
func ignoredGoPlugin(path string) Info {
	return Info{Path: path, Type: TypeGo, Filters: []FilterInfo{}, Error: goPluginError}
}

// TestInitPluginsGoPluginEdgeCases checks which entries count as Go plugins: .so files in nested directories are
// found, a directory named x.so is walked into instead, and the suffix is case-sensitive, so x.SO is no Go plugin
func TestInitPluginsGoPluginEdgeCases(t *testing.T) {
	t.Cleanup(ClosePlugins)
	logs := captureLogs(t)
	dir := t.TempDir()
	nested := filepath.Join(dir, "a", "b")
	soDir := filepath.Join(dir, "dir.so")
	for _, d := range []string{nested, soDir} {
		assert.NoError(t, os.MkdirAll(d, 0o755))
	}
	files := []string{filepath.Join(nested, "deep.so"), filepath.Join(soDir, "inner.so"), filepath.Join(dir, "upper.SO")}
	for _, f := range files {
		assert.NoError(t, os.WriteFile(f, []byte("s3cret-content"), 0o644))
	}

	InitPlugins(dir)

	// WalkDir is lexical, so a/b/deep.so comes before dir.so/inner.so
	assert.Equal(t, []Info{ignoredGoPlugin(files[0]), ignoredGoPlugin(files[1])}, Plugins())
	assert.NotContains(t, logs.String(), "s3cret-content")
	assert.NotContains(t, logs.String(), "upper.SO")
}

// TestInitPluginsSymlinkedGoPlugin checks that a symbolic link named *.so is ignored like the file, without following it
func TestInitPluginsSymlinkedGoPlugin(t *testing.T) {
	skipOnWindows(t)
	t.Cleanup(ClosePlugins)
	dir := t.TempDir()
	target := filepath.Join(dir, "target.txt")
	assert.NoError(t, os.WriteFile(target, []byte("x"), 0o644))
	link := filepath.Join(dir, "link.so")
	assert.NoError(t, os.Symlink(target, link))
	broken := filepath.Join(dir, "broken.so")
	assert.NoError(t, os.Symlink(filepath.Join(dir, "missing"), broken))

	InitPlugins(dir)

	assert.Equal(t, []Info{ignoredGoPlugin(broken), ignoredGoPlugin(link)}, Plugins())
}

// TestInitPluginsGoPluginNextToExecutablePlugin checks that an ignored Go plugin does not prevent a valid executable
// plugin in the same directory from starting, and that both are listed
func TestInitPluginsGoPluginNextToExecutablePlugin(t *testing.T) {
	dir, prefix := installTestPlugin(t, "serve")
	goPlugin := filepath.Join(dir, "old.so")
	assert.NoError(t, os.WriteFile(goPlugin, []byte("s3cret-content"), 0o644))
	logs := captureLogs(t)

	InitPlugins(dir)
	t.Cleanup(ClosePlugins)

	infos := Plugins()
	if assert.Len(t, infos, 2) {
		assert.Equal(t, TypeExecutable, infos[0].Type)
		assert.Empty(t, infos[0].Error)
		assert.Equal(t, ignoredGoPlugin(goPlugin), infos[1])
	}
	assert.True(t, filter.IsKnown(prefix+"upper"))
	assert.NotContains(t, logs.String(), "s3cret-content")
}
