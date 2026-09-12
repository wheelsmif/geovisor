# GEO-Visor Remediation Plan — 2026-09-10 Code Review

Plan of record for the 48 findings in
[`2026-09-10-code-review.md`](2026-09-10-code-review.md), plus findings added
during remediation. Finding IDs are stable and are never renumbered; this
document tracks each `GV-NNN` to a phase, a task, and an exit condition.

Status: **Phases 1 and 2 complete.** Every finding was re-confirmed against the
working tree before this plan was written.

## Findings added during remediation

The review's IDs are stable, so work discovered afterward continues the
numbering.

### GV-049 — The runtime's role table is a subset of the extractor's · S2

**Where.** `client/src/extractor.ts:120-157` (`semanticRole`) versus
`internal/emitter/webmcp.go:127-148` (`__geovisorRole`).

**Evidence.** Found by the GV-036 harness on its first run. The extractor maps
`<nav>` to `navigation`, `<summary>` to `button`, `<dialog>` to `dialog`,
`<details>` and `<fieldset>` to `group`, and `h1`-`h6` to `heading`. The runtime
knows none of them and returns `""`. Executing the emitted module with the CSS
fallback removed:

```
tool "advanced-options": semantic: no element with role "group" and name "Help"
tool "reset-local-options": semantic: no element with role "group" and name "Help"
```

**Impact.** Any locator whose role or semantic scope uses an extractor-only role
is permanently unresolvable and degrades silently to CSS. This is the same
divergence class as GV-001, GV-003, and GV-004, and it is direct evidence for
the decision to share one implementation rather than pin two.

**Acceptance criteria.** Producer and consumer resolve roles through one
implementation, so a role the extractor can emit is always a role the runtime
can compute.

## Recorded decisions

The review left six resolutions open. They are decided as follows and are the
premises of everything below.

| # | Question | Decision |
| --- | --- | --- |
| 1 | Extractor/runtime divergence (cross-cutting root cause) | **Share one implementation.** Role resolution, accessible-name computation, and element addressing move into `client/src/shared/`, and the WebMCP runtime becomes a second committed bundle built from that source. |
| 2 | GV-007 duplicate form controls | **Drop standalone control tools** for controls owned by a form. The composite form tool is the single way to fill a form. |
| 3 | GV-008 / GV-009 safe exploration | **Make it real and restore state.** Snapshot and restore the `open` attribute, yield properly to the event loop, and enforce both budgets. |
| 4 | GV-014 emitter strictness | **Hoist into TIR.** `tir.Validate` and `schemas/tir.schema.json` own the shape invariant; every valid document emits on every format. |
| 5 | Delivery | Six phases, separate commits, tracked against this document. |
| 6 | GV-046 root `GEOVisor Plan` | **Delete it.** The ADRs and `docs/` are the authority. |

## Standing constraints

Every phase is subject to these, from `AGENTS.md`,
`.cursor/rules/product-guardrails.mdc`, and `docs/product-spec.md`:

- No host MCP daemon; browser execution stays agent-owned.
- `internal/tir` stays free of browser and emitter dependencies.
- No generated timestamps, random IDs, or unordered maps in core TIR.
- Coverage gaps are always explicit, never hidden.
- stdout carries artifacts; stderr carries diagnostics.
- Typed errors at package boundaries.
- The JSON Schema stays synchronized with the Go DTOs.

## Verification gate

Every commit must pass `./scripts/check.sh` (or `check.ps1`). Once Phase 1
lands, that gate additionally includes the GV-036 round-trip harness, the new
linter, and a browser-required test run. Phases 2 and 3 change documented
guarantees, so `docs/product-spec.md`, `README.md`, and `SECURITY.md` move in
the same commit as the behavior they describe — never after.

The "Confirmed sound — do not regress" list at the end of the review is treated
as a set of invariants. In particular: the clone → normalize → canonicalize →
validate → marshal ordering in `internal/tir/canonical.go`, the reversed-input
byte-identical determinism test, and the reproducible bundle staleness check.

---

## Phase 1 — Make the gate trustworthy

Nothing else can be verified until the feedback loop exists. This phase adds no
product behavior; it only makes failure visible.

**Deviation from the review's sequencing:** GV-047 and GV-048 are pulled forward
from Phase 6, because they are defects in the gate itself and this is the phase
that establishes the gate.

### 1.1 GV-036 — Execute the generated WebMCP runtime (S1)

The highest-leverage item in the review. Today the emitted module is only
`node --check`-ed for syntax and substring-searched; GV-001, GV-003, and GV-004
all live in code no test has ever run.

Build a round-trip harness driven from `internal/integration`:

1. A Node driver loads a corpus HTML file into `jsdom`, evaluates the committed
   `internal/payload/extractor.js`, and prints the observation batch as JSON.
2. The Go test compiles that batch and emits the WebMCP module.
3. The driver re-loads the *same* HTML, stubs
   `document.modelContext.registerTool`, imports the emitted module, invokes
   every registered tool, and prints per-tool resolution and application results.
4. The Go test asserts every action resolves to the correct element and applies.

`jsdom` 30.0.1 is already a dev dependency and `internal/integration/pipeline_test.go`
already shells out to `node`, so this needs no new dependencies or patterns.

**As built.** `client/test/webmcp-roundtrip.mjs` plus
`internal/integration/webmcp_roundtrip_test.go`. Two details differ from the
sketch above and are worth recording:

