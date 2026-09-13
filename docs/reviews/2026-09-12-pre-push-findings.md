# GEO-Visor Pre-Push Findings and Remediation Plan — 2026-09-12

Handoff document for an implementing agent. Do not treat this as history;
it is the work to do. Finding IDs (`P1`–`P23`) are stable and must not be
renumbered. Close them against the acceptance criteria, not by weakening
tests.

Status: **not started.** Reviewed against local `main` at `4e40766`, which
is 8 commits ahead of `origin/main`. Working tree was clean at review time.

The GitHub repo `github.com/wheelsmif/geovisor` is already public. These
commits must not be pushed until **P23** is green. The rest of the S1s
should land before anyone treats `main` as user-ready.

## How to use this document

1. Read `AGENTS.md`, `.cursor/rules/product-guardrails.mdc`, and
   `docs/product-spec.md`. Those win over this file if they conflict.
2. Treat **Recorded decisions** as binding. Do not reopen them unless a
   decision is physically impossible; then stop and ask.
3. Implement by phase. Each commit must pass `./scripts/check.sh` or
   `./scripts/check.ps1`. After Phase 1, that includes
   `GEOVISOR_REQUIRE_BROWSER=1` on a machine with Chromium when you touch
   `internal/browser`.
4. Docs that state a guarantee (`docs/product-spec.md`, `README.md`,
   `SECURITY.md`, `docs/compiler.md`, `docs/format-webmcp.md`,
   `testdata/corpus/README.md`) move in the same commit as the behavior.
5. Rebuild and commit both bundles (`internal/payload/extractor.js`,
   `internal/emitter/webmcp-runtime.js`) whenever `client/src` changes.
   `npm run verify:bundle` must stay green.
6. Do not consult archived or superseded implementation trees.

## Context the implementing agent needs

GEO-Visor observes a Chromium page (launch or CDP attach), extracts
interactions in an isolated world, compiles them to canonical TIR, and
emits WebMCP / MCP / OpenAI artifacts.

A 2026-09-10 review (`docs/reviews/2026-09-10-code-review.md`) and
six-phase remediation already landed. That work is **done**. This document
is a **new** pre-push review of the remediated tree. Do not re-litigate
closed `GV-NNN` items unless a finding below says a specific one is
incomplete.

Highest-leverage test in the repo: `client/test/webmcp-roundtrip.mjs` plus
`internal/integration/webmcp_roundtrip_test.go`. It extracts a fixture,
emits WebMCP, and executes the module against the same DOM. Several S1s
are invisible to it today (P15). Fix the harness when you fix the behavior
it should have caught.

## Recorded decisions

These are the premises of the plan. They resolve the product questions the
review left open.

| # | Question | Decision |
| --- | --- | --- |
| 1 | What does a form tool do? (P1, P10) | **Fill only.** A form tool must not include `click` / submission actions. Submit is a standalone action tool. `executeTool` must not submit as a side effect of filling. |
| 2 | `input type=submit\|reset\|button\|image` (P2) | **Actions, not controls.** They are never form parameters and never `fill` tools. |
| 3 | Custom ARIA widgets (P3) | **Do not advertise an apply kind the runtime cannot perform.** Native `input`/`select`/`textarea`/contenteditable keep `fill`/`select`/`check`. A non-native `role=checkbox\|switch` may be a `click` action. Non-native textbox/combobox/slider with no apply path are omitted (optional warning), not registered as tools that throw. |
| 4 | `input type=file` (P5) | **Omit** until a real file action exists. Do not emit a string `fill`. |
| 5 | Cross-origin / closed-shadow tools in WebMCP (P7) | **TIR may describe them; WebMCP must not register an executable tool it cannot resolve.** MCP/OpenAI definitions may still list them; bindings stay recipes. Failures that remain must be explicit, never a silent wrong element. |
| 6 | What is a frame for indexing? (P9) | **Both walks count `iframe`/`frame` by `localName`, not computed role.** Only light DOM and **open** shadow trees. Closed-shadow frames are warnings / uncovered, and must not consume an index the runtime cannot see. |
| 7 | Query strings in source/frame URLs (P12) | **Strip query and fragment** from `requestedUrl`, `finalUrl`, `FrameReference.Src`, and coverage URLs. Keep scheme, host, and path. Document that page URLs in TIR are origin+path only. |
| 8 | Unpinned CI Chrome (P16) | **Accepted risk, documented.** Do not add a third-party `setup-chrome` action in this pass. State the trade in `CONTRIBUTING.md` and the workflow comment so it is not rediscovered as a defect. |
| 9 | LICENSE copyright (P22) | Replace the Apache appendix placeholder with `Copyright 2026 the GEO-Visor authors` unless the owner supplies a different legal name before you commit. |
| 10 | `docs/reviews/*` | **Keep.** This file is the handoff. Do not delete the 2026-09-10 review docs in this pass. |

