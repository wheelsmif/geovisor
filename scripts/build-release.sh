#!/usr/bin/env sh
set -eu

version=${1:-0.0.0-dev}
case "$version" in
  0.0.0-dev|v[0-9]*.[0-9]*.[0-9]*|[0-9]*.[0-9]*.[0-9]*) ;;
  *) echo "version must be 0.0.0-dev or a semantic version" >&2; exit 2 ;;
esac

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
dist="$root/dist"
stage_root="$dist/.stage"
rm -rf "$dist"
mkdir -p "$stage_root"
trap 'rm -rf "$stage_root"' EXIT HUP INT TERM

export CGO_ENABLED=0
export GOTOOLCHAIN
GOTOOLCHAIN=go$(cat "$root/.go-version")
host_os=$(go env GOHOSTOS)
host_arch=$(go env GOHOSTARCH)
ldflags="-s -w -buildid= -X github.com/geo-suite/geovisor/internal/version.Version=$version"

cd "$root"
for target in windows/amd64 windows/arm64 linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do
  GOOS=${target%/*}
  GOARCH=${target#*/}
  export GOOS GOARCH
  suffix=
  [ "$GOOS" = windows ] && suffix=.exe
  stage="$stage_root/$GOOS-$GOARCH"
  mkdir -p "$stage"

  go build -trimpath -buildvcs=false -ldflags "$ldflags" -o "$stage/geovisor$suffix" ./cmd/geovisor
  go build -trimpath -buildvcs=false -ldflags "$ldflags" -o "$stage/gv$suffix" ./cmd/geovisor
  cp LICENSE README.md SECURITY.md CONTRIBUTING.md "$stage/"
  tar -czf "$dist/geovisor_${version}_${GOOS}_${GOARCH}.tar.gz" -C "$stage" .

  if [ "$GOOS" = "$host_os" ] && [ "$GOARCH" = "$host_arch" ]; then
    actual=$("$stage/geovisor$suffix" --version 2>&1)
    [ "$actual" = "$version" ] || {
      echo "native version output was '$actual', want '$version'" >&2
      exit 1
    }
  fi
done

cd "$dist"
if command -v sha256sum >/dev/null 2>&1; then
  sha256sum ./*.tar.gz > checksums.txt
else
  shasum -a 256 ./*.tar.gz > checksums.txt
fi