- The emitted artifact is an ES module, which `jsdom` cannot execute. The driver
  therefore builds the DOM with `jsdom`, points the Node global `document` at
  it, and imports the module in Node. The module only needs `document`,
  `element.ownerDocument.defaultView.Event`, and `DOMException`, all of which
  resolve correctly that way.
- Resolution identity comes from events, not state diffs. The runtime dispatches
  `click` for click actions and `input`/`change` for the rest, so capturing
  listeners identify the element each action resolved to even when applying the
  value changed nothing — setting an already-checked checkbox, for example.

`TestWebMCPRuntimeSemanticStrategyResolvesWithoutCSSFallback` deserves its own
note. GV-003, GV-004, and GV-049 all fail by *silently degrading* to the CSS
fallback, so a tool can pass while its semantic locator is broken. The test
strips every CSS fallback that a semantic strategy can replace, leaving the
semantic half as the only strategy, which makes the degradation observable
using production code paths alone — no test hooks in generated artifacts.

**Known-failure gating.** The three harness tests assert correct behavior the
pipeline does not yet produce, so each calls `knownFailure(t, …)`, which skips
with a greppable message naming the finding. The assertions are the
specification; Phase 2 closes each one by deleting the `knownFailure` call, not
by weakening the assertion. This keeps every commit green without the review's
GV-008 anti-pattern of asserting that buggy behavior is correct.

**Exit:** The harness reproduces GV-001 by execution, reproduces GV-004 and
GV-049 through the semantic-only variant, and runs as part of `go test ./...`.

### 1.2 GV-044 — A real linter (S3), sweeping GV-019 and GV-020

`go vet` does not report unused unexported functions, which is precisely why the
dead `flattenFrameTree` (GV-019) and the entirely unreferenced
`semanticLocatorKey` (GV-020) survived. Add `staticcheck` (at minimum `U1000`)
or `golangci-lint`, pinned by version, to CI and to `scripts/check.*`.

Then delete `internal/compiler/compiler.go`'s `semanticLocatorKey` and
`internal/browser/frames.go`'s `flattenFrameTree`. Deleting the latter breaks
`TestFlattenFrameTreePreservesTreeOrder`; that test is not repaired here, it is
removed, and its replacement is GV-037 in Phase 2.

**As built.** `staticcheck` pinned to `v0.7.0` (2026.1, the release built
against Go 1.26) in CI and both check scripts, run as
`go run honnef.co/go/tools/cmd/staticcheck@…` to match the existing
`govulncheck` pattern. It was verified to flag an unused unexported function
with `U1000` and exit non-zero before the dead code was removed, so the
justification for the finding is established rather than assumed.

**Exit:** The linter is part of both gates and reports clean. Neither dead
function remains.

### 1.2a `scripts/check.ps1` did not fail on error (not in the review)

Windows PowerShell does not surface native command exit codes through
`$ErrorActionPreference`, so `go vet`, `go test`, `npm run check`, and
`verify-release.ps1` could all fail while `check.ps1` reported success. Since
`CONTRIBUTING.md` presents `check.sh`/`check.ps1` as the gate, Windows
contributors had no working gate at all. Every external step now runs through an
`Invoke-Step` helper that throws on a non-zero exit code.

### 1.3 GV-039 — CI requires a browser (S2)

`.github/workflows/ci.yml` runs `go test ./...` with no
`GEOVISOR_REQUIRE_BROWSER=1`, and the browser integration tests skip silently
when no Chromium is found. Locally `internal/browser` completed in 2.7s, which
is too fast for three Chromium launches — it skipped. The entire browser layer
can stop working without CI turning red.

Set `GEOVISOR_REQUIRE_BROWSER=1` on at least one job with a known-good Chromium,
so a missing browser is a failure rather than a silent pass.

**As built.** A dedicated `browser` job on `ubuntu-latest` sets
`GEOVISOR_REQUIRE_BROWSER=1` and runs `./internal/browser/...` and
`./internal/cli/...`. It asserts Chrome exists by running
`google-chrome --version` before the tests, so an image without a browser fails
on an obvious step rather than inside `launcher.LookPath`.

**Open tradeoff.** This relies on the Chrome preinstalled in the runner image
rather than a version pinned by this repository. Pinning would mean adding a
third-party action such as `browser-actions/setup-chrome`, which is the only new
supply-chain dependency this phase would introduce, so it was left as a
decision rather than taken silently.

**Exit:** A deliberately broken browser source turns CI red.

### 1.4 GV-047 — Align the local and CI gates (S3)

`scripts/check.sh` runs `verify-release.sh`; CI does not. `CONTRIBUTING.md` and
`docs/releasing.md` both present `check.sh` as *the* gate, so the documented
gate is strictly stronger than the enforced one. Add the missing step to CI, or
state the difference in both documents.

### 1.5 GV-048 — `paths-ignore` deadlock (S4)

A docs-only pull request produces no check run at all, so branch protection
requiring the `Quality` job can never be satisfied. Replace the skip with a
path-filtered job that still reports success, or drop `paths-ignore` from the
`pull_request` trigger.

**Phase exit — met.** The gate is trustworthy. GV-019, GV-020, GV-039, GV-044,
GV-047, GV-048 closed, plus the unlogged `check.ps1` defect. The GV-036 harness
is in place, reproduces GV-001, GV-004, and the newly found GV-049 by execution,
and is gated so the tree stays green. `gofmt`, `go vet`, `staticcheck`,
`go test ./...`, and `npm run check` all pass.

---

## Phase 2 — Make generated tools work

This is the largest phase and the one that repays the Phase 1 investment. Every
item lands with a case in the GV-036 harness.

