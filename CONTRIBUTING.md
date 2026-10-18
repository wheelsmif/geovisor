# Contributing to GEO-Visor

## Development setup

Install Go 1.26.9 (`.go-version`; `go.mod` has `toolchain go1.26.9`), Node.js
24.15.0, npm, and a Chromium-compatible browser. `go build` and `go install`
do not need Node; the extractor bundle is committed. Node is required for
client edits and for the check scripts below.
Then run:

```sh
npm ci
npm run check
go test ./...
go vet ./...
```

`./scripts/check.sh` and `./scripts/check.ps1` set `GEOVISOR_REQUIRE_NODE=1`
after `npm` succeeds so the apply round-trip harness cannot skip. Both run
`go tool -modfile=tools/go.mod staticcheck ./...` on every invocation, and
`go tool -modfile=tools/go.mod govulncheck ./...` when `--security` or
`-Security` is set. Use
`GEOVISOR_REQUIRE_BROWSER=1 go test ./...` when Chromium integration is
required. On PowerShell:

```powershell
$env:GEOVISOR_REQUIRE_BROWSER = "1"
go test ./...
```

Before submitting a change, also run `gofmt -w` on changed Go files and verify
that `gofmt -l .` prints nothing. `npm run build` regenerates the committed
`internal/payload/extractor.js`; commit it whenever the TypeScript extractor
changes. Do not commit `node_modules`, `dist`, local profiles, or generated
inspection output.

`UPDATE_GOLDEN=1` refreshes compiler goldens (`internal/compiler`) and emitter
goldens (`internal/emitter`). Use it only when the contract change is
intentional.

## Design constraints

- Keep browser execution agent-owned; do not add a host MCP daemon.
- Keep canonical TIR independent of browser libraries and emitter formats.
- Treat URL launch and same-session CDP attach as source adapters.
- Treat MCP and OpenAI as emitters.
- Keep safe exploration non-navigating and non-submitting.
- Make stealth opt-in and partial frame coverage explicit.
- Do not add timestamps, random IDs, maps, or unstable ordering to core TIR.
- Reserve stdout for artifacts and stderr for diagnostics.
- Keep JSON Schema files synchronized with Go DTOs.

Add focused tests for contract and safety behavior. Browser fixtures belong in
`testdata/corpus`; they must be deterministic and self-contained.

## Performance and release checks

Run the post-extraction benchmark without enforcing a local timing threshold:

```sh
go test ./internal/integration -run '^$' -bench '^BenchmarkCorpusCompileAndEmit$' -benchmem -count=3
```

Verify deterministic local binaries and injected versions with:

```sh
./scripts/verify-release.sh
```

or:

```powershell
./scripts/verify-release.ps1
```

Run all local checks with `./scripts/check.sh` or
`./scripts/check.ps1`. Add `--require-browser --security` on POSIX, or
`-RequireBrowser -Security` on PowerShell, for Chromium-required tests and
dependency scanning.

CI does not run that exact script. The Quality matrix runs formatters, `go vet`,
`go tool -modfile=tools/go.mod staticcheck ./...`, `npm` checks, and
`go test ./...` (with Node required; Chromium required only on the Ubuntu
Quality leg). The Linux race and security job runs
`go tool -modfile=tools/go.mod govulncheck ./...` and
`npm audit --audit-level=high`. `verify-release` and the six-archive
checksums run in the Source cross-build job. Chromium-backed packages also run
in the dedicated Browser job, which asserts `google-chrome --version` first.

Browser CI uses the Chrome binary that ships on the GitHub-hosted runner image
on purpose. Do not add a third-party `setup-chrome` action: pinning a downloaded
browser would add supply-chain surface the project is not taking.
The `google-chrome --version` step is required so a missing image browser fails
loudly instead of skipping. Chrome version drift across runner images is an
accepted risk; record a fixture or protocol break against a specific version
when one appears.

Create all six local platform archives with:

```sh
./scripts/build-release.sh v1.0.0
```

or:

```powershell
./scripts/build-release.ps1 -Version v1.0.0
```

Local release scripts cross-compile with `CGO_ENABLED=0` into ignored `dist/`
archives. They compile from a `git archive` of `HEAD` and do not start
Chromium. Only a native binary's version output may be executed;
cross-compiled binaries are validated by successful builds and checksums.
See `docs/releasing.md`.

## Issues and changes

Use the [repository issue tracker](https://github.com/wheelsmif/geovisor/issues)
for defects and proposals. Include concise reproduction steps and expected
behavior, but never post credentials, private URLs, captured sensitive values,
or security exploit details. Follow `SECURITY.md` for vulnerabilities.

Unless explicitly stated otherwise, intentionally submitted contributions are
licensed under Apache-2.0 as described by Section 5 of `LICENSE`.
