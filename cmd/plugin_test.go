package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/denglertai/gonfig/internal/filter"
	"github.com/denglertai/gonfig/internal/plugin"
	"github.com/stretchr/testify/assert"
)

// buildDummyPlugin builds the dummy plugin into a new plugin directory and returns the directory and the name of the
// plugin. The test is skipped without the go tool.
func buildDummyPlugin(t *testing.T) (string, string) {
	t.Helper()

	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Skip("building the dummy plugin requires the go tool")
	}

	// Plugins are only found with the suffix .exe on Windows
	name := "gonfig-plugin-dummy"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}

	pluginDir := t.TempDir()
	build := exec.Command(goTool, "build", "-o", filepath.Join(pluginDir, name), ".")
	build.Dir = filepath.Join("..", "plugins", "dummy")
	output, err := build.CombinedOutput()
	if !assert.NoError(t, err, string(output)) {
		t.FailNow()
	}
	return pluginDir, name
}

// TestDummyPlugin builds the dummy plugin as an executable plugin and processes a file with its reverse filter,
// as an end-to-end test of executable plugins. It runs Execute like main, which stops the plugin afterwards.
func TestDummyPlugin(t *testing.T) {
	pluginDir, _ := buildDummyPlugin(t)

	fileName := filepath.Join(t.TempDir(), "config.yaml")
	assert.NoError(t, os.WriteFile(fileName, []byte("name: ${PLUGIN_NAME | reverse}\nid: ${PLUGIN_ID | to_int | reverse | to_int}\n"), 0o644))
	t.Setenv("PLUGIN_NAME", "gonfig")
	t.Setenv("PLUGIN_ID", "123")

	var out bytes.Buffer
	rootCmd.SetOut(&out)
	t.Cleanup(func() { rootCmd.SetOut(nil) })
	// Stops the plugin if the test fails before Execute does
	t.Cleanup(plugin.ClosePlugins)

	resetFlags(t)
	rootCmd.SetArgs([]string{"--plugin-path", pluginDir, "config", "process", "-f", fileName})
	Execute()
	assert.Equal(t, "name: gifnog\nid: 321\n", out.String())
	assert.False(t, filter.IsKnown("reverse"), "the plugin must be stopped and its filters removed")
}

// runPluginList runs gonfig plugin list with the args like main and returns its output
func runPluginList(t *testing.T, args ...string) string {
	t.Helper()

	var out bytes.Buffer
	rootCmd.SetOut(&out)
	t.Cleanup(func() { rootCmd.SetOut(nil) })
	t.Cleanup(plugin.ClosePlugins)

	resetFlags(t)
	rootCmd.SetArgs(args)
	Execute()
	return out.String()
}

// TestPluginList lists the dummy plugin with its description and its filter declaring no params
func TestPluginList(t *testing.T) {
	pluginDir, name := buildDummyPlugin(t)
	path := filepath.Join(pluginDir, name)

	text := runPluginList(t, "--plugin-path", pluginDir, "plugin", "list")
	assert.Contains(t, text, path)
	assert.Contains(t, text, "\n  Example plugin, see PLUGINS.md\n  reverse()\n      Reverses the value\n")

	var listed struct {
		Paths   []string      `json:"paths"`
		Plugins []plugin.Info `json:"plugins"`
	}
	assert.NoError(t, json.Unmarshal([]byte(runPluginList(t, "--plugin-path", pluginDir, "plugin", "list", "-o", "json")), &listed))
	assert.Equal(t, []string{pluginDir}, listed.Paths)
	if assert.Len(t, listed.Plugins, 1) {
		assert.Equal(t, path, listed.Plugins[0].Path)
		assert.Equal(t, "Example plugin, see PLUGINS.md", listed.Plugins[0].Description)
		assert.Equal(t, []plugin.FilterInfo{{Name: "reverse", Description: "Reverses the value", Params: []string{}}}, listed.Plugins[0].Filters)
	}
}

// TestPluginListWithoutPlugins checks the message naming the searched paths, and an empty list as JSON
func TestPluginListWithoutPlugins(t *testing.T) {
	dir := t.TempDir()
	assert.Equal(t, "No plugins found in "+dir+"\n", runPluginList(t, "--plugin-path", dir, "plugin", "list"))
	assert.JSONEq(t, `{"paths": [`+jsonQuote(dir)+`], "plugins": []}`, runPluginList(t, "--plugin-path", dir, "plugin", "list", "-o", "json"))
}

