package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/security/keyvault/azsecrets"
)

// suffixEnv sets the DNS suffix of vaults in short references, e.g. vault.azure.cn for Azure China
const suffixEnv = "GONFIG_AZURE_KEYVAULT_SUFFIX"

// defaultSuffix is the DNS suffix of vaults in the Azure public cloud
const defaultSuffix = "vault.azure.net"

// provider reads secrets of Azure Key Vault, see secret.Provider
type provider struct {
	// credential authenticates the requests, DefaultAzureCredential unless set by tests
	credential azcore.TokenCredential
	// options are the options of the clients, e.g. a fake transport of tests
	options *azsecrets.ClientOptions
	// clients are the clients of the vaults used, by URL
	clients map[string]*azsecrets.Client
}

// Params returns no params, since the reference names the vault, the secret and its version
func (p *provider) Params() []string {
	return nil
}

// Connect creates the credential, which authenticates on the first request with environment variables, workload
// identity, managed identity or the Azure CLI, see azidentity.DefaultAzureCredential
func (p *provider) Connect(context.Context) error {
	if p.credential == nil {
		credential, err := azidentity.NewDefaultAzureCredential(nil)
		if err != nil {
			return fmt.Errorf("failed to create the Azure credential: %w", err)
		}
		p.credential = &failOnceCredential{credential: credential}
	}
	p.clients = map[string]*azsecrets.Client{}
	return nil
}

// Read reads the value of the secret the reference refers to, see parseReference
func (p *provider) Read(ctx context.Context, ref string, _ map[string]string) (any, error) {
	vaultURL, name, version, err := parseReference(ref, os.Getenv(suffixEnv))
	if err != nil {
		return nil, err
	}

	response, err := p.client(vaultURL).GetSecret(ctx, name, version, nil)
	if err != nil {
		return nil, describe(err)
	}
	if response.Value == nil {
		return nil, errors.New("the secret has no value")
	}
	return *response.Value, nil
}

// client returns the client of the vault, which is created on first use
func (p *provider) client(vaultURL string) *azsecrets.Client {
	if client, found := p.clients[vaultURL]; found {
		return client
	}
	// NewClient doesn't check the URL and only fails for invalid options of the SDK's pipeline, which are fixed here
	client, _ := azsecrets.NewClient(vaultURL, p.credential, p.options)
	p.clients[vaultURL] = client
	return client
}

// parseReference returns the URL of the vault, the name of the secret and its version, which is empty for the current
// one. A reference is either <vault>/<secret>[/<version>], where the vault is a name completed with the suffix or a
// host like myvault.vault.azure.net, or the URL of the secret, e.g. https://myvault.vault.azure.net/secrets/db/<version>.
func parseReference(ref string, suffix string) (vaultURL string, name string, version string, err error) {
	if strings.Contains(ref, "://") {
		u, err := url.Parse(ref)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.RawQuery != "" {
			return "", "", "", errors.New("invalid secret URL, use https://<vault host>/secrets/<secret>[/<version>]")
		}
		segments := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(segments) < 2 || len(segments) > 3 || segments[0] != "secrets" || segments[1] == "" {
			return "", "", "", errors.New("invalid secret URL, use https://<vault host>/secrets/<secret>[/<version>]")
		}
		if !vaultHostRe.MatchString(u.Host) {
			return "", "", "", fmt.Errorf("invalid vault %q", u.Host)
		}
		if len(segments) == 3 {
			version = segments[2]
		}
		return "https://" + u.Host, segments[1], version, nil
	}

	segments := strings.Split(ref, "/")
	if len(segments) < 2 || len(segments) > 3 || slicesContainEmpty(segments) {
		return "", "", "", errors.New("invalid reference, use <vault>/<secret>[/<version>]")
	}
	host := segments[0]
	if !strings.Contains(host, ".") {
		if suffix = strings.Trim(suffix, "."); suffix == "" {
			suffix = defaultSuffix
		}
		host += "." + suffix
	}
	if !vaultHostRe.MatchString(host) {
		return "", "", "", fmt.Errorf("invalid vault %q", segments[0])
	}
	if len(segments) == 3 {
		version = segments[2]
	}
	return "https://" + host, segments[1], version, nil
}

// singleLine joins the lines of an error, e.g. of DefaultAzureCredential, which lists the credentials it attempted:
// "...token.\nAttempted credentials:\n\tEnvironmentCredential: ...\n\tWorkloadIdentityCredential: ..." becomes
// "...token. Attempted credentials: EnvironmentCredential: ...; WorkloadIdentityCredential: ...".
func singleLine(message string) string {
	message = strings.TrimSpace(message)
	message = strings.ReplaceAll(message, ":\n\t", ": ")
	message = strings.ReplaceAll(message, "\n\t", "; ")
	return strings.ReplaceAll(message, "\n", " ")
}

// vaultHostRe matches the hosts of vaults: DNS names, optionally with a port. Vault names consist of letters, digits
// and hyphens.
var vaultHostRe = regexp.MustCompile(`^[A-Za-z0-9.-]+(:[0-9]+)?$`)

// slicesContainEmpty returns whether any of the values is empty
func slicesContainEmpty(values []string) bool {
	for _, value := range values {
		if value == "" {
			return true
		}
	}
	return false
}

// describe returns the status code, the error code and the message of errors of Key Vault, without the URL and the
// headers of the request, which the errors of the SDK contain. Other errors, e.g. of the credential, are joined into a
// single line, which reports of gonfig config check show indented.
func describe(err error) error {
	var response *azcore.ResponseError
	if !errors.As(err, &response) {
		return errors.New(singleLine(err.Error()))
	}

	message := ""
	if response.RawResponse != nil {
		// The body is read by the SDK already, so Payload returns it from its buffer
		if body, readErr := runtime.Payload(response.RawResponse); readErr == nil {
			var parsed struct {
				Error struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			if json.Unmarshal(body, &parsed) == nil {
				message = parsed.Error.Message
			}
		}
	}

	switch {
	case response.ErrorCode != "" && message != "":
		return fmt.Errorf("status %d %s: %s", response.StatusCode, response.ErrorCode, message)
	case response.ErrorCode != "":
		return fmt.Errorf("status %d %s", response.StatusCode, response.ErrorCode)
	default:
		return fmt.Errorf("status %d", response.StatusCode)
	}
}
