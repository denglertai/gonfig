package plugin

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/denglertai/gonfig/internal/filter"
	"github.com/denglertai/gonfig/pkg/logging"
)

// running are the executable plugins started by InitPlugins, which ClosePlugins stops
var running []*processPlugin

// systemPluginDir is the plugin directory of installed packages, e.g. the apk packages of the plugins of this
// repository. It is a variable, so tests can use another directory.
var systemPluginDir = "/usr/libexec/gonfig/plugins"

// localSystemPluginDir is the plugin directory searched on macOS after systemPluginDir, since System Integrity
// Protection makes /usr read-only there, except for /usr/local. It is a variable, so tests can use another directory.
var localSystemPluginDir = "/usr/local/libexec/gonfig/plugins"

// goPluginError is why a leftover Go plugin (.so file) is ignored, logged and listed by gonfig plugin list
const goPluginError = "Go plugins are no longer supported, see PLUGINS.md#migrating-from-go-plugins"

// defaultPluginPaths returns the directories searched for plugins if no plugin path is configured. Nothing is
// searched relative to the working directory, so running gonfig in a directory never starts plugins found there.
// Windows has no system plugin directory, so nothing is searched there.
func defaultPluginPaths(goos string) []string {
	switch goos {
	case "windows":
		return nil
	case "darwin":
		return []string{systemPluginDir, localSystemPluginDir}
	default:
		return []string{systemPluginDir}
	}
}

// InitPlugins starts all executable plugins (gonfig-plugin-*) found in the plugin path, or in the default plugin
// paths if it is empty, see defaultPluginPaths. Plugins that fail to start are logged and skipped, so they don't
// prevent other plugins from starting. Leftover Go plugins (.so files) are not loaded anymore, but logged and listed
// with an error. ClosePlugins stops the executable plugins again.
func InitPlugins(pluginPath string) {
	paths := []string{pluginPath}
	if pluginPath == "" {
		paths = defaultPluginPaths(runtime.GOOS)
		if len(paths) == 0 {
			// Debug only, since this applies to every run on Windows without a configured plugin path
			logging.Debug("No plugin path configured and no default plugin path on this OS, so no plugins are started", "os", runtime.GOOS)
		}
	}
	// The plugins found describe the latest initialization, see Plugins
	searched = paths
	found = nil
	for _, path := range paths {
		loadPluginDir(path)
	}
}

// loadPluginDir starts the plugins of a plugin directory and its subdirectories.
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
			// Warn at the default level, so users of a Go plugin notice why its filters are missing. Only the path is
			// logged, the file is never read.
			logging.Warn("Ignoring Go plugin (.so): "+goPluginError, "path", path)
			found = append(found, Info{Path: path, Type: TypeGo, Filters: []FilterInfo{}, Error: goPluginError})
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
