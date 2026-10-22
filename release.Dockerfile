# Container image published by GoReleaser (dockers_v2 in .goreleaser.yaml).
# It copies the prebuilt binaries from the build context, so it cannot be built from the sources with docker build.
#
# The binaries are linked against glibc for cgo (Go plugins), so the base provides glibc.
# Once cgo is removed (#244), cgr.dev/chainguard/static suffices.
FROM cgr.dev/chainguard/glibc-dynamic:latest@sha256:00696e56634cf72d966ee0e0f26224c83a192569035caf1e31297f77b0e85212

ARG TARGETPLATFORM

COPY $TARGETPLATFORM/gonfig /usr/bin/gonfig
COPY LICENSE /usr/share/licenses/gonfig/LICENSE

# The base image runs as the nonroot user (uid 65532)
ENTRYPOINT ["/usr/bin/gonfig"]