## Standing constraints

From `AGENTS.md`, `.cursor/rules/product-guardrails.mdc`, and
`docs/product-spec.md`:

- No host MCP daemon; browser execution stays agent-owned.
- `internal/tir` stays free of browser and emitter dependencies.
- No generated timestamps, random IDs, or unordered maps in core TIR.
- Coverage gaps are always explicit, never hidden.
- stdout carries artifacts; stderr carries diagnostics.
- Safe exploration must never navigate or submit.
- Typed errors at package boundaries.
- JSON Schema stays synchronized with Go DTOs.
- Shared extractor/runtime code stays realm-agnostic (no `instanceof`
  against DOM constructors in `client/src/shared/` or `webmcp-runtime.ts`).

## Verification gate

Every commit:

```sh
./scripts/check.sh
# Windows: ./scripts/check.ps1
```

When `internal/browser` or corpus fixtures used by browser tests change:

```sh
GEOVISOR_REQUIRE_BROWSER=1 go test ./internal/browser/... ./internal/cli/...
```

Do not land a commit that makes `TestLaunchObservesNestedShadowAndCrossOriginFrames`
fail. That is P23.

Preserve these invariants (do not regress):

- `tir.Marshal` is clone → normalize → canonicalize → validate on a copy.
- `TestPayloadCompilerEmitterPipeline` reverses input order and demands
  byte-identical output.
- `assertNoSensitiveValues` and the extractor leak test remain, and must
  grow to cover textarea `textContent` (P20).
- Bundle staleness check covers both committed JS bundles.
- Isolated-world observation for ready, quiet, and extract.
- Attach does not debugger-attach other tabs to probe focus.
- `Leakless(false)` on owned launches.
- Option `value` identifiers never appear in artifacts.

## Severity

| Level | Meaning |
| --- | --- |
| **S1** | Generated artifacts are wrong on real pages, operate the wrong element, leak page values, or break a stated safety/privacy guarantee |
| **S2** | Significant correctness, contract, or CI failure under common conditions |
| **S3** | Real defect with bounded blast radius |
| **S4** | Hygiene |

Count: 4 × S1, 10 × S2, 7 × S3, 2 × S4.

---

## Findings

### P1 — Form tools click every submit button · S1 · Executed

**Where.** `client/src/extractor.ts` (`formInteraction`);
`client/src/webmcp-runtime.ts` (`executeTool`).

**Evidence.** A Login form with Save and Save and continue emitted
`fill(user)`, `fill(note)`, `click:submission`, `click:submission`.
`executeTool` always runs actions that have no `inputParameter`, so both
submits fire after the fills. The round-trip harness `preventDefault`s
those clicks (P15), so the suite stays green.

**Impact.** Calling the composite form tool submits. A two-submit form
fires both buttons.

**Acceptance.**

- A form tool's `actions` are only the fill/select/check bindings for its
  parameters. No submission `click`.
- Standalone submit tools still exist for each submit control (after P2).
- Round-trip on a two-submit form: executing the form tool changes fields
  and does **not** click either submit. Executing each submit tool clicks
  only that button.
