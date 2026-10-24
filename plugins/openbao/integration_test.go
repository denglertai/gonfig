package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/openbao/openbao/api/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Integration tests run against a dev server of OpenBao or Vault if these variables are set, e.g. for
// "bao server -dev -dev-root-token-id=root": TEST_OPENBAO_ADDR=http://127.0.0.1:8200 TEST_OPENBAO_TOKEN=root.
// They don't start with BAO_, VAULT_ or GONFIG_, so TestMain keeps them.
const (
	integrationAddrEnv  = "TEST_OPENBAO_ADDR"
	integrationTokenEnv = "TEST_OPENBAO_TOKEN"
)

// integrationServer holds the mounts and credentials created on the server for a test run
type integrationServer struct {
	addr  string
	token string
	// kv1 and kv2 are the mounts of the secrets engines, kv2 with two segments
	kv1 string
	kv2 string
	// approle is the mount of the AppRole auth method, whose role may only read the secrets below app/
	approle  string
	roleID   string
	secretID string
}

// setUpIntegrationServer creates secrets engines, secrets, a policy and an AppRole on the server. Every run uses its
// own mounts, so tests can run again against the same server. The test is skipped without a server.
func setUpIntegrationServer(t *testing.T) *integrationServer {
	t.Helper()

	server := &integrationServer{addr: os.Getenv(integrationAddrEnv), token: os.Getenv(integrationTokenEnv)}
	if server.addr == "" || server.token == "" {
		t.Skipf("set %s and %s to run the integration tests against a dev server", integrationAddrEnv, integrationTokenEnv)
	}
	prefix := fmt.Sprintf("gonfig-%d", time.Now().UnixNano())
	server.kv1, server.kv2, server.approle = prefix+"-kv1", prefix+"/team", prefix+"-approle"

	config := api.DefaultConfig()
	config.Address = server.addr
	client, err := api.NewClient(config)
	require.NoError(t, err)
	client.SetToken(server.token)
	ctx := context.Background()

	require.NoError(t, client.Sys().MountWithContext(ctx, server.kv1, &api.MountInput{Type: "kv", Options: map[string]string{"version": "1"}}))
	require.NoError(t, client.Sys().MountWithContext(ctx, server.kv2, &api.MountInput{Type: "kv", Options: map[string]string{"version": "2"}}))

	_, err = client.KVv2(server.kv2).Put(ctx, "app/db", map[string]any{"user": "old"})
	require.NoError(t, err)
	_, err = client.KVv2(server.kv2).Put(ctx, "app/db", map[string]any{"user": "app", "password": "s3cret", "port": 5432})
	require.NoError(t, err)
	_, err = client.KVv2(server.kv2).Put(ctx, "app/deleted", map[string]any{"password": "gone"})
	require.NoError(t, err)
	require.NoError(t, client.KVv2(server.kv2).Delete(ctx, "app/deleted"))
	_, err = client.KVv2(server.kv2).Put(ctx, "other/db", map[string]any{"password": "forbidden"})
	require.NoError(t, err)
	require.NoError(t, client.KVv1(server.kv1).Put(ctx, "app/db", map[string]any{"password": "v1"}))

	policy := fmt.Sprintf("path %q { capabilities = [\"read\"] }\npath %q { capabilities = [\"read\"] }\n", server.kv2+"/data/app/*", server.kv1+"/app/*")
	require.NoError(t, client.Sys().PutPolicyWithContext(ctx, prefix, policy))
	require.NoError(t, client.Sys().EnableAuthWithOptionsWithContext(ctx, server.approle, &api.EnableAuthOptions{Type: "approle"}))
	_, err = client.Logical().WriteWithContext(ctx, "auth/"+server.approle+"/role/gonfig", map[string]any{"token_policies": prefix})
	require.NoError(t, err)
	roleID, err := client.Logical().ReadWithContext(ctx, "auth/"+server.approle+"/role/gonfig/role-id")
	require.NoError(t, err)
	server.roleID = roleID.Data["role_id"].(string)
	secretID, err := client.Logical().WriteWithContext(ctx, "auth/"+server.approle+"/role/gonfig/secret-id", nil)
	require.NoError(t, err)
	server.secretID = secretID.Data["secret_id"].(string)

	return server
}