### 2.1 Structural: one implementation of role, name, and addressing

The review's cross-cutting finding is that `client/src/extractor.ts` and the
runtime embedded in `internal/emitter/webmcp.go` are two independent
implementations of the same semantics. Reading both confirms they already
disagree beyond the three logged bugs: the extractor maps `<summary>`, `<nav>`,
`<dialog>`, `<details>`, and headings to roles `__geovisorRole` does not know at
all, and `labelFor` and `__geovisorName` use different precedence orders.

Restructure so the divergence is impossible rather than merely tested:

- `client/src/shared/role.ts` — the single role-resolution function.
- `client/src/shared/name.ts` — the single accessible-name computation. The
  extractor consumes the full `{ text, reference, score }` result for evidence;
  the runtime consumes only `text` for matching.
- `client/src/shared/locate.ts` — frame-path, shadow-path, and semantic-scope
  resolution, consumed by the runtime.
- `client/src/webmcp-runtime.ts` — new build entry point.

Build it with the existing pinned esbuild toolchain as
`format: "iife"` plus `globalName`, producing a `var __geovisorRuntime = (() => {
… })();` that is module-scoped when concatenated. Commit the output to
`internal/emitter/webmcp-runtime.js`, embed it with `go:embed` exactly as
`internal/payload/embed.go` embeds the extractor, and extend
`client/scripts/build.mjs` and `npm run verify:bundle` to cover both bundles.

The emitted module becomes: the existing capability guard, then
`const __geovisorDefinitions = …;`, then the embedded runtime bundle, then the
registration loop. This preserves the current module shape, the `</script>` and
U+2028 escaping behavior, and golden-file testing. The golden fixtures change
once, reviewably.

**Exit:** `__geovisorRole` and `__geovisorName` no longer exist as
hand-maintained Go string literals. Both bundles are staleness-checked.

**Done.** The shared tree is `client/src/shared/{text,dom,role,name,option,
locate}.ts`. `internal/emitter/webmcp.go` shrank from 334 lines to 129 and now
holds only the capability guard, the statement separator, and the registration
call; the runtime is `//go:embed webmcp-runtime.js`.

Two modules were not in the original sketch. `shared/option.ts` was added
because GV-001's fix requires the advertised enum and the applied selection to
come from one function, not merely one file. `shared/text.ts` was split out
because `cleanText` is what makes a recorded name comparable to a recomputed
one; normalizing differently on the two sides would reintroduce the bug in a
subtler form.

Closed along the way, because each was a one-function consequence of sharing
rather than a separable task:

- **GV-001** — `applySelect` matches an option by the advertised label and sets
  `selectedIndex`. Verified by execution, not substring search: the harness
  asserts the selected option's *label* equals the requested enum value, and
  now requests the *last* enum value so a runtime that cannot change the
  selection at all cannot pass by accident.
- **GV-049** — one role table. The `<details>`-scoped tools that previously
  reported `no element with role "group" and name "Help"` now resolve with CSS
  fallbacks stripped.
- **GV-005** — `isContentEditable` inspects the attribute *value*, so
  `contenteditable="false"` is no longer a textbox. It lives in `shared/role.ts`
  because both sides ask the same question.
- **GV-035** — `matchAll` computes role and name once per element in a single
  subtree scan.

**New constraint on shared code: it must be realm-agnostic.** The extractor only
ever sees elements from the document it was injected into, but the runtime
resolves *across frame boundaries*, and an element inside an iframe is an
instance of that frame's `HTMLInputElement`, not the top document's. Reusing the
extractor's `instanceof` checks in the runtime therefore introduced a latent
cross-frame bug; the original hand-written runtime had used `localName` and was
correct here. The harness caught it immediately — as
`HTMLSelectElement is not defined`, since Node's realm has no DOM constructors —
which is the first time the Phase 1 investment paid for itself. Shared code now
identifies elements structurally and the rule is recorded in `client/README.md`.

**Partial GV-038.** `TestWebMCPModuleSafetyAndRuntime` asserted runtime behavior
by searching for the substring `"execute: async"`, which minification removes.
It is renamed `TestWebMCPModuleSafetyInvariants`, asserts only text properties
it can actually establish, and points at the harness for behavior.

**Follow-on cost.** `internal/emitter/testdata/webmcp.golden` embeds the runtime
bundle, so any runtime source change now requires `UPDATE_GOLDEN=1`. This is
worth stating plainly rather than working around: it means the committed golden
records the exact runtime bytes a reviewer is approving.

After 2.1, the semantic-only harness case has exactly one remaining cause —
GV-004's display name in the match key — instead of three.

### 2.2 GV-001 — `<select>` tools that actually work (S1)

The extractor builds enums from option *labels*; the runtime assigns the enum
entry to `element.value` and throws when it does not stick. Every `<select>`
whose option values differ from their visible text — including this repo's own
`testdata/corpus/forms.html` — produces a permanently broken tool.

The advertised enum and the assigned value must derive from one source, with no
second convention.

**Constraint discovered in Phase 1.** The obvious repair — emit the option
`value` alongside the label — conflicts with an existing deliberate decision.
`forms.html` uses `HIDDEN-BASIC-ID` and `HIDDEN-PRO-ID` as option values, and
both are in `sensitiveSentinels`, so `assertNoSensitiveValues` asserts that
option values never reach an emitted artifact. Option values are therefore
treated as page data that must not leave the page. The fix that satisfies both
constraints is for the runtime to select by the same label it advertises,
matching the option and setting `selectedIndex`, rather than assigning
`element.value`.

