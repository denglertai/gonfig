package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	azfake "github.com/Azure/azure-sdk-for-go/sdk/azcore/fake"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/security/keyvault/azsecrets"
	"github.com/Azure/azure-sdk-for-go/sdk/security/keyvault/azsecrets/fake"
	"github.com/denglertai/gonfig/pkg/plugin/secret"
	"github.com/stretchr/testify/assert"
)

// TestMain removes the variables configuring Azure and gonfig on the machine, so the tests don't depend on them
func TestMain(m *testing.M) {
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "AZURE_") || strings.HasPrefix(key, "GONFIG_") {
			_ = os.Unsetenv(key)
		}
	}
	os.Exit(m.Run())
}

// fakeVault returns a provider whose requests are served by a fake Key Vault with the secrets, by name and version.
// The current version of a secret has the key "<name>/". The secret "forbidden" fails with status 403.
// requests counts the requests per secret and version.
func fakeVault(t *testing.T, secrets map[string]string) (*provider, map[string]int) {
	t.Helper()

	requests := map[string]int{}
	server := fake.Server{
		GetSecret: func(_ context.Context, name string, version string, _ *azsecrets.GetSecretOptions) (resp azfake.Responder[azsecrets.GetSecretResponse], errResp azfake.ErrorResponder) {
			// The regular expression of the fake server matches slashes within names, so the name contains the
			// version, e.g. "db/v1", and a trailing slash for the current version, e.g. "db/"
			name, version, _ = strings.Cut(strings.Trim(name+"/"+version, "/"), "/")
			key := name + "/" + version
			requests[key]++
			value, found := secrets[key]
			switch {
			case name == "forbidden":
				errResp.SetResponseError(http.StatusForbidden, "Forbidden")
			case name == "novalue":
				resp.SetResponse(http.StatusOK, azsecrets.GetSecretResponse{}, nil)
			case !found:
				errResp.SetResponseError(http.StatusNotFound, "SecretNotFound")
			default:
				resp.SetResponse(http.StatusOK, azsecrets.GetSecretResponse{Secret: azsecrets.Secret{Value: to.Ptr(value)}}, nil)
			}
			return
		},
	}

	p := &provider{
		credential: &azfake.TokenCredential{},
		options:    &azsecrets.ClientOptions{ClientOptions: azcore.ClientOptions{Transport: fake.NewServerTransport(&server)}},
	}
	assert.NoError(t, p.Connect(context.Background()))
	return p, requests
}

// TestRead checks reading current and older versions of secrets and the errors of Key Vault
func TestRead(t *testing.T) {
	p, _ := fakeVault(t, map[string]string{"db-password/": "s3cret", "db-password/v1": "old", "db/": `{"user": "app"}`})

	testCases := []struct {
		desc    string
		ref     string
		want    string
		wantErr string
	}{
		{desc: "vault name", ref: "myvault/db-password", want: "s3cret"},
		{desc: "version", ref: "myvault/db-password/v1", want: "old"},
		{desc: "vault host", ref: "myvault.vault.azure.cn/db-password", want: "s3cret"},
		{desc: "secret URL", ref: "https://myvault.vault.azure.net/secrets/db-password", want: "s3cret"},
		{desc: "secret URL with version", ref: "https://myvault.vault.azure.net/secrets/db-password/v1", want: "old"},
		{desc: "secret URL with trailing slash", ref: "https://myvault.vault.azure.net/secrets/db-password/", want: "s3cret"},
		{desc: "missing secret", ref: "myvault/missing", wantErr: "status 404 SecretNotFound"},
		{desc: "missing version", ref: "myvault/db-password/v9", wantErr: "status 404 SecretNotFound"},
		{desc: "forbidden", ref: "myvault/forbidden", wantErr: "status 403 Forbidden"},
		{desc: "no value", ref: "myvault/novalue", wantErr: "the secret has no value"},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			result, err := p.Read(context.Background(), tC.ref, nil)
			if tC.wantErr != "" {
				assert.EqualError(t, err, tC.wantErr)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tC.want, result)
		})
	}
}

