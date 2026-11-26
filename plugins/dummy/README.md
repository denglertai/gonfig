# Dummy plugin

A minimal example plugin providing the filter `reverse`, which reverses the value.
See [PLUGINS.md](../../PLUGINS.md) for how plugins work.

## Build

Run the following command within this directory:
```console
go build -o gonfig-plugin-dummy .
```

On Windows, gonfig only starts plugins ending in `.exe`:
```console
go build -o gonfig-plugin-dummy.exe .
```

Point `--plugin-path` to this directory to use it, e.g. `gonfig --plugin-path ./plugins/dummy value ...` from the root of the repository.
The plugin is built against the gonfig sources of the repository, see the `replace` directive in [go.mod](./go.mod).
