# Container image published by GoReleaser (dockers_v2 in .goreleaser.yaml).
# It copies the prebuilt binaries from the build context, so it cannot be built from the sources with docker build.
#
# The binaries are linked against glibc for cgo (Go plugins), so the base provides glibc.
# Once cgo is removed (#244), cgr.dev/chainguard/static suffices.
FROM cgr.dev/chainguard/glibc-dynamic:latest@sha256:5bb932663cff4fb276186720e0357af55c5e47f12e4d9f44ea0060fa3e7bca88

ARG TARGETPLATFORM

COPY $TARGETPLATFORM/gonfig /usr/bin/gonfig
# The plugins for secret managers, in the directory gonfig searches by default. They authenticate on the first
# placeholder using them, so gonfig runs without credentials for secret managers that aren't used.
COPY $TARGETPLATFORM/gonfig-plugin-openbao $TARGETPLATFORM/gonfig-plugin-azure-keyvault /usr/libexec/gonfig/plugins/
COPY LICENSE /usr/share/licenses/gonfig/LICENSE

# The base image runs as the nonroot user (uid 65532)
ENTRYPOINT ["/usr/bin/gonfig"]