**Acceptance:** A `<select>` whose option values differ from their text produces
a tool that changes the selection when the emitted module runs, the harness
asserts the resulting `selectedIndex`, and no option value appears in any
artifact.

**Done in 2.1.** `shared/option.ts` owns both the advertised enum and the
lookup, so there is one function rather than one convention.
`TestWebMCPRuntimeSelectAppliesAdvertisedOption` passes;
`assertNoSensitiveValues` still passes, so no option value leaks.

### 2.3 GV-002 — Ancestor-aware hidden detection (S1)

`isHidden` is entirely element-local: a descendant of a `display:none` wrapper
reports its own `display`, and `aria-hidden` is read only from the element
itself. The review verified that a document with a `display:none` wrapper, an
`aria-hidden="true"` wrapper, and one visible input emits **all three** inputs.
On any page with modals, drawers, or inactive tab panels this is the largest
single source of output noise.

**Acceptance:** Descendants of `display:none`, `visibility:hidden`, `hidden`,
and `aria-hidden="true"` ancestors are excluded. A fixture covering all four
ancestor cases plus one visible control asserts exactly one emitted interaction.

**Done.** `traverse` is a pre-order DFS that carries inherited hiding across
elements and open shadow roots. Only subtree-hiding conditions propagate
(`display: none`, the `hidden` attribute, `aria-hidden="true"`);
`visibility: hidden` is checked locally so a descendant may still opt back in
with `visibility: visible`. Both cases have extractor tests. Hidden records are
skipped before any interaction is built, so they never become tools.

### 2.4 GV-003 — Correct frame locators (S1), with GV-037

Both halves are wrong. The CSS half emits
`:is(iframe, frame):nth-child(N of iframe, frame)`, which counts among element
siblings, while `FrameReference.Index` is an index among a frame's *child
frames* — on two single-iframe `<div>`s, index 0 matches two elements and index
1 matches none. The semantic half matches `SemanticNode.Name` against the CDP
frame name, but the runtime's name computation never reads the `name` attribute;
and unnamed frames omit the field via `omitempty`, so matching is skipped and
`matches[0]` — always the first iframe in the document — is returned.

The two failures compound: the semantic strategy silently returns a wrong
element instead of failing over to CSS. ADR 0003 and ADR 0004 both depend on
this path.

Frame index semantics must be defined once and identical on both sides, and
frame-name matching must either read an attribute the runtime actually reads or
be dropped from matching. Section 2.1 makes "both sides" a single side.

**GV-037 (S2)** is closed here: `flattenCompleteFrameTree`,
`mergeFrameOwnerOrder`, `attachOOPIFSessions`, `addUnattachedOOPIF`,
`augmentBatch`, and `frameTraversalNodes` currently have no unit tests, while
the deleted dead function had one. Backfill the live functions.

**Acceptance:** A nested, multi-parent iframe fixture resolves each frame to the
correct element when the emitted module executes.

**Done.** Frames are addressed by `nth` among the containing document's frames
and carry no name and no CSS fallback. `frameTraversalNodes` writes that shape;
`resolveFrameNode` / `frameCandidates` consume it. The two walks — Go
`collectFrameOwnerOrder` and TypeScript `frameCandidates` — are the same
contract: pre-order, light children before shadow content, no descent past a
frame. `TestWebMCPRuntimeResolvesFramePathsToTheCorrectFrame` executes the
emitted module against two identical unnamed frames in separate wrappers.

GV-037 is closed by `internal/browser/frames_test.go`: `frameTraversalNodes`,
`augmentBatch` (including slice independence), `flattenCompleteFrameTree`
(owner-order fallback and unattached OOPIFs), `collectFrameOwnerOrder` (the
session-free half of `mergeFrameOwnerOrder`), `addUnattachedOOPIF`, and
`originForURL`. `attachOOPIFSessions` remains CDP-bound and is exercised by the
browser job, not by a unit test that would have to fake a browser.

### 2.5 GV-004 — Separate display name from match key (S1)

`disambiguateInteractions` writes the disambiguated *display* name
(`"Query (2)"`) into `locator.semantic.name`, which the runtime matches for
exact equality against the real accessible name (`"Query"`). No such name exists
in the DOM, so every disambiguated element — exactly the ambiguous cases where a
precise locator matters most — can never resolve semantically.

The root cause is that one field serves as both display name and match key.
Split them: the value used for matching must always be a name that exists in the
DOM, and disambiguation among equal matches needs its own mechanism (an ordinal
within the match set) rather than a mutated name.

**Acceptance:** A two-identical-labels fixture resolves both tools to
*different*, correct elements when the emitted module executes, and tool
identities remain distinct and stable.

**Done.** `disambiguateInteractions` now mutates only the display name.
`verifiedSemantic` records the element's index in the match set as `nth` when
the match is not unique, and omits the semantic half entirely when the shared
matcher cannot resolve back to the element. TIR, the observation DTO, the
schema, and compiler identity all carry `nth`.
`TestWebMCPRuntimeResolvesIdenticalNamesToDistinctElements` asserts the three
"Query" tools in `forms.html` resolve to three different elements with CSS
fallbacks stripped.

### 2.6 GV-005 — `contenteditable="false"` is not editable (S2)

Both `isNativeControl` and `semanticRole` use `hasAttribute("contenteditable")`,
which is true for the literal string `"false"`.

**Acceptance:** `contenteditable="false"` produces no control interaction;
`contenteditable`, `contenteditable=""`, and `contenteditable="true"` still do.