// TestIntegrationToken reads secrets with a token from a real server
func TestIntegrationToken(t *testing.T) {
	server := setUpIntegrationServer(t)
	t.Setenv("BAO_ADDR", server.addr)
	t.Setenv("BAO_TOKEN", server.token)
	p := &provider{}
	require.NoError(t, p.Connect(context.Background()))

	testCases := []struct {
		desc    string
		ref     string
		params  map[string]string
		want    map[string]any
		wantErr string
	}{
		{desc: "kv2", ref: server.kv2 + "/app/db", params: map[string]string{"mount": server.kv2}, want: map[string]any{"user": "app", "password": "s3cret", "port": "5432"}},
		{desc: "version", ref: server.kv2 + "/app/db", params: map[string]string{"mount": server.kv2, "version": "1"}, want: map[string]any{"user": "old"}},
		{desc: "kv1", ref: server.kv1 + "/app/db", params: map[string]string{"kv": "1"}, want: map[string]any{"password": "v1"}},
		{desc: "deleted", ref: server.kv2 + "/app/deleted", params: map[string]string{"mount": server.kv2}, wantErr: "the secret is deleted"},
		{desc: "missing", ref: server.kv2 + "/app/missing", params: map[string]string{"mount": server.kv2}, wantErr: "not found"},
		{desc: "missing version", ref: server.kv2 + "/app/db", params: map[string]string{"mount": server.kv2, "version": "9"}, wantErr: "not found"},
		{desc: "missing kv1", ref: server.kv1 + "/app/missing", params: map[string]string{"kv": "1"}, wantErr: "not found"},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			result, err := p.Read(context.Background(), tC.ref, tC.params)
			if tC.wantErr != "" {
				assert.EqualError(t, err, tC.wantErr)
				return
			}
			require.NoError(t, err)
			// Numbers are returned as json.Number, which are compared as text
			got := map[string]any{}
			for key, value := range result.(map[string]any) {
				got[key] = fmt.Sprint(value)
			}
			assert.Equal(t, tC.want, got)
		})
	}
}

// TestIntegrationAppRole logs in with AppRole and checks that the policy of the role applies
func TestIntegrationAppRole(t *testing.T) {
	server := setUpIntegrationServer(t)
	t.Setenv("BAO_ADDR", server.addr)
	t.Setenv("GONFIG_OPENBAO_AUTH", "approle")
	t.Setenv("GONFIG_OPENBAO_AUTH_MOUNT", server.approle)
	t.Setenv("GONFIG_OPENBAO_ROLE_ID", server.roleID)
	t.Setenv("GONFIG_OPENBAO_SECRET_ID", server.secretID)
	p := &provider{}
	require.NoError(t, p.Connect(context.Background()))

	result, err := p.Read(context.Background(), server.kv2+"/app/db", map[string]string{"mount": server.kv2})
	require.NoError(t, err)
	assert.Equal(t, "s3cret", result.(map[string]any)["password"])

	_, err = p.Read(context.Background(), server.kv2+"/other/db", map[string]string{"mount": server.kv2})
	assert.EqualError(t, err, "status 403: permission denied")

	t.Setenv("GONFIG_OPENBAO_SECRET_ID", "wrong")
	err = (&provider{}).Connect(context.Background())
	assert.ErrorContains(t, err, "failed to log in with "+server.approle+": status 400: ")
}

// TestIntegrationGonfig builds gonfig and the plugin and fills a config file with secrets of a real server
func TestIntegrationGonfig(t *testing.T) {
	server := setUpIntegrationServer(t)

	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "plugins")
	executable := func(name string) string {
		if runtime.GOOS == "windows" {
			return name + ".exe"
		}
		return name
	}
	gonfig := filepath.Join(dir, executable("gonfig"))
	goTool, err := exec.LookPath("go")
	require.NoError(t, err, "building gonfig and the plugin requires the go tool")
	// Every binary is built within its own module, which has the go.sum entries of its dependencies
	build := func(output string, moduleDir string) {
		cmd := exec.Command(goTool, "build", "-o", output, ".")
		cmd.Dir = moduleDir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
	}
	build(filepath.Join(pluginDir, executable("gonfig-plugin-openbao")), ".")
	build(gonfig, filepath.Join("..", ".."))

	template := filepath.Join(dir, "app.yaml")
	require.NoError(t, os.WriteFile(template, []byte("password: ${DB_PASSWORD | openbao(mount="+server.kv2+")}\n"+
		"port: ${DB_PORT | openbao(mount="+server.kv2+") | to_int}\n"+
		"db: ${DB | openbao(mount="+server.kv2+") | fragment}\n"), 0o600))

	cmd := exec.Command(gonfig, "--plugin-path", pluginDir, "config", "process", "-f", template)
	cmd.Env = append(os.Environ(),
		"BAO_ADDR="+server.addr,
		"BAO_TOKEN="+server.token,
		"DB_PASSWORD="+server.kv2+"/app/db#password",
		"DB_PORT="+server.kv2+"/app/db#port",
		"DB="+server.kv2+"/app/db",
	)
	out, err := cmd.Output()
	require.NoError(t, err)
	assert.Equal(t, "password: s3cret\nport: 5432\ndb:\n  password: s3cret\n  port: 5432\n  user: app\n", string(out))
}
