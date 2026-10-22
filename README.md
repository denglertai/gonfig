# gonfig

[![codecov](https://codecov.io/gh/denglertai/gonfig/graph/badge.svg?token=QYIQ2BO0IV)](https://codecov.io/gh/denglertai/gonfig)

gonfig fills config files with values from environment variables, e.g. when starting an application inside a container.

It parses each file in its own format instead of replacing text, so values are escaped as the format requires.
An example would be `&` (ampersand) within `xml` files, which has to be escaped as `&amp;`.
Values may be transformed by [filters](./FILTERS.md), e.g. to trim, encode or hash them, and custom filters may be added by [plugins](./PLUGINS.md).

```console
$ DB_PASSWORD='s3cr&t' gonfig value '${DB_PASSWORD | base64}'
czNjciZ0
```

- [Features](#features)
- [Installation](#installation)
- [Quick start](#quick-start)
- [Use cases](#use-cases)
- [Usage](#usage)
- [Filters](#filters)
- [Configuration](#configuration)
- [Plugins](#plugins)
- [Benchmarks](#benchmarks)
- [Releasing](#releasing)

## Features

| | YAML | JSON | XML | Properties | Plain text | TOML |
| --- | --- | --- | --- | --- | --- | --- |
| File extensions | `.yaml`, `.yml` | `.json` | `.xml` | `.properties` | any, with `-t plain` | planned ([#161](https://github.com/denglertai/gonfig/issues/161)) |
| Placeholders in values | ✅ | ✅ | ✅ element text, CDATA and attributes | ✅ | ✅ every line | |
| Placeholders in keys | ✅ | ✅ | ❌ | ❌ | ✅ | |
| [Filters](./FILTERS.md) and [plugins](./PLUGINS.md) | ✅ | ✅ | ✅ | ✅ | ✅ | |
| Comments preserved | ✅ | – | ✅ | ✅ written as `#` | ✅ | |
| Key order preserved | ✅ | ✅ | ✅ | ✅ | ✅ | |
| Formatting preserved | ✅ including multiple documents | ❌ indented with 2 spaces | ✅ | ❌ written as `key = value` | ✅ | |
| [Fragments](./FILTERS.md#fragments) | ✅ values and keys | ✅ values and keys | ✅ element content | ❌ | ❌ | |
| [Typed values](./FILTERS.md#types) | ✅ with `to_int`, `to_float`, `to_bool` | ✅ with `to_int`, `to_float`, `to_bool` | ❌ text | ❌ text | ❌ text | |

## Installation

Installing `gonfig` can currently be done by downloading the latest release from [https://github.com/denglertai/gonfig/releases/latest](https://github.com/denglertai/gonfig/releases/latest) 

Installing the apk from [https://denglertai.github.io/apk/](https://denglertai.github.io/apk/).
```bash
# Add the repository to the list of apk repositories
$ echo "https://denglertai.github.io/apk/" >> /etc/apk/repositories
# Download the key used for signing the packages
$ curl https://denglertai.github.io/apk/melange.rsa.pub > /etc/apk/keys/denglertai.rsa.pub
# Update the package list
$ apk update
# Install gonfig
$ apk add gonfig
```

Using [apko](https://github.com/chainguard-dev/apko/) for building images:
```yaml
contents:
  keyring:
    - https://denglertai.github.io/apk/melange.rsa.pub
  repositories:
    - https://denglertai.github.io/apk/
  packages:
    - gonfig
accounts:
  groups:
    - groupname: nonroot
      gid: 65532
  users:
    - username: nonroot
      uid: 65532
  run-as: 65532
entrypoint:
  command: /usr/bin/gonfig
```

Using the container image `ghcr.io/denglertai/gonfig` for `linux/amd64` and `linux/arm64`, e.g. to copy the binary into an image in a multi-stage build:
```dockerfile
FROM ghcr.io/denglertai/gonfig:0.2.0 AS gonfig

FROM eclipse-temurin:21-jre
COPY --from=gonfig /usr/bin/gonfig /usr/bin/gonfig
```

The image is tagged with the version both with and without a leading `v`, e.g. `v0.2.0` and `0.2.0`, and `latest` for the newest release that is not a prerelease.
The binary is linked against glibc, so it runs in images based on e.g. Debian, Ubuntu, Wolfi or distroless `base`, but not in Alpine or `scratch` images.

The image contains no config file and sets no `GONFIG_*` variables, so the built-in defaults apply. To change them, set the [environment variables](#options) in your own image or deployment, or mount a [config file](#config-file) as `/etc/gonfig/.gonfig.yaml`.

The images are signed with [cosign](https://github.com/sigstore/cosign) by the release workflow. Verify the signature with:
```console
$ cosign verify ghcr.io/denglertai/gonfig:0.2.0 \
    --certificate-identity-regexp '^https://github\.com/denglertai/gonfig/\.github/workflows/release\.yml@refs/tags/' \
    --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

The SBOM of each platform image, listing the packages of the base image and the Go modules of gonfig, is attached as a signed [SPDX](https://spdx.dev/) attestation, since v0.2.0.
It is attached to the image of each platform, e.g. for scanners, and to the multi-platform image, which tags refer to.
Verify the attestations of a release by its image tag, replacing `VERSION` with e.g. `0.2.0`. `latest` would verify the newest release that is not a prerelease, which may be another one:
```console
$ cosign verify-attestation --type spdxjson ghcr.io/denglertai/gonfig:VERSION \
    --certificate-identity-regexp '^https://github\.com/denglertai/gonfig/\.github/workflows/release\.yml@refs/tags/' \
    --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

The image, the release archives and the apk have a signed [SLSA build provenance](https://slsa.dev/spec/v1.0/provenance), since v0.2.0, recording the workflow and commit they were built from.
Verify it with the [GitHub CLI](https://cli.github.com/), replacing `VERSION` with the version of the release as above:
```console
$ gh attestation verify oci://ghcr.io/denglertai/gonfig:VERSION --repo denglertai/gonfig
$ gh attestation verify gonfig_Linux_x86_64.tar.gz --repo denglertai/gonfig
```

## Quick start

1. [Install](#installation) gonfig.
1. Add placeholders like `${ENV_VAR}` to a config file, optionally with [filters](./FILTERS.md) like `${ENV_VAR | trim}`.
1. Run `gonfig config process -f <file>`. The file type is inferred from the extension, or set with `-t`.

The result is printed to stdout, while logs are written to stderr.
Use `-i` to update the file in place or `-o` to write to another file, see [config](#config).

### YAML

`app.yaml`:
```yaml
# Database settings
database:
  host: ${DB_HOST}
  password: ${DB_PASSWORD}
features:
  ${FEATURE | lower}: true
```

```console
$ DB_HOST=db.internal DB_PASSWORD=s3cret FEATURE=NEW_UI gonfig config process -f app.yaml
# Database settings
database:
  host: db.internal
  password: s3cret
features:
  new_ui: true
```

Quote placeholders within flow collections like `["${A}", "${B}"]`, otherwise the file is not valid YAML.

### JSON

`app.json`:
```json
{
  "url": "postgres://${DB_USER}@${DB_HOST}:5432/app",
  "pool": 10
}
```

```console
$ DB_USER=app DB_HOST=db.internal gonfig config process -f app.json
{
  "url": "postgres://app@db.internal:5432/app",
  "pool": 10
}
```

### XML

`server.xml`:
```xml
<?xml version="1.0"?>
<server>
  <database host="${DB_HOST}">
    <password>${DB_PASSWORD}</password>
  </database>
</server>
```

```console
$ DB_HOST=db.internal DB_PASSWORD='s3cr&t<1>' gonfig config process -f server.xml
<?xml version="1.0"?>
<server>
  <database host="db.internal">
    <password>s3cr&amp;t&lt;1&gt;</password>
  </database>
</server>
```

### Properties

`app.properties`:
```properties
# Database settings
db.host=${DB_HOST}
db.password=${DB_PASSWORD | base64}
```

```console
$ DB_HOST=db.internal DB_PASSWORD=s3cret gonfig config process -f app.properties
# Database settings
db.host = db.internal
db.password = czNjcmV0
```

### Plain text

Any other text file is processed line by line with `-t plain`, e.g. `nginx.conf`:
```nginx
server {
    listen 80;
    server_name ${DOMAIN};
    location / {
        proxy_pass http://${UPSTREAM_HOST}:8080;
        proxy_set_header Host $host;
    }
}
```

```console
$ DOMAIN=example.com UPSTREAM_HOST=app gonfig config process -f nginx.conf -t plain
server {
    listen 80;
    server_name example.com;
    location / {
        proxy_pass http://app:8080;
        proxy_set_header Host $host;
    }
}
```

Every `${...}` is replaced. Files using the same syntax for other purposes, like shell scripts, write a literal `${` as `$${`, see [placeholder syntax](FILTERS.md#placeholder-syntax).

`deploy.sh`:
```sh
#!/bin/sh
echo "Deploying ${VERSION} to $${TARGET:-staging} at $(date) by $1"
```

```console
$ VERSION=1.4.2 gonfig config process -f deploy.sh -t plain
#!/bin/sh
echo "Deploying 1.4.2 to ${TARGET:-staging} at $(date) by $1"
```

### TOML

TOML support is planned, see [#161](https://github.com/denglertai/gonfig/issues/161).

## Use cases

**Container entrypoints:** Render the config files of an application from environment variables when the container starts, then start the application:
```sh
#!/bin/sh
gonfig config process -i -f /etc/app/server.xml -f '/etc/app/conf.d/*.yaml'
exec /usr/bin/app
```
gonfig expands the pattern itself, so it works in exec-form entrypoints of images without a shell as well.

**Read-only templates:** Render templates mounted read-only, e.g. from a Kubernetes ConfigMap, into a writable directory like an `emptyDir` with `-o <dir>/`, see [several files](#several-files).

**Secrets mounted as files:** Docker and Kubernetes mount secrets as files. Point a variable to the file with `@` instead of copying the secret into the environment, e.g. `DB_PASSWORD=@/run/secrets/db_password` with `${DB_PASSWORD | trim}`. See [recipes](./FILTERS.md#read-secrets-from-files).

**Values with special characters:** Passwords or URLs containing `&`, `<`, `"` or `:` are escaped as required by XML, JSON or YAML, which plain text replacement tools like `envsubst` do not do.

**Password hashes:** Generate bcrypt hashes at deploy time instead of storing them, e.g. `${ADMIN_PASSWORD | bcrypt}`. See [recipes](./FILTERS.md#generate-password-hashes).

**Reusable settings:** Define filter chains and placeholders like a JDBC URL with defaults once in `.gonfig.yaml` and use them in all config files, e.g. `${@db_url}`. See [templates](./FILTERS.md#templates).

**Environment-specific structures:** Insert whole blocks like a list of log appenders or data sources, or merge environment-specific overrides into a mapping, see [fragments](./FILTERS.md#fragments).

**Templates per environment:** Keep a template in the repository and render it for each environment with `-o`:
```console
$ gonfig config process -f config.template.yaml -o config.yaml -w
```

**Scripts:** Use `gonfig value` to transform single values, e.g. `gonfig value '${CREDENTIALS | base64}'`.

## Usage

`gonfig` may just be used as any other CLI by simply invoking it.

`gonfig --version` (or `-v`) prints the version, e.g. `gonfig version 0.1.0 (commit 0274352, 2026-10-18T08:05:01Z)` for a release, followed by the commit and its date if they are known.
Binaries installed with `go install` print the version only, and the commit is marked with `-dirty` for binaries built from modified sources.

### config

`gonfig config` is intended to interact with various kinds of config file types including `.json`, `.xml`, `.yaml` and `.properties` files, as well as plain text files (`-t plain`).

It has two subcommands: `process` processes the given config files and is described below, while [`check`](#check) reports missing variables and errors without writing anything.

In general, process takes the given input file and creates a flat list of all given keys, nodes and attributes depending on the file type.
Afterwards every entry in that list is being processed individually by applying the filters.

Only the processed values are changed, the rest of the file is preserved as far as the format allows: comments and key order are kept for YAML, JSON and properties files, and YAML files may contain multiple documents.

Usage:

1. Process a file and print the output to stdout
    ```console
    $ gonfig config process -f /path/to/file.xml
    ```
   This will take the given file as an input, read it, apply filters and print the result to stdout.
   Because no file extension is given, the type will be inferred from the file's extension (xml).

1. Process a file and directly write the changes back to the given input file (`-i` / `--inline` flag)
    ```console
    $ gonfig config process -i -f /path/to/file.xml
    ```
   This will take the given file as an input, read it, apply filters and write back to `/path/to/file.xml`.
   Because no file extension is given, the type will be inferred from the file's extension (xml).

1. Process a file with a custom extension that does not allow inferring the file's type (`-t` / `--file-type` flag)
    ```console
    $ gonfig config process -f /path/to/file.abc -t xml
    ```
   This will take the given file as an input, read it, apply filters and print the result to stdout.

1. Process a file and write the result to another file (`-o` / `--output` flag)
    ```console
    $ gonfig config process -f /path/to/template.yaml -o /path/to/config.yaml -w
    ```
   An already existing output file is only overwritten if `-w` / `--overwrite` is given.

1. Set the file mode of the output file (`--mode` flag)
    ```console
    $ gonfig config process -f /path/to/template.yaml -o /path/to/config.yaml --mode 0600
    ```
   Rendered files often contain secrets, so their mode is not left to the umask:
   a new output file gets the mode of the input file, so restricting the template restricts the output, and an existing output file keeps its mode.
   `--mode` sets the mode in both cases, also for `-i`.

1. Process several files (`-f` per file or glob patterns), see [several files](#several-files)
    ```console
    $ gonfig config process -i -f /etc/app/server.xml -f '/etc/app/conf.d/*.yaml'
    $ gonfig config process -f '/templates/*' -o /etc/app/
    ```

Output files are replaced atomically where possible: the result is written to a temporary file next to the output file, which then replaces it.
If writing the temporary file fails, e.g. because the disk is full, the output file is kept as it was, which matters most for `-i`, where it is the template as well.
Symbolic links are kept and the file they point to is replaced, e.g. for files mounted from a Kubernetes ConfigMap.
On Linux and macOS, the owner and group of the file are kept as well.
If the file cannot be replaced, e.g. because its directory is not writable or it is bind-mounted into a container, it is written in place instead; an error during this fallback may leave partial output.

#### Several files

`-f` can be repeated and accepts glob patterns like `'conf.d/*.yaml'` (`*`, `?` and `[...]`, but not `**`), which gonfig expands itself, so quote them to keep the shell from expanding them first.
The files of a pattern are processed in lexical order, directories are skipped, and a pattern matching no file is an error, so a typo cannot skip files silently.
A file given several times is processed once, and `-t` applies to every file.

Several files are written back with `-i`, or into a directory with `-o <dir>/`, where each file keeps its base name:

`templates/app.yaml`:
```yaml
db:
  host: ${DB_HOST}
```

`templates/server.xml`:
```xml
<server><port>${PORT | to_int}</port></server>
```

```console
$ DB_HOST=db.internal PORT=8080 gonfig config process -f 'templates/*' -o rendered/
$ cat rendered/app.yaml rendered/server.xml
db:
  host: db.internal
<server><port>8080</port></server>
```

`-o` names a directory if it ends with `/` or is an existing directory, for a single file as well. Missing directories are created with mode `0755`, minus the umask.
Two files with the same base name for one directory are an error, and stdout only works for a single file.
Each output file gets its mode as for a single file: from its own input file if it is new, or it keeps its mode if it exists.
`--mode` applies to every output file, so avoid it for files with different modes, e.g. configs with `0644` and secrets with `0600`, which would all get the same mode.

All files are processed before any is written, so an error, e.g. a missing variable with `fail-on-missing`, leaves all files unchanged.
Without `-w`, all output files are checked before any is written as well.
Writing happens in two phases: first, the temporary files of all output files are written, and if one fails, e.g. because the disk is full, no file is changed. Then they replace the output files.
Files that cannot be replaced and are written in place are written last. Errors of this phase name the files written already.

Testdata can be found within [cmd/testdata/](cmd/testdata/).

For example processing [cmd/testdata/xml/customers_param.xml](cmd/testdata/xml/customers_param.xml) will print the following result
```console
$ BLA_BLUB=hello INT=5 BOOL=true STRING=test_string FLOAT=123.45 SPECIAL_CHARACTERS="(&\$%(\"§\$//§" gonfig config process -f cmd/testdata/xml/customers_param.xml
<?xml version="1.0"?>
<customers>
   <customer id="10">
      <name>HELLO</name>
      <address>
         <street>true</street>
         <city>Framingham</city>
         <state>MA</state>
         <zip>01701</zip>
      </address>
      <address>
         <street>720 Prospect</street>
         <city>test_string</city>
         <state>MA</state>
         <zip>123.45</zip>
      </address>
      <address ding="(&amp;$%(&quot;§$//§">
         <street>120 Ridge</street>
         <state>(&amp;$%(&quot;§$//§</state>
         <zip>01760</zip>
      </address>
   </customer>
</customers>
```

### check

`gonfig config check` processes files like `config process`, but writes nothing and reports every entry containing placeholders with the environment variables and templates it uses.
Values are not printed, only whether a variable is set, empty or missing, unless [`log-values`](#logging-notes) is enabled for debugging.
This is useful in CI or before a deployment, e.g. to find typos in variable names.

`check.yaml`:
```yaml
database:
  host: ${DB_HOST | default(localhost)}
  user: ${DB_USER}
  password: ${DB_PASWORD}
  port: ${DB_PORT | to_int}
```

```console
$ DB_USER=app DB_PORT=eighty gonfig config check -f check.yaml
check.yaml: 4 entries with placeholders
  database.host
    DB_HOST                  missing, default used
  database.user
    DB_USER                  set
  database.password
    DB_PASWORD               missing
  database.port
    DB_PORT                  set
    error: filter to_int: the value is not an integer
1 missing variable, 1 error
Error: check failed: 1 missing variable, 1 error
```

The check fails with exit code `1` if processing a file would fail, or if a variable is not set and the placeholder provides no [default](./FILTERS.md#default--required), independent of `fail-on-missing`.
Variables set to an empty value are reported, but don't fail the check.
Several files are checked like [several files](#several-files) are processed, e.g. `-f 'conf.d/*.yaml'`: each file is reported, followed by the total.
A file that cannot be read, e.g. because it is invalid, is reported with the error and counts as one; the other files are still checked.

Use `-o json` for a report in JSON. It lists each file with its entries and the number of its missing variables and errors, followed by the totals, for a single file as well:

```console
$ DB_USER=app DB_PORT=eighty gonfig config check -f check.yaml -o json | jq -c '{files: [.files[] | {file, missingVariables, errors}], missingVariables, errors, failed}'
{"files":[{"file":"check.yaml","missingVariables":1,"errors":1}],"missingVariables":1,"errors":1,"failed":true}
Error: check failed: 1 missing variable, 1 error
```

#### Showing the changes

Use `--diff` to review how processing would change a file before deploying it, e.g. where a [fragment](./FILTERS.md#fragments) is inserted.
It adds a unified diff of the changes to the report of each file, and the field `diff` to each file of the JSON report, which is empty if nothing would change.
The results of placeholders and the values inserted by fragments may be secrets, so they are shown as `***`, while the keys and elements of fragments are shown.
Enable [`log-values`](#logging-notes) to see the values.

`deploy.yaml`:
```yaml
database:
  url: postgres://${DB_HOST}:5432/app
  password: ${DB_PASSWORD}
logging:
  level: info
  appenders: ${LOG_APPENDERS | fragment}
```

```console
$ DB_HOST=db.internal DB_PASSWORD=s3cret LOG_APPENDERS='[{name: console}, {name: file, path: /var/log/app.log}]' gonfig config check -f deploy.yaml --diff
deploy.yaml: 3 entries with placeholders
  database.url
    DB_HOST                  set
  database.password
    DB_PASSWORD              set
  logging.appenders
    LOG_APPENDERS            set
0 missing variables, 0 errors
--- deploy.yaml
+++ deploy.yaml
@@ -1,6 +1,9 @@
 database:
-  url: postgres://${DB_HOST}:5432/app
-  password: ${DB_PASSWORD}
+  url: postgres://***:5432/app
+  password: '***'
 logging:
   level: info
-  appenders: ${LOG_APPENDERS | fragment}
+  appenders:
+    - name: '***'
+    - name: '***'
+      path: '***'
```

The diff shows what `config process` would write, so it includes changes of the formatting as well, e.g. the indentation of YAML and JSON files.
Masked values are strings, so typed results like `${PORT | to_int}` are shown as `'***'` in YAML files and `"***"` in JSON files.
Entries of the file that cannot be processed keep their placeholders, while values inserted by fragments are masked even if they cannot be processed, since the text of a fragment may contain secrets. The report names the failed entry either way.

### value

`gonfig value` basically does the same as `gonfig config process` but expects an input string instead. This may be useful if any transformation has to be done in scripts or other places where no file is directly involved.

The output is always echoed to stdout.

Usage:

1. Echo the value of an environment variable (but why?)
    ```console
    $ ABC=123 gonfig value "\${ABC}"
    123
    ```

1. Apply a filter to a variable
    ```console
    $ ABC=123 gonfig value "\${ABC | bcrypt}"
    $2a$10$rX0TunRMPGlntJT6PKDgLuFYMud2gmTvDMFGhsTjpzYX7bLJeXF6G
    ```

1. End each result with a newline (`--newline`)
    ```console
    $ A=first B=second gonfig value --newline "\${A}" "\${B}"
    first
    second
    ```
   By default, results are printed exactly as they are, without a newline, so they can be written to files or piped into other tools byte for byte.
   For a single value, command substitution like `TOKEN=$(gonfig value ...)` works the same with and without the flag, since it removes trailing newlines.
   With several values, `--newline` keeps them on separate lines, while the default joins them, e.g. `firstsecond`.
   If processing any value fails, nothing is printed.

## Filters

Filters transform the values of environment variables. They are added to a placeholder with a pipe `|` and applied from left to right:
```console
${ENV_VAR | filter1 | filter2(param) | filter3}
```

[FILTERS.md](./FILTERS.md) documents the syntax, params, every filter with examples and common recipes.

| Filter | Description | Example |
| --- | --- | --- |
| `upper` | Converts the value to upper case | `${VAL \| upper}` |
| `lower` | Converts the value to lower case | `${VAL \| lower}` |
| `trim` | Removes leading and trailing whitespace | `${VAL \| trim}` |
| `trimleft` | Removes leading spaces | `${VAL \| trimleft}` |
| `trimright` | Removes trailing spaces | `${VAL \| trimright}` |
| `default(value)` | Replaces an empty value, e.g. of a variable that is not set | `${VAL \| default(5432)}` |
| `required` | Fails if the value is empty | `${VAL \| required}` |
| `to_int` | Converts the value to an integer, fails if it is not numeric | `${VAL \| to_int}` |
| `to_float` | Converts the value to a finite floating point number | `${VAL \| to_float}` |
| `to_bool` | Converts the value to a boolean, e.g. from `true` or `1` | `${VAL \| to_bool}` |
| `multiply(m=factor)` | Multiplies an integer value by `m`, which defaults to `1` and may be given by position | `${VAL \| to_int \| multiply(2)}` |
| `md5` | Hashes the value with MD5 (hex encoded) | `${VAL \| md5}` |
| `bcrypt(cost)` | Hashes the value with bcrypt, fails for values longer than 72 bytes. `cost` from 4 to 31 defaults to `10` and may be given by position | `${VAL \| bcrypt(cost=12)}` |
| `base64` | Encodes the value to base64 | `${VAL \| base64}` |
| `base64_decode` | Decodes a base64 value, fails on invalid input | `${VAL \| base64_decode}` |
| `url_encode(mode)` | Escapes the value for URL query parameters, or path segments with mode `path` | `${VAL \| url_encode}` |
| `url_decode(mode)` | Reverses `url_encode` | `${VAL \| url_decode}` |
| `fragment` | Inserts the value as a structure, e.g. a YAML mapping, XML elements or a list created by `split`, see [fragments](./FILTERS.md#fragments) | `${VAL \| split \| fragment}` |
| `replace(old,new)` | Replaces every occurrence of `old` by `new` | `${VAL \| replace(foo,bar)}` |
| `regex(pattern,replacement)` | Replaces every match of the [regular expression](https://pkg.go.dev/regexp/syntax) `pattern`. Capture groups are referenced by `\1` | `${EMAIL \| regex(^\(\w+\)@.+,\1)}` |
| `split(delimiter)` | Splits the value into a [list](./FILTERS.md#lists), the delimiter defaults to `,` | `${VAL \| split(;) \| join}` |
| `join(delimiter)` | Joins a list into a single value, the delimiter defaults to `,` | `${VAL \| split \| join(;)}` |

Additional filters may be added by [plugins](./PLUGINS.md).

## Configuration

Global CLI configuration is loaded via `viper` from multiple sources.

Order of precedence (highest to lowest):
1. CLI flags
1. Environment variables
1. Config file (`.gonfig.yaml`)
1. Built-in defaults

### Options

| Option | CLI flag | Environment variable | Default | Description |
| --- | --- | --- | --- | --- |
| Log level | `--log-level`, `-l` | `GONFIG_LOG_LEVEL` | `info` | Logging verbosity (`trace`, `debug`, `info`, `warn`, `error`, `fatal`) |
| Include source location | `--log-source`, `-s` | `GONFIG_LOG_SOURCE` | `false` | Includes source location in log output |
| Config directory | `--config-path` | `GONFIG_CONFIG_PATH` | empty | Directory that contains `.gonfig.yaml` |
| Plugin directory | `--plugin-path` | `GONFIG_PLUGIN_PATH` | `./plugins` | Directory scanned recursively for executable plugins (`gonfig-plugin-*`) and deprecated Go plugins (`.so` files) |
| Log values | `--log-values` | `GONFIG_LOG_VALUES` | `false` | Includes processed values in debug logs, error messages of filters, the parser messages of invalid fragments and the diff of `config check --diff`. Values may contain secrets, so only use it for debugging; gonfig warns on startup while it is enabled |
| Fail on missing variables | `--fail-on-missing` | `GONFIG_FAIL_ON_MISSING` | `false` | Fails for [environment variables that are not set](./FILTERS.md#missing-variables) instead of using empty strings, unless the placeholder provides a default |
| Allow unknown filters | `--allow-unknown-filters` | `GONFIG_ALLOW_UNKNOWN_FILTERS` | `false` | Passes values on unchanged with a warning instead of failing for [unknown filters](./FILTERS.md#order-of-operations). This may write secrets that were meant to be hashed or encrypted |

### Config File

By default, `gonfig` searches for a `.gonfig.yaml` file in these directories:
1. `$HOME`
1. Current working directory (`.`)
1. `/etc/gonfig`

If `--config-path` (or `GONFIG_CONFIG_PATH`) is set, only that directory is used.

Example config file:

```yaml
log-level: debug
log-source: true
plugin-path: ./plugins
# Keep processed values, which may contain secrets, out of debug logs (default)
log-values: false
# Replace environment variables that are not set by empty strings (default)
fail-on-missing: false
# Fail on unknown filters instead of passing values on unchanged (default)
allow-unknown-filters: false
# Reusable filter chains and placeholders, used as ${VAR | @secret} and ${@db_url}
templates:
  filters:
    secret:
      description: Trims a secret and encodes it to base64
      filters: trim | base64
  placeholders:
    db_host:
      env: DB_HOST
      default: localhost
    db_url:
      description: JDBC URL of the database
      value: "jdbc:postgresql://${@db_host}:5432/app"
```

`plugin-path` can be configured in `.gonfig.yaml` and is resolved relative to the current working directory when provided as a relative path.

`templates` can only be configured in `.gonfig.yaml`, see [templates](./FILTERS.md#templates).

### Examples

Set options via environment variables:

```bash
export GONFIG_LOG_LEVEL=debug
export GONFIG_LOG_SOURCE=true
gonfig config process -f ./config.yaml
```

Use a custom config directory:

```bash
gonfig --config-path /etc/myapp config process -f ./config.yaml
```

Override everything via CLI flag:

```bash
GONFIG_LOG_LEVEL=error gonfig --log-level trace config process -f ./config.yaml
```

### Logging Notes

Logging is implemented with Go's `log/slog`.
The wrappers in `./pkg/logging` also support `trace` and `fatal` levels.
`fatal` exits with status code `1`.

Since the CLI command output may be written to stdout, logs are written to stderr by default.
Successful runs write nothing to stderr at the default level `info`, so gonfig can be used in scripts and entrypoints without noise. Use `--log-level debug` to see which files are processed and written.

Processed values are not logged by default, even at `trace` level, since they may contain secrets. Debug logs show the entry and the type of its result instead, e.g. `"entry":"database.password","type":"string"`.
Error messages of filters don't contain values either, e.g. `filter to_int: the value is not an integer`, together with the entry that failed.
Invalid fragments only report their format and, if known, the location, e.g. `invalid YAML fragment on line 3`, since the messages of the parsers may quote parts of the fragment.
The diff of [`config check --diff`](#showing-the-changes) shows values as `***`.
To include the values for debugging, enable `log-values` with `--log-values`, `GONFIG_LOG_VALUES=true` or in `.gonfig.yaml`. gonfig then logs a warning on startup, so the option is not left enabled by accident.

## Plugins

Plugins add custom filters. They are separate executables named `gonfig-plugin-*`, which gonfig starts from the plugin directory and talks to over stdin and stdout. The plugin directory defaults to `./plugins` and may be changed with `--plugin-path`, `GONFIG_PLUGIN_PATH` or `plugin-path` in `.gonfig.yaml`.

Plugins don't depend on the Go version or build flags of the gonfig binary and may be written in any language. Go plugins (`.so` files) are still loaded, but deprecated.
See [PLUGINS.md](./PLUGINS.md) for how plugins work, a step-by-step guide, examples and the migration from Go plugins, and [plugins/dummy](./plugins/dummy/) for a minimal plugin.

## Benchmarks

Benchmarks for processing config files of at least 1 MiB per file type and for single values with filter chains are located in [cmd/benchmarks](./cmd/benchmarks/).

```bash
# Run all benchmarks
make bench
# Run a subset of the benchmarks
make bench BENCH=ProcessFile/yaml
# Profile the benchmarks and open the CPU profile in pprof
make profile BENCH=ProcessFile/yaml
```

See [cmd/benchmarks/README.md](./cmd/benchmarks/README.md) for comparing runs, baseline metrics and known bottlenecks.

## Releasing

Releases are created by pushing a version tag. All build and publishing steps run in GitHub Actions (`release.yml` and `melange.yml`).

Prerequisites:
1. You are on `main` and it is fully synced with `origin/main`
1. Your working tree has no uncommitted changes

Recommended flow:

1. Create a release preparation branch and update `melange.yaml` automatically:
  ```bash
  make prepare-release-branch VERSION=vX.Y.Z
  ```
  This creates `release/vX.Y.Z` by default, updates `package.version` in `melange.yaml` to `X.Y.Z`, commits, and pushes the branch.
1. Open a PR from the release branch, review, and merge it into `main`.
1. Switch back to `main` and pull latest merged changes:
  ```bash
  git switch main
  git pull --ff-only origin main
  ```
1. Run tests:
  ```bash
  make test
  ```
1. Run a local dry-run build (no publish):
  ```bash
  make install-goreleaser
  make release-dry-run
  ```
1. Draft a new release tag and push it:
  ```bash
  make release-draft
  ```
  `release-draft` reads `package.version` from `melange.yaml` and uses tag `v<package.version>`.

What `release-draft` does:
1. Verifies you are on `main` and `HEAD` matches `origin/main`
1. Verifies the git working tree is clean
1. Verifies `VERSION` uses semantic tag format `vX.Y.Z`
1. Verifies `melange.yaml` version matches `VERSION` (without `v`)
1. Runs tests
1. Creates and pushes tag `VERSION`
1. Triggers GitHub Actions workflows that build and publish release assets and APK updates

Optional:
1. Use a custom branch name:
  ```bash
  make prepare-release-branch VERSION=vX.Y.Z RELEASE_BRANCH=chore/release-vX.Y.Z
  ```
1. Override the release tag explicitly:
  ```bash
  make release-draft VERSION=vX.Y.Z
  ```

After the tag is pushed, monitor the workflow runs in GitHub Actions and verify the generated release assets and APK publication.

## Future goals

Stability:
* Improve error handling
* More tests
* More useful logging

Filters:
* Datatype conversion: Write the values back to the file with the correct datatypes applied (int, string, bool, ...) while respecting the possibilities of the given file type
* Validation: 
  * Ensure that a value is filled (required values)
  * Ensure that the input can be converted to a datatype (int, bool, float, ...)
  * Ensure that the input is in a list of expected values (enum)