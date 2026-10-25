package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/denglertai/gonfig/pkg/plugin/secret"
	"github.com/stretchr/testify/assert"
)

// TestMain removes the variables configuring OpenBao, Vault and gonfig on the machine, so the tests don't depend on them
func TestMain(m *testing.M) {
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "BAO_") || strings.HasPrefix(key, "VAULT_") || strings.HasPrefix(key, "GONFIG_") {
			_ = os.Unsetenv(key)
		}
	}
	os.Exit(m.Run())
}

// fakeOpenBao serves the parts of the OpenBao API the plugin uses: KV v2 secrets at the mounts "secret" and
// "kv/team", KV v1 secrets at "kv1", and AppRole and Kubernetes logins. Every request except logins needs a token.
func fakeOpenBao(t *testing.T) *httptest.Server {
	t.Helper()

	tokens := map[string]bool{"root": true, "approle-token": true, "kubernetes-token": true}
	kv2 := func(data any, version int) map[string]any {
		return map[string]any{"data": map[string]any{"data": data, "metadata": map[string]any{"version": version}}}
	}
	reads := map[string]any{
		"/v1/secret/data/app/db":           kv2(map[string]any{"user": "app", "password": "s3cret", "port": 5432}, 2),
		"/v1/secret/data/app/db?version=1": kv2(map[string]any{"user": "old"}, 1),
		"/v1/secret/data/deleted":          kv2(nil, 3),
		"/v1/kv/team/data/app/db":          kv2(map[string]any{"password": "team"}, 1),
		"/v1/kv1/app/db":                   map[string]any{"data": map[string]any{"password": "v1"}},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reply := func(status int, body any) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(body)
		}

		if r.Method == http.MethodPut || r.Method == http.MethodPost {
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			switch {
			case r.Header.Get("X-Vault-Token") != "":
				reply(http.StatusBadRequest, map[string]any{"errors": []string{"logins must not send a token"}})
			case r.URL.Path == "/v1/auth/approle/login" && body["role_id"] == "role" && body["secret_id"] == "id",
				r.URL.Path == "/v1/auth/team-approle/login" && body["role_id"] == "role" && body["secret_id"] == "id":
				reply(http.StatusOK, map[string]any{"auth": map[string]any{"client_token": "approle-token"}})
			case r.URL.Path == "/v1/auth/kubernetes/login" && body["role"] == "app" && body["jwt"] == "service-account":
				reply(http.StatusOK, map[string]any{"auth": map[string]any{"client_token": "kubernetes-token"}})
			case r.URL.Path == "/v1/auth/notoken/login":
				reply(http.StatusOK, map[string]any{"auth": nil})
			default:
				reply(http.StatusBadRequest, map[string]any{"errors": []string{"invalid credentials"}})
			}
			return
		}

		key := r.URL.Path
		if r.URL.RawQuery != "" {
			key += "?" + r.URL.RawQuery
		}
		switch {
		case !tokens[r.Header.Get("X-Vault-Token")]:
			// OpenBao wraps the messages in a list like this
			reply(http.StatusForbidden, map[string]any{"errors": []string{"1 error occurred:\n\t* permission denied\n\n"}})
		case strings.HasSuffix(key, "/invalid-json"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("not json"))
		case strings.HasSuffix(key, "/proxy-error"):
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte("<html>bad gateway</html>"))
		case reads[key] != nil:
			reply(http.StatusOK, reads[key])
		default:
			reply(http.StatusNotFound, map[string]any{"errors": []string{}})
		}
	}))
	t.Cleanup(server.Close)
	return server
}

// connect returns a provider connected to the server with the environment of the test.
// Errors aren't retried, which the client does with a backoff for status 5xx.
func connect(t *testing.T, server *httptest.Server) (*provider, error) {
	t.Helper()
	t.Setenv("BAO_ADDR", server.URL)
	t.Setenv("BAO_MAX_RETRIES", "0")
	p := &provider{}
	return p, p.Connect(context.Background())
}