- `docs/compiler.md` "One tool per capability" matches this.

### P2 — Button-like inputs are classified as fillable controls · S1 · Executed

**Where.** `client/src/extractor.ts` (`isNativeControl`, `isControl`,
`isAction`, `formMembers`).

**Evidence.** `isNativeControl` matches every `input` except `type=hidden`.
The extract loop prefers `isControl` over `isAction`.

- Checkout form with `<input type="submit" value="Place order">` emitted
  parameters `email:string` and `checkoutButton:string`, plus a click.
- Standalone `<input type="reset">` became an unnamed fill tool
  (`button5:string`).
- `<button type="submit">` is handled as an action; the input equivalents
  are not.

**Impact.** Common submit markup is a value-mutating parameter. Executing
the form tool can overwrite the submit control's `value` before clicking.

**Acceptance.**

- `input type=submit|reset|button|image` are actions only.
- They are never form parameters and never `fill` tools.
- A form that uses `<input type="submit">` has a standalone submit action
  and no `checkoutButton`-style string parameter.
- Corpus or extractor tests cover `input` and `button` submit variants.

### P19 — Sibling shadow hosts record identical CSS paths · S1 · Executed

**Where.** `client/src/extractor.ts` (`simpleSelector`, `cssFallback`);
`client/src/shared/locate.ts` (`tryCSS`, `resolvePathNode`).

**Evidence.** Direct children of a `ShadowRoot` have `parentElement ===
null`, so `simpleSelector` returns the bare tag. Finder is skipped when
`getRootNode()` is not a Document (`nodeType !== 9`). Two nested
open-shadow hosts both recorded:

```
shadowPath: [{ css: "div" }, { css: "div" }]
css: "button"
```

`resolveCandidate` uses `querySelector` (first match), enters that host,
fails the semantic name inside the wrong tree, and CSS-degrades to the
first button. A Beta tool can operate Alpha with no error.

**Impact.** Silent wrong-element resolution. Same class as the old frame
locator bug: a strategy "succeeds" on the wrong node.

**Acceptance.**

- `simpleSelector` is unique among siblings **inside a ShadowRoot**, not
  only under `parentElement`.
- Finder (or an equivalent unique selector) runs against the shadow root,
  not only `Document`.
- A two-level open-shadow fixture with unnamed sibling hosts: Alpha and
  Beta tools resolve to different buttons with CSS fallbacks stripped.
- Add this fixture to the GV-036 / WebMCP round-trip harness.

### P20 — Textarea current values leak into names and descriptions · S1 · Executed

**Where.** `client/src/shared/name.ts` (`labelsText`);
`client/src/shared/dom.ts` (`referencedText`).

**Evidence.** Both use `textContent`. `input.value` is not in
`textContent`, so `client/test/extractor.test.mjs` leak test passes.
`textarea.textContent` **is** the current value.

Reproduced on the committed bundle:

| Markup | Artifact |
| --- | --- |
| `<label>Bio <textarea>TEXTAREA-SECRET</textarea></label>` | name `Bio TEXTAREA-SECRET` |
| `<textarea id="src">LABELLEDBY-SECRET</textarea>` + `aria-labelledby` | input name `LABELLEDBY-SECRET` |
| `<textarea id="hint">DESCRIBED-SECRET</textarea>` + `aria-describedby` | description contains the secret |
| `<label>Name <input value="INPUT-SECRET"></label>` | no leak (existing case) |

**Impact.** Prefill, drafts, and PII in textareas leave the page as tool
names, match keys, and descriptions. Both sides leak the same string, so
round-trip will not catch it.

**Acceptance.**

- Accessible-name and description computation never includes the current
  value of `textarea`, `input`, or `select` (ACCNAME skips embedded
  controls).
- The four cases above, plus the existing input/option sentinels, are
  asserted in extractor and pipeline leak tests.
- `SECURITY.md` / README privacy language stays true.

