# GoReleaser Build Instructions for Multi-Arch with CGO

This project supports building multi-architecture binaries with CGO enabled using GoReleaser and Zig.

## Prerequisites

### Using Zig for Cross-Compilation

Zig provides a simple, single-toolchain cross-compilation experience for CGO:

```bash
# Install Zig (version 0.11.0 or later, 0.15.2 recommended)
# On Linux
wget https://ziglang.org/download/0.15.2/zig-x86_64-linux-0.15.2.tar.xz
tar xf zig-x86_64-linux-0.15.2.tar.xz
sudo mv zig-x86_64-linux-0.15.2 /usr/local/zig
rm zig-x86_64-linux-0.15.2.tar.xz
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
- Container image `ghcr.io/denglertai/gonfig`: linux/amd64, linux/arm64

## Container Image

`dockers_v2` in `.goreleaser.yaml` builds a multi-arch image from the Linux binaries with [release.Dockerfile](release.Dockerfile) and pushes it to ghcr.io as part of `goreleaser release`.
The Dockerfile only copies the prebuilt binaries, so it cannot be built with `docker build` from the sources, and arm64 needs no QEMU.
`docker_signs` signs the image keyless with cosign, using the OIDC identity of the release workflow (`id-token: write`).
Buildx attestations are disabled (`sbom: false`, `--provenance=false`), since ghcr.io lists them as images of the platform `unknown/unknown`. Instead, the release workflow scans each platform image with syft after GoReleaser and attaches the SBOM with `cosign attest --type spdxjson` to the platform image and to the image index.

The base image `cgr.dev/chainguard/glibc-dynamic` is pinned by digest and updated by Renovate. It provides glibc, which the cgo binaries need.

`goreleaser release --snapshot --clean` builds the image locally without pushing or signing; it requires Docker with Buildx.

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
