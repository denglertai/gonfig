# Dummy plugin

A minimal example plugin providing the filter `reverse`, which reverses the value.
See [PLUGINS.md](../../PLUGINS.md) for how plugins work.

## Build

Plugins have to be built with the same Go version and build flags as the gonfig binary loading them.
Run the following command within this directory:
```console
go build -buildmode=plugin -o dummy.so .
```
