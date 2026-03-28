package plugin

import (
	"os"
	"path/filepath"
	"plugin"
	"strings"

	"github.com/denglertai/gonfig/internal/filter"
	"github.com/denglertai/gonfig/pkg/logging"
	pkgplugin "github.com/denglertai/gonfig/pkg/plugin"
)

func InitPlugins(pluginPath string) {
	if pluginPath == "" {
		pluginPath = "./plugins"
	}

	wd, err := os.Getwd()
	if err != nil {
		logging.Error("Failed to get current working directory", "error", err)
		return
	}

	logging.Trace("Starting loading plugins", "pwd", wd, "pluginPath", pluginPath)

	filepath.Walk(pluginPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			logging.Debug("Failed to walk plugin path", "pluginPath", pluginPath, "error", err)
			return err
		}

		// Skip directories and non .so files
		if info.IsDir() || !strings.HasSuffix(path, ".so") {
			return nil
		}

		// Load the plugin
		plugin, err := plugin.Open(path)
		if err != nil {
			logging.Error("Failed to load plugin", "path", path, "error", err)
			return err
		}

		// Lookup for PluginFilter
		pf, err := lookUpSymbol[pkgplugin.PluginFilter](plugin, "Filter")

		if err == nil {
			filters := (*pf).Filters()
			filter.AddPluginFilters(filters)
			logging.Debug("Loaded filter plugin", "plugin", path, "filters", len(filters))
		} else {
			logging.Error("Failed to lookup PluginFilter", "path", path, "error", err)
		}

		// Lookup for PluginCommand
		// pc, err := lookUpSymbol[pkgplugin.PluginCommand](plugin, "Command")
		// if err == nil {
		// 	commands := (*pc).Commands()

		// }

		return nil
	})

	logging.Trace("Finished loading plugins", "pwd", wd, "pluginPath", pluginPath)
}

func lookUpSymbol[M any](plugin *plugin.Plugin, symbolName string) (*M, error) {
	symbol, err := plugin.Lookup(symbolName)
	if err != nil {
		return nil, err
	}
	result := symbol.(M)
	return &result, nil
	// switch symbol.(type) {
	// case *M:
	// 	return symbol.(*M), nil
	// case M:
	// 	result := symbol.(M)
	// 	return &result, nil
	// default:
	// 	return nil, fmt.Errorf("unexpected type from module symbol: %T", symbol)
	// }
}