### P23 — Required Chromium test asserts a fixture that no longer exists · S2 · Code read

**Where.** `internal/browser/integration_test.go`
(`TestLaunchObservesNestedShadowAndCrossOriginFrames`);
`testdata/corpus/frames.html`; `testdata/corpus/README.md`.

**Evidence.** The test serves `/` as `frames.html` and demands 4 frames
plus `Root action`, `Nested frame action`,
`Recovered field Submit recovered form`, `Shadow action`, and a
cross-origin OOPIF with `{{CROSS_ORIGIN}}`. Current `frames.html` is two
unnamed srcdoc iframes with Query inputs and no token.
`nested-frame.html` / `cross-frame.html` / `shadow-frame.html` exist as
separate files; this test never navigates to them. Linux Quality and the
Browser job set `GEOVISOR_REQUIRE_BROWSER=1`.

**Impact.** Pushing the 8 commits turns public `main` red. Nested/OOPIF
coverage is no longer asserted by any passing required test.

**Acceptance.**

- `GEOVISOR_REQUIRE_BROWSER=1 go test ./internal/browser/...` passes.
- The required test still exercises: root page, open shadow, nested
  same-origin frame, and a real cross-origin OOPIF.
- `frames.html` remains the GV-003 two-wrapper locator fixture (or the
  kitchen-sink is a **new** file and the test points at it). Do not smash
  two jobs into one broken page without updating both consumers.
- `testdata/corpus/README.md` matches the files that exist.

**Close in the same change.** Failed OOPIF attach must still produce an
uncovered fact, not silently omit descendants (`attachOOPIFSessions`,
`addUnattachedOOPIF` without `ParentID`). `Target.getTargets` errors must
not be swallowed without a diagnostic. Those are the same coverage-honesty
bug as a test that no longer runs.

### P3 — Custom ARIA widgets are advertised and then fail · S2 · Executed

**Where.** `client/src/extractor.ts` (`CUSTOM_CONTROL_ROLES`,
`controlAction`); `client/src/webmcp-runtime.ts`
(`applyFill`, `applySelect`, `applyCheck`).

**Evidence.** Extraction emitted `div[role=checkbox]` as `check`,
`div[role=textbox]` as `fill`, `div[role=combobox]` as `select`,
`button[role=switch]` as `check`. Runtime: `applyCheck` requires
`localName === "input"` and type checkbox/radio; `applySelect` requires
`select`; `fill` requires a `value` property or contenteditable.
`extractor.test.mjs` asserts Dark mode is a boolean `check` and stops.
`forms.html` has no custom widgets.

**Acceptance.** Decision 3. Extractor tests that currently lock in
impossible apply kinds are rewritten to the new rule. Round-trip executes
whatever is still emitted.

### P4 — inert, disabled, and readonly controls still become tools · S2 · Executed

**Where.** `client/src/extractor.ts` (`hidesSubtree`, `isHiddenLocally`).

**Evidence.** Ancestor `display:none`, `visibility`, `hidden`, and
`aria-hidden` are handled. `inert`, `[disabled]`, `fieldset[disabled]`,
and `readonly` are not. A fixture with those four plus one live field
emitted all five as fill tools. `dialog.showModal()` uses `inert` on the
backdrop.

**Acceptance.**

- Descendants of `inert` are omitted.
- `disabled` controls and descendants of `fieldset[disabled]` are omitted.
- `readonly` / `aria-readonly="true"` are not emitted as fill/select/check
  tools.
- Existing hide tests still pass, including `visibility:visible` override.

### P5 — type=file is emitted as a string textbox · S2 · Executed

**Where.** `client/src/shared/role.ts`; `webmcp-runtime.ts` `applyFill`.

**Evidence.** `input type=file` mapped to role `textbox`, parameter
`resume:string`, action `fill`. Browsers reject assigning `.value`.

**Acceptance.** Decision 4. No file input becomes a string fill tool.
A fixture asserts it is absent from interactions.

