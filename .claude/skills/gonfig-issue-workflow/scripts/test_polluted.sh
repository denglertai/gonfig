#!/bin/sh
# Run all tests with every gonfig option enabled through environment variables, a config file in the home
# directory and a config path. Tests must pass anyway, since the packages reading config isolate themselves
# in TestMain (testutil.IsolateFromEnvironment). A failure means a test depends on the machine running it.
#
# Usage (from the repository root): test_polluted.sh [go test flags]
set -e

dir=$(mktemp -d)
trap 'rm -rf "$dir"' EXIT

# The tests run with another HOME, which would download all modules into a read-only cache below it
# that rm cannot remove, so the caches of the user are kept
GOMODCACHE=$(go env GOMODCACHE)
GOCACHE=$(go env GOCACHE)
export GOMODCACHE GOCACHE

config='log-level: debug
log-source: true
log-values: true
fail-on-missing: true
allow-unknown-filters: true'
mkdir -p "$dir/home" "$dir/config"
printf '%s\n' "$config" > "$dir/home/.gonfig.yaml"
printf '%s\n' "$config" > "$dir/config/.gonfig.yaml"

status=0
echo "== GONFIG_* variables and home config file"
HOME="$dir/home" GONFIG_LOG_LEVEL=debug GONFIG_LOG_SOURCE=true GONFIG_LOG_VALUES=true GONFIG_FAIL_ON_MISSING=true \
	GONFIG_ALLOW_UNKNOWN_FILTERS=true GONFIG_PLUGIN_PATH="$dir/no-plugins" go test -count=1 "$@" ./... || status=1

echo "== GONFIG_CONFIG_PATH"
HOME="$dir/home" GONFIG_CONFIG_PATH="$dir/config" go test -count=1 "$@" ./... || status=1

exit $status
