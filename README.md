# gonfig

gonfig is a project intended to process different typs of config files and fill their contents using environment variables

## Plugins

Plugins are expected to be located in `./plugins/` and are loaded dynamically on startup by walking through the directory looking for files ending with `.so`.

By implementing plugins it is possible to extend gonfig by either supplying a set of `cobra.Commands` or a list of filters which may then be used within config files.

The expected interfaces are defined in [./pkg/plugin/plugin.go](./pkg/plugin/plugin.go)

Since filters are bound to their names, it is not possible to add two filters with the same name.
If a filter with a given name is already registered the next one will be skipped. This may change in the future.

See [./plugins/dummy](./plugins/dummy/) for a simple example.

## Filters

Filters are being used to process the values retrieved from environment variables.