### P6 — Fills will not update most framework-controlled inputs · S2 · Code read

**Where.** `client/src/webmcp-runtime.ts` (`applyFill`, `applyCheck`).

**Evidence.** Assigns the value/`checked` property and dispatches
`window.Event("input")` and `Event("change")`, not `InputEvent`, and not
the native `HTMLInputElement` value setter.

**Acceptance.**

- Fill and check use the native prototype setter when present, then
  dispatch a bubbling `InputEvent` (and `change`).
- Document any remaining framework limitation in `docs/format-webmcp.md`
  if a native setter is still not enough for a specific library. Do not
  add a React runtime dependency.
- jsdom round-trip still passes; add a unit test that the dispatched event
  is an `InputEvent` and that the native setter was used when available.

### P7 — CDP can observe frames page JS can never act on · S2 · Code read

**Where.** `internal/browser/frames.go` (`extractFrame`);
`client/src/shared/locate.ts` (`resolveCandidate`);
`internal/emitter/webmcp.go`.

**Evidence.** CDP extracts OOPIFs into TIR. WebMCP registers executable
tools. `contentDocument` / closed `shadowRoot` throw at execute. The
limitation is documented; the tools are still callable.

**Acceptance.** Decision 5. WebMCP emit skips (or refuses to register)
actions whose locator cannot be resolved in page JS: cross-origin frame
path or closed-shadow-only addressing. TIR coverage for those frames
stays explicit. `docs/format-webmcp.md` states the filter.

### P8 — Nested OOPIFs are discovered from a single target snapshot · S2 · Code read

**Where.** `internal/browser/frames.go` (`attachOOPIFSessions`).

**Evidence.** The comment says nested OOPIFs may appear only after a
parent is attached, and the loop repeats until a pass attaches nothing
new. `Target.getTargets` is called **once** before the loop. New targets
never enter the candidate list. Coverage can look complete while sessions
are missing.

**Acceptance.**

- Each loop iteration re-fetches `Target.getTargets` (or otherwise sees
  targets created after a parent attach).
- `Target.getTargets` failure is a diagnostic, not a silent empty list.
- A nested-OOPIF fixture or a unit-level test of the refetch contract
  exists. If a live nested OOPIF is too flaky for CI, the refetch is still
  tested with a fake target list that grows after the first attach.

### P9 — Producer and consumer count frames with different predicates · S2 · Code read

**Where.** `internal/browser/frames.go` (`collectFrameOwnerOrder`);
`client/src/shared/locate.ts` (`frameCandidates`);
`client/src/shared/role.ts` (`semanticRole`).

**Evidence.** Go counts `localName` `iframe|frame` with a CDP `FrameID`,
including pierced **closed** shadows. The runtime counts
`matchRole === "iframe"` and only sees `element.shadowRoot` (open).
`semanticRole` returns an explicit role before the iframe tag mapping, so
`role="presentation"` / `none` / `application` iframes are not frame
candidates on the consumer.

**Impact.** A closed-shadow iframe or a role-overridden sibling shifts
every later `FrameReference.Index`. Tools resolve to the wrong frame or
to none.

**Acceptance.** Decision 6. A fixture with (a) `iframe role=presentation`
and (b) a later normal iframe: both sides assign the same indexes.
Closed-shadow-hosted frames are warned and do not steal an index from
later open-tree frames. Comments that claim the walks are identical must
be true.

### P10 — Missing form parameters are skipped; submit still runs · S2 · Code read

**Where.** `client/src/webmcp-runtime.ts` (`executeTool`).

**Evidence.** If `inputParameter` is absent from the argument object, that
action is skipped. Click actions have no `inputParameter`, so they always
run. Schema `required[]` is not enforced in `execute`.

**Acceptance.** After decision 1, form tools have no submit click, so this
specific foot-gun dies. Still: `executeTool` must not treat a missing
required parameter as success-plus-side-effect. If a required parameter
is omitted, throw before applying later actions. Optional omitted
parameters stay skipped.

