// Command gonfig-plugin-azure-keyvault is a gonfig plugin providing the filter azure_keyvault, which reads secrets
// from Azure Key Vault, e.g. ${DB_PASSWORD | azure_keyvault} with DB_PASSWORD=myvault/db-password. See README.md.
package main

import (
	"fmt"
	"os"

	"github.com/denglertai/gonfig/pkg/plugin"
	"github.com/denglertai/gonfig/pkg/plugin/secret"
)

// version is the version of the plugin, set by release builds with -ldflags "-X main.version=..."
var version string

// main serves the filter to gonfig until gonfig is done
func main() {
	filters := map[string]any{"azure_keyvault": secret.NewFilter(&provider{})}
	if err := plugin.Serve(filters, plugin.WithVersion(version), plugin.WithDescription("Reads secrets from Azure Key Vault")); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
