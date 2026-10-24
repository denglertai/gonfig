# OpenBao / Vault plugin

`gonfig-plugin-openbao` provides the filter `openbao`, which reads secrets from the KV secrets engine of [OpenBao](https://openbao.org) or HashiCorp Vault.
See [PLUGINS.md](../../PLUGINS.md) for how plugins work.

- [Usage](#usage)
- [References and params](#references-and-params)
- [Configuration](#configuration)
- [Authentication](#authentication)
- [Errors](#errors)
- [Build](#build)

## Usage

The variable of a placeholder holds the reference to the secret, and the filter returns the secret:

```console
$ export BAO_ADDR=https://bao.example.com BAO_TOKEN=...
$ bao kv put secret/app/db user=app password=s3cret
$ DB_PASSWORD=secret/app/db#password gonfig value '${DB_PASSWORD | openbao}'
s3cret
```

Without a field, the whole secret is returned as a JSON object, which the [`fragment`](../../FILTERS.md#fragments) filter inserts as a structure.
Every secret is read once per run, also if several of its fields are used.

`app.yaml`:
```yaml
database:
  host: db.internal
  password: ${DB_PASSWORD | openbao}
credentials: ${DB | openbao | fragment}
```

```console
$ DB_PASSWORD=secret/app/db#password DB=secret/app/db gonfig config process -f app.yaml
database:
  host: db.internal
  password: s3cret
credentials:
  password: s3cret
  user: app
```

A [placeholder template](../../FILTERS.md#templates) keeps the reference out of the config files, with a default that a variable may override:

```yaml
# .gonfig.yaml
templates:
  placeholders:
    db_password:
      env: DB_PASSWORD_REF
      default: "secret/app/db#password"
      filters: "openbao"
```

```console
$ gonfig value 'password=${@db_password}'
password=s3cret
```

## References and params

A reference is the mount of the secrets engine followed by the path of the secret, and optionally `#` and a field: `<mount>/<path>[#<field>]`.

| Param | Default | Description | Example |
| --- | --- | --- | --- |
| `mount` | the first segment of the reference | The mount of the secrets engine, for mounts with several segments. The reference has to start with it | `${DB \| openbao(mount=kv/team)}` with `DB=kv/team/app/db#password` |
| `kv` | `2` | The version of the KV secrets engine, `1` or `2` | `${DB \| openbao(kv=1)}` |
| `version` | the latest | The version of a KV version 2 secret | `${DB \| openbao(version=3)}` |

Fields that aren't strings are returned as JSON, e.g. `5432` for a number, and secrets without a field as a JSON object with sorted keys.

## Configuration

The plugin is configured with the variables of the OpenBao client, which falls back to the `VAULT_*` variables if a `BAO_*` variable isn't set, so the plugin works with Vault without further configuration:

| Variable | Description |
| --- | --- |
| `BAO_ADDR` / `VAULT_ADDR` | The address of the server, required |
| `BAO_TOKEN` / `VAULT_TOKEN` | The token for the `token` authentication |
| `BAO_NAMESPACE` / `VAULT_NAMESPACE` | The namespace |
| `BAO_CACERT`, `BAO_CAPATH`, `BAO_CLIENT_CERT`, `BAO_CLIENT_KEY`, `BAO_SKIP_VERIFY` | TLS settings, and their `VAULT_*` variants |
| `GONFIG_SECRET_TIMEOUT` | The time a request may take including logging in, e.g. `10s`. Defaults to `20s` and has to be shorter than `30s` |

The plugin connects on the first placeholder using it, so gonfig runs without these variables as long as no placeholder uses the filter.

## Authentication

`GONFIG_OPENBAO_AUTH` selects the authentication method, and `GONFIG_OPENBAO_AUTH_MOUNT` the mount of the auth method, which defaults to the name of the method.

| Method | Variables |
| --- | --- |
| `token` (default) | `BAO_TOKEN` or `VAULT_TOKEN`, or the file in `GONFIG_OPENBAO_TOKEN_FILE` |
| `approle` | `GONFIG_OPENBAO_ROLE_ID` and `GONFIG_OPENBAO_SECRET_ID`, or the files in `GONFIG_OPENBAO_ROLE_ID_FILE` and `GONFIG_OPENBAO_SECRET_ID_FILE` |
| `kubernetes` | `GONFIG_OPENBAO_ROLE`, and the service account token of the pod, or the file in `GONFIG_OPENBAO_JWT_FILE` |

Prefer files or the Kubernetes service account over tokens in the environment, which other processes of the same user may read.
Files may end with a line break, which is removed.

```console
$ export GONFIG_OPENBAO_AUTH=kubernetes GONFIG_OPENBAO_ROLE=app
$ DB_PASSWORD=secret/app/db#password gonfig config process -f app.yaml
```

## Errors

Errors name the reference and the status and messages of the server, but never the secret:

```console
$ DB_PASSWORD=secret/app/missing#password gonfig value '${DB_PASSWORD | openbao}'
Error: filter openbao: failed to read the secret "secret/app/missing": not found
$ DB_PASSWORD=secret/app/db#pass gonfig value '${DB_PASSWORD | openbao}'
Error: filter openbao: the secret "secret/app/db" has no field "pass"
$ BAO_TOKEN=invalid DB_PASSWORD=secret/app/db#password gonfig value '${DB_PASSWORD | openbao}'
Error: filter openbao: failed to read the secret "secret/app/db": status 403: permission denied
$ gonfig value '${DB_PASSWORD | openbao}'
Error: filter openbao: the reference is empty, e.g. as its variable is not set
```

A failed login fails every placeholder of the run with the same error, without logging in again:

```console
$ env -u BAO_TOKEN DB_PASSWORD=secret/app/db#password gonfig value '${DB_PASSWORD | openbao}'
Error: filter openbao: failed to connect: no token, set BAO_TOKEN, VAULT_TOKEN or GONFIG_OPENBAO_TOKEN_FILE, or another GONFIG_OPENBAO_AUTH
```

`gonfig config check` runs the filter as well, so it reads the secrets and reports these errors, while the [diff](../../README.md#showing-the-changes) masks the secrets.

## Build

Within this directory:

```console
$ go build -o gonfig-plugin-openbao .
```

Copy the binary into a plugin directory, e.g. `/usr/libexec/gonfig/plugins`, which gonfig searches by default.

The tests include integration tests against a dev server, which run if `TEST_OPENBAO_ADDR` and `TEST_OPENBAO_TOKEN` are set:

```console
$ bao server -dev -dev-root-token-id=root &
$ TEST_OPENBAO_ADDR=http://127.0.0.1:8200 TEST_OPENBAO_TOKEN=root go test ./...
```