**Done in 2.1**, since both call sites now route through one shared predicate.
The dedicated fixture is in `client/test/extractor.test.mjs` and asserts
`contenteditable="false"` is skipped while the empty, bare, `true`, and
`plaintext-only` values still produce controls.

### 2.7 GV-007 — Stop emitting form controls twice (S2)

Per decision 2: a control owned by a form is a parameter of the form tool and is
not also emitted as a standalone tool. This roughly halves tool count on
form-heavy pages and removes the two-competing-ways-to-fill ambiguity. Record
the rationale in `docs/compiler.md`.

Note that `extract` currently also lacks an `else` between the form branch and
the control branch, so a `<form>` that is itself a control-like element can
produce two interactions from one record; the same change covers it.

**Done.** Forms claim their controls first; claimed elements are skipped in the
main loop, and the form / control / action branches are exclusive. Ownership
follows `element.form` / the `form` attribute, not containment. Submit buttons
are not claimed — they stay standalone actions.
`TestWebMCPRuntimeDoesNotRegisterStandaloneFormControls` asserts the
`forms.html` profile fields are not registered as their own tools, that
`save-profile` still is, and that the three unowned Query inputs still are.
Recorded in `docs/compiler.md` under "One tool per capability".

### 2.8 GV-035 — Stop rescanning the DOM per semantic node (S3)

`__geovisorElements` does `Array.from(root.querySelectorAll("*"))` and then
computes role *and* accessible name — including `textContent` — for every
element, once per scope node per resolution attempt. This is the same code
Section 2.1 rewrites, so it is folded in rather than deferred: resolve scope
chains with a single scan and memoize per root.

**Done in 2.1.** `matchAll` computes role and name once per element per scan.
Per-root memoization was deliberately *not* added: resolution mutates the DOM
between actions, so a cache would have to be invalidated on every apply, and
the single-scan change already removes the quadratic factor. Revisit only with
a measurement.

### 2.9 GV-049 — One role table (S2)

**Done in 2.1**, rather than separately: once role resolution has a single
implementation, the runtime cannot know fewer roles than the extractor emits.
`TestWebMCPRuntimeSemanticStrategyResolvesWithoutCSSFallback` no longer reports
`role "group"` failures for `<details>`.

One subtlety the shared table had to absorb: the extractor records `"generic"`
for a shadow host with no mapped role, while role resolution returns `""`. Both
sides now compare through `normalizeRole`, so absent and `"generic"` are the
same role. Two unreachable fallbacks remain — `semanticRole(...) || "control"`
and `|| "button"` in the extractor would record a role the runtime could never
compute — but every element reaching them already has a role. Left alone rather
than "fixed" speculatively; noted here so a future change to `isControl` or
`isAction` does not silently make them reachable.

A related naming weakness the harness exposed is still open: the
`<details>` scope in `forms.html` is named `"Help"`, taken by `adjacentText`
from an unrelated preceding `<dialog>`. Scope names sourced from adjacent text
are low-confidence and worth reconsidering alongside GV-018.

**Phase exit — met.** GV-001 through GV-005, GV-007, GV-035, GV-037, and GV-049
are closed. Every `knownFailure` call is gone, and so is the helper: with no
callers left, staticcheck flagged it as dead code, which is the outcome the
phase was aiming for. The GV-036 harness passes on every corpus fixture.

One principle emerged across 2.3, 2.4, and 2.5, and is worth keeping: **the
extractor records only locators it has already resolved.** `verifiedSemantic`
runs the shared matcher from 2.1 and omits the semantic half when it does not
resolve back to the element it describes. GV-003, GV-004, and GV-049 were all
the same failure wearing different clothes — a locator recorded without ever
being tried, which then degraded silently to a CSS fallback. Producer-side
verification makes that class of defect unrepresentable rather than merely
tested for. It is also why the ordinal mechanism added for GV-004 could be
reused unchanged for GV-003.

Costs accepted, recorded so they are not rediscovered as surprises:

- **Tool ID churn.** Scope identity now includes the match ordinal, so
  `compiler.golden.json` IDs changed once. Correct, since two scopes sharing a
  role and name but selecting different elements are different scopes. GV-018
  owns the broader ID-stability design.
- **Frame path nodes carry no CSS fallback.** No CSS selector expresses "the Nth
  frame of this document". A frame that cannot be addressed by ordinal now fails
  loudly instead of resolving to the wrong frame.
- **Shadow-hosted frames.** `frameCandidates` in `shared/locate.ts` mirrors
  `mergeFrameOwnerOrder`: pre-order, light children before shadow content, no
  descent past a frame. The two orderings are stated in comments that name each
  other, but nothing yet *tests* that they agree for a frame inside a shadow
  root, because neither jsdom nor the corpus exercises it. Noted under 6.3.

---

## Phase 3 — Safety and honesty

These change documented guarantees, so the docs move with the code.

### 3.1 GV-008 + GV-009 — Safe exploration becomes safe and useful (S1/S2)

Today `exploreSafely` writes `open=""` onto `<details>` elements and never
restores it, which in attach mode permanently modifies a live tab the user owns
and fires `toggle`, running arbitrary page JavaScript. `docs/product-spec.md`
permits interaction "only when an action is classified safe"; this write is
neither classified nor restored. Adjacent code advertises "extraction never
changes its value" — this is the one place it does. A test at
`client/test/extractor.test.mjs:109` asserts the mutation *persists*, locking the
behavior in.

