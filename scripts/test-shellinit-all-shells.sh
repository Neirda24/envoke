#!/usr/bin/env bash
# One-off verification: runs internal/shellinit's full test suite in a
# single container with all five supported shells (bash, zsh, fish, tcsh,
# powershell) installed at once, so requireInterpreter never skips a test --
# every TestGenerate_* runs for real, not just the ones for whatever happens
# to be on the machine invoking this script.
#
# Deliberately NOT a permanent `.dagger` check: `.dagger/main.go`'s five
# test-shell-* checks intentionally isolate one shell per container each
# (see CLAUDE.md's Status section) so each stays cheap to build/cache and
# `dagger check -m .dagger` runs them in parallel -- this script exists for
# the rarer case of wanting one combined zero-skip confirmation run.
#
# Requires Docker. Never run this suite's `go test` directly on the host --
# see CLAUDE.md's Go conventions section.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

# Same pinned image .dagger/main.go's goImage const uses, for consistency.
IMAGE="golang:1.26-bookworm@sha256:1ecb7edf62a0408027bd5729dfd6b1b8766e578e8df93995b225dfd0944eb651"

docker run --rm -v "$(pwd):/src" -w /src "$IMAGE" bash -c '
set -e
apt-get update -qq
apt-get install -y --no-install-recommends -qq zsh fish tcsh wget apt-transport-https software-properties-common gnupg2 ca-certificates >/dev/null
wget -q https://packages.microsoft.com/config/debian/12/packages-microsoft-prod.deb -O /tmp/packages-microsoft-prod.deb
dpkg -i /tmp/packages-microsoft-prod.deb >/dev/null
apt-get update -qq
apt-get install -y -qq powershell >/dev/null
go test ./internal/shellinit/... -race -v
'