// writeFile writes the content to a new file and returns its path
func writeFile(t *testing.T, content string) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "file")
	assert.NoError(t, os.WriteFile(file, []byte(content), 0o600))
	return file
}

// TestRead checks reading KV v1 and v2 secrets, versions and mounts, and the errors of the server
func TestRead(t *testing.T) {
	server := fakeOpenBao(t)
	t.Setenv("BAO_TOKEN", "root")
	p, err := connect(t, server)
	assert.NoError(t, err)

	testCases := []struct {
		desc    string
		ref     string
		params  map[string]string
		want    any
		wantErr string
	}{
		{desc: "kv2", ref: "secret/app/db", want: map[string]any{"user": "app", "password": "s3cret", "port": json.Number("5432")}},
		{desc: "kv2 explicitly", ref: "secret/app/db", params: map[string]string{"kv": "2"}, want: map[string]any{"user": "app", "password": "s3cret", "port": json.Number("5432")}},
		{desc: "slashes around the reference", ref: "/secret/app/db/", params: map[string]string{"kv": "2"}, want: map[string]any{"user": "app", "password": "s3cret", "port": json.Number("5432")}},
		{desc: "version", ref: "secret/app/db", params: map[string]string{"version": "1"}, want: map[string]any{"user": "old"}},
		{desc: "mount with several segments", ref: "kv/team/app/db", params: map[string]string{"mount": "kv/team"}, want: map[string]any{"password": "team"}},
		{desc: "mount with slashes", ref: "kv/team/app/db", params: map[string]string{"mount": "/kv/team/"}, want: map[string]any{"password": "team"}},
		{desc: "kv1", ref: "kv1/app/db", params: map[string]string{"kv": "1"}, want: map[string]any{"password": "v1"}},
		{desc: "missing kv2 secret", ref: "secret/missing", wantErr: "not found"},
		{desc: "missing kv1 secret", ref: "kv1/missing", params: map[string]string{"kv": "1"}, wantErr: "not found"},
		{desc: "missing version", ref: "secret/app/db", params: map[string]string{"version": "7"}, wantErr: "not found"},
		{desc: "deleted secret", ref: "secret/deleted", wantErr: "the secret is deleted"},
		{desc: "raw error of a proxy", ref: "secret/proxy-error", wantErr: "status 502"},
		// Other errors of the client are returned as they are
		{desc: "invalid response", ref: "secret/invalid-json", wantErr: "error encountered while reading secret at secret/data/invalid-json: invalid character 'o' in literal null (expecting 'u')"},
		{desc: "no path", ref: "secret", wantErr: "no path after the mount, e.g. secret/app/db"},
		{desc: "reference outside the mount", ref: "secret/app/db", params: map[string]string{"mount": "kv/team"}, wantErr: `the reference has to start with the mount "kv/team", followed by the path`},
		{desc: "reference equal to the mount", ref: "kv/team", params: map[string]string{"mount": "kv/team"}, wantErr: `the reference has to start with the mount "kv/team", followed by the path`},
		{desc: "invalid kv", ref: "secret/app/db", params: map[string]string{"kv": "3"}, wantErr: `invalid KV version "3", use 1 or 2`},
		{desc: "invalid version", ref: "secret/app/db", params: map[string]string{"version": "latest"}, wantErr: `invalid version "latest", use a positive number`},
		{desc: "version zero", ref: "secret/app/db", params: map[string]string{"version": "0"}, wantErr: `invalid version "0", use a positive number`},
		{desc: "version of kv1", ref: "kv1/app/db", params: map[string]string{"kv": "1", "version": "1"}, wantErr: "versions require the KV version 2"},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			result, err := p.Read(context.Background(), tC.ref, tC.params)
			if tC.wantErr != "" {
				assert.EqualError(t, err, tC.wantErr)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tC.want, result)
		})
	}
}

