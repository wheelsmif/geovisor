# Browser Sources

`internal/browser` provides two agent-owned observation adapters:

- Launch starts an isolated Chromium profile, navigates only to the caller's
  validated HTTP(S) URL, observes the page, and closes the browser and process
  resources it created.
- Attach connects to an existing CDP endpoint and selects a top-level target by
  focused fallback, exact target ID, or exact URL. It navigates only when the
  caller supplies `RequestedURL`, disconnects its own CDP transport, and never
  closes or kills the existing browser or selected target.

Both adapters return one coverage-reporting observation batch in stable frame
tree order. Inaccessible frame contexts remain explicit uncovered facts rather
than disappearing from the result.

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
