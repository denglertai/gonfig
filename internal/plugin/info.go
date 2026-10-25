package plugin

import "slices"

// Types of plugins, see Info
const (
	// TypeExecutable is an executable plugin (gonfig-plugin-*)
	TypeExecutable = "executable"
	// TypeGo is a deprecated Go plugin (.so file)
	TypeGo = "go"
)

// Info describes a plugin found by InitPlugins, for gonfig plugin list
type Info struct {
	// Path is the path of the plugin, as found in the plugin path
	Path string `json:"path"`
	// Type is TypeExecutable or TypeGo
	Type string `json:"type"`
	// Version and Description are provided by the plugin, if it supports them
	Version     string `json:"version,omitempty"`
	Description string `json:"description,omitempty"`
	// Filters are the filters the plugin provides, sorted by name
	Filters []FilterInfo `json:"filters"`
	// Error is why the plugin failed to start or load, in which case it has no filters
	Error string `json:"error,omitempty"`
}

// FilterInfo describes a filter of a plugin
type FilterInfo struct {
	Name string `json:"name"`
	// Description is provided by the plugin, if it supports it
	Description string `json:"description,omitempty"`
	// Params are the params the plugin declared, which gonfig validates. nil accepts any params.
	Params []string `json:"params"`
	// Skipped is set if another filter has the name already, e.g. a built-in filter, so this one isn't used
	Skipped bool `json:"skipped,omitempty"`
}

// found are the plugins found by InitPlugins, in the order they were found
var found []Info

// searched are the plugin directories searched by InitPlugins, see defaultPluginPaths
var searched []string

// Plugins returns the plugins found by InitPlugins in the order they were found, including the ones that failed
func Plugins() []Info {
	return found
}

// SearchedPaths returns the plugin directories searched by InitPlugins
func SearchedPaths() []string {
	return searched
}

// filterInfos returns the info of the filters with the names, sorted by name, marking the filters that weren't added
// as skipped. describe returns the description and params of a filter.
func filterInfos(names []string, added []string, describe func(name string) FilterInfo) []FilterInfo {
	infos := make([]FilterInfo, 0, len(names))
	for _, name := range slices.Sorted(slices.Values(names)) {
		info := describe(name)
		info.Name = name
		info.Skipped = !slices.Contains(added, name)
		infos = append(infos, info)
	}
	return infos
}
