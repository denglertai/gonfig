package cmd

import (
	"bytes"
	"os"
	"os/exec"
	"path"
	"testing"

	"github.com/denglertai/gonfig/internal/filter"
	"github.com/denglertai/gonfig/internal/plugin"
	"github.com/stretchr/testify/assert"
)

// TestDummyPlugin builds the dummy plugin as an executable plugin and processes a file with its reverse filter,
// as an end-to-end test of executable plugins. It runs Execute like main, which stops the plugin afterwards.
func TestDummyPlugin(t *testing.T) {
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Skip("building the dummy plugin requires the go tool")
	}

	pluginDir := t.TempDir()
	build := exec.Command(goTool, "build", "-o", path.Join(pluginDir, "gonfig-plugin-dummy"), ".")
	build.Dir = path.Join("..", "plugins", "dummy")
	output, err := build.CombinedOutput()
	if !assert.NoError(t, err, string(output)) {
		return
	}

	fileName := path.Join(t.TempDir(), "config.yaml")
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
