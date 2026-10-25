package plugin

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"plugin"
	"runtime"
	"slices"
	"strings"

	"github.com/denglertai/gonfig/internal/filter"
	"github.com/denglertai/gonfig/pkg/logging"
	pkgplugin "github.com/denglertai/gonfig/pkg/plugin"
)

// running are the executable plugins started by InitPlugins, which ClosePlugins stops
var running []*processPlugin

// systemPluginDir is the plugin directory of installed packages, e.g. the apk packages of the plugins of this
// repository. It is a variable, so tests can use another directory.
var systemPluginDir = "/usr/libexec/gonfig/plugins"

// localPluginDir is the plugin directory relative to the working directory
const localPluginDir = "./plugins"

// defaultPluginPaths returns the directories searched for plugins if no plugin path is configured. The system directory
// comes first, so plugins in the working directory cannot replace the filters of installed plugins, since the plugin
// registering a filter name first wins. Windows has no system directory.
func defaultPluginPaths(goos string) []string {
	if goos == "windows" {
		return []string{localPluginDir}
	}
	return []string{systemPluginDir, localPluginDir}
}

// InitPlugins starts all executable plugins (gonfig-plugin-*) and loads all Go plugins (.so files) found in the
// plugin path, or in the default plugin paths if it is empty, see defaultPluginPaths. Plugins that fail to start or
// load are logged and skipped, so they don't prevent other plugins from loading. ClosePlugins stops the executable
// plugins again.
func InitPlugins(pluginPath string) {
	paths := []string{pluginPath}
	if pluginPath == "" {
		paths = defaultPluginPaths(runtime.GOOS)
	}
	// The plugins found describe the latest initialization, see Plugins
	searched = paths
	found = nil
	for _, path := range paths {
		loadPluginDir(path)
	}
}

// loadPluginDir starts and loads the plugins of a plugin directory and its subdirectories.
// A directory that does not exist is skipped, since no plugins may be installed.
func loadPluginDir(pluginPath string) {
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
				found = append(found, Info{Path: path, Type: TypeGo, Filters: []FilterInfo{}, Error: err.Error()})
			}
		case strings.HasPrefix(d.Name(), executablePrefix):
			// Stat follows symbolic links, so linked plugins are started as well
			info, err := os.Stat(path)
			if err != nil || !isExecutablePlugin(d.Name(), info.Mode(), runtime.GOOS) {
				logging.Debug("Skipping file that is not executable", "path", path)
				return nil //nolint:nilerr // e.g. broken links are skipped like other files that are no plugins
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
	found = nil
	searched = nil
}

// runPlugin starts an executable plugin and registers the filters it provides. Filters with declared params get
// their params validated by gonfig.
func runPlugin(path string) error {
	p, names, err := startPlugin(path)
	if err != nil {
		found = append(found, Info{Path: path, Type: TypeExecutable, Filters: []FilterInfo{}, Error: err.Error()})
		return err
	}
	running = append(running, p)
	handshake := p.handshake

	filters := make(map[string]any, len(names))
	for _, name := range names {
		remote := remoteFilter{plugin: p, name: name}
		if info, ok := handshake.FilterInfo[name]; ok && info.Params != nil {
			filters[name] = &declaredRemoteFilter{remoteFilter: remote, declared: info.Params}
		} else {
			filters[name] = &remote
		}
	}
	p.filters = filter.AddPluginFilters(filters)
	logging.Debug("Started filter plugin", "plugin", path, "filters", len(p.filters))

	info := Info{Path: path, Type: TypeExecutable, Version: handshake.Version, Description: handshake.Description}
	info.Filters = filterInfos(names, p.filters, func(name string) FilterInfo {
		return FilterInfo{Description: handshake.FilterInfo[name].Description, Params: handshake.FilterInfo[name].Params}
	})
	found = append(found, info)
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

	registerGoPlugin(path, pf.Filters())
	return nil
}

// registerGoPlugin registers the filters of a loaded Go plugin and records the plugin for Plugins. Go plugins have no
// version, but their filters may describe themselves and declare their params, which gonfig validates.
func registerGoPlugin(path string, filters map[string]any) {
	added := filter.AddPluginFilters(filters)
	logging.Debug("Loaded filter plugin", "plugin", path, "filters", len(filters))

	names := slices.Sorted(maps.Keys(filters))
	found = append(found, Info{Path: path, Type: TypeGo, Filters: filterInfos(names, added, func(name string) FilterInfo {
		return goFilterInfo(filters[name])
	})})
}

// goFilterInfo returns the description and declared params of a filter of a Go plugin. The filters run within gonfig,
// which validates the params of filters implementing filter.ParamsDeclarer and accepts none if they declare none, so
// nil declared params are listed as an empty list. Filters without it accept any params.
func goFilterInfo(f any) FilterInfo {
	var info FilterInfo
	if described, ok := f.(interface{ Description() string }); ok {
		info.Description = described.Description()
	}
	if declarer, ok := f.(filter.ParamsDeclarer); ok {
		info.Params = append([]string{}, declarer.DeclaredParams()...)
	}
	return info
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