It is also inert: `await Promise.resolve()` is a microtask, not a yield, so no
layout, no `toggle` handler, and no lazily inserted content runs before the
re-traverse. `timeoutMs` is only read as a loop-break deadline in a loop that
performs no async work, so it never elapses. The feature pays GV-008's full
safety cost for almost none of its benefit.

Per decision 3, fix both together:

- Snapshot each element's prior `open` state and restore it in a `finally`, so
  extraction leaves the page as it found it even on failure.
- Yield genuinely to the event loop so `toggle` handlers and layout run.
- Enforce the time budget as a real deadline across the async work, and keep the
  operation budget independent of it.
- Update the locking test to assert restoration instead of persistence.

**Acceptance:** A fixture whose `<details>` content is populated by a `toggle`
handler yields the revealed controls; both budgets are demonstrably enforced;
page state after extraction is byte-identical to before.

### 3.2 GV-010 — `--depth 0` means no exploration (S3)

`detailsDepth` of a top-level `<details>` is `0`, and the guard is
`<= options.maxDepth`, so `--depth 0` still opens top-level elements even though
`--depth` is advertised as `0-16`. The test named "safe exploration obeys
operation and depth caps" sets `maxOperations: 1`, so the operations cap is what
stops it and the depth cap is never exercised.

**Acceptance:** `--depth 0` performs zero operations, and a test distinguishes
the depth cap from the operations cap.

### 3.3 GV-011 — Attach mode stays inside the selected target (S2)

`chooseTarget` attaches a debugger session to *every* top-level page to evaluate
`document.hasFocus()`, and `attachOOPIFSessions` iterates every `iframe`-type
target in the whole browser rather than only descendants of the selected root.
When nothing reports focus, `selectTarget` silently returns `candidates[0]` — an
arbitrary URL-sorted tab — with no diagnostic. The user's other tabs are touched
and the tool may observe a page the user never selected.

**Acceptance:** Sessions attach only to the selected target and its descendant
frames. The no-focus fallback either fails explicitly or emits a `Diagnostic`
naming the target it chose.

### 3.4 GV-012 — Probes leave the page's main world (S2)

`waitForDocumentReady`, `waitForDOMQuiet`, and `targetHasFocus` all call
`Runtime.evaluate` with no `ContextID`, so they run in the page's own world, and
`waitForDOMQuiet` installs a `MutationObserver` there. `extractFrame` already
does this correctly with an isolated world, so the pattern exists in the
codebase. The page can currently observe and interfere with its own observation,
which undercuts both the stealth story and determinism.

**Acceptance:** No observation-side JavaScript evaluates in the page's main
world.

### 3.5 GV-013 — Mark page-derived text as untrusted (S2)

Tool names and descriptions originate in page text — `aria-label`, `title`,
`textContent` — and flow into the model's tool list, while
`UntrustedContentHint` is hardcoded `false` and nothing on the Go side bounds or
sanitizes description content. The golden fixture already exercises `</script>`
escaping, so JS injection was considered; prompt injection was not.

Set the hint to reflect actual provenance and document the threat in
`SECURITY.md`.

Also decide the related question the review raises: `internal/tir/validation.go`
forces `safeForExploration: false` only for `navigation` and `submission`, so a
binding with class `unknown` may still assert it is safe to explore. Given the
guardrail that safe exploration must never navigate or submit, `unknown` should
not be assertable as safe; make that explicit in validation.

### 3.6 GV-030 + GV-031 — Element-level gaps become explicit (S2/S3)

`read`'s `catch` and `extract`'s bare `catch {}` discard every element-level
failure with no counter, no warning, and no evidence record. For a project whose
spec requires coverage gaps to be explicit, this is a direct asymmetry: frames
report gaps, elements do not. Closed shadow roots (GV-031) are skipped the same
way, with no equivalent of the uncovered-frame record.

**Acceptance:** Suppressed element failures surface as a count or warning in the
batch and reach the TIR `warnings` collection. Closed shadow roots produce a
coverage record.

### 3.7 Documentation

`docs/product-spec.md` and `README.md` state the guarantee that is actually
held, in matching language, in the same commits as the behavior above.

**Phase exit:** GV-008 through GV-013, GV-030, GV-031 closed. Safety claims in
the docs are true statements about the code.

---

## Phase 4 — Contract integrity

Both halves of this phase are the same idea: put each invariant at the layer
that owns it.

### 4.1 GV-014 — TIR is the single gate (S2)

`convertShape` rejects arrays without `items`, objects carrying enums, arrays
carrying properties or enums, strings carrying items or properties, and
primitives carrying enums. Neither `tir.Validate` nor `schemas/tir.schema.json`
enforces any of it, so a document can be canonically valid, schema-valid, and
emitted as `tir.json`, then fail with `unsupported_shape` on the MCP, OpenAI,
and WebMCP lanes — `--format all` failing after `--format tir` succeeded on
identical input.

Per decision 4, hoist the constraint set into `tir.Validate` and the JSON
Schema. It is a small, enumerable list, and hoisting keeps the emitters free of
independent opinions about validity.

**Acceptance:** Any document accepted by `tir.Validate` emits successfully on
every registered format, asserted by a test that runs all formats over the
validation corpus.

### 4.2 GV-016 — Validate the compiler's input boundary (S2)

The package defines `compiler.Error` "for malformed raw input" and uses it for
interaction kind, evidence score, frame index, and locator index — but
`parameter.Type`, `action.Kind`, and `sideEffect.Class` pass through unchecked.
Bad input therefore surfaces as
`validate compiled TIR: tools[3].parameters[1].type: …`, a field path into the
*output* document that is useless for locating the offending input.

