# Deterministic browser corpus

These fixtures exercise GEO-Visor without external network dependencies. Tests
serve them from loopback HTTP servers; `{{CROSS_ORIGIN}}` is replaced with a
second loopback origin so Chromium creates a real cross-origin frame/OOPIF.

- `forms.html`: required and optional text, select, checkbox, and password
  controls, duplicate labels in distinct scopes, tabs, dialog, and details.
- `spa.html`: bounded asynchronous DOM mutation followed by a stable state.
- `frames.html`, `nested-frame.html`, and `cross-frame.html`: same-origin
  nesting, open Shadow DOM, and a cross-origin frame.
- `blocked.html`: an access-interstitial marker with still-extractable content.
- `malformed.html`: intentionally malformed HTML that browsers recover.
- `inaccessible-observation.json`: deterministic compiler input for a frame
  that could not be evaluated. Browser-level inaccessibility varies by Chromium
  and platform, so the contract case is represented without pretending every
  browser can reproduce the same failure.

Fixtures contain conspicuous sentinel current values. Pipeline tests assert
that none appear in observations or emitted artifacts.
