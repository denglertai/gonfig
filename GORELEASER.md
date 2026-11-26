# GoReleaser Build Instructions

Releases are built with [GoReleaser](https://goreleaser.com/), configured in [.goreleaser.yaml](./.goreleaser.yaml). gonfig, its plugins and the container image are built without cgo (`CGO_ENABLED=0`), so every target is cross-compiled from Linux with the Go toolchain alone and the binaries are static. No C compiler or other cross toolchain is needed.

## Building

```bash
# Build snapshot (for testing)
goreleaser build --snapshot --clean

# Full release
goreleaser release --clean
```

Releases are published by GitHub Actions when a version tag is pushed, see [Releasing](./README.md#releasing). The snapshot workflow `release-snapshot.yml` checks the archives and the image without publishing.

## Makefile Targets

```bash
# Build for current platform
make build

# Build all platforms (snapshot)
make build-all

# Run a snapshot release locally, without publishing
make release-dry-run
```

## Supported Platforms

- Linux: amd64, arm64
- macOS: amd64, arm64
- Windows: amd64
- Container image `ghcr.io/denglertai/gonfig`: linux/amd64, linux/arm64

GoReleaser builds all of them, including the macOS archives, so they are part of `gonfig_<version>_checksums.txt` and have an SBOM like the Linux and Windows archives. Their SLSA build provenance is created by the release workflow for all archives and the checksums file.
Releases up to v0.3.1 built the macOS archives in a separate job, so they are missing from the checksums and have no SBOM.

## Container Image

`dockers_v2` in `.goreleaser.yaml` builds a multi-arch image from the Linux binaries with [release.Dockerfile](release.Dockerfile) and pushes it to ghcr.io as part of `goreleaser release`.
The Dockerfile only copies the prebuilt binaries, so it cannot be built with `docker build` from the sources, and arm64 needs no QEMU.
`docker_signs` signs the image keyless with cosign, using the OIDC identity of the release workflow (`id-token: write`).
Buildx attestations are disabled (`sbom: false`, `--provenance=false`), since ghcr.io lists them as images of the platform `unknown/unknown`. Instead, the release workflow scans each platform image with syft after GoReleaser and attaches the SBOM with `cosign attest --type spdxjson` to the platform image and to the image index.
`actions/attest` creates the SLSA build provenance of the image index, which is pushed to the registry as well, and of the archives, in the job building them. `melange.yml` creates the provenance of the apk. Storage records are disabled, since they are only available for repositories owned by organizations.

The base image `cgr.dev/chainguard/static` is pinned by digest and updated by Renovate. The binaries are static, so the image needs no libc.

`goreleaser release --snapshot --clean` builds the image locally without pushing or signing; it requires Docker with Buildx.

## Troubleshooting

### Binary is not static

Check the build with `file`, which reports `statically linked` for the Linux binaries. Make sure `CGO_ENABLED=0` is set for the build in `.goreleaser.yaml`, and for `go build` outside of GoReleaser: `CGO_ENABLED=0 go build -o gonfig .`.
