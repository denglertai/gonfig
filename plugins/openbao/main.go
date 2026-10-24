// Command gonfig-plugin-openbao is a gonfig plugin providing the filter openbao, which reads secrets from OpenBao or
// HashiCorp Vault, e.g. ${DB_PASSWORD | openbao} with DB_PASSWORD=secret/app/db#password. See README.md.
package main

import (
	"fmt"
	"os"

	"github.com/denglertai/gonfig/pkg/plugin"
	"github.com/denglertai/gonfig/pkg/plugin/secret"
)

// main serves the filter to gonfig until gonfig is done
func main() {
	if err := plugin.Serve(map[string]any{"openbao": secret.NewFilter(&provider{})}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
