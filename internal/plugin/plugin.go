package plugin

import (
	"os"
	"path/filepath"
	"plugin"
	"strings"

	"github.com/denglertai/gonfig/internal/filter"
	pkgplugin "github.com/denglertai/gonfig/pkg/plugin"
)

func InitPlugins() {
	filepath.Walk("plugins", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories and non .so files
		if info.IsDir() || !strings.HasSuffix(path, ".so") {
			return nil
		}

		// Load the plugin
		plugin, err := plugin.Open(path)
		if err != nil {
			return err
		}

		// Lookup for PluginFilter
		pf, err := lookUpSymbol[pkgplugin.PluginFilter](plugin, "Filter")

		if err == nil {
			filters := (*pf).Filters()
			filter.AddPluginFilters(filters)
		}

		// Lookup for PluginCommand
		// pc, err := lookUpSymbol[pkgplugin.PluginCommand](plugin, "Command")
		// if err == nil {
		// 	commands := (*pc).Commands()

		// }

		return nil
	})
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