// TestReadForbidden checks that the messages of OpenBao are kept, while the URL of the request is left out
func TestReadForbidden(t *testing.T) {
	server := fakeOpenBao(t)
	t.Setenv("BAO_TOKEN", "invalid")
	p, err := connect(t, server)
	assert.NoError(t, err)

	_, err = p.Read(context.Background(), "secret/app/db", nil)
	assert.EqualError(t, err, "status 403: permission denied")
}

// TestFilter checks the filter of the plugin end to end with the shared secret package, e.g. fields and JSON
func TestFilter(t *testing.T) {
	server := fakeOpenBao(t)
	t.Setenv("BAO_ADDR", server.URL)
	t.Setenv("BAO_TOKEN", "root")
	filter := secret.NewFilter(&provider{})

	for value, want := range map[string]string{
		"secret/app/db#password": "s3cret",
		"secret/app/db#port":     "5432",
		"secret/app/db":          `{"password":"s3cret","port":5432,"user":"app"}`,
	} {
		result, err := filter.Process(value)
		assert.NoError(t, err, value)
		assert.Equal(t, want, result, value)
	}

	_, err := filter.Process("secret/missing#password")
	assert.EqualError(t, err, `failed to read the secret "secret/missing": not found`)
}

// TestConnect checks the configuration and authentication methods
func TestConnect(t *testing.T) {
	server := fakeOpenBao(t)

	testCases := []struct {
		desc    string
		env     map[string]string
		files   map[string]string
		wantErr string
	}{
		{desc: "token", env: map[string]string{"BAO_TOKEN": "root"}},
		{desc: "token method", env: map[string]string{"GONFIG_OPENBAO_AUTH": "token", "BAO_TOKEN": "root"}},
		{desc: "vault token", env: map[string]string{"VAULT_TOKEN": "root"}},
		{desc: "token file", files: map[string]string{"GONFIG_OPENBAO_TOKEN_FILE": "root\n"}},
		{desc: "token file replaces the environment", env: map[string]string{"BAO_TOKEN": "invalid"}, files: map[string]string{"GONFIG_OPENBAO_TOKEN_FILE": "root"}},
		{desc: "no token", wantErr: "no token, set BAO_TOKEN, VAULT_TOKEN or GONFIG_OPENBAO_TOKEN_FILE, or another GONFIG_OPENBAO_AUTH"},
		{desc: "token and token file", env: map[string]string{"GONFIG_OPENBAO_TOKEN": "root"}, files: map[string]string{"GONFIG_OPENBAO_TOKEN_FILE": "root"}, wantErr: "set either GONFIG_OPENBAO_TOKEN or GONFIG_OPENBAO_TOKEN_FILE, not both"},
		{desc: "empty token file", files: map[string]string{"GONFIG_OPENBAO_TOKEN_FILE": " \n"}, wantErr: "FILE is empty"},
		{desc: "missing token file", env: map[string]string{"GONFIG_OPENBAO_TOKEN_FILE": "/does/not/exist"}, wantErr: "failed to read GONFIG_OPENBAO_TOKEN_FILE: open /does/not/exist: "},
		{desc: "approle", env: map[string]string{"GONFIG_OPENBAO_AUTH": "approle", "GONFIG_OPENBAO_ROLE_ID": "role", "GONFIG_OPENBAO_SECRET_ID": "id", "BAO_TOKEN": "sent-with-login"}},
		{desc: "approle with files", env: map[string]string{"GONFIG_OPENBAO_AUTH": "approle"}, files: map[string]string{"GONFIG_OPENBAO_ROLE_ID_FILE": "role\n", "GONFIG_OPENBAO_SECRET_ID_FILE": "id\n"}},
		{desc: "approle at another mount", env: map[string]string{"GONFIG_OPENBAO_AUTH": "approle", "GONFIG_OPENBAO_AUTH_MOUNT": "/team-approle/", "GONFIG_OPENBAO_ROLE_ID": "role", "GONFIG_OPENBAO_SECRET_ID": "id"}},
		{desc: "approle without role id", env: map[string]string{"GONFIG_OPENBAO_AUTH": "approle", "GONFIG_OPENBAO_SECRET_ID": "id"}, wantErr: "no role id, set GONFIG_OPENBAO_ROLE_ID or GONFIG_OPENBAO_ROLE_ID_FILE"},
		{desc: "approle without secret id", env: map[string]string{"GONFIG_OPENBAO_AUTH": "approle", "GONFIG_OPENBAO_ROLE_ID": "role"}, wantErr: "no secret id, set GONFIG_OPENBAO_SECRET_ID or GONFIG_OPENBAO_SECRET_ID_FILE"},
		{desc: "approle with secret id and file", env: map[string]string{"GONFIG_OPENBAO_AUTH": "approle", "GONFIG_OPENBAO_ROLE_ID": "role", "GONFIG_OPENBAO_SECRET_ID": "id"}, files: map[string]string{"GONFIG_OPENBAO_SECRET_ID_FILE": "id"}, wantErr: "set either GONFIG_OPENBAO_SECRET_ID or GONFIG_OPENBAO_SECRET_ID_FILE, not both"},
		{desc: "approle with wrong secret id", env: map[string]string{"GONFIG_OPENBAO_AUTH": "approle", "GONFIG_OPENBAO_ROLE_ID": "role", "GONFIG_OPENBAO_SECRET_ID": "wrong"}, wantErr: "failed to log in with approle: status 400: invalid credentials"},
		{desc: "login without token in response", env: map[string]string{"GONFIG_OPENBAO_AUTH": "approle", "GONFIG_OPENBAO_AUTH_MOUNT": "notoken", "GONFIG_OPENBAO_ROLE_ID": "role", "GONFIG_OPENBAO_SECRET_ID": "id"}, wantErr: "failed to log in with notoken: the response contains no token"},
		{desc: "kubernetes", env: map[string]string{"GONFIG_OPENBAO_AUTH": "kubernetes", "GONFIG_OPENBAO_ROLE": "app"}, files: map[string]string{"GONFIG_OPENBAO_JWT_FILE": "service-account\n"}},
		// The default path of the service account token doesn't exist outside of pods
		{desc: "kubernetes with the default service account token", env: map[string]string{"GONFIG_OPENBAO_AUTH": "kubernetes", "GONFIG_OPENBAO_ROLE": "app"}, wantErr: "failed to read the service account token: open /var/run/secrets/kubernetes.io/serviceaccount/token: "},
		{desc: "kubernetes without role", env: map[string]string{"GONFIG_OPENBAO_AUTH": "kubernetes"}, wantErr: "no role, set GONFIG_OPENBAO_ROLE"},
		{desc: "kubernetes without service account token", env: map[string]string{"GONFIG_OPENBAO_AUTH": "kubernetes", "GONFIG_OPENBAO_ROLE": "app", "GONFIG_OPENBAO_JWT_FILE": "/does/not/exist"}, wantErr: "failed to read the service account token: open /does/not/exist: "},
		{desc: "unknown method", env: map[string]string{"GONFIG_OPENBAO_AUTH": "ldap"}, wantErr: `unknown GONFIG_OPENBAO_AUTH "ldap", use token, approle or kubernetes`},
		{desc: "invalid configuration", env: map[string]string{"BAO_TOKEN": "root", "BAO_CLIENT_TIMEOUT": "soon"}, wantErr: "invalid configuration: "},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			for key, value := range tC.env {
				t.Setenv(key, value)
			}
			for key, content := range tC.files {
				file := writeFile(t, content)
				// The name of the file is part of some errors, so it is the same for every test
				named := filepath.Join(filepath.Dir(file), "FILE")
				assert.NoError(t, os.Rename(file, named))
				t.Setenv(key, named)
			}

			p, err := connect(t, server)
			if tC.wantErr != "" {
				assert.ErrorContains(t, err, tC.wantErr)
				assert.Nil(t, p.client)
				return
			}
			assert.NoError(t, err)
			_, err = p.Read(context.Background(), "secret/app/db", nil)
			assert.NoError(t, err)
		})
	}
}

