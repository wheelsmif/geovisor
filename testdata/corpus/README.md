# Deterministic browser corpus

These fixtures exercise GEO-Visor without external network dependencies. Tests
serve them from loopback HTTP servers; `{{CROSS_ORIGIN}}` is replaced with a
second loopback origin so Chromium creates a real cross-origin frame/OOPIF.

- `forms.html`: required and optional text, select, checkbox, and password
  controls, duplicate labels in distinct scopes, tabs, dialog, and details.
- `links.html`: two named article links, citation/DOI/RFC/#cite junk, and one
  unique button. Used to assert junk actions are omitted and remaining links
  compile as one family tool. Do not fold this into `frames.html`.
- `spa.html`: bounded asynchronous DOM mutation followed by a stable state.
- `frames.html`: the two-wrapper locator fixture. Two unnamed, otherwise
  identical frames sit in sibling wrappers so only document-order frame indexes
  distinguish them.
- `nested-shadow-cross.html`, `nested-frame.html`, `cross-frame.html`, and
  `shadow-frame.html`: required Chromium coverage of a root page, an open
  shadow, nested same-origin frames, a shadow-hosted frame, and a real
  cross-origin OOPIF (`{{CROSS_ORIGIN}}`).
- `sibling-shadow.html`: two unnamed sibling hosts inside one open shadow.
- `two-submit.html` and `input-submit.html`: a form with two submit buttons, and
  a form whose submit control is `<input type="submit">`.
- `presentation-frames.html`: an `iframe role="presentation"` followed by a
  normal iframe, so both frame walks assign the same indexes.
- `blocked.html`: an access-interstitial marker with still-extractable content.
- `malformed.html`: intentionally malformed HTML that browsers recover.
- `inaccessible-observation.json`: deterministic compiler input for a frame
  that could not be evaluated. Browser-level inaccessibility varies by Chromium
  and platform, so the contract case is represented without pretending every
  browser can reproduce the same failure.

Fixtures contain conspicuous sentinel current values. Pipeline tests assert
that none appear in observations or emitted artifacts.