// TestParseReference checks references by vault name, host and URL, and invalid references
func TestParseReference(t *testing.T) {
	testCases := []struct {
		ref, suffix                    string
		wantURL, wantName, wantVersion string
		wantErr                        string
	}{
		{ref: "myvault/db", wantURL: "https://myvault.vault.azure.net", wantName: "db"},
		{ref: "myvault/db/v1", wantURL: "https://myvault.vault.azure.net", wantName: "db", wantVersion: "v1"},
		{ref: "myvault/db", suffix: "vault.azure.cn", wantURL: "https://myvault.vault.azure.cn", wantName: "db"},
		{ref: "myvault/db", suffix: ".vault.usgovcloudapi.net", wantURL: "https://myvault.vault.usgovcloudapi.net", wantName: "db"},
		{ref: "myvault.vault.azure.cn/db", suffix: "ignored.example", wantURL: "https://myvault.vault.azure.cn", wantName: "db"},
		{ref: "https://myvault.vault.azure.net/secrets/db/v1", wantURL: "https://myvault.vault.azure.net", wantName: "db", wantVersion: "v1"},
		{ref: "https://myvault.vault.azure.net:443/secrets/db", wantURL: "https://myvault.vault.azure.net:443", wantName: "db"},
		{ref: "myvault", wantErr: `invalid reference, use <vault>/<secret>[/<version>]`},
		{ref: "myvault/", wantErr: `invalid reference, use <vault>/<secret>[/<version>]`},
		{ref: "/db", wantErr: `invalid reference, use <vault>/<secret>[/<version>]`},
		{ref: "myvault/db/v1/extra", wantErr: `invalid reference, use <vault>/<secret>[/<version>]`},
		{ref: "http://myvault.vault.azure.net/secrets/db", wantErr: `invalid secret URL, use https://<vault host>/secrets/<secret>[/<version>]`},
		{ref: "https:///secrets/db", wantErr: `invalid secret URL, use https://<vault host>/secrets/<secret>[/<version>]`},
		{ref: "https://myvault.vault.azure.net/keys/db", wantErr: `invalid secret URL, use https://<vault host>/secrets/<secret>[/<version>]`},
		{ref: "https://myvault.vault.azure.net/secrets/", wantErr: `invalid secret URL, use https://<vault host>/secrets/<secret>[/<version>]`},
		{ref: "https://myvault.vault.azure.net/secrets/db?api-version=1", wantErr: `invalid secret URL, use https://<vault host>/secrets/<secret>[/<version>]`},
		{ref: "https://myvault.vault.azure.net/secrets/db/v1/x", wantErr: `invalid secret URL, use https://<vault host>/secrets/<secret>[/<version>]`},
		{ref: "my_vault/db", wantErr: `invalid vault "my_vault"`},
		{ref: "https://my_vault.vault.azure.net/secrets/db", wantErr: `invalid vault "my_vault.vault.azure.net"`},
		{ref: "https://user:pass@myvault.vault.azure.net/secrets/db", wantErr: "the secret URL must not contain credentials"},
		{ref: "https://%zz/secrets/db", wantErr: `invalid secret URL, use https://<vault host>/secrets/<secret>[/<version>]`},
	}
	for _, tC := range testCases {
		vaultURL, name, version, err := parseReference(tC.ref, tC.suffix)
		if tC.wantErr != "" {
			assert.EqualError(t, err, tC.wantErr, tC.ref)
			continue
		}
		assert.NoError(t, err, tC.ref)
		assert.Equal(t, []string{tC.wantURL, tC.wantName, tC.wantVersion}, []string{vaultURL, name, version}, tC.ref)
	}
}

// TestReadSuffix checks that GONFIG_AZURE_KEYVAULT_SUFFIX completes vault names
func TestReadSuffix(t *testing.T) {
	t.Setenv(suffixEnv, "vault.azure.cn")
	p, _ := fakeVault(t, map[string]string{"db/": "cn"})

	result, err := p.Read(context.Background(), "myvault/db", nil)
	assert.NoError(t, err)
	assert.Equal(t, "cn", result)
	assert.Contains(t, p.clients, "https://myvault.vault.azure.cn")
}

// TestFilter checks the filter of the plugin with the shared secret package: caching, fields of JSON secrets and errors
func TestFilter(t *testing.T) {
	p, requests := fakeVault(t, map[string]string{"db-password/": "s3cret", "db/": `{"user": "app", "port": 5432}`})
	filter := secret.NewFilter(p)

	for value, want := range map[string]string{
		"myvault/db-password": "s3cret",
		"myvault/db#user":     "app",
		"myvault/db#port":     "5432",
	} {
		result, err := filter.Process(value)
		assert.NoError(t, err, value)
		assert.Equal(t, want, result, value)
	}
	assert.Equal(t, 1, requests["db/"], "every secret is read once")

	_, err := filter.Process("myvault/missing")
	assert.EqualError(t, err, `failed to read the secret "myvault/missing": status 404 SecretNotFound`)
	_, err = filter.Process("myvault/db-password#user")
	assert.EqualError(t, err, `the secret "myvault/db-password" has no fields, since it doesn't contain a JSON object`)
}

// TestReadInvalidReference checks that invalid references fail before a request
func TestReadInvalidReference(t *testing.T) {
	p, requests := fakeVault(t, nil)
	_, err := p.Read(context.Background(), "my vault/db", nil)
	assert.EqualError(t, err, `invalid vault "my vault"`)
	assert.Empty(t, requests)
}

// TestConnectInvalidCredentialConfiguration checks that an invalid configuration of the default credential fails
func TestConnectInvalidCredentialConfiguration(t *testing.T) {
	t.Setenv("AZURE_TOKEN_CREDENTIALS", "invalid")
	err := (&provider{}).Connect(context.Background())
	assert.ErrorContains(t, err, `failed to create the Azure credential: invalid AZURE_TOKEN_CREDENTIALS value "invalid"`)
}

// errorTransport fails every request with a response of the status and body
type errorTransport struct {
	status int
	body   string
}