// TestPluginListIgnoredGoPlugin checks that a leftover Go plugin (.so file) is listed as ignored with the reason, and
// keeps the type go in JSON, so users and scripts see why its filters are missing
func TestPluginListIgnoredGoPlugin(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "old.so")
	assert.NoError(t, os.WriteFile(path, []byte("not a plugin"), 0o644))
	reason := "Go plugins are no longer supported, see PLUGINS.md#migrating-from-go-plugins"

	assert.Equal(t, path+" (Go plugin, ignored)\n  ignored: "+reason+"\n", runPluginList(t, "--plugin-path", dir, "plugin", "list"))
	assert.JSONEq(t, `{"paths": [`+jsonQuote(dir)+`], "plugins": [{"path": `+jsonQuote(path)+`, "type": "go", "filters": [], "error": "`+reason+`"}]}`,
		runPluginList(t, "--plugin-path", dir, "plugin", "list", "-o", "json"))
}

// TestWritePluginsWithoutPaths checks the output without searched paths, as on Windows without a plugin path, which
// has no default plugin directory
func TestWritePluginsWithoutPaths(t *testing.T) {
	var text, jsonOut bytes.Buffer
	assert.NoError(t, writePluginsText(&text, nil, nil))
	assert.Equal(t, "No plugins found, no plugin path is set, see --plugin-path\n", text.String())
	assert.NoError(t, writePluginsJson(&jsonOut, nil, nil))
	assert.JSONEq(t, `{"paths": [], "plugins": []}`, jsonOut.String())
}

// jsonQuote quotes the text as a JSON string, e.g. for paths with backslashes on Windows
func jsonQuote(text string) string {
	quoted, _ := json.Marshal(text)
	return string(quoted)
}

// TestWritePluginsText checks the text of plugins without optional fields, failed and skipped ones and ignored Go plugins
func TestWritePluginsText(t *testing.T) {
	var out bytes.Buffer
	assert.NoError(t, writePluginsText(&out, []string{"./plugins"}, []plugin.Info{
		{Path: "plugins/gonfig-plugin-a", Type: plugin.TypeExecutable, Version: "1.0.0", Description: "Plugin A",
			Filters: []plugin.FilterInfo{{Name: "a", Description: "Filter A", Params: []string{"x", "y"}}, {Name: "upper", Skipped: true}}},
		{Path: "plugins/gonfig-plugin-b", Type: plugin.TypeExecutable, Filters: []plugin.FilterInfo{}, Error: "no handshake: the plugin exited"},
		{Path: "plugins/old.so", Type: plugin.TypeGo, Filters: []plugin.FilterInfo{}, Error: "Go plugins are no longer supported"},
	}))
	assert.Equal(t, `plugins/gonfig-plugin-a 1.0.0
  Plugin A
  a(x, y)
      Filter A
  upper
      skipped: another filter has the name already
plugins/gonfig-plugin-b
  failed to start: no handshake: the plugin exited
plugins/old.so (Go plugin, ignored)
  ignored: Go plugins are no longer supported
`, out.String())
}

// TestPluginListInvalidOutput checks the error for an unsupported output format
func TestPluginListInvalidOutput(t *testing.T) {
	resetFlags(t)
	t.Cleanup(plugin.ClosePlugins)
	rootCmd.SetArgs([]string{"--plugin-path", t.TempDir(), "plugin", "list", "-o", "yaml"})
	assert.EqualError(t, rootCmd.Execute(), `unsupported output "yaml", use text or json`)
}

// TestWritePluginsErrors checks that errors of the output are returned
func TestWritePluginsErrors(t *testing.T) {
	assert.Error(t, writePluginsText(failingCheckWriter{}, nil, nil))
	assert.Error(t, writePluginsJson(failingCheckWriter{}, nil, nil))
}

// TestPluginListMixedEntries checks the text and JSON of a started plugin next to an ignored Go plugin in the same
// directory, and that the content of the .so file is never printed
func TestPluginListMixedEntries(t *testing.T) {
	dir, name := buildDummyPlugin(t)
	goPath := filepath.Join(dir, "old.so")
	assert.NoError(t, os.WriteFile(goPath, []byte("s3cret-content"), 0o644))
	reason := "Go plugins are no longer supported, see PLUGINS.md#migrating-from-go-plugins"

	text := runPluginList(t, "--plugin-path", dir, "plugin", "list")
	assert.Contains(t, text, filepath.Join(dir, name))
	assert.Contains(t, text, goPath+" (Go plugin, ignored)\n  ignored: "+reason+"\n")
	assert.NotContains(t, text, "failed to start")
	assert.NotContains(t, text, "s3cret-content")

	jsonOut := runPluginList(t, "--plugin-path", dir, "plugin", "list", "-o", "json")
	var result struct {
		Plugins []plugin.Info `json:"plugins"`
	}
	assert.NoError(t, json.Unmarshal([]byte(jsonOut), &result))
	if assert.Len(t, result.Plugins, 2) {
		assert.Equal(t, plugin.TypeExecutable, result.Plugins[0].Type)
		assert.Empty(t, result.Plugins[0].Error)
		assert.Equal(t, plugin.Info{Path: goPath, Type: plugin.TypeGo, Filters: []plugin.FilterInfo{}, Error: reason}, result.Plugins[1])
	}
	assert.NotContains(t, jsonOut, "s3cret-content")
}
