package secret

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// fakeProvider serves secrets from a map and counts the calls
type fakeProvider struct {
	secrets    map[string]any
	params     []string
	connectErr error
	readErr    error
	// block makes Connect or Read wait until the context is done
	block    string
	connects int
	reads    []string
}

// Connect counts the call and fails with connectErr
func (p *fakeProvider) Connect(ctx context.Context) error {
	p.connects++
	if p.block == "connect" {
		<-ctx.Done()
		return ctx.Err()
	}
	return p.connectErr
}

// Read returns the secret of the reference, followed by the params, e.g. "a?version=2"
func (p *fakeProvider) Read(ctx context.Context, ref string, params map[string]string) (any, error) {
	key := ref
	if version, found := params["version"]; found {
		key += "?version=" + version
	}
	p.reads = append(p.reads, key)
	if p.block == "read" {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if p.readErr != nil {
		return nil, p.readErr
	}
	secret, found := p.secrets[key]
	if !found {
		return nil, errors.New("not found")
	}
	return secret, nil
}

// Params returns the accepted params
func (p *fakeProvider) Params() []string {
	return p.params
}

// newFake returns a filter with a fake provider serving the secrets
func newFake(secrets map[string]any) (*Filter, *fakeProvider) {
	provider := &fakeProvider{secrets: secrets, params: []string{"version"}}
	return NewFilter("fake", provider), provider
}

// process processes the value with a copy of the filter and the params, like plugin.Serve does for every request
func process(f *Filter, params map[string]string, value any) (any, error) {
	request := *f
	if params != nil {
		request.AcceptParams(params)
	}
	return request.Process(value)
}

// TestProcess checks resolving references to strings, fields and whole secrets
func TestProcess(t *testing.T) {
	f, _ := newFake(map[string]any{
		"plain":  "s3cret",
		"kv":     map[string]any{"password": "pw", "port": float64(5432), "tls": true, "nested": map[string]any{"b": 2, "a": 1}},
		"json":   `{"password": "from json"}`,
		"array":  `["a"]`,
		"null":   `null`,
		"broken": `{"password": `,
	})

	testCases := []struct {
		desc    string
		value   any
		want    any
		wantErr string
	}{
		{desc: "string secret", value: "plain", want: "s3cret"},
		{desc: "field", value: "kv#password", want: "pw"},
		{desc: "number field as JSON", value: "kv#port", want: "5432"},
		{desc: "boolean field as JSON", value: "kv#tls", want: "true"},
		{desc: "nested field as JSON with sorted keys", value: "kv#nested", want: `{"a":1,"b":2}`},
		{desc: "whole secret as JSON", value: "kv", want: `{"nested":{"a":1,"b":2},"password":"pw","port":5432,"tls":true}`},
		{desc: "field of a JSON string", value: "json#password", want: "from json"},
		{desc: "missing field", value: "kv#user", wantErr: `filter fake: the secret "kv" has no field "user"`},
		{desc: "field of a plain string", value: "plain#password", wantErr: `filter fake: the secret "plain" has no fields, since it doesn't contain a JSON object`},
		{desc: "field of a JSON array", value: "array#password", wantErr: `filter fake: the secret "array" has no fields, since it doesn't contain a JSON object`},
		{desc: "field of JSON null", value: "null#password", wantErr: `filter fake: the secret "null" has no fields, since it doesn't contain a JSON object`},
		{desc: "field of invalid JSON", value: "broken#password", wantErr: `filter fake: the secret "broken" has no fields, since it doesn't contain a JSON object`},
		{desc: "missing secret", value: "missing", wantErr: `filter fake: failed to read the secret "missing": not found`},
		{desc: "empty reference", value: "", wantErr: "filter fake: the reference is empty, e.g. as its variable is not set"},
		{desc: "field without secret", value: "#password", wantErr: `filter fake: the reference "#password" names no secret before the #`},
		{desc: "reference of another type", value: 5, wantErr: "filter fake: the reference has to be a string, not int"},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			result, err := process(f, nil, tC.value)
			if tC.wantErr != "" {
				assert.EqualError(t, err, tC.wantErr)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tC.want, result)
		})
	}
}

// TestProcessCachesSecrets checks that every secret is read once per run, also for several fields, while params
// selecting another secret are read separately
func TestProcessCachesSecrets(t *testing.T) {
	f, provider := newFake(map[string]any{
		"kv":           map[string]any{"user": "u", "password": "pw"},
		"kv?version=2": map[string]any{"user": "u2"},
	})

	for _, tC := range []struct {
		params map[string]string
		value  string
		want   string
	}{
		{value: "kv#user", want: "u"},
		{value: "kv#password", want: "pw"},
		{params: map[string]string{"version": "2"}, value: "kv#user", want: "u2"},
		{params: map[string]string{"version": "2"}, value: "kv#user", want: "u2"},
		{value: "kv#user", want: "u"},
	} {
		result, err := process(f, tC.params, tC.value)
		assert.NoError(t, err)
		assert.Equal(t, tC.want, result)
	}

	assert.Equal(t, 1, provider.connects)
	assert.Equal(t, []string{"kv", "kv?version=2"}, provider.reads)
}

