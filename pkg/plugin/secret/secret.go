// Package secret provides what plugins reading secrets from secret managers share: a filter that resolves references
// to secrets, e.g. "secret/app/db#password", with a Provider of the secret manager. The filter connects to the secret
// manager on its first request, reads every secret once per run, selects fields, limits the time of requests and keeps
// secrets out of its errors.
//
// A plugin serves the filter with plugin.Serve:
//
//	func main() {
//		filter := secret.NewFilter("openbao", &provider{})
//		if err := plugin.Serve(map[string]any{"openbao": filter}); err != nil {
//			fmt.Fprintln(os.Stderr, err)
//			os.Exit(1)
//		}
//	}
//
// The variable of a placeholder holds the reference, e.g. ${DB_PASSWORD | openbao} with
// DB_PASSWORD=secret/app/db#password. See PLUGINS.md for how plugins work.
package secret

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"sync"
	"time"
)

// Provider reads secrets of a secret manager. Its errors must not contain secrets, tokens or response bodies that may
// contain them, since gonfig shows them to the user.
type Provider interface {
	// Connect creates the client of the secret manager and authenticates it. It is called once, on the first request,
	// so plugins start without credentials and only fail the values using them. If it fails, every request fails with
	// its error, so a missing or wrong configuration is not retried for every value.
	Connect(ctx context.Context) error
	// Read reads the secret the reference without a field refers to, with the params of the usage. The result is
	// either a string or a map[string]any, e.g. the data of a key/value secret.
	Read(ctx context.Context, ref string, params map[string]string) (any, error)
	// Params returns the names of the params Read accepts, e.g. "version". Other params fail the request.
	Params() []string
}

// TimeoutEnv is the environment variable setting the time a request may take, including connecting, as a Go
// duration, e.g. "10s". It has to be shorter than the 30 s gonfig waits for an answer of a plugin.
const TimeoutEnv = "GONFIG_SECRET_TIMEOUT"

// DefaultTimeout is the time a request may take if TimeoutEnv is not set
const DefaultTimeout = 20 * time.Second

// pluginTimeout is the time gonfig waits for an answer of a plugin, which a timeout has to stay below, so its error
// reaches the user instead of gonfig stopping the plugin
const pluginTimeout = 30 * time.Second

// Filter resolves references to secrets with a Provider. It is served with plugin.Serve, which works on a copy of the
// filter per request; the copies share the connection and the cache.
type Filter struct {
	*state
	// params are the params of the usage, e.g. {"version": "3"} for openbao(version=3)
	params map[string]string
}

// state is shared by all copies of a Filter
type state struct {
	name     string
	provider Provider

	mu         sync.Mutex
	connected  bool
	connectErr error
	// cache holds the secrets read, by reference without field and params
	cache map[string]any
}

// NewFilter returns a filter named name, which is used in errors, reading secrets with the provider
func NewFilter(name string, provider Provider) *Filter {
	return &Filter{state: &state{name: name, provider: provider, cache: map[string]any{}}}
}

// AcceptParams keeps the params of the usage
func (f *Filter) AcceptParams(params map[string]string) {
	f.params = params
}

// Process returns the secret the value refers to. A reference may end with "#field" to select a field of the secret.
// Without a field, a secret with fields is returned as a JSON object, e.g. for use with gonfig's fragment filter.
// Fields that aren't strings are returned as JSON as well, e.g. "5432" for a number.
func (f *Filter) Process(value any) (any, error) {
	reference, ok := value.(string)
	if !ok {
		return nil, fmt.Errorf("filter %s: the reference has to be a string, not %T", f.name, value)
	}
	if reference == "" {
		return nil, fmt.Errorf("filter %s: the reference is empty, e.g. as its variable is not set", f.name)
	}
	if err := f.validateParams(); err != nil {
		return nil, err
	}

	ref, field, hasField := strings.Cut(reference, "#")
	if ref == "" {
		return nil, fmt.Errorf("filter %s: the reference %q names no secret before the #", f.name, reference)
	}
	secret, err := f.read(ref)
	if err != nil {
		return nil, err
	}

	result := secret
	if hasField {
		if result, err = f.selectField(ref, secret, field); err != nil {
			return nil, err
		}
	}
	formatted, err := format(result)
	if err != nil {
		return nil, fmt.Errorf("filter %s: the secret %q %w", f.name, ref, err)
	}
	return formatted, nil
}

