# Container image published by GoReleaser (dockers_v2 in .goreleaser.yaml).
# It copies the prebuilt binaries from the build context, so it cannot be built from the sources with docker build.
#
# The binaries are linked against glibc for cgo (Go plugins), so the base provides glibc.
# Once cgo is removed (#244), cgr.dev/chainguard/static suffices.
FROM cgr.dev/chainguard/glibc-dynamic:latest@sha256:709673274b417a48f56374da8a7633ef7b2f5b27e34612ce98a335737318cc94

ARG TARGETPLATFORM

COPY $TARGETPLATFORM/gonfig /usr/bin/gonfig
# The plugins for secret managers, in the directory gonfig searches by default. They authenticate on the first
# placeholder using them, so gonfig runs without credentials for secret managers that aren't used.
COPY $TARGETPLATFORM/gonfig-plugin-openbao $TARGETPLATFORM/gonfig-plugin-azure-keyvault /usr/libexec/gonfig/plugins/
COPY LICENSE /usr/share/licenses/gonfig/LICENSE

# The base image runs as the nonroot user (uid 65532)
ENTRYPOINT ["/usr/bin/gonfig"]
