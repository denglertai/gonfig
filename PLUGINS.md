# Plugins

Plugins add custom [filters](./FILTERS.md) to gonfig without changing gonfig itself, e.g. filters for confidential encryption that cannot be part of gonfig.

- [How plugins work](#how-plugins-work)
- [Writing a filter plugin](#writing-a-filter-plugin)
- [Values and params](#values-and-params)
- [The dummy plugin](#the-dummy-plugin)
- [Example: hash filters](#example-hash-filters)
- [Plugins for secret managers](#plugins-for-secret-managers)
- [Packaging with melange and apko](#packaging-with-melange-and-apko)
- [The protocol](#the-protocol)
- [Migrating from Go plugins](#migrating-from-go-plugins)
- [Troubleshooting](#troubleshooting)

## How plugins work

A plugin is a separate executable, which gonfig starts once and talks to over stdin and stdout.
Plugins and gonfig don't have to be built with the same Go version, and plugins may be written in any language.

On startup gonfig walks the plugin directory recursively and starts every executable whose name starts with `gonfig-plugin-`, e.g. `gonfig-plugin-crypto` (`gonfig-plugin-crypto.exe` on Windows).
Other files are ignored, so build scripts next to the sources of a plugin are never started. The exception are deprecated Go plugins (`.so` files), which are still loaded, see [migrating from Go plugins](#migrating-from-go-plugins).
By default, gonfig searches two plugin directories:
1. `/usr/libexec/gonfig/plugins`, where plugin packages install their plugins, see [packaging](#packaging-with-melange-and-apko). Windows has no such directory.
1. `./plugins`, relative to the current working directory

Directories that don't exist are skipped. The system directory is searched first, so a plugin in `./plugins` cannot replace a filter of an installed plugin, since filter names are unique (see below).
Another plugin directory may be set, which is then the only one searched, by:
1. the CLI flag `--plugin-path`
1. the environment variable `GONFIG_PLUGIN_PATH`
1. the key `plugin-path` in `.gonfig.yaml`

Every plugin tells gonfig which filters it provides. When a placeholder uses one of them, gonfig sends the value and the params of the usage to the plugin and uses its answer as the result.
When gonfig is done, it closes the plugin's stdin and the plugin exits.

- **Environment:** Plugins inherit gonfig's environment, so they can read keys from environment variables.
- **Logs:** Plugins may log to stderr, which gonfig passes on to its own stderr. Stdout is reserved for the protocol. Keep values out of logs and error messages, since they may be secrets.
- **Names are unique:** Filter names cannot be registered twice, which includes the [built-in filters](./FILTERS.md#built-in-filters).
  If a name is already taken, the plugin's filter is skipped with a warning.
- **Timeouts:** A plugin has 10 seconds to start and 30 seconds to answer a request.

If a plugin fails to start, gonfig logs an error and continues without it.
Its filters are then unknown, and using an unknown filter fails processing, so no output is written:

```console
$ V=hello gonfig value '${V | sha256}'
{"time":"...","level":"ERROR","msg":"Failed to start plugin","path":"plugins/gonfig-plugin-hash","error":"no handshake: the plugin exited"}
Error: unknown filter "sha256": check the filter name, or the log for plugins that failed to load
```

A plugin that crashes, answers with an error or doesn't answer in time fails the value it processes, and every later request to it fails as well.

The option [`allow-unknown-filters`](./FILTERS.md#order-of-operations) makes gonfig pass the values of unknown filters on unchanged instead.
Avoid it for plugins that hash or encrypt secrets, since a plugin failing to start would then write the plain secrets.

## Writing a filter plugin

Plugins written in Go use `plugin.Serve` from [pkg/plugin](./pkg/plugin/), which implements the [protocol](#the-protocol).
This guide creates a plugin providing a filter `shout`, which converts the value to upper case and appends an exclamation mark.

1. Create a new Go module for the plugin and add gonfig as a dependency:
    ```console
    $ mkdir shout && cd shout
    $ go mod init example.com/gonfig-shout
    $ go get github.com/denglertai/gonfig@latest
    ```

1. Create `main.go`, which serves the filters by the names they are used with in placeholders:
    ```go
    package main

    import (
    	"fmt"
    	"log"
    	"strings"

    	"github.com/denglertai/gonfig/pkg/plugin"
    )

    // main serves the filters until gonfig is done
    func main() {
    	err := plugin.Serve(map[string]any{
    		"shout": ShoutFilter{},
    	})
    	if err != nil {
    		log.Fatal(err)
    	}
    }

    // ShoutFilter converts the value to upper case and appends an exclamation mark
    type ShoutFilter struct{}

    // Process transforms the value
    func (ShoutFilter) Process(value any) (any, error) {
    	return strings.ToUpper(fmt.Sprint(value)) + "!", nil
    }
    ```

1. Build the plugin with a name starting with `gonfig-plugin-`. Any Go version and build flags work:
    ```console
    $ go build -o gonfig-plugin-shout .
    ```

1. Copy `gonfig-plugin-shout` into the plugin directory, e.g. `./plugins` or the directory given by `--plugin-path`.

1. Use the filter. The debug log shows which plugins were started:
    ```console
    $ NAME=gonfig gonfig --log-level debug value '${NAME | shout}'
    {"time":"...","level":"DEBUG","msg":"Started filter plugin","plugin":"plugins/gonfig-plugin-shout","filters":1}
    GONFIG!
    ```

## Values and params

Every filter has to provide a `Process` method, and may provide an `AcceptParams` method to receive [params](./FILTERS.md#filter-params):

```go
// Required: transforms the value, which is a string, int, float64, bool or []string (e.g. after split).
// Integers, floats and booleans returned for a whole value are written as such to YAML and JSON files.
Process(value any) (any, error)

// Optional: receives the params given in brackets, e.g. sha256(encoding=base64)
AcceptParams(params map[string]string)
```

- **Copies per usage:** Every value is processed by its own shallow copy of the filter, which receives the params of its usage, so params of one usage do not affect another one.
  Filters storing params have to be registered as pointers. Maps, slices and pointers within a filter are still shared between copies.
- **Positional params:** Params given by position are passed with their position as key, e.g. `sha256(base64)` passes `{"0": "base64"}`.
- **Validating params:** gonfig passes any params to plugin filters, unlike built-in filters, which reject params they do not accept. Return an error for unknown params, so typos don't go unnoticed.
- **Results:** Filters may return the same types they receive. Other types, e.g. maps, fail the value.
- **Errors** fail processing with the message prefixed by the filter name, e.g. `filter shout: ...`. A filter that panics fails the value with `the filter panicked`, the plugin keeps running.

## The dummy plugin

[plugins/dummy](./plugins/dummy/) is a minimal plugin providing the filter `reverse`:

- `ReverseFilter` implements `Process` and reverses the value rune by rune, so multi-byte characters stay intact.
- `DummyFilterRegister` returns the filters, which `main` passes to `plugin.Serve`. This is how [Go plugins](#migrating-from-go-plugins) provided their filters, and shows that they can be served unchanged.

Build it within its directory and use it from the root of the repository, where it is found in the default plugin directory `./plugins`:

```console
$ cd plugins/dummy && go build -o gonfig-plugin-dummy . && cd ../..
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
	"errors"
	"fmt"
	"log"

	"github.com/denglertai/gonfig/pkg/plugin"
)

// main serves the filters until gonfig is done
func main() {
	err := plugin.Serve(map[string]any{
		"sha256":      &SHA256Filter{},
		"hmac_sha256": &HMACSHA256Filter{},
	})
	if err != nil {
		log.Fatal(err)
	}
}

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
		return "", errors.New("the param key is required")
	}

	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(fmt.Sprint(value)))
	return encode(mac.Sum(nil), f.params["encoding"])
}
```

Build it as `gonfig-plugin-hash` as described in [writing a filter plugin](#writing-a-filter-plugin) and use it:

```console
$ V=hello gonfig value '${V | sha256}'
2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824
$ V=hello gonfig value '${V | sha256(encoding=base64)}'
LPJNul+wow4m6DsqxbninhsWHlwfp0JecwQzYpOLmCQ=
$ V=hello gonfig value '${V | hmac_sha256(key=secret)}'
88aab3ede8d3adf94d26ab90d3bafd4a2083070c3bcce9c014ee04a443847c0b
$ V=hello gonfig value '${V | hmac_sha256}'
Error: filter hmac_sha256: the param key is required
```

Since the key is a param, it may contain [escaped](./FILTERS.md#filter-params) special characters, but it is part of the config file.
To keep the key out of the file, read it from an environment variable in the plugin instead, since plugins inherit gonfig's environment.

## Plugins for secret managers

This repository provides plugins that read secrets from secret managers:

| Plugin | Filter | Reference | Authentication |
| --- | --- | --- | --- |
| [OpenBao and HashiCorp Vault](./plugins/openbao/) | `openbao` | `secret/app/db#password` | token, AppRole, Kubernetes |
| [Azure Key Vault](./plugins/azure-keyvault/) | `azure_keyvault` | `myvault/db-password` | `DefaultAzureCredential`: environment, workload identity, managed identity, Azure CLI |

The variable of a placeholder holds the reference to the secret, and the filter returns the secret, e.g. `${DB_PASSWORD | openbao}` with `DB_PASSWORD=secret/app/db#password`.
A [placeholder template](./FILTERS.md#templates) can keep the reference out of the config files. See the README of each plugin for its references, configuration and examples.

### Shared behavior

The plugins build on [pkg/plugin/secret](./pkg/plugin/secret/), so they behave the same:

- **Fields:** `#` selects a field of the secret, also of secrets containing a JSON object. Without a field, a secret with fields is returned as a JSON object, which the [`fragment`](./FILTERS.md#fragments) filter inserts as a structure. Fields that aren't strings are returned as JSON, e.g. `5432`.
- **Caching:** every secret is read once per run, also if several of its fields are used.
- **Lazy authentication:** a plugin authenticates on the first placeholder using it. gonfig starts every plugin of the plugin directory on every run, so gonfig works without credentials for secret managers that no placeholder uses. If authentication fails, every placeholder using the plugin fails with the same error, without trying again.
- **Timeouts:** a request may take 20 s including authentication, or the duration in `GONFIG_SECRET_TIMEOUT`, e.g. `10s`, which has to be shorter than the 30 s gonfig waits for an answer of a plugin.
- **Errors** name the reference and what the secret manager reported, but never the secret, e.g. `filter openbao: failed to read the secret "secret/app/db": status 403: permission denied`. An empty reference, e.g. of a variable that is not set, fails instead of requesting an empty path.
- **Configuration:** plugins inherit gonfig's environment, so they are configured with the usual variables of the secret manager, e.g. `BAO_ADDR` or `AZURE_CLIENT_ID`, and `GONFIG_<PLUGIN>_*` variables for settings of gonfig, e.g. `GONFIG_OPENBAO_AUTH`.

`gonfig config check` runs the filters as well, so it authenticates, reads the secrets and reports errors like a missing permission for every entry, while the [diff](./README.md#showing-the-changes) shows the secrets as `***`.

### Security

- **Don't use `allow-unknown-filters` with secret plugins.** If a plugin fails to start, its filter is unknown. gonfig then fails by default, but with `allow-unknown-filters` it writes the reference instead of the secret, with exit code `0`:

  ```console
  $ DB_PASSWORD=secret/app/db#password gonfig --allow-unknown-filters value 'password=${DB_PASSWORD | openbao}'
  {"time":"...","level":"WARN","msg":"Unknown filter, passing the value on unchanged","filter":"openbao"}
  password=secret/app/db#password
  ```
- **Prefer files or workload identities to credentials in the environment**, which other processes of the same user may read: e.g. `GONFIG_OPENBAO_TOKEN_FILE` or Kubernetes authentication for OpenBao, and workload or managed identity for Azure.
- **Secrets travel only through the pipe** between gonfig and the plugin. The plugins never put them into errors, and gonfig leaves processed values out of its logs unless [`log-values`](./README.md#configuration) is enabled for debugging.
- **gonfig starts every plugin it finds**, so installed plugins run even if no placeholder uses them. They don't contact a secret manager before a placeholder uses them.

### Writing a plugin for another secret manager

A plugin for another secret manager implements the `Provider` of [pkg/plugin/secret](./pkg/plugin/secret/): `Connect` creates the client and authenticates on the first request, `Read` returns a secret as a string or a map of fields, and `Params` declares the params of the filter.
`secret.NewFilter` adds the shared behavior, and `plugin.Serve` serves the filter:

```go
func main() {
	filter := secret.NewFilter(&provider{})
	if err := plugin.Serve(map[string]any{"mysecrets": filter}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
```

Errors of a `Provider` must not contain secrets, tokens or response bodies that may contain them. gonfig prefixes them with the name of the filter.

## Packaging with melange and apko

Plugins don't depend on the Go version gonfig was built with, so a plugin package can be built independently of the gonfig package and combined with it in an image.

A [melange](https://github.com/chainguard-dev/melange) package for the plugin builds it with the `go/build` pipeline and installs it into `/usr/libexec/gonfig/plugins`, which gonfig searches by default.
`install-dir` sets the directory below `/usr` the binary is installed to, and the runtime dependency installs gonfig together with the plugin:

```yaml
package:
  name: gonfig-plugin-crypto
  version: 1.0.0
  epoch: 0
  description: Filters for gonfig
  dependencies:
    runtime:
      - gonfig

environment:
  contents:
    repositories:
      - https://packages.wolfi.dev/os
    keyring:
      - https://packages.wolfi.dev/os/wolfi-signing.rsa.pub
    packages:
      - go

pipeline:
  - uses: go/build
    with:
      packages: .
      output: gonfig-plugin-crypto
      install-dir: libexec/gonfig/plugins
```

The runtime dependency doesn't pin a gonfig version, since plugins and gonfig only share the versioned [protocol](#the-protocol).

The [apko](https://github.com/chainguard-dev/apko) configuration of the image adds the plugin package from the repository it is published to, which installs gonfig as well.
These are the relevant parts of the configuration, e.g. for packages built with melange into its output directory `./packages`:

```yaml
contents:
  repositories:
    - https://packages.wolfi.dev/os
    # The packages built with melange
    - ./packages
  keyring:
    - https://packages.wolfi.dev/os/wolfi-signing.rsa.pub
    # The public key of the key melange signed the packages with
    - ./melange.rsa.pub
  packages:
    - gonfig-plugin-crypto
```

gonfig finds the plugin in `/usr/libexec/gonfig/plugins` without further configuration. Versions of gonfig before the default search paths need `GONFIG_PLUGIN_PATH: /usr/libexec/gonfig/plugins` in the `environment` of the image.

## The protocol

Plugins written in other languages implement the protocol themselves. It consists of JSON objects, one per line, and is defined in [pkg/plugin/protocol.go](./pkg/plugin/protocol.go).

1. The plugin writes a handshake with the protocol version and the names of its filters:
    ```json
    {"protocol":1,"filters":["sha256","hmac_sha256"]}
    ```
1. For every value, gonfig writes a request to the plugin's stdin. `params` is left out for usages without params:
    ```json
    {"id":0,"filter":"sha256","params":{"encoding":"base64"},"value":"hello"}
    ```
1. The plugin answers every request in order, with the result or an error, and the `id` of the request:
    ```json
    {"id":0,"value":"LPJNul+wow4m6DsqxbninhsWHlwfp0JecwQzYpOLmCQ="}
    {"id":1,"error":"the param key is required"}
    ```
1. When gonfig is done, it closes the plugin's stdin and the plugin exits. A plugin that does not exit within 5 seconds is killed.

Values are JSON strings, numbers, booleans or arrays of strings. Numbers with a decimal point or an exponent, e.g. `5.0`, are floats, others integers.
Error messages must not contain the value, since it may be a secret.

For example, `gonfig-plugin-rot13` in Python:

```python
#!/usr/bin/env python3
import codecs
import json
import sys

print(json.dumps({"protocol": 1, "filters": ["rot13"]}), flush=True)

for line in sys.stdin:
    request = json.loads(line)
    value = request["value"]
    if isinstance(value, str):
        response = {"id": request["id"], "value": codecs.encode(value, "rot13")}
    else:
        response = {"id": request["id"], "error": "only strings are supported"}
    print(json.dumps(response), flush=True)
```

```console
$ V=gonfig gonfig value '${V | rot13}'
tbasvt
```

## Migrating from Go plugins

Before executable plugins, plugins were built with Go's [plugin](https://pkg.go.dev/plugin) package as shared objects (`.so` files).
gonfig still loads them, but they are deprecated and will be removed in a future version, since they have to be built with exactly the same Go version, build flags and dependency versions as the gonfig binary, and don't work on Windows.

The filters of a Go plugin can be served unchanged:

1. Add gonfig as a dependency of the plugin: `go get github.com/denglertai/gonfig@latest`.
1. Add a `main` function serving the filters the plugin's `Filter` symbol returns:
    ```go
    func main() {
    	if err := plugin.Serve(Filter.Filters()); err != nil {
    		log.Fatal(err)
    	}
    }
    ```
1. Build it as an executable instead of a shared object, e.g. `go build -o gonfig-plugin-hash .` instead of `go build -buildmode=plugin -o hash.so .`.
1. Replace the `.so` file in the plugin directory by the executable.

## Troubleshooting

Using a filter of a plugin that failed to start results in the error `unknown filter "..."`.
The reason is logged with the message `Failed to start plugin` when gonfig starts. Common errors:

| Error | Cause |
| --- | --- |
| `no handshake: the plugin exited` | The plugin exited before writing its handshake, e.g. because `plugin.Serve` returned an error, which the plugin logs to stderr |
| `no handshake: the plugin did not answer within 10s` | The plugin did not write its handshake, e.g. because it is not a gonfig plugin |
| `no handshake: invalid output of the plugin: ...` | The plugin wrote something else than JSON to stdout |
| `unsupported protocol version ...` | The plugin uses a protocol version gonfig does not support |
| `fork/exec ...: exec format error` | The plugin was built for another operating system or architecture |

The debug log (`--log-level debug`) lists the started plugins with the number of their filters, and files named `gonfig-plugin-*` that were skipped because they are not executable.

Go plugins (`.so` files) that fail to load are logged with the message `Failed to load plugin`.
The usual cause is `plugin was built with a different version of package ...`, which [migrating](#migrating-from-go-plugins) to an executable plugin solves.
A Go plugin built with a much older Go version may crash gonfig on startup instead, e.g. with `fatal error: runtime: no plugin module data`.