### P21 — Validate and the JSON Schema disagree on nth · S2 · Code read

**Where.** `internal/tir/validation.go`; `schemas/tir.schema.json`
(`minimum: 0` on `nth`); `internal/compiler/compiler.go` (`normalizeNth`).

**Evidence.** `Validate` never reads `Nth`. `Marshal` can emit `nth: -1`.
The schema rejects it. The compiler rewrites negatives to `0` before
identity keys, so `nth=-1` and `nth=0` can merge. The schema-sync test
compares `schemaVersion` only.

**Acceptance.**

- `tir.Validate` rejects `nth < 0` on semantic locators and path nodes.
- Compiler rejects negative observation `nth` at the input boundary
  (`compiler.Error` with an input field path). Do not silently coerce.
- A table-driven test: the same negative-`nth` document fails Validate
  and fails schema validation.
- `TestValidDocumentEmitsOnEveryFormat` still holds.

### P11 — Attach trusts webSocketDebuggerUrl from the version JSON · S3 · Code read

**Where.** `internal/browser/connection.go` (`resolveControlURL`,
`newEndpointClient`); `internal/browser/validation.go`
(`validateEndpoint`).

**Evidence.** After a validated HTTP(S) fetch, any `ws`/`wss` URL with a
host is accepted. The unit test requires a **foreign** port. Redirects
are blocked, but `CheckRedirect`'s response body is not closed.
`Transport` is nil, so `ProxyFromEnvironment` applies. `--cdp` allows
URL userinfo; launch/navigate/`--target url:` reject it.

**Acceptance.**

- Rewrite or reject `webSocketDebuggerUrl` unless host (and, for
  loopback, port) matches the validated endpoint. Chrome advertising
  `ws://127.0.0.1:9222` for a remote `--cdp http://192.168.x.x:9222`
  must not silently attach to this machine's 9222.
- CDP HTTP client: no environment proxy (`Proxy: nil`), still no
  redirects, always close the response body on redirect error.
- `--cdp` rejects URL credentials, same as other URL policy.
- Tests lock the pin and the redirect-body close.

**Close in the same change if cheap.** `sanitizeURL` currently leaves the
DevTools path token (`/devtools/browser/<uuid>`) in stderr. Redact that
path or treat the whole control URL as a secret consistently.

### P12 — Source and frame URLs keep query strings · S3 · Code read

**Where.** `internal/compiler/compiler.go` (copies `RequestedURL` /
`FinalURL` via `cleanText` only);
`internal/browser/frames.go` (`FrameReference.Src = child.URL`).

**Evidence.** README says query values are not serialized into tool
definitions and that page URLs remain source metadata. Tokens in iframe
`src` still land in TIR.

**Acceptance.** Decision 7. Pipeline sentinels
`CURRENT-URL-SECRET` / `CURRENT-FRAGMENT-SECRET` fail if they appear in
any emitted artifact, including `source` and `frameCoverage`.

### P13 — Duplicate-name ordinals still churn tool IDs · S3 · Code read

**Where.** `client/src/extractor.ts` (`disambiguateInteractions`);
`internal/compiler/compiler.go` (`prepareInteraction` identity uses
`source.Name`).

**Evidence.** Positional fallback names were removed from identity.
Disambiguated display names (`Query (2)`) still are identity. Inserting
another same-label control renumbers later IDs.

**Acceptance.** Display-name suffixes are not part of compiler identity.
Identity uses the pre-disambiguation name plus locator `nth` / paths
(already present). A test inserts a third identical label earlier and
asserts the original two tools keep their IDs.

### P14 — `--target active` requires a single HTTP(S) page · S3 · Code read

**Where.** `internal/browser/target.go` (`canonicalTopLevelTargets`,
`selectTarget`).

**Evidence.** Non-HTTP(S) tabs are filtered, then zero HTTP(S) pages
yields `no matching top-level HTTP(S) target` with no mention that
`chrome://` / `about:blank` were ignored.

