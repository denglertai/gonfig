# Container image published by GoReleaser (dockers_v2 in .goreleaser.yaml).
# It copies the prebuilt binaries from the build context, so it cannot be built from the sources with docker build.
#
# The binaries are linked against glibc for cgo (Go plugins), so the base provides glibc.
# Once cgo is removed (#244), cgr.dev/chainguard/static suffices.
FROM cgr.dev/chainguard/glibc-dynamic:latest@sha256:2a3f1ec8a825dfbb1211b5835a9b8b3fb8d76c7bd22a189b1f84157ed2028293

ARG TARGETPLATFORM

COPY $TARGETPLATFORM/gonfig /usr/bin/gonfig
COPY LICENSE /usr/share/licenses/gonfig/LICENSE

# The base image runs as the nonroot user (uid 65532)
ENTRYPOINT ["/usr/bin/gonfig"]
