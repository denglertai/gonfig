// Command gonfig-plugin-azure-keyvault is a gonfig plugin providing the filter azure_keyvault, which reads secrets
// from Azure Key Vault, e.g. ${DB_PASSWORD | azure_keyvault} with DB_PASSWORD=myvault/db-password. See README.md.
package main

import (
	"fmt"
	"os"

	"github.com/denglertai/gonfig/pkg/plugin"
	"github.com/denglertai/gonfig/pkg/plugin/secret"
)

// main serves the filter to gonfig until gonfig is done
func main() {
	if err := plugin.Serve(map[string]any{"azure_keyvault": secret.NewFilter(&provider{})}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
