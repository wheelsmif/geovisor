#!/usr/bin/env sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
work=${TMPDIR:-/tmp}/geovisor-release-verify-$$
trap 'rm -rf "$work"' EXIT HUP INT TERM
mkdir -p "$work"
cd "$root"
export GOTOOLCHAIN
GOTOOLCHAIN=$(cat .go-version)

version=v0.0.0-test
ldflags="-buildid= -X github.com/geo-suite/geovisor/internal/version.Version=$version"
common="-trimpath -buildvcs=false"

go build $common -ldflags "$ldflags" -o "$work/geovisor-a" ./cmd/geovisor
go build $common -ldflags "$ldflags" -o "$work/geovisor-b" ./cmd/geovisor
go build $common -ldflags "$ldflags" -o "$work/gv" ./cmd/geovisor

cmp "$work/geovisor-a" "$work/geovisor-b"
test "$("$work/geovisor-a" --version 2>&1)" = "$version"
test "$("$work/gv" --version 2>&1)" = "$version"
