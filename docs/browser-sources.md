# Browser Sources

`internal/browser` provides two agent-owned observation adapters:

- Launch starts an isolated Chromium profile, navigates only to the caller's
  validated HTTP(S) URL, observes the page, and closes the browser and process
  resources it created.
- Attach connects to an existing CDP endpoint and selects a top-level target by
  exact target ID, exact URL, or the sole top-level HTTP(S) page. If more than
  one such page is open, attach fails unless the caller names the target. It
  never attaches a debugger session to another tab to probe focus. Sessions
  attach only to the selected target and its descendant frames. It navigates
  only when the caller supplies `RequestedURL`, disconnects its own CDP
  transport, and never closes or kills the existing browser or selected target.

Both adapters return one coverage-reporting observation batch in stable frame
tree order. Inaccessible frame contexts remain explicit uncovered facts rather
than disappearing from the result. Readiness probes and extraction run in an
isolated world so the page cannot observe that JavaScript. Closed shadow roots
discovered by CDP pierce are reported as batch warnings.

Access-interstitial detection is a substring hint over a bounded slice of the
root document title and body text (`verify you are human`, `checking your
browser`, `attention required`, `access denied`, `captcha`, `unusual traffic`).
It is not a classifier. A match is a non-fatal diagnostic; extraction still
runs. The phrase list is not expanded without a dedicated review.

Launch discovery uses go-rod's Chromium search, then well-known Windows paths
for Chrome, Chromium, and Edge (`Microsoft\Edge\Application\msedge.exe`) when
that search misses. `--browser-executable` / `ExecutablePath` still wins.

## Launch process policy

Every GEO-Visor-owned launch explicitly calls `Leakless(false)`. This avoids
starting go-rod's separate leakless helper executable, which endpoint security
software may quarantine. GEO-Visor instead installs deterministic cleanup as
soon as launch succeeds: it requests `Browser.close`, closes its CDP transport,
kills any remaining owned process tree, waits for launcher cleanup, and removes
the isolated profile. Deferred cleanup also runs on ordinary errors, context
cancellation, and Go panics.

The `github.com/ysmood/leakless` module can still appear as an unused transitive
module because go-rod imports it in its launcher implementation. Runtime launch
configuration prevents that helper from being invoked; dependency metadata
alone does not mean GEO-Visor executes it.

## Isolation and stealth

GEO-Visor removes go-rod's default site-isolation-disabling flags and explicitly
enables `site-per-process`, allowing Chromium to retain normal OOPIF security
boundaries. Stealth is disabled by default. Its opt-in mode only removes common
automation markers and does not promise to bypass bot detection, challenges, or
access controls.
