// Command gonfig-plugin-openbao is a gonfig plugin providing the filter openbao, which reads secrets from OpenBao or
// HashiCorp Vault, e.g. ${DB_PASSWORD | openbao} with DB_PASSWORD=secret/app/db#password. See README.md.
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
	filters := map[string]any{"openbao": secret.NewFilter(&provider{})}
	if err := plugin.Serve(filters, plugin.WithVersion(version), plugin.WithDescription("Reads secrets from OpenBao and HashiCorp Vault")); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
