# GoReleaser Build Instructions for Multi-Arch with CGO

This project supports building multi-architecture binaries with CGO enabled using GoReleaser and Zig.

## Prerequisites

### Using Zig for Cross-Compilation

Zig provides a simple, single-toolchain cross-compilation experience for CGO:

```bash
# Install Zig (version 0.11.0 or later, 0.15.2 recommended)
# On Linux
wget https://ziglang.org/download/0.15.2/zig-linux-x86_64-0.15.2.tar.xz
tar xf zig-linux-x86_64-0.15.2.tar.xz
sudo mv zig-linux-x86_64-0.15.2 /usr/local/zig
export PATH=$PATH:/usr/local/zig

# On macOS
brew install zig

# On any platform with Go
go install github.com/dosgo/zigtool/zig@latest
```

## Building

```bash
# Build snapshot (for testing)
goreleaser build --snapshot --clean

# Full release
goreleaser release --clean
```

## Using Docker/Podman

For a consistent build environment, use the provided Dockerfile:

```bash
# Build the builder image
docker build -t gonfig-builder -f Dockerfile.goreleaser .

# Run goreleaser in container
docker run --rm -v "$PWD":/workspace -w /workspace gonfig-builder \
    goreleaser build --snapshot --clean
```

## Makefile Targets

```bash
# Build for current platform
make build

# Build all platforms (snapshot)
make build-all

# Release
make release

# Install Zig
make install-zig
```

## Supported Platforms

- Linux: amd64, arm64
- macOS: amd64, arm64
- Windows: amd64

## Troubleshooting

### macOS Cross-Compilation

Zig handles macOS cross-compilation automatically from Linux without requiring the macOS SDK.

### CGO Issues

If you encounter CGO-related errors:
1. Ensure Zig is installed and in PATH: `zig version`
2. Verify the Zig version is 0.11.0 or later
3. Check that required C libraries are available for the target platform

## Why Zig?

This project uses Zig for CGO cross-compilation because it provides:
- Single toolchain for all targets (Linux, macOS, Windows)
- No need to install multiple cross-compilers
- Automatic libc handling
- Native macOS cross-compilation from Linux
- Simpler configuration and better reproducibility
