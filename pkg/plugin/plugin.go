// Package plugin provides what filter plugins need: Serve runs a plugin executable that gonfig talks to over
// stdin and stdout, and the protocol types describe the messages for plugins written in other languages.
// See PLUGINS.md for how plugins work.
package plugin

// PluginFilter represents a filter plugin built as a Go plugin (.so file) and is expected to provide a set of filters
// that can be applied to a configuration file.
//
// Deprecated: Go plugins have to be built with exactly the same Go version and flags as gonfig. Build an executable
// plugin with Serve instead, which takes the same map of filters.
type PluginFilter interface {
	// Filters returns a set of filters that can be applied to a configuration file
	Filters() map[string]interface{}
}
