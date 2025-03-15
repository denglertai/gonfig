# gonfig

gonfig is a project intended to process different types of config files and fill their contents using environment variables.

The idea for creating this was while having to handle application's config files inside containers by filling their parameters and making sure to not to have to rely on secure input values. 

An example would be `&` (ampersand) within `xml` files which has to be escaped with `&amp;`

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

## Usage

`gonfig` may just be used as any other CLI by simply invoking it.

### config

`gonfig config` is intended to interact with various kinds of config file types including `.json`, `.xml`, `.yaml` and `.properties` files.

By now the only subcommand included is `process`. This is being used to actually process the given config file.

In general, process takes the given input file and creates a flat list of all given keys, nodes and attributes depending on the file type.
Afterwards every entry in that list is being processed individually by applying the filters.

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

1. Process a file with a custom extension that does not allow inferring the file's type (`-t` / `--inline` flag)
    ```console
    $ gonfig config process -f /path/to/file.abc -t xml
    ```
   This will take the given file as an input, read it, apply filters and print the result to stdout.

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
</customers
```

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

## Logging

Logging is being done using `log/slog` package. 
Within `./pkg/logging` are some wrapper functions to also support additional log levels `trace` and `fatal`.
The latter automatically exits with `1`.

Logging may be configured using the switches `-log-level` / `l` and `-log-source` / `-s` on the cli itself.
Since the cli outputs to stdout, all logs are being written to stderr.

## Plugins

Plugins are expected to be located in `./plugins/` and are loaded dynamically on startup by walking through the directory looking for files ending with `.so`.

By implementing plugins it is possible to extend gonfig by either supplying a set of `cobra.Commands` or a list of filters which may then be used within config files.

The expected interfaces are defined in [./pkg/plugin/plugin.go](./pkg/plugin/plugin.go)

Since filters are bound to their names, it is not possible to add two filters with the same name.
If a filter with a given name is already registered the next one will be skipped. This may change in the future.

See [./plugins/dummy](./plugins/dummy/) for a simple example.

## Filters

Filters are being used to process the values retrieved from environment variables.

The currently supported set of filters may be found in [filters](./internal/filter/filter.go).

The syntax for applying a filter is by adding it using a pipe `|`
```console
${ENV_VAR | filter1 | filter2 | filter3}
```

In this case the value for `ENV_VAR` will be read, afterwards the filters `filter1`, `filter2` and `filter3` are being applied.

This may also be used as part of substrings 
```console
$ gonfig value https://${DOMAIN}/search?${QUERY | url_escape}
https://google.com/search?q=Nanowar+of+Steel+-+HelloWorld.java
```
Note: The filter `url_escape` does not exist. But it may be added either to the list of default filters or as a plugin.

## Future goals

Docs:
* Improve documentation

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