// validateParams fails for params the provider does not accept, e.g. typos in param names
func (f *Filter) validateParams() error {
	accepted := f.provider.Params()
	for _, name := range slices.Sorted(maps.Keys(f.params)) {
		if !slices.Contains(accepted, name) {
			if len(accepted) == 0 {
				return fmt.Errorf("filter %s accepts no params, but got %q", f.name, name)
			}
			return fmt.Errorf("filter %s has no param %q, it accepts %s", f.name, name, strings.Join(accepted, ", "))
		}
	}
	return nil
}

// read returns the secret from the cache, or connects if needed and reads it within the timeout
func (f *Filter) read(ref string) (any, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	key := cacheKey(ref, f.params)
	if secret, found := f.cache[key]; found {
		return secret, nil
	}

	timeout, err := requestTimeout()
	if err != nil {
		return nil, fmt.Errorf("filter %s: %w", f.name, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if err := f.connect(ctx); err != nil {
		return nil, err
	}

	secret, err := f.provider.Read(ctx, ref, f.params)
	if errors.Is(err, context.DeadlineExceeded) {
		return nil, fmt.Errorf("filter %s: reading the secret %q took longer than %s", f.name, ref, timeout)
	}
	if err != nil {
		return nil, fmt.Errorf("filter %s: failed to read the secret %q: %w", f.name, ref, err)
	}
	switch secret.(type) {
	case string, map[string]any:
	default:
		return nil, fmt.Errorf("filter %s: the provider returned a %T for the secret %q instead of a string or map", f.name, secret, ref)
	}

	f.cache[key] = secret
	return secret, nil
}

// connect connects the provider on the first call and returns the error of that call on every later one
func (f *Filter) connect(ctx context.Context) error {
	if f.connected {
		return f.connectErr
	}
	f.connected = true

	if err := f.provider.Connect(ctx); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			err = errors.New("connecting took too long")
		}
		f.connectErr = fmt.Errorf("filter %s: failed to connect: %w", f.name, err)
	}
	return f.connectErr
}

// selectField returns the field of the secret. Secrets that are strings have to contain a JSON object.
func (f *Filter) selectField(ref string, secret any, field string) (any, error) {
	fields, ok := secret.(map[string]any)
	if !ok {
		// The parser's error is not included, since it may quote the secret
		if err := json.Unmarshal([]byte(secret.(string)), &fields); err != nil || fields == nil {
			return nil, fmt.Errorf("filter %s: the secret %q has no fields, since it doesn't contain a JSON object", f.name, ref)
		}
	}

	value, found := fields[field]
	if !found {
		return nil, fmt.Errorf("filter %s: the secret %q has no field %q", f.name, ref, field)
	}
	return value, nil
}

// format returns strings as they are and other values as JSON
func format(value any) (string, error) {
	if s, ok := value.(string); ok {
		return s, nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		// The error is not included, since it may quote parts of the secret
		return "", errors.New("cannot be encoded as JSON")
	}
	return string(encoded), nil
}

// cacheKey returns the key of a secret in the cache: the reference without field and the params, which may select
// another secret, e.g. a version. Every part is prefixed by its length, so different references and params never
// result in the same key, whatever characters they contain.
func cacheKey(ref string, params map[string]string) string {
	var key strings.Builder
	part := func(s string) { fmt.Fprintf(&key, "%d:%s", len(s), s) }
	part(ref)
	for _, name := range slices.Sorted(maps.Keys(params)) {
		part(name)
		part(params[name])
	}
	return key.String()
}

// requestTimeout returns the time a request may take, see TimeoutEnv
func requestTimeout() (time.Duration, error) {
	value := os.Getenv(TimeoutEnv)
	if value == "" {
		return DefaultTimeout, nil
	}
	timeout, err := time.ParseDuration(value)
	if err != nil || timeout <= 0 || timeout >= pluginTimeout {
		return 0, fmt.Errorf("invalid %s %q, use a positive duration below %s like 10s", TimeoutEnv, value, pluginTimeout)
	}
	return timeout, nil
}