**Acceptance.** The safety rule stays (never pick `about:blank` as
"active"). The error names how many HTTP(S) pages were found and that
non-HTTP(S) tabs were ignored. `README.md` troubleshooting mentions a
fresh Chrome window with only a new tab.

### P15 — The WebMCP harness cannot see form submission · S3 · Code read

**Where.** `client/test/webmcp-roundtrip.mjs` (`executeModule`).

**Evidence.** Click listeners `preventDefault` to avoid jsdom navigation,
then treat a form tool that clicked Save as success. That hid P1 and P10.

**Acceptance.**

- The driver still cancels navigation (jsdom cannot complete it) but
  **records** which submit/button was clicked.
- Assertions distinguish: form-tool execute must not click submit (P1);
  submit-tool execute must click exactly one submit.
- `forms.html` plus a two-submit fixture and an `input type=submit`
  fixture are covered.

### P16 — Browser jobs use whatever Chrome the runner image ships · S3 · Code read

**Where.** `.github/workflows/ci.yml` (Quality Ubuntu + Browser job).

**Acceptance.** Decision 8. Workflow comment and `CONTRIBUTING.md` state
that Chrome is the image binary on purpose (no new supply-chain pin).
`google-chrome --version` remains a required step so a missing browser
fails loudly.

### P17 — `exploration.focused` is never populated · S4 · Code read

**Where.** `client/src/extractor.ts` (`exploreSafely`,
`explorationEvidence`).

**Acceptance.** Delete the dead `focused` set and the
`exploration:focused-without-input` branch, unless you implement real
focus exploration in this pass (out of scope; do not add a new feature).

### P18 — Access-interstitial detection is a substring list · S4 · Code read

**Where.** `internal/browser/frames.go` (`accessInterstitial`).

**Acceptance.** Keep the heuristic. Document in `docs/browser-sources.md`
that it is a substring hint, not a classifier. Do not expand the phrase
list in this pass.

### P22 — LICENSE still has the Apache copyright placeholder · S3 · Code read

**Where.** `LICENSE` appendix; `scripts/build-release.sh` copies LICENSE
into archives.

**Acceptance.** Decision 9. The placeholder line is gone. Release scripts
still copy LICENSE.

---

## Related residue (do not open new IDs)

Fold these into the finding they belong to. They are not extra scope.

| Residue | Fold into |
| --- | --- |
| `waitForDocumentReady` can accept launch `about:blank` as `complete` before `Page.navigate` commits (no `loaderId`) | P23 / browser readiness while you are in `frames.go`. Wait for the navigate loader or a lifecycle event, not the first `complete`. |
| Exclusive `<details name>` accordion: restore does not reopen the sibling that was closed | Only if you touch `exploreSafely`. Snapshot/restore every `details.open` you observed, not only ones you opened. jsdom 30 may not implement `details.name`; test in comments or a browser test. |
| `form="id"` on a contenteditable `div` is treated as form ownership; a test locks that in | P2 / form membership. Ownership follows HTML form-associated elements, not a raw `form` attribute on arbitrary elements. |
| MCP/WebMCP `ReadOnlyHint` is true when every action is `SideEffectNone`, including a click | If you touch annotation helpers: a click/fill/check/select is never advertised read-only. |
| Compiler cancel becomes `compiler.Error` without wrapping `context.Canceled`, so Ctrl+C during compile exits 4 not 130 | If you touch `compiler.go` context handling: wrap `ctx.Err()` so `ExitCode` can return 130. |
| `WriteFiles` can commit after the last replace with no cancel check; rollback errors swallowed | If you touch `internal/output`: check `ctx` before `committed = true`; surface rollback failure. |

Do not expand this list into a second review. If a residue is not next to
code you already have open, leave it.

---

## Phased plan

Ordering is dependency and CI, not just severity.

### Phase 1 — Unblock the gate (P23, P16)

Nothing else can be trusted in CI until the Browser job is honest.

