![GEO-VISOR](./banner.svg)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)
[![CI](https://github.com/wheelsmif/geovisor/actions/workflows/ci.yml/badge.svg)](https://github.com/wheelsmif/geovisor/actions/workflows/ci.yml)

GEO-Visor observes browser interfaces and produces deterministic tool contracts
through a canonical, emitter-agnostic Tool Intermediate Representation (TIR).
It can launch an isolated Chromium instance or attach to an existing
agent-owned Chromium CDP session.

## Requirements

- Go 1.26.6 or newer 1.26 patch release
- Node.js 24 and npm (source builds and client tests only)
- Chromium or a compatible Chrome executable for browser inspection

## Install

Build from a source checkout:

```sh
npm ci
npm run verify:bundle
go build -trimpath -o geovisor ./cmd/geovisor
```

Or install the latest source revision with Go:

```sh
go install github.com/wheelsmif/geovisor/cmd/geovisor@latest
```

Release archives contain the same command under both `geovisor` and the shorter
`gv` filename. Put either executable on `PATH`. Chromium remains a runtime
requirement for inspection; Node.js is not required by a release binary.

## Build and test

```sh
npm ci
npm run check
go test ./...
go vet ./...
npm run verify:bundle
```

Browser integration tests skip when Chromium is unavailable. Set
`GEOVISOR_REQUIRE_BROWSER=1` to make its absence a test failure.

## Inspect a page

Launch an isolated, headless Chromium profile and emit canonical TIR to stdout:

```sh
geovisor inspect https://example.com
```

Write one selected format to a file:

```sh
geovisor inspect https://example.com --format webmcp --output tools.webmcp.js
geovisor inspect https://example.com --format mcp --output tools.mcp.json
geovisor inspect https://example.com --format openai --openai-strict \
  --output tools.openai.json
```

MCP and OpenAI are static tool definitions, so they also produce executable
browser-binding companions. With primary file output, the companion is written
beside it using its emitter-provided name. Use `--bindings-output` to choose a
different path. If the primary artifact is sent to stdout without a bindings
path, GEO-Visor warns on stderr that the bindings were not persisted.

Emit every built-in format and companion into a directory:

```sh
geovisor inspect https://example.com --format all --output ./generated
```

`--format all` stages every emitter result before replacing output files and
uses deterministic emitter names:

- `tir.json`
- `tools.webmcp.js`
- `tools.mcp.json` and `tools.mcp.bindings.json`
- `tools.openai.json` and `tools.openai.bindings.json`

## Attach to an existing browser

Attach mode accepts no positional URL and does not navigate by default:

```sh
geovisor inspect --cdp http://127.0.0.1:9222 --target active
geovisor inspect --cdp ws://127.0.0.1:9222/devtools/browser/ID \
  --target id:TARGET_ID
geovisor inspect --cdp http://127.0.0.1:9222 \
  --target url:https://example.com/exact/path
```

Navigation during attach is explicit:

```sh
geovisor inspect --cdp http://127.0.0.1:9222 --target active \
  --navigate https://example.com
```

Targets are `active`, `id:<id>`, or `url:<exact-http(s)-url>`. Attach mode
disconnects only GEO-Visor's CDP transport; it does not close the existing
browser or target.

## Safety and runtime controls

Safe exploration is opt-in and never navigates or submits:

```sh
geovisor inspect https://example.com --safe-explore \
  --depth 3 --max-operations 20 --exploration-timeout 1s
```

Useful launch and readiness controls include `--headful`, `--stealth`,
`--browser-executable`, `--timeout`, `--dom-quiet`, and
`--dom-quiet-timeout`. Stealth mode only removes common automation markers; it
does not promise to bypass bot detection or access controls. Inaccessible
frames are reported as typed diagnostics on stderr while partial artifacts
still succeed.

## Privacy and safety model

Extraction is local and deterministic: GEO-Visor does not call an LLM, vision
service, telemetry endpoint, or hosted MCP daemon. It reads labels, roles,
structure, option display text, and locator metadata. It deliberately does not
serialize current input values, password values, hidden controls, option
`value` identifiers, URL fragments, or URL query values into tool definitions.
Page URLs remain source metadata, so callers should avoid sensitive query
parameters in requested URLs and treat artifacts as potentially sensitive
site-structure data.

Safe exploration is off by default. When enabled it only opens `<details>`
elements by setting local DOM state; it does not click, submit, fetch, or
navigate. Stealth behavior is separately opt-in. Cross-origin frame gaps and
access interstitials are surfaced as diagnostics rather than hidden.

GEO-Visor-owned Chromium launches explicitly disable go-rod's leakless helper
at runtime. Some antivirus products flag that helper; its module may still
appear in `go.sum` because it is a transitive go-rod dependency, but GEO-Visor
does not execute it.

## Streams and exit status

Stdout contains only the selected primary artifact. Help, version output,
warnings, diagnostics, and errors go to stderr, including:

```sh
geovisor --help
geovisor --version
```

Exit codes are stable: `0` success, `1` generic failure, `2` command usage,
`3` browser/source, `4` compile/validation, `5` emitter, and `6` output I/O.
Ctrl-C cancellation exits with `130`.

## Format limitations

- `tir` is canonical `tir-json` and has no companion.
- WebMCP is an executable browser-local ES module for the Chrome 153 imperative
  API and has no companion. Browser page JavaScript cannot traverse
  cross-origin frames or closed Shadow DOM, even when CDP extraction observed
  those contexts.
- MCP is a static `tools/list` result, not a hosted MCP server.
- OpenAI is an unwrapped Responses API function-tool array.
- MCP and OpenAI bindings are separate GEO-Visor browser execution recipes.

## Compatibility

- Windows: amd64 and arm64 release archives; Chromium/Chrome required.
- Linux: amd64 and arm64 release archives; Chromium/Chrome required.
- macOS: amd64 and arm64 release archives; Chromium/Chrome required.
- Browser extraction uses CDP and is tested with installed Chromium-compatible
  browsers. WebMCP output specifically targets the documented Chrome 153 API.
- Source development is pinned by `.go-version` and `.node-version` to Go
  1.26.6 and Node.js 24.15.0.

## Performance and reproducibility

The post-extraction target is under 50 ms for compiler plus all emitters on the
committed 100-tool benchmark corpus. The local benchmark reports samples
instead of enforcing a flaky wall-clock unit assertion. Canonical JSON,
emitter payloads, browser bundles, and release binaries have deterministic
comparison checks. See `docs/performance.md`.

## Troubleshooting

- `Chromium executable not found`: install Chromium/Chrome or pass
  `--browser-executable` with the full path.
- `dom_not_quiet`: increase `--dom-quiet-timeout`; the partial observation is
  still explicit and deterministic for the state that was captured.
- `frame_uncovered`: the frame disappeared, blocked execution, or could not be
  attached. Review stderr and `frameCoverage.uncovered`.
- `access_interstitial`: the browser returned a challenge or blocked page.
  GEO-Visor does not bypass it; complete access manually or inspect a permitted
  page.
- WebMCP registration failure: use a compatible browser with
  `document.modelContext.registerTool`; generated code feature-detects the API.
- Antivirus leakless alert: use official project builds and verify checksums.
  GEO-Visor disables leakless at runtime as described above.

## Contributing and releases

See `CONTRIBUTING.md` for setup, formatting, tests, browser requirements, and
benchmark commands. Security reports follow `SECURITY.md`. Local release
scripts cross-compile ignored archives with checksums and both executable
names; see `docs/releasing.md`.

## License

Licensed under the Apache License, Version 2.0. See `LICENSE`.