**Acceptance:** Every enum-valued field arriving from `observation` is rejected
at the compiler boundary with a `compiler.Error` whose `Field` names the *input*
path.

### 4.3 GV-015 — Each emitter validates its own format (S3)

OpenAI validates `tool.ID` against `^[A-Za-z0-9_-]{1,64}$`; MCP and WebMCP emit
the same ID unchecked. Either each emitter enforces its target format's
identifier constraints, or the constraint is hoisted into TIR alongside 4.1.

### 4.4 GV-017 — Unambiguous frame path keys (S3)

`framePathKey` builds `"%d:%s:%s;"` from page-controlled `Name` and `Src`, which
may contain `:` and `;`, so distinct frame paths can collide into a spurious
`duplicate_frame_path`; `%d` also sorts index `10` before `2`. The compiler's
own `joinedKey` is length-prefixed and unambiguous — the right pattern already
in the codebase.

**Acceptance:** No two distinct frame paths produce the same key; ordering is
numeric on `index`.

### 4.5 GV-018 — Tool IDs stop churning (S3)

`fallbackIndex` comes from `record.sourceOrder`, a document-wide traversal
counter, so inserting one element anywhere shifts every downstream fallback
name — and the name feeds tool identity and therefore the ID hash. Output is
deterministic for a fixed DOM but unstable across trivial page edits, which is
what a consumer of a "stable ID" will actually rely on.

**Acceptance:** Adding an unrelated element earlier in the document does not
change the IDs of unaffected tools; a test asserts this.

### 4.6 GV-029 — `Options.Strict` is honored or rejected (S3)

MCP and WebMCP both discard `Options` entirely. Silently ignoring a
caller-supplied option is worse than rejecting it: either honor it or return a
typed error.

**Phase exit:** GV-014 through GV-018, GV-029 closed.

---

## Phase 5 — Resilience and performance

### 5.1 GV-033 + GV-034 — Bounded extraction, honest timeouts (S2, paired)

`cssFallback` calls `finder` with `timeoutMs: Number.MAX_SAFE_INTEGER`, once per
interaction and once per shadow-path node, and `finder` verifies uniqueness with
repeated `document.querySelectorAll` on top of the `querySelectorAll("*")` that
`traverse` already performs per root. Meanwhile `frameTimeout` defaults to
`max(3s, TimeoutMS+2s)` — 3 seconds with default flags — and exceeding it yields
zero interactions for the frame under the reason "browser lost the frame context
during extraction," which describes a different failure and will send someone
debugging the wrong thing.

These are paired because the correct frame timeout depends on the extraction
budget.

**Acceptance:** `finder` has a real per-call budget and degrades to
`simpleSelector` on exhaustion rather than consuming the frame budget.
Extraction timeout is textually distinguishable from context loss, separately
configurable, and defaulted proportionately to the bounded extraction cost.

### 5.2 GV-024 + GV-040 — `WriteFiles` guarantee matches reality (S2/S3)

The doc comment says the package "writes generated artifacts without exposing
partial files" and "atomically replaces each file" — true per file, misleading in
aggregate, since a failure on file three leaves files one and two already
replaced. Per the acceptance criteria, either make it genuinely all-or-nothing
or state the per-file guarantee precisely.

GV-040 is closed alongside: `writer_test.go` covers only staging failure, and
the replace-phase partial state, `MkdirAll` failure, empty-path rejection,
`Chmod` failure, and the `replace_windows.go` / `replace_other.go` split are all
uncovered.

### 5.3 GV-006 — Sanitizer stops shredding diagnostics (S2)

`sanitizeText` runs `strings.ReplaceAll` for every query-parameter key *and*
value from the endpoint URL across the whole message, so endpoint
`http://localhost:9222/?a=1&user=bob` turns
`cannot attach to target…` into `c[redacted]nnot [redacted]tt[redacted]ch…`.
Redacting keys is also wrong in principle: keys are not secrets, values are.
`TestSanitizedErrorRedactsEndpointSecrets` passes only because its key happens
to be the long word `token`, so the test design conceals the bug.

**Acceptance:** Redaction is scoped to credentials and parameter values, not
names, and cannot alter text containing no secret. A test asserts an unrelated
message is byte-identical after sanitization when the endpoint carries a
single-character query key.

### 5.4 Remaining reliability items

- **GV-025 (S3)** — `file.Sync()` is called but the parent directory is never
  synced after rename, leaving the durability effort incomplete on crash.
- **GV-026 (S3)** — `.cursor/rules/go-quality.mdc` asks for `context.Context`
  first on I/O boundaries. `WriteFiles` performs filesystem I/O; `Compile` is
  CPU-bound but is the long pole on large pages.
- **GV-027 (S3)** — `http.DefaultClient` is a package global with default
  redirect following, used against a caller-supplied endpoint.
- **GV-028 (S3)** — `canonicalDocument` fully marshals and unmarshals the whole
  document per emitter, four round-trips under `--format all`, with no semantic
  gain over `Clone` + `Normalize`.

**Phase exit:** GV-006, GV-024 through GV-028, GV-033, GV-034, GV-040 closed.

---

## Phase 6 — Hygiene and test backfill

Low blast radius, done last, but not skipped.

### 6.1 Duplication and naming

- **GV-021 (S3)** — `compileLocators` and `compileActions` each recompute
  locator IDs via `stableIDs` with a duplicated key-builder closure. If they
  ever drift, action bindings silently reference locator IDs that do not exist
  and nothing detects it. Compute once, share.
