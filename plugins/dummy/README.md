# Dummy plugin

A minimal example plugin providing the filter `reverse`, which reverses the value.
See [PLUGINS.md](../../PLUGINS.md) for how plugins work.

## Build

Run the following command within this directory:
```console
go build -o gonfig-plugin-dummy .
```

gonfig finds the plugin when it runs from the root of the repository, since `./plugins` is the default plugin directory.
The plugin is built against the gonfig sources of the repository, see the `replace` directive in [go.mod](./go.mod).