// TestProcessConnectsLazilyOnce checks that the provider connects on the first request only, and that a failed
// connection fails every request without connecting again
func TestProcessConnectsLazilyOnce(t *testing.T) {
	f, provider := newFake(map[string]any{"a": "1"})
	provider.connectErr = errors.New("no token")
	assert.Equal(t, 0, provider.connects)

	for range 2 {
		_, err := process(f, nil, "a")
		assert.EqualError(t, err, "filter fake: failed to connect: no token")
	}
	assert.Equal(t, 1, provider.connects)
	assert.Empty(t, provider.reads)
}

// TestCacheKey checks that different references and params never share a key, also if they contain the characters
// separating them
func TestCacheKey(t *testing.T) {
	keys := map[string]string{}
	for desc, tC := range map[string]struct {
		ref    string
		params map[string]string
	}{
		"value with =":       {ref: "a", params: map[string]string{"a": "b=c"}},
		"name with =":        {ref: "a", params: map[string]string{"a=b": "c"}},
		"nul in value":       {ref: "a", params: map[string]string{"x": "1\x00y=2"}},
		"two params":         {ref: "a", params: map[string]string{"x": "1", "y": "2"}},
		"reference":          {ref: "a\x001:x1:1"},
		"reference + params": {ref: "a", params: map[string]string{"x": "1"}},
		"no params":          {ref: "a"},
	} {
		key := cacheKey(tC.ref, tC.params)
		if other, found := keys[key]; found {
			t.Errorf("%s and %s share the key %q", desc, other, key)
		}
		keys[key] = desc
	}
}

// TestProcessDoesNotCacheErrors checks that a failed read is retried by the next request
func TestProcessDoesNotCacheErrors(t *testing.T) {
	f, provider := newFake(map[string]any{"a": "1"})
	provider.readErr = errors.New("unavailable")

	_, err := process(f, nil, "a")
	assert.EqualError(t, err, `filter fake: failed to read the secret "a": unavailable`)

	provider.readErr = nil
	result, err := process(f, nil, "a")
	assert.NoError(t, err)
	assert.Equal(t, "1", result)
	assert.Equal(t, []string{"a", "a"}, provider.reads)
}

// TestProcessParams checks that params the provider does not accept fail the request
func TestProcessParams(t *testing.T) {
	f, provider := newFake(map[string]any{"a": "1"})

	_, err := process(f, map[string]string{"verison": "2"}, "a")
	assert.EqualError(t, err, `filter fake has no param "verison", it accepts version`)

	provider.params = nil
	_, err = process(f, map[string]string{"version": "2"}, "a")
	assert.EqualError(t, err, `filter fake accepts no params, but got "version"`)
	assert.Empty(t, provider.reads)
}

// TestProcessTimeout checks that connecting and reading are limited by the timeout of TimeoutEnv
func TestProcessTimeout(t *testing.T) {
	t.Setenv(TimeoutEnv, "10ms")

	f, provider := newFake(map[string]any{"a": "1"})
	provider.block = "read"
	_, err := process(f, nil, "a")
	assert.EqualError(t, err, `filter fake: reading the secret "a" took longer than 10ms`)

	f, provider = newFake(map[string]any{"a": "1"})
	provider.block = "connect"
	_, err = process(f, nil, "a")
	assert.EqualError(t, err, "filter fake: failed to connect: connecting took too long")
}

// TestRequestTimeout checks the default and invalid values of TimeoutEnv
func TestRequestTimeout(t *testing.T) {
	timeout, err := requestTimeout()
	assert.NoError(t, err)
	assert.Equal(t, DefaultTimeout, timeout)

	t.Setenv(TimeoutEnv, "5s")
	timeout, err = requestTimeout()
	assert.NoError(t, err)
	assert.Equal(t, 5*time.Second, timeout)

	t.Setenv(TimeoutEnv, "29s")
	timeout, err = requestTimeout()
	assert.NoError(t, err)
	assert.Equal(t, 29*time.Second, timeout)

	// gonfig stops waiting for the plugin after 30 s, so longer timeouts would never take effect
	for _, value := range []string{"5", "-1s", "0s", "30s", "1m"} {
		t.Setenv(TimeoutEnv, value)
		_, err = requestTimeout()
		assert.EqualError(t, err, `invalid GONFIG_SECRET_TIMEOUT "`+value+`", use a positive duration below 30s like 10s`)
	}

	f, _ := newFake(map[string]any{"a": "1"})
	_, err = process(f, nil, "a")
	assert.EqualError(t, err, `filter fake: invalid GONFIG_SECRET_TIMEOUT "1m", use a positive duration below 30s like 10s`)
}

// TestProcessInvalidSecrets checks the errors for secrets of unsupported types and secrets that cannot be encoded
func TestProcessInvalidSecrets(t *testing.T) {
	f, _ := newFake(map[string]any{"int": 5, "func": map[string]any{"f": func() {}}})

	_, err := process(f, nil, "int")
	assert.EqualError(t, err, `filter fake: the provider returned a int for the secret "int" instead of a string or map`)

	_, err = process(f, nil, "func")
	assert.EqualError(t, err, `filter fake: the secret "func" cannot be encoded as JSON`)
}