- **GV-022 (S3)** — `sortedUniqueStrings`, `canonicalProvenance`, and
  `sortedUnique` all use the `values[:0]` in-place reuse trick while reading as
  pure functions. Correct today because every call site passes a fresh slice.
  Either stop mutating or state the contract at each declaration.
- **GV-023 (S4)** — `deriveMCPAnnotations` and `deriveWebMCPAnnotations` are
  near-identical; `validateOutputCollisions` duplicates `validateFiles` exactly.

### 6.2 GV-032 — Assorted small defects (S4)

- The `role="search"` branch in `formInteraction` is unreachable: if
  `role="search"`, `explicitRole` already returned `"search"`.
- `parameterName`'s 80-character cap applies only on the non-numeric branch, so
  names starting with a digit are unbounded.
- `webMCPPrefix`'s length is omitted from the emitted-data capacity calculation,
  forcing a reallocation.
- `cleanText`'s `slice(limit)` on UTF-16 code units can split a surrogate pair.

### 6.3 Test backfill

- **GV-038 (S3)** — Test names overstate their assertions:
  `TestWebMCPModuleSafetyAndRuntime` cannot distinguish a working module from a
  file containing the right substrings (largely resolved by GV-036, but the name
  and scope still need correcting); the annotation tests call the helpers
  directly rather than through `Emit`, so the wiring could regress undetected;
  `types_test.go` uses `encoding/json.Marshal` rather than the production
  `tir.Marshal`, so the canonical path is not what is exercised.
- **GV-041 (S3)** — No coverage for `ExitGeneric` fallback,
  `validateDependencies`, the browser-config-to-usage remap,
  `validateArtifactName`, `writeBytes` short writes, unknown subcommand, or most
  flag-bound validation.
- **GV-042 (S3)** — Eleven `tir` validation codes are defined but never
  asserted, including `unsupported_version`, `invalid_execution_boundary`, the
  coverage-consistency codes, `duplicate_frame_path`,
  `duplicate_parameter_name`, `missing_locator_strategy`,
  `duplicate_locator_reference`, and `duplicate_enum_value`. A table-driven case
  per code suits the go-quality rule.
- **GV-043 (S4)** — Unchecked type assertions in three test files panic instead
  of failing, obscuring which assertion broke.
- Shadow-hosted frames: `frameCandidates` and `collectFrameOwnerOrder` state
  the same walk in comments that name each other, and each side has its own
  test, but nothing yet runs them against one DOM that hosts a frame in a
  shadow root. Neither jsdom nor the corpus exercises that case.

### 6.4 Documentation

- **GV-045 (S3)** — `client/README.md`, `docs/adr/0001-execution-boundary.md`,
  and `docs/architecture.md` describe source adapters as future work; they are
  implemented. Move them to the present tense.
- **GV-046 (S3)** — Delete the root `GEOVisor Plan`. It describes
  `visor inspect`, `--exploration-budget`, a Go host daemon, `go-rod/stealth`,
  `map[string]any` TIR metadata, and Playwright-style locator strings; several
  are things ADR 0001 and ADR 0003 explicitly decided against, and `AGENTS.md`
  forbids building from it. Sitting at the repository root, it invites exactly
  that.

**Phase exit:** All 48 findings closed.

---

## Coverage

All 49 findings are assigned. Two deviations from the review's suggested
sequencing, both noted in place: GV-047 and GV-048 move to Phase 1 as gate
defects, and GV-010, GV-031, GV-029, GV-035, and GV-037 move to the phase that
touches the same code.

| Phase | Findings |
| --- | --- |
| 1 — Gate | GV-019, GV-020, GV-036, GV-039, GV-044, GV-047, GV-048 |
| 2 — Generated tools | GV-001, GV-002, GV-003, GV-004, GV-005, GV-007, GV-035, GV-037, GV-049 |
| 3 — Safety | GV-008, GV-009, GV-010, GV-011, GV-012, GV-013, GV-030, GV-031 |
| 4 — Contract | GV-014, GV-015, GV-016, GV-017, GV-018, GV-029 |
| 5 — Resilience | GV-006, GV-024, GV-025, GV-026, GV-027, GV-028, GV-033, GV-034, GV-040 |
| 6 — Hygiene | GV-021, GV-022, GV-023, GV-032, GV-038, GV-041, GV-042, GV-043, GV-045, GV-046 |

Count by severity, including GV-049: 6 × S1, 16 × S2, 22 × S3, 5 × S4.

## Risks

**The Phase 2 restructure is the largest single change.** Sharing role, name,
and addressing logic touches the extractor, the committed bundle, the WebMCP
emitter, and every golden fixture. It is sequenced immediately after the GV-036
harness specifically so the harness can catch regressions the golden files
cannot. If the restructure proves larger than expected, the fallback is to land
the harness plus the individual GV-001/002/003/004 fixes first and restructure
afterward — but that ordering lets the next divergence through, which is the
failure mode the review is warning about.

**Golden fixtures will change repeatedly** across Phases 2 through 4. Each
change must be reviewed as a diff of intent, not accepted wholesale, or the
fixtures stop being evidence.

**Phases 2 and 3 change output shape and documented guarantees.** GV-007 alone
roughly halves tool count on form-heavy pages. Anything downstream that consumes
GEO-Visor output should expect a breaking change, and it belongs in release
notes.

**GV-039 makes CI depend on a working Chromium**, which trades silent test skips
for a new source of CI flakiness. That trade is the point of the finding, but
the job needs a pinned browser rather than whatever the runner happens to ship.
