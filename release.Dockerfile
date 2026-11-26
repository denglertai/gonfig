# Container image published by GoReleaser (dockers_v2 in .goreleaser.yaml).
# It copies the prebuilt binaries from the build context, so it cannot be built from the sources with docker build.
#
# gonfig and the plugins are built without cgo, so they are static and need no libc.
FROM cgr.dev/chainguard/static:latest@sha256:599c3cd7597b84aa149c4282beecc8c6d4e1cb5c95dc8d982e70cd0a2a9147a2

ARG TARGETPLATFORM

COPY $TARGETPLATFORM/gonfig /usr/bin/gonfig
# The plugins for secret managers, in the directory gonfig searches by default. They authenticate on the first
# placeholder using them, so gonfig runs without credentials for secret managers that aren't used.
COPY $TARGETPLATFORM/gonfig-plugin-openbao $TARGETPLATFORM/gonfig-plugin-azure-keyvault /usr/libexec/gonfig/plugins/
COPY LICENSE /usr/share/licenses/gonfig/LICENSE

# The base image runs as the nonroot user (uid 65532)
ENTRYPOINT ["/usr/bin/gonfig"]
