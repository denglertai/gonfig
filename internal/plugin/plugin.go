package plugin

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"plugin"
	"runtime"
	"strings"

	"github.com/denglertai/gonfig/internal/filter"
	"github.com/denglertai/gonfig/pkg/logging"
	pkgplugin "github.com/denglertai/gonfig/pkg/plugin"
)

// running are the executable plugins started by InitPlugins, which ClosePlugins stops
var running []*processPlugin

// InitPlugins starts all executable plugins (gonfig-plugin-*) and loads all Go plugins (.so files) found in the
// plugin path. Plugins that fail to start or load are logged and skipped, so they don't prevent other plugins from
// loading. ClosePlugins stops the executable plugins again.
func InitPlugins(pluginPath string) {
	if pluginPath == "" {
		pluginPath = "./plugins"
	}

	logging.Trace("Starting loading plugins", "pluginPath", pluginPath)

	err := filepath.WalkDir(pluginPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		switch {
		case strings.HasSuffix(path, ".so"):
			logging.Debug("Loading deprecated Go plugin, see PLUGINS.md for migrating to an executable plugin", "path", path)
			if err := loadPlugin(path); err != nil {
				logging.Error("Failed to load plugin", "path", path, "error", err)
			}
		case strings.HasPrefix(d.Name(), executablePrefix):
			// Stat follows symbolic links, so linked plugins are started as well
			info, err := os.Stat(path)
			if err != nil || !isExecutablePlugin(d.Name(), info.Mode(), runtime.GOOS) {
				logging.Debug("Skipping file that is not executable", "path", path)
				return nil
			}
			if err := runPlugin(path); err != nil {
				logging.Error("Failed to start plugin", "path", path, "error", err)
			}
		}

		return nil
	})

	if errors.Is(err, fs.ErrNotExist) {
		logging.Debug("Plugin path does not exist", "pluginPath", pluginPath)
	} else if err != nil {
		logging.Error("Failed to walk plugin path", "pluginPath", pluginPath, "error", err)
	}

	logging.Trace("Finished loading plugins", "pluginPath", pluginPath)
}

// ClosePlugins stops the executable plugins started by InitPlugins and removes their filters, so plugins can be
// started again, e.g. by the next command run in the same process
func ClosePlugins() {
	for _, p := range running {
		filter.RemovePluginFilters(p.filters)
		p.close()
	}
	running = nil
}

// runPlugin starts an executable plugin and registers the filters it provides
func runPlugin(path string) error {
	p, names, err := startPlugin(path)
	if err != nil {
		return err
	}
	running = append(running, p)

	filters := make(map[string]any, len(names))
	for _, name := range names {
		filters[name] = &remoteFilter{plugin: p, name: name}
	}
	p.filters = filter.AddPluginFilters(filters)
	logging.Debug("Started filter plugin", "plugin", path, "filters", len(p.filters))

	return nil
}

// loadPlugin loads a single Go plugin and registers the filters it provides.
// It uses the deprecated PluginFilter, since loading the deprecated Go plugins is what it exists for.
//
//nolint:staticcheck
func loadPlugin(path string) error {
	p, err := plugin.Open(path)
	if err != nil {
		return err
	}

	pf, err := lookUpSymbol[pkgplugin.PluginFilter](p, "Filter")
	if err != nil {
		return err
	}

	filters := pf.Filters()
	filter.AddPluginFilters(filters)
	logging.Debug("Loaded filter plugin", "plugin", path, "filters", len(filters))

	return nil
}

// lookUpSymbol looks up the symbol in the plugin and ensures it is of the expected type
func lookUpSymbol[M any](p *plugin.Plugin, symbolName string) (M, error) {
	var zero M

	symbol, err := p.Lookup(symbolName)
	if err != nil {
		return zero, err
	}

	result, ok := symbol.(M)
	if !ok {
		return zero, fmt.Errorf("symbol %s is of type %T, which does not implement %T", symbolName, symbol, (*M)(nil))
	}

	return result, nil
}
