![GEO-VISOR](./banner.svg)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)
[![CI](https://github.com/wheelsmif/geovisor/actions/workflows/ci.yml/badge.svg)](https://github.com/wheelsmif/geovisor/actions/workflows/ci.yml)

GEO-Visor is a compiler with one `inspect` command. It observes browser
interfaces and produces deterministic tool contracts through a canonical,
emitter-agnostic Tool Intermediate Representation (TIR). It can launch an
isolated Chromium instance or attach to an existing agent-owned Chromium CDP
session. MCP and OpenAI output are static catalogs. GEO-Visor does not host an
MCP server, and it does not ship an executor.

## Requirements

- Go 1.26.9. `.go-version` is `1.26.9`. `go.mod` declares `go 1.26` and
  `toolchain go1.26.9`.
- Node.js 24.15.0 and npm for client edits and the check scripts. `go build`
  and `go install` do not need Node. The extractor bundle
  `internal/payload/extractor.js` is committed.
- A Chromium-family browser for inspection. Discovery is described in
  `docs/browser-sources.md`. `--browser-executable` still wins.

## Install

Build from a source checkout:

```sh
go build -trimpath -o geovisor ./cmd/geovisor
```

On Windows, name the output `geovisor.exe`:

```powershell
go build -trimpath -o geovisor.exe ./cmd/geovisor
```

Or install the module with Go:

```sh
go install github.com/wheelsmif/geovisor/cmd/geovisor@latest
```

`go install` prints the module version from build info when that version is
not `(devel)` or empty. A local development build still prints `0.0.0-dev`
(`internal/version/version.go`). There are no git tags and no published GitHub
releases, and there are no downloadable release archives. Local archives come
from `scripts/build-release.sh` and `scripts/build-release.ps1` and include
both `geovisor` and `gv` (`.exe` on Windows). See `docs/releasing.md`.

PowerShell defines `gv` as `Get-Variable`, which shadows a `gv.exe` on `PATH`.
Run `geovisor` or `.\gv.exe`. Chromium remains a runtime requirement for
inspection.

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
geovisor inspect https://example.com --format mcp --output tools.mcp.json
geovisor inspect https://example.com --format openai --openai-strict \
  --output tools.openai.json
```

MCP and OpenAI are static tool definitions, so they also produce binding
recipe companions. GEO-Visor does not execute those recipes.
`client/src/apply-runtime.ts` is the in-repo test interpreter and is not
bundled into the `geovisor` binary. With primary file output, the companion is
written beside it using its emitter-provided name. Use `--bindings-output` to
choose a different path. If the primary artifact is sent to stdout without a
bindings path, GEO-Visor warns on stderr that the binding recipes were not
persisted.

Keep the catalog on stdout and persist the recipes with `--bindings-output`.
Redirect stdout when you want both artifacts on disk without `--output`:

```sh
geovisor inspect https://example.com --format mcp \
  --bindings-output tools.mcp.bindings.json > tools.mcp.json
```

Emit every built-in format and companion into a directory:

```sh
geovisor inspect https://example.com --format all --output ./generated
```

`--format all` stages every emitter result before replacing output files and
uses deterministic emitter names:

- `tir.json`
- `tools.mcp.json` and `tools.mcp.bindings.json`
- `tools.openai.json` and `tools.openai.bindings.json`

## Attach to an existing browser

Start Chrome yourself with `--remote-debugging-port` and a non-default
`--user-data-dir`. Current Chrome refuses the default profile for that port
flag.

Windows:

```powershell
& "$env:ProgramFiles\Google\Chrome\Application\chrome.exe" --remote-debugging-port=9222 --user-data-dir="$env:TEMP\geovisor-chrome-profile"
```

A 32-bit install is
`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`, which discovery
also searches.

macOS:

```sh
"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" \
  --remote-debugging-port=9222 \
  --user-data-dir="$HOME/geovisor-chrome-profile"
```

Linux:

```sh
google-chrome --remote-debugging-port=9222 \
  --user-data-dir="$HOME/geovisor-chrome-profile"
```

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

Targets are `active`, `id:<id>`, or `url:<exact-http(s)-url>`. `--target active`
selects the only top-level HTTP(S) page; if more than one is open, pass `id:`
or `url:` so attach never touches another tab. It never treats `about:blank` or
`chrome://` as that page. A fresh Chrome window that only shows a new tab
therefore has zero HTTP(S) pages, and the error says so. Attach mode
disconnects only GEO-Visor's CDP transport; it does not close the existing
browser or target.

## Safety and runtime controls

Safe exploration is opt-in and never navigates or submits:

```sh
geovisor inspect https://example.com --safe-explore \
  --depth 3 --max-operations 20 --exploration-timeout 1s
```

Useful launch and readiness controls include `--headful`, `--stealth`,
`--browser-executable`, `--timeout`, `--dom-quiet`,
`--dom-quiet-timeout`, and `--frame-timeout`. `--frame-timeout` is the
per-frame extraction deadline. When the flag is omitted, the default is the
exploration budget plus a 2 second selector allowance plus 2 seconds of frame
overhead (`DefaultSelectorAllowance` and `DefaultFrameOverhead` in
`internal/browser/validation.go`). The default `--exploration-timeout` is 1
second, so the default frame deadline is 5 seconds. An explicit
`--frame-timeout` must be positive and at least `--exploration-timeout`. A
frame that exceeds that deadline is reported as uncovered because extraction
timed out, not because the browsing context was lost. Stealth mode only removes
common automation markers; it does not promise to bypass bot detection or
access controls. Inaccessible frames are reported as typed diagnostics on
stderr and as TIR warnings while partial artifacts still succeed.

## Privacy and safety model

Extraction is local and deterministic: GEO-Visor does not call an LLM, vision
service, telemetry endpoint, or hosted MCP daemon. It reads labels, roles,
structure, option display text, and locator metadata. It deliberately does not
serialize current input values, textarea contents, password values, hidden
controls, option `value` identifiers, URL fragments, or URL query values into
tool definitions or names. Page URLs in TIR are origin+path only; query strings
and fragments are stripped from source and frame coverage metadata. Treat
artifacts as potentially sensitive site-structure data.

Safe exploration is off by default. When enabled it sets `<details open>`,
yields so reveal handlers can run, and restores each element's prior `open`
state before returning. It does not consult `safeForExploration`. `--depth 0`
and `--max-operations 0` are sent as JSON zeros and perform no exploration.
It does not click, submit, fetch, or navigate. Bindings classified `unknown`
still cannot be marked safe in the schema.
Stealth behavior is separately opt-in. Cross-origin frame gaps, closed shadow
roots, suppressed element-level extraction failures, and access interstitials
are surfaced as diagnostics or warnings rather than hidden. Page-derived tool
names and descriptions originate in page text and must be treated as untrusted
model input.

Launch starts Chromium with `os/exec` (`internal/browser/launch.go`) and reads
`DevToolsActivePort`. Attach closes GEO-Visor's CDP transport and detaches the
sessions it opened. Nothing in this module imports `rod/lib/launcher`. The
`geovisor` binary's import graph does not include `github.com/ysmood/leakless`
or `github.com/ysmood/fetchup`. `go.mod`'s require block does not list them.
`go.sum` still has their checksums because `go mod why` reaches them through
`github.com/go-rod/rod/lib/cdp`'s tests, which import
`github.com/go-rod/rod/lib/launcher`.

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
- MCP is a static `tools/list` result, not a hosted MCP server.
- OpenAI is an unwrapped Responses API function-tool array.
- MCP and OpenAI bindings are separate GEO-Visor browser execution recipes.

## Compatibility

- Local release scripts target Windows, Linux, and macOS, each for amd64 and
  arm64. A Chromium-family browser is required for inspection. Those archives
  are local builds, not published GitHub downloads.
- Browser extraction uses CDP and is tested with installed Chromium-compatible
  browsers.
- Source development is pinned by `.go-version` and `.node-version` to Go
  1.26.9 and Node.js 24.15.0.

## Performance and reproducibility

The post-extraction target is under 50 ms for compiler plus all emitters on the
committed 100-tool benchmark corpus. The local benchmark reports samples
instead of enforcing a flaky wall-clock unit assertion. Canonical JSON,
emitter payloads, browser bundles, and release binaries have deterministic
comparison checks. See `docs/performance.md`.

## Troubleshooting

- `no matching top-level HTTP(S) target`: `--target active` requires exactly one
  top-level HTTP(S) page. A fresh Chrome window with only a new tab
  (`chrome://`, `about:blank`) is ignored; open the page first or pass `id:` /
  `url:`.
- `Chromium executable not found`: install Chrome, Chromium, or Edge, or pass
  `--browser-executable` with the full path. Search order is in
  `docs/browser-sources.md`. `--browser-executable` still wins.
- `dom_not_quiet`: increase `--dom-quiet-timeout`; the partial observation is
  still explicit and deterministic for the state that was captured.
- `frame_uncovered`: the frame disappeared, blocked execution, or could not be
  attached. Review stderr and `frameCoverage.uncovered`.
- `access_interstitial`: the browser returned a challenge or blocked page.
  GEO-Visor does not bypass it; complete access manually or inspect a permitted
  page.
- The same browser diagnostics are copied into TIR `warnings`. Standard error
  still prints them. Warning codes are listed in `docs/compiler.md`.

## Contributing and releases

See `CONTRIBUTING.md` for setup, formatting, tests, browser requirements, and
benchmark commands. Security reports follow `SECURITY.md`. Local release
scripts cross-compile ignored archives with checksums and both executable
names; see `docs/releasing.md`.

See also `docs/product-spec.md`, `docs/architecture.md`,
`docs/format-tir-json.md`, `docs/format-mcp.md`, `docs/format-openai.md`,
and `docs/adr`. Browser sources and the compiler are described in
`docs/browser-sources.md` and `docs/compiler.md`.

## License

Licensed under the Apache License, Version 2.0. See `LICENSE`.
