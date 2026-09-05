# Contributing to GEO-Visor

## Development setup

Install Go 1.26.6, Node.js 24.15.0, npm, and a Chromium-compatible browser.
Then run:

```sh
npm ci
npm run check
go test ./...
go vet ./...
```

Use `GEOVISOR_REQUIRE_BROWSER=1 go test ./...` when Chromium integration is
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

## Design constraints

- Keep browser execution agent-owned; do not add a host MCP daemon.
- Keep canonical TIR independent of browser libraries and emitter formats.
- Treat URL launch and same-session CDP attach as source adapters.
- Treat WebMCP, MCP, and OpenAI as emitters.
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

Create all six local platform archives with:

```sh
./scripts/build-release.sh v1.0.0
```

or:

```powershell
./scripts/build-release.ps1 -Version v1.0.0
```

Local release scripts cross-compile with `CGO_ENABLED=0` into ignored `dist/`
archives. Do not run or enable go-rod leakless in release commands. Only a
native binary's version output may be executed; cross-compiled binaries are
validated by successful builds and checksums.

## Issues and changes

Use the [repository issue tracker](https://github.com/wheelsmif/geovisor/issues)
for defects and proposals. Include concise reproduction steps and expected
behavior, but never post credentials, private URLs, captured sensitive values,
or security exploit details. Follow `SECURITY.md` for vulnerabilities.

Unless explicitly stated otherwise, intentionally submitted contributions are
licensed under Apache-2.0 as described by Section 5 of `LICENSE`.