1. Retarget or restore the kitchen-sink coverage so
   `TestLaunchObservesNestedShadowAndCrossOriginFrames` passes and still
   covers shadow + nested + OOPIF.
2. Update `testdata/corpus/README.md`.
3. Document unpinned Chrome (P16).
4. While in `attachOOPIFSessions`, refetch targets (P8) and stop
   swallowing `Target.getTargets` errors.

**Exit.** `GEOVISOR_REQUIRE_BROWSER=1 go test ./internal/browser/...`
passes locally. P23, P8, P16 closed.

### Phase 2 — Make generated tools honest (P19, P20, P2, P1, P10, P15)

This is the product-facing phase.

1. Unique shadow-path CSS (P19) and a round-trip fixture that would have
   resolved Beta to Alpha.
2. ACCNAME-safe name/description (P20) and leak-test sentinels.
3. Button-like inputs are actions (P2).
4. Form tool is fill-only (P1); required-parameter execute (P10).
5. Harness records submit clicks and asserts the new spec (P15).

**Exit.** Round-trip fails if a form tool submits or if two shadow tools
share an element. `assertNoSensitiveValues` catches textarea leaks.
`docs/compiler.md` matches decision 1.

### Phase 3 — Stop advertising impossible apply (P3, P4, P5, P6)

1. Apply-kind filter (P3).
2. inert / disabled / readonly (P4).
3. Omit `type=file` (P5).
4. Native setter + `InputEvent` (P6).

**Exit.** Extractor fixtures for custom ARIA, file, inert, disabled,
readonly. Runtime tests for InputEvent / native setter. No tool remains
whose apply path is a guaranteed throw on the originating element.

### Phase 4 — Frame contract (P9, P7)

1. Unify frame predicates and closed-shadow indexing (P9).
2. WebMCP skips unexecutable cross-origin / closed-shadow tools (P7).

**Exit.** Presentation-role iframe fixture agrees on both walks.
WebMCP emit test: a TIR tool whose only locators are cross-origin is not
registered. TIR still reports the frame.

### Phase 5 — Contract and identity (P21, P13, P12)

1. `nth` in Validate + compiler input + schema-sync test (P21).
2. Display-name suffixes out of identity (P13).
3. Strip query/fragment from TIR URLs (P12).

**Exit.** Negative `nth` fails Validate and schema. ID-stability test for
duplicate names. URL sentinels absent from `source` and `frameCoverage`.

### Phase 6 — Attach hardening and hygiene (P11, P14, P17, P18, P22)

1. Pin/rewrite debugger WebSocket; no env proxy; close redirect bodies;
   reject `--cdp` userinfo; redact DevTools path if still leaked (P11).
2. Clearer `--target active` error (P14).
3. Delete dead `exploration.focused` (P17).
4. Document interstitial heuristic (P18).
5. Fill LICENSE copyright (P22).

**Exit.** Connection tests cover pin, proxy-off, redirect-body, and
userinfo. LICENSE placeholder gone.

---

## Suggested commit shape

Prefer one commit per phase, or split Phase 2 if the diff is large
(P19+P20, then P1+P2+P10+P15). Do not mix Phase 1 CI fixture work with
extractor behavior.

Do not create a git commit unless the user asked for one.

## What not to do

- Do not add a host MCP daemon, Playwright locators, timestamps, or maps
  in core TIR.
- Do not "fix" P1 by making the harness ignore extra clicks.
- Do not emit option `value` identifiers to "fix" selects (already
  closed; labels + `selectedIndex`).
- Do not reintroduce `instanceof HTML*` in shared runtime code.
- Do not force-push `main`. History is already public.
- Do not treat `docs/reviews/2026-09-10-*` as the spec for this work.

## Walk order if you get stuck

P23 is the only finding that fails CI by itself. After that: P19 (wrong
element), P20 (value leak), P2+P1 (forms). Everything else is secondary
once those four are closed and the harness can see them.
