package main

import (
	"os"
	"testing"

	"github.com/denglertai/gonfig/pkg/plugin/secret"
	"github.com/stretchr/testify/assert"
)

// The live test reads a secret of a real vault with DefaultAzureCredential if these variables are set, e.g.
// TEST_AZURE_KEYVAULT_REF=myvault/gonfig-test TEST_AZURE_KEYVAULT_VALUE=<its value>, after az login.
// They don't start with AZURE_ or GONFIG_, so TestMain keeps them; the variables of the credential have to be set as
// TEST_AZURE_* as well, see liveCredentialEnv.
const (
	liveRefEnv   = "TEST_AZURE_KEYVAULT_REF"
	liveValueEnv = "TEST_AZURE_KEYVAULT_VALUE"
)

// liveCredentialEnv maps variables of the live test to the variables of the environment credential, which TestMain
// removes, so tests don't use the credential of the machine by accident
var liveCredentialEnv = map[string]string{
	"TEST_AZURE_TENANT_ID":     "AZURE_TENANT_ID",
	"TEST_AZURE_CLIENT_ID":     "AZURE_CLIENT_ID",
	"TEST_AZURE_CLIENT_SECRET": "AZURE_CLIENT_SECRET",
}

// TestLive reads a secret of a real vault through the filter, if the live test is configured
func TestLive(t *testing.T) {
	ref, want := os.Getenv(liveRefEnv), os.Getenv(liveValueEnv)
	if ref == "" || want == "" {
		t.Skipf("set %s and %s to read a secret of a real vault", liveRefEnv, liveValueEnv)
	}
	for from, to := range liveCredentialEnv {
		if value := os.Getenv(from); value != "" {
			t.Setenv(to, value)
		}
	}

	result, err := secret.NewFilter(&provider{}).Process(ref)
	assert.NoError(t, err)
	assert.Equal(t, want, result)
}
