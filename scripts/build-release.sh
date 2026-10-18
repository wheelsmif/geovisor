#!/usr/bin/env sh
# Release build from a git archive of HEAD, so the compiler reads the LF blobs
# stored in git. Uncommitted edits are absent from that archive.
set -eu

version=${1:-0.0.0-dev}
if ! printf '%s\n' "$version" | grep -Eq '^(0\.0\.0-dev|v?[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?)$'; then
  echo "version must be 0.0.0-dev or a semantic version" >&2
  exit 2
fi

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
dist="$root/dist"
export_dir=$(mktemp -d)
archive_tar=$(mktemp)
stage_root=
cleanup() {
  rm -rf "$export_dir"
  if [ -n "$stage_root" ]; then
    rm -rf "$stage_root"
  fi
  rm -f "$archive_tar"
}
trap cleanup EXIT HUP INT TERM

git -c core.autocrlf=false -C "$root" archive --format=tar -o "$archive_tar" HEAD
tar -xf "$archive_tar" -C "$export_dir"
rm -f "$archive_tar"
# Notices are packaged, not compiled. Overlay the working-tree copy so a
# release build includes the file before it is committed, and strip CR so a
# core.autocrlf checkout cannot put CRLF into the archive.
if [ -f "$root/THIRD_PARTY_NOTICES" ]; then
  tr -d '\r' < "$root/THIRD_PARTY_NOTICES" > "$export_dir/THIRD_PARTY_NOTICES"
fi

stage_root="$dist/.stage"
rm -rf "$dist"
mkdir -p "$stage_root"

export CGO_ENABLED=0
export GOTOOLCHAIN
GOTOOLCHAIN=go$(cat "$export_dir/.go-version")
host_os=$(go env GOHOSTOS)
host_arch=$(go env GOHOSTARCH)
ldflags="-s -w -buildid= -X github.com/wheelsmif/geovisor/internal/version.Version=$version"
tar_help=$(tar --help 2>&1 || true)

write_archive() {
  stage=$1
  archive=$2
  if printf '%s\n' "$tar_help" | grep -q -- '--sort' &&
     printf '%s\n' "$tar_help" | grep -q -- '--mtime' &&
     printf '%s\n' "$tar_help" | grep -q -- '--numeric-owner' &&
     command -v gzip >/dev/null 2>&1; then
    raw=$(mktemp)
    LC_ALL=C tar --sort=name --mtime=@0 --owner=0 --group=0 --numeric-owner -cf "$raw" -C "$stage" .
    gzip -n -c "$raw" > "$archive"
    rm -f "$raw"
  else
    tar -czf "$archive" -C "$stage" .
  fi
}

check_version() {
  bin=$1
  actual=$("$bin" --version 2>&1) || {
    echo "native version command failed: $bin" >&2
    exit 1
  }
  [ "$actual" = "$version" ] || {
    echo "native version output was '$actual', want '$version'" >&2
    exit 1
  }
}

cd "$export_dir"
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
  test -f "$stage/gv$suffix"
  metadata=$(go version -m "$stage/geovisor$suffix")
  printf '%s\n' "$metadata" | grep -F "github.com/wheelsmif/geovisor" >/dev/null
  printf '%s\n' "$metadata" | grep -F "CGO_ENABLED=0" >/dev/null
  cp LICENSE README.md SECURITY.md CONTRIBUTING.md THIRD_PARTY_NOTICES "$stage/"
  write_archive "$stage" "$dist/geovisor_${version}_${GOOS}_${GOARCH}.tar.gz"

  if [ "$GOOS" = "$host_os" ] && [ "$GOARCH" = "$host_arch" ]; then
    check_version "$stage/geovisor$suffix"
    check_version "$stage/gv$suffix"
  fi
done

cd "$dist"
names=$(mktemp)
for archive in *.tar.gz; do
  printf '%s\n' "$archive"
done | LC_ALL=C sort > "$names"
: > checksums.txt
while IFS= read -r archive; do
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum -- "$archive"
  else
    shasum -a 256 -- "$archive"
  fi
done < "$names" > checksums.txt
rm -f "$names"
