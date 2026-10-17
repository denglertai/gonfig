# Plugins

Plugins add custom [filters](./FILTERS.md) to gonfig without changing gonfig itself.

- [How plugins work](#how-plugins-work)
- [Requirements](#requirements)
- [Writing a filter plugin](#writing-a-filter-plugin)
- [The dummy plugin](#the-dummy-plugin)
- [Example: hash filters](#example-hash-filters)
- [Troubleshooting](#troubleshooting)

## How plugins work

Plugins are built with Go's [plugin](https://pkg.go.dev/plugin) package as shared objects (`.so` files).

On startup gonfig walks the plugin directory recursively and loads every file ending with `.so`.
The plugin directory defaults to `./plugins`, relative to the current working directory, and may be changed by:
1. the CLI flag `--plugin-path`
1. the environment variable `GONFIG_PLUGIN_PATH`
1. the key `plugin-path` in `.gonfig.yaml`

From every plugin gonfig looks up the exported symbol `Filter`, which has to implement the `PluginFilter` interface from [pkg/plugin/plugin.go](./pkg/plugin/plugin.go):

```go
type PluginFilter interface {
	// Filters returns a set of filters that can be applied to a configuration file
	Filters() map[string]interface{}
}
```

The keys of the map are the filter names used in placeholders, the values are the filters.
Every filter has to provide a `Process` method, and may provide an `AcceptParams` method to receive [params](./FILTERS.md#filter-params):

```go
// Required: transforms the value, which is usually a string but may be of any type (e.g. after to_int)
Process(value any) (any, error)

// Optional: receives the params given in brackets, e.g. sha256(encoding=base64)
AcceptParams(params map[string]string)
```

gonfig checks these methods by their signature, so a plugin does not need to import any gonfig package.

Things to keep in mind:

- **Names are unique:** Filter names cannot be registered twice, which includes the [built-in filters](./FILTERS.md#built-in-filters).
  If a name is already taken, the plugin's filter is skipped with a warning.
- **Copies per usage:** Each usage of a filter in a placeholder works on its own shallow copy of the registered filter, so params of one usage do not affect another one.
  Maps, slices and pointers within a filter are still shared between copies.
- **Positional params:** Params given by position are passed with their position as key, e.g. `sha256(base64)` passes `{"0": "base64"}`.
- **Command plugins:** [pkg/plugin/plugin.go](./pkg/plugin/plugin.go) also defines a `PluginCommand` interface for adding CLI commands. gonfig does not load command plugins yet.

If a plugin fails to load, gonfig logs an error and continues without it.
Its filters are then unknown, and using an unknown filter fails processing, so no output is written:

```console
$ V=hello gonfig value '${V | sha256}'
{"time":"...","level":"ERROR","msg":"Failed to load plugin","path":"plugins/hash.so","error":"plugin.Open(\"plugins/hash\"): plugin was built with a different version of package internal/goarch"}
Error: unknown filter "sha256": check the filter name, or the log for plugins that failed to load
```

The option [`allow-unknown-filters`](./FILTERS.md#order-of-operations) makes gonfig pass the values of unknown filters on unchanged instead.
Avoid it for plugins that hash or encrypt secrets, since a plugin failing to load would then write the plain secrets.

## Requirements

Go plugins come with strict requirements, otherwise they fail to load:

- **Operating system:** Plugins only work on Linux and macOS, not on Windows.
- **CGO:** The gonfig binary has to be built with `CGO_ENABLED=1`. The release binaries and the apk are.
- **Go version:** The plugin has to be built with exactly the same Go version as the gonfig binary.
- **Build flags:** Flags like `-trimpath` have to match. The release binaries are built with `-trimpath`.
- **Dependencies:** Packages used by both gonfig and the plugin have to be of the same version. Using only the standard library avoids this.

`go version -m` shows how a gonfig binary was built:

```console
$ go version -m $(which gonfig) | grep -E 'go1|trimpath|CGO_ENABLED'
/usr/bin/gonfig: go1.27.2
	build	-trimpath=true
	build	CGO_ENABLED=1
```

Plugins have to be rebuilt whenever gonfig is upgraded to a version built with another Go version.

## Writing a filter plugin

This guide creates a plugin providing a filter `shout`, which converts the value to upper case and appends an exclamation mark.

1. Create a new Go module for the plugin:
    ```console
    $ mkdir shout && cd shout
    $ go mod init example.com/gonfig-shout
    ```

1. Create `main.go`. Plugins have to be in package `main` and export the symbol `Filter`:
    ```go
    package main

    import (
    	"fmt"
    	"strings"
    )

    // ShoutFilters provides the filters of this plugin
    type ShoutFilters struct{}

    // Filters returns the filters by the name they are used with in placeholders
    func (ShoutFilters) Filters() map[string]interface{} {
    	return map[string]interface{}{
    		"shout": ShoutFilter{},
    	}
    }

    // Filter is the symbol gonfig looks up when loading the plugin
    var Filter ShoutFilters

    // ShoutFilter converts the value to upper case and appends an exclamation mark
    type ShoutFilter struct{}

    // Process transforms the value
    func (ShoutFilter) Process(value any) (any, error) {
    	return strings.ToUpper(fmt.Sprint(value)) + "!", nil
    }
    ```
    Declare `Filter` as a variable of the struct type, not as a pointer: gonfig receives a pointer to the variable, and a pointer to a pointer does not implement `PluginFilter`.

1. Build the plugin with the Go version and flags of your gonfig binary (see [requirements](#requirements)), e.g. for a release binary:
    ```console
    $ go build -buildmode=plugin -trimpath -o shout.so .
    ```

1. Copy `shout.so` into the plugin directory, e.g. `./plugins` or the directory given by `--plugin-path`.

1. Use the filter. The debug log shows which plugins were loaded:
    ```console
    $ NAME=gonfig gonfig --log-level debug value '${NAME | shout}'
    {"time":"...","level":"DEBUG","msg":"Loaded filter plugin","plugin":"plugins/shout.so","filters":1}
    GONFIG!
    ```

## The dummy plugin

[plugins/dummy](./plugins/dummy/) is a minimal plugin providing the filter `reverse`:

- `DummyFilterRegister` implements `PluginFilter` and returns the filter `reverse`.
- `var Filter = DummyFilterRegister{}` exports the symbol gonfig looks up. `Filters` has a pointer receiver, which works because gonfig receives a pointer to the variable.
- `ReverseFilter` implements `Process` and reverses the value rune by rune, so multi-byte characters stay intact.

Build it within its directory and use it:

```console
$ cd plugins/dummy && go build -buildmode=plugin -o dummy.so . && cd ../..
$ V=gonfig gonfig value '${V | reverse | upper}'
GIFNOG
```

## Example: hash filters

A more realistic plugin providing two filters:

- `sha256` hashes the value with SHA-256.
- `hmac_sha256(key=...)` signs the value with HMAC-SHA256, e.g. for webhook secrets.

Both accept the param `encoding`, which is either `hex` (default) or `base64`.
The filters are registered as pointers, because `AcceptParams` has to store the params in the filter.

```go
package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

// HashFilters provides the filters of this plugin
type HashFilters struct{}

// Filters returns the filters by the name they are used with in placeholders
func (HashFilters) Filters() map[string]interface{} {
	return map[string]interface{}{
		"sha256":      &SHA256Filter{},
		"hmac_sha256": &HMACSHA256Filter{},
	}
}

// Filter is the symbol gonfig looks up when loading the plugin
var Filter HashFilters

// encode returns the digest in the requested encoding, hex by default
func encode(digest []byte, encoding string) (string, error) {
	switch encoding {
	case "", "hex":
		return hex.EncodeToString(digest), nil
	case "base64":
		return base64.StdEncoding.EncodeToString(digest), nil
	default:
		return "", fmt.Errorf("unsupported encoding %q, use hex or base64", encoding)
	}
}

// SHA256Filter hashes the value with SHA-256, e.g. ${VAL | sha256(encoding=base64)}
type SHA256Filter struct {
	params map[string]string
}

// AcceptParams receives the params given in brackets
func (f *SHA256Filter) AcceptParams(params map[string]string) {
	f.params = params
}

// Process hashes the value
func (f *SHA256Filter) Process(value any) (any, error) {
	digest := sha256.Sum256([]byte(fmt.Sprint(value)))
	return encode(digest[:], f.params["encoding"])
}

// HMACSHA256Filter signs the value with HMAC-SHA256, e.g. ${VAL | hmac_sha256(key=secret)}
type HMACSHA256Filter struct {
	params map[string]string
}

// AcceptParams receives the params given in brackets
func (f *HMACSHA256Filter) AcceptParams(params map[string]string) {
	f.params = params
}

// Process signs the value with the key given as param
func (f *HMACSHA256Filter) Process(value any) (any, error) {
	key, found := f.params["key"]
	if !found {
		return "", fmt.Errorf("filter hmac_sha256 requires the param key")
	}

	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(fmt.Sprint(value)))
	return encode(mac.Sum(nil), f.params["encoding"])
}
```

Build it as described in [writing a filter plugin](#writing-a-filter-plugin) and use it:

```console
$ V=hello gonfig value '${V | sha256}'
2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824
$ V=hello gonfig value '${V | sha256(encoding=base64)}'
LPJNul+wow4m6DsqxbninhsWHlwfp0JecwQzYpOLmCQ=
$ V=hello gonfig value '${V | hmac_sha256(key=secret)}'
88aab3ede8d3adf94d26ab90d3bafd4a2083070c3bcce9c014ee04a443847c0b
```

Since the key is a param, it may contain [escaped](./FILTERS.md#filter-params) special characters but cannot be read from another environment variable.

## Troubleshooting

Using a filter of a plugin that failed to load results in the error `unknown filter "..."`.
The reason is logged with the message `Failed to load plugin` when gonfig starts. Common errors:

| Error | Cause |
| --- | --- |
| `plugin was built with a different version of package ...` | The Go version, build flags (e.g. `-trimpath`) or dependency versions differ from the gonfig binary |
| `plugin: not implemented` | The gonfig binary was built without CGO, or runs on Windows |
| `plugin: symbol Filter not found in plugin ...` | The plugin does not export a variable named `Filter` |
| `symbol Filter is of type ..., which does not implement ...` | `Filter` has the wrong type, e.g. it is declared as a pointer or misses the `Filters` method |

A plugin built with a much older Go version may crash gonfig on startup instead of logging an error, e.g. with `fatal error: runtime: no plugin module data`.
Rebuild or remove outdated `.so` files in the plugin directory, including the ones in `./plugins` of the current working directory.
