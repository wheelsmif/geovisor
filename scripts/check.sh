#!/usr/bin/env sh
set -eu

require_browser=0
security=0
for argument in "$@"; do
  case "$argument" in
    --require-browser) require_browser=1 ;;
    --security) security=1 ;;
    *) echo "unknown option: $argument" >&2; exit 2 ;;
  esac
done

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$root"
export GOTOOLCHAIN
GOTOOLCHAIN=go$(cat .go-version)
[ "$require_browser" -eq 0 ] || export GEOVISOR_REQUIRE_BROWSER=1

npm ci
npm run check
# npm succeeded, so Node is present. Fail the apply harness instead of skipping.
export GEOVISOR_REQUIRE_NODE=1
test -z "$(gofmt -l .)"
go vet ./...
go run honnef.co/go/tools/cmd/staticcheck@v0.7.0 ./...
go test ./...
./scripts/verify-release.sh

if [ "$security" -eq 1 ]; then
  npm audit --audit-level=high
  go run golang.org/x/vuln/cmd/govulncheck@v1.7.0 ./...
fi