// TestConnectWithoutAddress checks that an address is required, instead of the client's default 127.0.0.1:8200
func TestConnectWithoutAddress(t *testing.T) {
	t.Setenv("BAO_TOKEN", "root")
	assert.EqualError(t, (&provider{}).Connect(context.Background()), "no address, set BAO_ADDR or VAULT_ADDR")

	// The address of Vault works as well, since the client falls back to the VAULT_* variables
	server := fakeOpenBao(t)
	t.Setenv("VAULT_ADDR", server.URL)
	p := &provider{}
	assert.NoError(t, p.Connect(context.Background()))
	_, err := p.Read(context.Background(), "secret/app/db", nil)
	assert.NoError(t, err)
}

// TestConnectInvalidAddress checks that an address the client cannot use fails connecting
func TestConnectInvalidAddress(t *testing.T) {
	t.Setenv("BAO_ADDR", "http://[::1")
	t.Setenv("BAO_TOKEN", "root")
	assert.ErrorContains(t, (&provider{}).Connect(context.Background()), "invalid configuration: ")
}

// TestReadUnreachable checks that errors of requests without a response, e.g. to a closed port, name the method and
// URL without the path the KV client adds, and without a password of the address
func TestReadUnreachable(t *testing.T) {
	server := fakeOpenBao(t)
	t.Setenv("BAO_ADDR", strings.Replace(server.URL, "http://", "http://user:hunter2@", 1))
	t.Setenv("BAO_MAX_RETRIES", "0")
	t.Setenv("BAO_TOKEN", "root")
	p := &provider{}
	assert.NoError(t, p.Connect(context.Background()))
	server.Close()

	_, err := p.Read(context.Background(), "secret/app/db", nil)
	redacted := strings.Replace(server.URL, "http://", "http://user:***@", 1)
	assert.ErrorContains(t, err, `Get "`+redacted+`/v1/secret/data/app/db": `)
	assert.NotContains(t, err.Error(), "hunter2")
	assert.NotContains(t, err.Error(), "error encountered")

	t.Setenv("GONFIG_OPENBAO_AUTH", "approle")
	t.Setenv("GONFIG_OPENBAO_ROLE_ID", "role")
	t.Setenv("GONFIG_OPENBAO_SECRET_ID", "id")
	err = (&provider{}).Connect(context.Background())
	assert.ErrorContains(t, err, `failed to log in with approle: Put "`+redacted+`/v1/auth/approle/login": `)
	assert.NotContains(t, err.Error(), "hunter2")
}

// TestUnwrapMessages checks unwrapping the messages of OpenBao from its lists of errors
func TestUnwrapMessages(t *testing.T) {
	for message, want := range map[string][]string{
		"1 error occurred:\n\t* permission denied\n\n":          {"permission denied"},
		"2 errors occurred:\n\t* first\n\t* second\n\n":         {"first", "second"},
		"invalid credentials":                                   {"invalid credentials"},
		"something occurred:\nwithout items":                    {"something occurred:\nwithout items"},
		"multi\nline occurred:\n\t* not a header in one line\n": {"multi\nline occurred:\n\t* not a header in one line\n"},
	} {
		assert.Equal(t, want, unwrapMessages(message), message)
	}
}

// TestParams checks the params the filter accepts, and its description
func TestParams(t *testing.T) {
	assert.Equal(t, []string{"mount", "kv", "version"}, (&provider{}).Params())
	assert.Equal(t, "Reads a secret of the KV secrets engine, e.g. secret/app/db#password", secret.NewFilter(&provider{}).Description())
}
