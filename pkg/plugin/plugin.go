package plugin

import "github.com/spf13/cobra"

// PluginFilter represents a filter plugin and is expected to provide a set of filters that can be applied to a configuration file
type PluginFilter interface {
	// Filters returns a set of filters that can be applied to a configuration file
	Filters() map[string]interface{}
}

// PluginCommand represents a command plugin and is expected to provide a cobra.Command that can be used to extend the CLI
type PluginCommand interface {
	// Commands returns a set of cobra.Commands that can be used to extend the CLI
	Commands() []*cobra.Command
}
