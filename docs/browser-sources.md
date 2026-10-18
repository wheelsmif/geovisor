# Browser Sources

`internal/browser` provides two agent-owned observation adapters. GEO-Visor is
a compiler: these adapters observe a page and return observations. They do not
execute tools.

- Launch starts an isolated Chromium profile with `os/exec`
  (`internal/browser/launch.go`), navigates only to the caller's validated
  HTTP(S) URL, observes the page, and closes the browser and process resources
  it created. The process is Chromium itself. After start, launch reads
  `DevToolsActivePort` from the profile directory and connects to that
  WebSocket.
- Attach connects to an existing CDP endpoint and selects a top-level target by
  exact target ID, exact URL, or the sole top-level HTTP(S) page. If more than
  one such page is open, attach fails unless the caller names the target. It
  never attaches a debugger session to another tab to probe focus. Sessions
  attach only to the selected target and its descendant frames. It navigates
  only when the caller supplies `RequestedURL`. When observation finishes,
  attach closes its CDP transport and detaches the sessions it opened. It does
  not close or kill the existing browser or selected target.

Both adapters return one coverage-reporting observation batch in stable frame
tree order. Inaccessible frame contexts remain explicit uncovered facts rather
than disappearing from the result. Readiness probes and extraction run in an
isolated world so the page cannot observe that JavaScript. Closed shadow roots
discovered by CDP pierce are reported as batch warnings
(`closed_shadow_root`, message `closed shadow root on ` plus a host label).

Access-interstitial detection is a substring hint over a bounded slice of the
root document title and body text (`verify you are human`, `checking your
browser`, `attention required`, `access denied`, `captcha`, `unusual traffic`).
It is not a classifier. A match is a non-fatal diagnostic; extraction still
runs. The phrase list is not expanded without a dedicated review.

## Chrome discovery

`LookPath` in `internal/browser/lookpath.go` searches a per-OS list copied from
go-rod's `launcher.LookPath`, then an extra Windows path list. This module does
not import `rod/lib/launcher`. A path that contains a slash or is absolute is
accepted when `os.Stat` finds a file. A bare name is resolved with
`exec.LookPath`. `--browser-executable` / `ExecutablePath` still wins and skips
discovery.

macOS (`darwin`), in order:

- `/Applications/Google Chrome.app/Contents/MacOS/Google Chrome`
- `/Applications/Chromium.app/Contents/MacOS/Chromium`
- `/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge`
- `/Applications/Google Chrome Canary.app/Contents/MacOS/Google Chrome Canary`
- `/usr/bin/google-chrome-stable`
- `/usr/bin/google-chrome`
- `/usr/bin/chromium`
- `/usr/bin/chromium-browser`

Linux, in order: `chrome`, `google-chrome`, `/usr/bin/google-chrome`,
`microsoft-edge`, `/usr/bin/microsoft-edge`, `chromium`, `chromium-browser`,
`/usr/bin/google-chrome-stable`, `/usr/bin/chromium`,
`/usr/bin/chromium-browser`, `/snap/bin/chromium`, and
`/data/data/com.termux/files/usr/bin/chromium-browser`.

Windows, in order: `chrome` and `edge` on `PATH`, then
`Google\Chrome\Application\chrome.exe`, `Chromium\Application\chrome.exe`, and
`Microsoft\Edge\Application\msedge.exe` under `ProgramFiles`,
`ProgramFiles(x86)`, and `LocalAppData`, skipping an empty environment
variable.

After that list, every OS also walks `extraChromiumPaths`. For each root, in
order, it joins each relative, and it skips a path already queued. The roots
are `ProgramFiles`, `ProgramFiles(x86)`, `LocalAppData`, the literals
`C:\Program Files (x86)` and `C:\Program Files`, and, when `ProgramFiles` is
set, that value with ` (x86)` appended. The relatives are
`Google\Chrome\Application\chrome.exe`, `Chromium\Application\chrome.exe`, and
`Microsoft\Edge\Application\msedge.exe`. Those literal Program Files (x86)
paths are how a 32-bit Chrome install is found when `ProgramFiles(x86)` is
absent from the process environment.

OpenBSD searches `chrome` and `chromium` on `PATH` before the same extra
Windows paths.

## Launch process

The owned launcher builds a Chromium argument list and starts it with
`os/exec`. Flags include `--remote-debugging-port=0`, `--user-data-dir` set to
a temporary profile, `--site-per-process`, and `--disable-features=TranslateUI`.
Headless mode adds `--headless`. Stealth, when requested, passes
`--disable-blink-features=AutomationControlled` and does not pass
`--enable-automation`. Stealth removes common automation markers. It does not
promise to bypass bot detection, challenges, or access controls.

Nothing in this module imports `rod/lib/launcher`. The `geovisor` binary's
import graph does not include `github.com/ysmood/leakless` or
`github.com/ysmood/fetchup`. `go.mod`'s require block does not list them.
`go.sum` still has their checksums because `go mod why` reaches them through
`github.com/go-rod/rod/lib/cdp`'s tests, which import
`github.com/go-rod/rod/lib/launcher`.

After a successful launch, cleanup requests `Browser.close`, closes the CDP
transport, kills any remaining owned process tree, and removes the isolated
profile. Deferred cleanup also runs on ordinary errors, context cancellation,
and Go panics.

## Diagnostics and TIR warnings

Browser diagnostics are non-fatal. Standard error still receives every one.
`appendDiagnosticWarnings` also copies some of them onto the observation batch
so the compiler emits them as TIR `warnings`.

Diagnostic codes in `internal/browser/types.go`:

- `dom_not_quiet`: `DOM continued changing until the bounded quiet-check deadline`.
  Copied into TIR warnings.
- `access_interstitial`: `page content resembles a bot challenge or access interstitial`.
  Copied into TIR warnings.
- `frame_uncovered`: copied into TIR warnings only when the message starts with
  `browser could not list targets`. The target-list failure message is
  `browser could not list targets for out-of-process frames: ` plus the
  underlying error. Other `frame_uncovered` diagnostics stay on stderr and
  become uncovered frames. The compiler then emits its own `frame_uncovered`
  warning, `frame coverage is incomplete`, for each inaccessible frame.

`frame_uncovered` messages produced in `internal/browser/frames.go` are:

- `browser has no session for this frame`
- `browser could not create an isolated execution world for this frame`
- `browser payload installation failed in this frame`
- `browser payload extraction function is missing in this frame`
- `extraction exceeded the frame timeout`
- `browser lost the frame context during extraction`
- `browser payload raised a JavaScript exception in this frame`
- `browser payload returned malformed observation JSON in this frame`
- `frame is not visible to page JavaScript (closed shadow or equivalent)`
- `interaction limit reached before every control was recorded`
- a reason already stored on the discovered frame

The interaction cap is 2000 controls per frame (`MaxInteractionsPerFrame`).
Crossing it is partial coverage. The extractor warning code is
`interaction_limit`, message `stopped after 2000 interactions in this frame`.
That warning is copied into TIR. The full warning catalog is in
`docs/compiler.md`.
