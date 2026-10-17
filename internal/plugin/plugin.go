package plugin

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"plugin"
	"strings"

	"github.com/denglertai/gonfig/internal/filter"
	"github.com/denglertai/gonfig/pkg/logging"
	pkgplugin "github.com/denglertai/gonfig/pkg/plugin"
)

// InitPlugins loads all plugins (.so files) found in the plugin path.
// Plugins that fail to load are logged and skipped, so they don't prevent other plugins from loading.
func InitPlugins(pluginPath string) {
	if pluginPath == "" {
		pluginPath = "./plugins"
	}

	logging.Trace("Starting loading plugins", "pluginPath", pluginPath)

	err := filepath.WalkDir(pluginPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip directories and non .so files
		if d.IsDir() || !strings.HasSuffix(path, ".so") {
			return nil
		}

		if err := loadPlugin(path); err != nil {
			logging.Error("Failed to load plugin", "path", path, "error", err)
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

// loadPlugin loads a single plugin and registers the filters it provides
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