// Do returns the response, or an error for status 0
func (e errorTransport) Do(req *http.Request) (*http.Response, error) {
	if e.status == 0 {
		return nil, errors.New("no route to host")
	}
	return &http.Response{
		StatusCode: e.status,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       ioNopCloser(e.body),
		Request:    req,
	}, nil
}

// ioNopCloser returns a body reading the text
func ioNopCloser(text string) *nopCloser {
	return &nopCloser{strings.NewReader(text)}
}

// nopCloser is a body that doesn't need closing
type nopCloser struct{ *strings.Reader }

// Close does nothing
func (*nopCloser) Close() error { return nil }

// TestDescribe checks that errors of Key Vault keep the status, error code and message, but not the URL
func TestDescribe(t *testing.T) {
	testCases := []struct {
		desc      string
		transport policy.Transporter
		wantErr   string
	}{
		{desc: "code and message", transport: errorTransport{status: 403, body: `{"error": {"code": "Forbidden", "message": "The user does not have secrets get permission on key vault 'myvault'."}}`},
			wantErr: "status 403 Forbidden: The user does not have secrets get permission on key vault 'myvault'."},
		{desc: "no body", transport: errorTransport{status: 500, body: ``}, wantErr: "status 500"},
		{desc: "body of a proxy", transport: errorTransport{status: 502, body: `<html>bad gateway</html>`}, wantErr: "status 502"},
		{desc: "no response", transport: errorTransport{}, wantErr: "no route to host"},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			p := &provider{
				credential: &azfake.TokenCredential{},
				options: &azsecrets.ClientOptions{ClientOptions: azcore.ClientOptions{
					Transport: tC.transport,
					Retry:     policy.RetryOptions{MaxRetries: -1},
				}},
			}
			assert.NoError(t, p.Connect(context.Background()))

			_, err := p.Read(context.Background(), "myvault/db", nil)
			assert.ErrorContains(t, err, tC.wantErr)
			assert.NotContains(t, err.Error(), "https://myvault.vault.azure.net")
		})
	}
}

// TestSingleLine checks joining the lines of errors, e.g. of DefaultAzureCredential
func TestSingleLine(t *testing.T) {
	for message, want := range map[string]string{
		"DefaultAzureCredential: failed to acquire a token.\nAttempted credentials:\n\tEnvironmentCredential: missing AZURE_TENANT_ID\n\tAzureCLICredential: not found\n": "DefaultAzureCredential: failed to acquire a token. Attempted credentials: EnvironmentCredential: missing AZURE_TENANT_ID; AzureCLICredential: not found",
		"no route to host": "no route to host",
		"first\nsecond":    "first second",
	} {
		assert.Equal(t, want, singleLine(message))
	}
}

// TestConnectDefaultCredential checks that the default credential is created without contacting Azure, so the plugin
// fails only the values using it if no credential is available
func TestConnectDefaultCredential(t *testing.T) {
	p := &provider{}
	assert.NoError(t, p.Connect(context.Background()))
	assert.NotNil(t, p.credential)
}

// countingCredential fails every request with err and counts them. Requests with a context that ended fail with its
// error, like a credential waiting for a response.
type countingCredential struct {
	err   error
	calls int
}

// GetToken counts the call and returns a token, or the error of an ended context, or err if set
func (c *countingCredential) GetToken(ctx context.Context, _ policy.TokenRequestOptions) (azcore.AccessToken, error) {
	c.calls++
	if ctx.Err() != nil {
		return azcore.AccessToken{}, ctx.Err()
	}
	if c.err != nil {
		return azcore.AccessToken{}, c.err
	}
	return azcore.AccessToken{Token: "token"}, nil
}

// TestFailOnceCredential checks that the first error of a credential is returned without asking it again, while
// successful requests are passed on
func TestFailOnceCredential(t *testing.T) {
	succeeding := &countingCredential{}
	credential := &failOnceCredential{credential: succeeding}
	for range 2 {
		token, err := credential.GetToken(context.Background(), policy.TokenRequestOptions{})
		assert.NoError(t, err)
		assert.Equal(t, "token", token.Token)
	}
	assert.Equal(t, 2, succeeding.calls)

	failing := &countingCredential{err: errors.New("no credential")}
	credential = &failOnceCredential{credential: failing}
	for range 3 {
		_, err := credential.GetToken(context.Background(), policy.TokenRequestOptions{})
		assert.EqualError(t, err, "no credential")
	}
	assert.Equal(t, 1, failing.calls)

	// A request whose context ended, e.g. by the timeout of a read, is tried again by the next read
	succeeding = &countingCredential{}
	credential = &failOnceCredential{credential: succeeding}
	ended, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := credential.GetToken(ended, policy.TokenRequestOptions{})
	assert.ErrorIs(t, err, context.Canceled)
	token, err := credential.GetToken(context.Background(), policy.TokenRequestOptions{})
	assert.NoError(t, err)
	assert.Equal(t, "token", token.Token)
}

// TestParams checks that the filter accepts no params, and its description
func TestParams(t *testing.T) {
	assert.Nil(t, (&provider{}).Params())
	assert.Equal(t, "Reads a secret by reference, e.g. myvault/db-password", secret.NewFilter(&provider{}).Description())
}
