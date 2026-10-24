/*
Copyright © 2024 Daniel Englert
*/
package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/denglertai/gonfig/internal/plugin"
	"github.com/spf13/cobra"
)

// pluginCmd groups the commands working with plugins
var pluginCmd = &cobra.Command{
	Use:   "plugin",
	Short: "Work with plugins",
	Long: `Work with the plugins in the plugin path, see --plugin-path.
By default, /usr/libexec/gonfig/plugins (except on Windows) and ./plugins are searched.`,
	TraverseChildren: true,
}

// pluginListCmd lists the plugins found in the plugin path
var pluginListCmd = &cobra.Command{
	Use:   "list",
	Short: "List the plugins and their filters",
	Long: `Lists the plugins found in the plugin path in the order they were found, with their version, description
and filters, if the plugins provide them. A filter shows its params if the plugin declares them, which gonfig then
validates like the params of its built-in filters.

Plugins that failed to start and filters skipped because another filter has their name already are listed as well,
with exit code 0, since listing them is the purpose of the command.`,
	Args:             noArgs,
	TraverseChildren: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		// The flag is defined in init, so reading it cannot fail
		output, _ := cmd.Flags().GetString("output")
		switch output {
		case "text":
			return writePluginsText(cmd.OutOrStdout(), plugin.SearchedPaths(), plugin.Plugins())
		case "json":
			return writePluginsJson(cmd.OutOrStdout(), plugin.SearchedPaths(), plugin.Plugins())
		default:
			return fmt.Errorf("unsupported output %q, use text or json", output)
		}
	},
}

// writePluginsText writes the plugins in a readable format, e.g.
//
//	/usr/libexec/gonfig/plugins/gonfig-plugin-openbao 0.3.0
//	  Reads secrets from OpenBao and HashiCorp Vault
//	  openbao(mount, kv, version)
//	      Reads a secret of the KV secrets engine, e.g. secret/app/db#password
func writePluginsText(w io.Writer, paths []string, plugins []plugin.Info) error {
	var sb strings.Builder
	if len(plugins) == 0 {
		fmt.Fprintf(&sb, "No plugins found in %s\n", strings.Join(paths, ", "))
	}

	for _, p := range plugins {
		sb.WriteString(p.Path)
		if p.Version != "" {
			sb.WriteString(" " + p.Version)
		}
		if p.Type == plugin.TypeGo {
			sb.WriteString(" (deprecated Go plugin)")
		}
		sb.WriteString("\n")
		if p.Description != "" {
			fmt.Fprintf(&sb, "  %s\n", p.Description)
		}
		if p.Error != "" {
			fmt.Fprintf(&sb, "  failed to start: %s\n", p.Error)
		}

		for _, f := range p.Filters {
			sb.WriteString("  " + f.Name)
			// Filters without declared params accept any params, which are not shown
			if f.Params != nil {
				fmt.Fprintf(&sb, "(%s)", strings.Join(f.Params, ", "))
			}
			sb.WriteString("\n")
			if f.Description != "" {
				fmt.Fprintf(&sb, "      %s\n", f.Description)
			}
			if f.Skipped {
				sb.WriteString("      skipped: another filter has the name already\n")
			}
		}
	}

	_, err := io.WriteString(w, sb.String())
	return err
}

// writePluginsJson writes the searched paths and the plugins as JSON, e.g. for scripts
func writePluginsJson(w io.Writer, paths []string, plugins []plugin.Info) error {
	if plugins == nil {
		plugins = []plugin.Info{}
	}
	// The plugins consist of strings, booleans and slices of them, so encoding them cannot fail
	data, _ := json.MarshalIndent(struct {
		Paths   []string      `json:"paths"`
		Plugins []plugin.Info `json:"plugins"`
	}{paths, plugins}, "", "  ")

	_, err := fmt.Fprintln(w, string(data))
	return err
}

// init registers the plugin commands and their flags
func init() {
	rootCmd.AddCommand(pluginCmd)
	pluginCmd.AddCommand(pluginListCmd)

	pluginListCmd.Flags().StringP("output", "o", "text", "Format of the list: text or json")
}
