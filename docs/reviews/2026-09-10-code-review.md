# GEO-Visor Code Review — 2026-09-10

## Scope and method

Full read of the Go pipeline (`cmd/`, `internal/`), the TypeScript extraction
payload (`client/src`), the committed browser bundle, schemas, scripts, CI, and
docs. Findings marked **Verified** were reproduced by executing the committed
`internal/payload/extractor.js` under jsdom or by running Go code directly;
everything else is from code reading.

Tooling baseline at review time, all clean:

| Check | Result |
| --- | --- |
| `gofmt -l .` | no output |
| `go vet ./...` | clean |
| `go test ./...` | all packages pass |

Note: `internal/browser` completed in 2.7s, which is too fast for three real
Chromium launches. The browser integration tests skipped silently. See GV-039.

No code was changed. The working tree was clean at the end of the review.

## How to use this document

Each finding has a stable ID (`GV-NNN`), a severity, a location, the evidence
behind it, the user-visible impact, and acceptance criteria stated as a
verifiable outcome rather than a prescribed patch. Plan against the IDs; they
will not be renumbered.

Severity scale:

| Level | Meaning |
| --- | --- |
| **S1** | Generated artifacts are wrong or unusable against real pages; or a safety guarantee the docs make is not held |
| **S2** | Significant correctness, contract, or safety weakness; wrong under common conditions |
| **S3** | Real defect or maintainability problem with bounded blast radius |
| **S4** | Minor defect, inconsistency, or hygiene issue |

## Triage summary

| ID | Finding | Sev | Area | Verified |
| --- | --- | --- | --- | --- |
| GV-001 | `<select>` tools fail end to end: enum holds labels, runtime assigns `.value` | S1 | client + emitter | yes |
| GV-002 | `isHidden` misses ancestor-hidden subtrees | S1 | client | yes |
| GV-003 | Frame locator is wrong in both its CSS and semantic halves | S1 | browser + emitter | yes |
| GV-004 | Name disambiguation invalidates its own semantic locator | S1 | client + emitter | yes |
| GV-005 | `contenteditable="false"` treated as an editable textbox | S2 | client | yes |
| GV-006 | Error sanitizer replaces query-param keys, shredding diagnostics | S2 | browser | yes |
| GV-007 | Form controls emitted twice (as form parameter and standalone tool) | S2 | client | yes |
| GV-008 | Safe exploration mutates the page and never restores it | S1 | client | — |
| GV-009 | Safe exploration is functionally inert (`await Promise.resolve()`) | S2 | client | — |
| GV-010 | `maxDepth: 0` still opens top-level `<details>` | S3 | client | — |
| GV-011 | Attach mode attaches sessions to every tab and every OOPIF in the browser | S2 | browser | — |
| GV-012 | Readiness, DOM-quiet, and focus probes run in the page's main world | S2 | browser | — |
| GV-013 | Page-derived text reaches the model with `untrustedContentHint: false` | S2 | emitter | — |
| GV-014 | Emitters are stricter than the TIR contract; `--format all` can fail where `tir` succeeded | S2 | tir + emitter | — |
| GV-015 | Tool-name constraints enforced only by the OpenAI emitter | S3 | emitter | — |
| GV-016 | Compiler does not validate its own input boundary | S2 | compiler | — |
| GV-017 | `framePathKey` is ambiguous over page-controlled text | S3 | tir | — |
| GV-018 | Tool IDs churn on unrelated DOM edits | S3 | client + compiler | — |
| GV-019 | `flattenFrameTree` is dead; only a test calls it | S3 | browser | — |
| GV-020 | `semanticLocatorKey` is entirely unreferenced | S4 | compiler | — |
| GV-021 | Locator-ID derivation duplicated across two functions | S3 | compiler | — |
| GV-022 | In-place mutating helpers named as pure functions | S3 | tir + compiler | — |
| GV-023 | Duplicated annotation derivation and output-collision validation | S4 | emitter + cli | — |
| GV-024 | `WriteFiles` is documented as atomic but is not all-or-nothing | S2 | output | — |
| GV-025 | No parent-directory fsync after rename | S3 | output | — |
| GV-026 | `context.Context` not threaded into `WriteFiles` / `Compile` | S3 | output + compiler | — |
| GV-027 | `http.DefaultClient` used for CDP endpoint resolution | S3 | browser | — |
| GV-028 | `canonicalDocument` full JSON round-trip per emitter | S3 | emitter | — |
| GV-029 | `Options.Strict` silently ignored by MCP and WebMCP | S3 | emitter | — |
| GV-030 | Element-level extraction failures swallowed with no diagnostic | S2 | client | — |
| GV-031 | Closed shadow roots skipped with no coverage record | S3 | client | — |
| GV-032 | Assorted small defects (unreachable branch, unbounded name, capacity bug) | S4 | mixed | — |
| GV-033 | `finder` runs with an unbounded time budget | S2 | client | — |
| GV-034 | 3s frame timeout converts slow pages into total, mislabelled coverage loss | S2 | browser | — |
| GV-035 | Generated runtime rescans the whole DOM per semantic node | S3 | emitter | — |
| GV-036 | Generated WebMCP JavaScript is never executed by any test | S1 | tests | — |
| GV-037 | A test asserts on dead code while the live equivalent is untested | S2 | tests | — |
| GV-038 | Test names claim behavior their assertions cannot distinguish | S3 | tests | — |
| GV-039 | CI never sets `GEOVISOR_REQUIRE_BROWSER=1` | S2 | tests + CI | — |
| GV-040 | `WriteFiles` replace-phase failure untested | S3 | tests | — |
| GV-041 | Most CLI exit-code and validation paths untested | S3 | tests | — |
| GV-042 | Many `tir` validation codes defined but never asserted | S3 | tests | — |
| GV-043 | Unchecked type assertions panic instead of failing tests | S4 | tests | — |
| GV-044 | No linter beyond `go vet` | S3 | CI | — |
| GV-045 | Docs describe implemented adapters in the future tense | S3 | docs | — |
| GV-046 | Root `GEOVisor Plan` contradicts accepted ADRs | S3 | docs | — |
| GV-047 | `check.sh` gate is strictly stronger than the CI gate | S3 | CI | — |
| GV-048 | `paths-ignore` can deadlock docs-only PRs under branch protection | S4 | CI | — |

---

## A. Correctness defects (execution-verified)

### GV-001 — `<select>` tools fail end to end · S1

**Where.** `client/src/extractor.ts:424-435` (`controlEnum`);
`internal/emitter/webmcp.go:301-305` (`__geovisorApply`, `select` branch).

**Evidence.** The extractor builds enums from option *labels*
(`option.label || option.textContent`). The generated runtime assigns the enum
value to `element.value` and then throws if the assignment did not stick.
Executing the committed bundle on
`<option value="basic-id">Basic</option>`:

```
emitted enum: ["Basic","Pro"]
el.value = "Basic"  ->  el.value === ""   ->  runtime throws
```

**Impact.** Every `<select>` whose option values differ from their visible text
produces a tool that always fails at execution. That includes this repo's own
`testdata/corpus/forms.html`. `select` is one of only four action kinds.

**Acceptance criteria.**
- A `<select>` whose option `value` differs from its text produces a tool that
  successfully changes the selection when the emitted WebMCP module is executed.
- The value actually assigned is derived from the same source as the advertised
  enum, with no second independent convention.
- A regression test executes the emitted module against the originating DOM and
  asserts the resulting `selectedIndex`.

### GV-002 — `isHidden` misses ancestor-hidden subtrees · S1

**Where.** `client/src/extractor.ts:160-171`.

**Evidence.** Every branch is element-local. The computed value of `display` on
a descendant of a `display:none` element is that descendant's own value, not
`none`, and `aria-hidden` is only read from the element itself. Running the
bundle on a document containing a `display:none` wrapper, an
`aria-hidden="true"` wrapper, and one genuinely visible input emitted **all
three** inputs as tools.

**Impact.** On any page with modal, drawer, off-screen, or inactive-tab-panel
markup, unreachable elements become tools that a model will treat as actionable.
This is likely the largest source of output noise on real pages.

**Acceptance criteria.**
- Descendants of `display:none`, `visibility:hidden`, `hidden`, and
  `aria-hidden="true"` ancestors are excluded.
- A fixture covering all four ancestor cases plus one visible control asserts
  exactly one emitted interaction.

### GV-003 — Frame locator is wrong in both halves · S1

**Where.** `internal/browser/frames.go:538-548` (`frameTraversalNodes`);
`internal/emitter/webmcp.go:180-192` (`__geovisorSemanticNode`),
`webmcp.go:150-170` (`__geovisorName`).

**Evidence, CSS half.** `FrameReference.Index` is an index among a frame's
*child frames*, assigned in `flattenCompleteFrameTree`. The emitted selector
`:is(iframe, frame):nth-child(N of iframe, frame)` counts among *element
siblings*. On `<div><iframe></div><div><iframe></div>`:

```
frame index 0 -> selector matches 2 elements
frame index 1 -> selector matches 0 elements
```

**Evidence, semantic half.** `SemanticNode.Name` carries the CDP frame name (the
`name`/`id` attribute), but `__geovisorName` never reads `name` — it checks
`aria-labelledby`, `aria-label`, `labels`, `alt`, `title`, then text content.
Named frames therefore never match. Unnamed frames omit `name` entirely via
`omitempty`, so name matching is skipped and `matches[0]` is returned — always
the first iframe in the document.

**Impact.** Cross-frame tools are effectively unresolvable, and the two failure
modes compound: the semantic strategy silently returns a wrong element rather
than failing over to CSS. ADR 0003 and ADR 0004 both rest on this path working.

**Acceptance criteria.**
- Frame index semantics are unambiguous and identical on the producer and
  consumer sides.
- Frame name matching either uses an attribute the runtime actually reads, or
  the field is not used for matching.
- A nested, multi-parent iframe fixture resolves each frame to the correct
  element when the emitted module is executed.

### GV-004 — Disambiguation invalidates its own locator · S1

**Where.** `client/src/extractor.ts:741-752` (`disambiguateInteractions`);
`internal/emitter/webmcp.go:183-185`.

**Evidence.** The disambiguated *display* name is written into
`locator.semantic.name`. The runtime requires exact equality against the real
accessible name. Confirmed on two `aria-label="Query"` inputs:

```
tool names:            ["Query", "Query (2)"]
semantic locator names:["Query", "Query (2)"]
real DOM aria-labels:  ["Query", "Query"]
```

**Impact.** Every disambiguated element — precisely the ambiguous cases where a
precise locator matters most — can never resolve semantically and silently
degrades to CSS.

**Root cause.** Display name and match key are the same field.

**Acceptance criteria.**
- The value used for matching is always a name that exists in the DOM.
- Disambiguation still yields distinct, stable tool identities.
- A two-identical-labels fixture resolves both tools to *different*, correct
  elements when the emitted module is executed.

### GV-005 — `contenteditable="false"` treated as editable · S2

**Where.** `client/src/extractor.ts:496-501` (`isNativeControl`),
`client/src/extractor.ts:129` (`semanticRole`).

**Evidence.** Both use `element.hasAttribute("contenteditable")`, which is true
for the string `"false"`. Confirmed: `<div contenteditable="false">` emits
`control/textbox` with a `fill` action.

**Acceptance criteria.** `contenteditable="false"` produces no control
interaction; `contenteditable`, `contenteditable=""`, and
`contenteditable="true"` still do.

### GV-006 — Error sanitizer shreds diagnostics · S2

**Where.** `internal/browser/validation.go:149-172` (`sanitizeText`).

**Evidence.** Every query-parameter key *and* value from the endpoint URL is
`strings.ReplaceAll`-ed across the whole message. With endpoint
`http://localhost:9222/?a=1&user=bob`:

```
input:  cannot attach to target: a page was already claimed by another agent
output: c[redacted]nnot [redacted]tt[redacted]ch to t[redacted]rget: [redacted] p[redacted]ge ...
```

**Impact.** Any endpoint carrying a short query key makes every browser
diagnostic unreadable. Redacting keys is also wrong in principle — keys are not
secrets, values are.

**Note.** `TestSanitizedErrorRedactsEndpointSecrets`
(`internal/browser/source_test.go:121-140`) passes only because its key happens
to be the long word `token`. The test design conceals the bug.

**Acceptance criteria.**
- Redaction is scoped to credentials and parameter values, not parameter names.
- Redaction cannot alter message text that does not contain a secret.
- A test asserts an unrelated message is byte-identical after sanitization when
  the endpoint carries a single-character query key.

### GV-007 — Form controls emitted twice · S2

**Where.** `client/src/extractor.ts:769-788` (`extract`).

**Evidence.** `<form aria-label="Login"><input aria-label="User"><button
type="submit">Go</button></form>` yields three interactions:
`form:Login`, `control:User`, `action:Go`. Each control is both a parameter of
the form tool and its own standalone tool.

**Impact.** Roughly doubles tool count and gives a model two competing ways to
fill the same field, with no signal about which to prefer.

**Acceptance criteria.** Either the duplication is removed, or the relationship
is explicit in TIR and documented in `docs/compiler.md` with a stated rationale.

---

## B. Safety and guardrails

### GV-008 — Safe exploration mutates the page and never restores it · S1

**Where.** `client/src/extractor.ts:690-715` (`exploreSafely`), specifically
`record.element.setAttribute("open", "")` at line 709.

**Evidence.** No rollback exists. `client/test/extractor.test.mjs:109` asserts
the mutation *persists* after extraction, so the behavior is locked in by test.

**Impact.** In attach mode this permanently modifies a live tab the user owns,
and firing `toggle` runs arbitrary page JavaScript. `docs/product-spec.md:20-21`
permits interaction "only when an action is classified safe"; this write is
unclassified and unrestored. Adjacent code advertises rationales such as
"extraction never changes its value" — this is the one place it does.

**Acceptance criteria.**
- Either page state is restored after extraction, or the mutation is gated
  behind an explicit opt-in distinct from `--safe-explore` and documented as
  page-modifying.
- The guarantee that is actually held is stated in `docs/product-spec.md` and
  `README.md` in matching language.

### GV-009 — Safe exploration is functionally inert · S2

**Where.** `client/src/extractor.ts:713` (`await Promise.resolve()`).

**Evidence.** A microtask, not a yield to the event loop. No layout, no `toggle`
handlers, no lazily-inserted content runs before `traverse(document)` is called
again. `options.timeoutMs` is only read as a loop-break deadline in a loop that
performs no async work, so it never elapses.

**Impact.** The feature pays the full safety cost of GV-008 while delivering
close to none of its benefit.

**Acceptance criteria.** A fixture whose `<details>` content is populated by a
`toggle` handler yields the revealed controls; the operation and time budgets
are both demonstrably enforced.

### GV-010 — `maxDepth: 0` still opens top-level `<details>` · S3

**Where.** `client/src/extractor.ts:704` (`detailsDepth(...) <= options.maxDepth`).

**Evidence.** `detailsDepth` of a top-level `<details>` is `0`, so `0 <= 0`
passes. `--depth` is advertised as `0-16` in `internal/cli/root.go:202`; a user
setting `0` reasonably expects exploration off.

**Note.** The test named "safe exploration obeys operation and depth caps"
(`client/test/extractor.test.mjs:121-136`) sets `maxOperations: 1`, so the
operations cap is what stops it. The depth cap is never actually exercised.

**Acceptance criteria.** `--depth 0` performs zero exploration operations, and a
test distinguishes the depth cap from the operations cap.

### GV-011 — Attach mode reaches beyond the selected target · S2

**Where.** `internal/browser/target.go:68-73` (`chooseTarget`);
`internal/browser/frames.go:204-207` (`attachOOPIFSessions`);
`internal/browser/target.go:102-104` (fallback).

**Evidence.** `chooseTarget` attaches a debugger session to *every* top-level
page to evaluate `document.hasFocus()`. `attachOOPIFSessions` iterates all
`iframe`-type targets in the entire browser, not only those under the selected
root. When nothing reports focus, `selectTarget` silently returns
`candidates[0]` — an arbitrary URL-sorted tab — with no diagnostic.

**Impact.** In attach mode the user's other tabs are touched, and the tool may
observe a page the user never selected without saying so.

**Acceptance criteria.**
- Sessions are attached only to the selected target and its descendant frames.
- The no-focus fallback either fails explicitly or emits a `Diagnostic` naming
  the target it chose.

### GV-012 — Probes run in the page's main world · S2

**Where.** `internal/browser/frames.go:134-151` (`waitForDocumentReady`),
`frames.go:153-182` (`waitForDOMQuiet`), `internal/browser/target.go:132-143`
(`targetHasFocus`).

**Evidence.** All three call `Runtime.evaluate` with no `ContextID`, so they run
in the page's own world. `waitForDOMQuiet` installs a `MutationObserver` there.
`extractFrame` (`frames.go:459-461`) correctly uses an isolated world; these
three do not.

**Impact.** The page can observe and interfere with observation, which
undercuts both the stealth story and determinism.

**Acceptance criteria.** No observation-side JavaScript is evaluated in the
page's main world.

### GV-013 — Page text reaches the model unmarked · S2

**Where.** `internal/emitter/webmcp.go:105` (`UntrustedContentHint: false`).

**Evidence.** Tool names and descriptions originate in page text — `aria-label`,
`title`, `textContent` — via `labelFor` and `descriptionFor`, and flow into the
model's tool list. Nothing on the Go side sanitizes or bounds description
content, and the hint is hardcoded to `false`.

**Impact.** A hostile page controls text placed directly into model context.
The golden fixture already exercises `</script>` escaping, so JS-injection was
considered; prompt injection was not.

**Acceptance criteria.** The hint reflects the actual provenance of the content,
and the threat is documented in `SECURITY.md`.

**Related.** `internal/tir/validation.go:127-130` forces
`safeForExploration: false` only for `navigation` and `submission`. A binding
with class `unknown` may still assert it is safe for exploration. Decide
whether "unknown" should be assertable as safe.

---

## C. Contract and layering

### GV-014 — Emitters are stricter than the TIR contract · S2

**Where.** `internal/emitter/schema.go:220-285` (`convertShape`) versus
`internal/tir/validation.go:224-282` and `schemas/tir.schema.json:153-201`.

**Evidence.** `convertShape` rejects arrays without `items`, objects carrying
enums, and primitives carrying enums. Neither `tir.Validate` nor the JSON Schema
enforces any of it.

**Impact.** A document can be canonically valid, schema-valid, and emitted as
`tir.json`, then fail with `unsupported_shape` on the MCP, OpenAI, and WebMCP
lanes. `--format all` fails after `--format tir` succeeded on identical input.

**Acceptance criteria.** Any document accepted by `tir.Validate` emits
successfully on every registered format, or the divergence is an explicit,
documented property of the contract.

### GV-015 — Name constraints enforced by one emitter only · S3

**Where.** `internal/emitter/openai.go:82-95` (`validateOpenAIName`);
`internal/emitter/mcp.go:59`; `internal/emitter/webmcp.go:67`.

**Evidence.** OpenAI validates `tool.ID` against `^[A-Za-z0-9_-]{1,64}$`. MCP
and WebMCP emit the same ID with no validation.

**Acceptance criteria.** Each emitter validates the identifier constraints of
its own target format, or the constraint is hoisted into TIR.

### GV-016 — Compiler does not validate its input boundary · S2

**Where.** `internal/compiler/compiler.go:601-613` (`compileParameter`),
`compiler.go:324-332` (action construction).

**Evidence.** The package defines `compiler.Error` "for malformed raw input" and
uses it for interaction kind, evidence score, frame index, and locator index —
but `parameter.Type`, `action.Kind`, and `sideEffect.Class` pass through
unchecked into TIR.

**Impact.** Bad input surfaces as
`validate compiled TIR: tools[3].parameters[1].type: ...` — a field path into
the *output* document, useless for locating the offending input.

**Acceptance criteria.** Every enum-valued field arriving from `observation` is
rejected at the compiler boundary with a `compiler.Error` whose `Field` names
the input path.

### GV-017 — `framePathKey` is ambiguous · S3

**Where.** `internal/tir/validation.go:346-352`.

**Evidence.** `fmt.Fprintf(&builder, "%d:%s:%s;", ...)` is used for canonical
ordering *and* duplicate detection. `Name` and `Src` are page-controlled and may
contain `:` and `;`, so distinct paths can collide and raise a spurious
`duplicate_frame_path`. `%d` also sorts index `10` before `2`.

**Note.** The compiler's own `joinedKey` (`internal/compiler/compiler.go:852-858`)
is length-prefixed and unambiguous — the right pattern, already in the codebase.

**Acceptance criteria.** No two distinct frame paths can produce the same key;
ordering is numeric on `index`.

### GV-018 — Tool IDs churn on unrelated DOM edits · S3

**Where.** `client/src/extractor.ts:279` (`fallbackIndex` from
`record.sourceOrder`); `internal/compiler/compiler.go:401-404` (ID = slug + digest).

**Evidence.** `sourceOrder` is a document-wide traversal counter. Inserting one
element anywhere shifts every downstream fallback name, and the name feeds tool
identity and therefore the ID hash.

**Impact.** Output is deterministic for a fixed DOM but unstable across trivial
page edits — which is what a consumer of a "stable ID" will actually rely on.

**Acceptance criteria.** Adding an unrelated element earlier in the document
does not change the IDs of unaffected tools; a test asserts this.

---

## D. Dead code and duplication

### GV-019 — `flattenFrameTree` is dead · S3

**Where.** `internal/browser/frames.go:253-283`. Production uses
`flattenCompleteFrameTree` (`frames.go:86`). The only caller is
`TestFlattenFrameTreePreservesTreeOrder`
(`internal/browser/source_test.go:142-177`).

**Acceptance criteria.** The dead function is removed and the live one has
equivalent unit coverage. See GV-037.

### GV-020 — `semanticLocatorKey` is unreferenced · S4

**Where.** `internal/compiler/compiler.go:817-826`. No callers anywhere.

### GV-021 — Locator-ID derivation duplicated · S3

**Where.** `internal/compiler/compiler.go:474-476` and `compiler.go:495-497`.

**Evidence.** `compileLocators` and `compileActions` each independently
recompute locator IDs via `stableIDs` with a duplicated key-builder closure.

**Impact.** If the two ever drift, action bindings silently reference locator
IDs that do not exist. Nothing enforces that they stay identical.

**Acceptance criteria.** Locator IDs are computed once and shared.

### GV-022 — Mutating helpers named as pure functions · S3

**Where.** `internal/compiler/compiler.go:997-1006` (`sortedUniqueStrings`);
`internal/tir/canonical.go:123-145` (`canonicalProvenance`, `sortedUnique`).

**Evidence.** All use the `values[:0]` in-place reuse trick. Every current call
site passes a freshly allocated slice, so the code is correct today.

**Acceptance criteria.** Either the helpers do not mutate their arguments, or
the contract is stated at each declaration.

### GV-023 — Duplicated derivation and validation · S4

`deriveMCPAnnotations` (`internal/emitter/mcp.go:84-102`) and
`deriveWebMCPAnnotations` (`internal/emitter/webmcp.go:89-107`) are near-identical.
`validateOutputCollisions` (`internal/cli/root.go:509-526`) duplicates
`validateFiles` (`internal/output/writer.go:70-89`) exactly.

---

## E. Reliability and smaller defects

### GV-024 — `WriteFiles` is not all-or-nothing · S2

**Where.** `internal/output/writer.go:29-30` (doc comment), `writer.go:58-66`
(replace loop).

**Evidence.** All files are staged, then replaced sequentially. A failure on
file three leaves files one and two already replaced. The comment says
"atomically replaces each file," true per-file and misleading in aggregate for a
package whose stated purpose is "writes generated artifacts without exposing
partial files."

**Acceptance criteria.** Either the operation is genuinely all-or-nothing, or
the package doc states the per-file guarantee precisely. A test covers a
mid-loop replace failure.

### GV-025 — No parent-directory fsync · S3

**Where.** `internal/output/writer.go:110-112`. `file.Sync()` is called, but the
directory is not synced after rename, so the durability effort is incomplete on
crash.

### GV-026 — `context.Context` not threaded · S3

**Where.** `internal/output/writer.go:31` (`WriteFiles`),
`internal/compiler/compiler.go:95` (`Compile`).

`.cursor/rules/go-quality.mdc` asks for `context.Context` first on I/O
boundaries. `WriteFiles` performs filesystem I/O; `Compile` is CPU-bound but is
the long pole on large pages.

### GV-027 — `http.DefaultClient` for endpoint resolution · S3

**Where.** `internal/browser/connection.go:61`. A package global with default
redirect-following, used against a caller-supplied endpoint. The go-quality rule
also asks to avoid package globals.

### GV-028 — Redundant JSON round-trip per emitter · S3

**Where.** `internal/emitter/emitter.go:115-128` (`canonicalDocument`). Full
marshal plus unmarshal of the whole document for every emitter; four round-trips
under `--format all`, with no semantic gain over `Clone` + `Normalize`.

### GV-029 — `Options.Strict` silently ignored · S3

**Where.** `internal/emitter/mcp.go:42`, `internal/emitter/webmcp.go:39` (both
discard `Options`). Silently ignoring a caller-supplied option is worse than
rejecting it.

### GV-030 — Extraction failures swallowed silently · S2

**Where.** `client/src/extractor.ts:86-92` (`read`), `extractor.ts:784-787`
(`catch {}` in `extract`).

**Evidence.** Every element-level failure is discarded with no counter, no
warning, and no evidence record.

**Impact.** For a project whose spec requires coverage gaps to be explicit
(`docs/product-spec.md:22-23`), silent per-element loss is a direct asymmetry:
frames report gaps, elements do not.

**Acceptance criteria.** Suppressed element failures surface as a count or
warning in the batch and reach the TIR `warnings` collection.

### GV-031 — Closed shadow roots produce no coverage record · S3

**Where.** `client/src/extractor.ts:342-343` (`shadowRoot?.mode === "open"`).
Closed roots are skipped with no equivalent of the uncovered-frame record that
frames get.

### GV-032 — Assorted small defects · S4

- `client/src/extractor.ts:620` — in
  `explicitRole(form) || (form.getAttribute("role") === "search" ? "search" : "form")`
  the middle branch is unreachable; if `role="search"`, `explicitRole` already
  returned `"search"`.
- `client/src/extractor.ts:450` — the 80-character cap is applied only on the
  non-numeric branch, so names starting with a digit are unbounded.
- `internal/emitter/webmcp.go:78` — the capacity calculation omits
  `len(webMCPPrefix)`, forcing a reallocation.
- `client/src/extractor.ts:83` — `slice(limit)` on UTF-16 code units can split a
  surrogate pair.

---

## F. Performance

### GV-033 — Unbounded selector-generation budget · S2

**Where.** `client/src/extractor.ts:315` (`timeoutMs: Number.MAX_SAFE_INTEGER`).

**Evidence.** `@medv/finder` is invoked once per interaction and once per shadow
path node, and verifies uniqueness with repeated `document.querySelectorAll`.
`traverse` already performs `querySelectorAll("*")` per root.

**Acceptance criteria.** A real per-call budget exists, and exceeding it
degrades to `simpleSelector` rather than consuming the frame budget.

### GV-034 — Frame timeout converts slow pages into mislabelled total loss · S2

**Where.** `internal/browser/frames.go:450-453`; reason strings at
`frames.go:491-494`.

**Evidence.** `frameTimeout` defaults to `max(3s, TimeoutMS+2s)` — 3 seconds with
default CLI flags. Exceeding it yields zero interactions for the frame and the
reason "browser lost the frame context during extraction," which describes a
different failure.

**Impact.** A page merely slow to extract reports as uncovered, with a diagnosis
that will send someone debugging the wrong thing.

**Acceptance criteria.** Extraction timeout is distinguishable from context loss
in the reason text, is separately configurable, and has a default proportionate
to the work in GV-033.

### GV-035 — Runtime rescans the DOM per semantic node · S3

**Where.** `internal/emitter/webmcp.go:120-126` (`__geovisorElements`),
`webmcp.go:180-192` (`__geovisorSemanticNode`).

`Array.from(root.querySelectorAll("*"))` followed by role *and* accessible-name
computation — including `textContent` — for every element, once per scope node
per resolution attempt.

---

## G. Testing

### GV-036 — The generated WebMCP runtime is never executed · S1

**Where.** `internal/integration/pipeline_test.go:165-178` (`node --check`,
syntax only); `internal/emitter/emitter_test.go:218-246`
(`TestWebMCPModuleSafetyAndRuntime`, substring search only).

**Impact.** GV-001, GV-003, and GV-004 all live in that runtime. Executing it
against the originating DOM would have caught all three.

**Acceptance criteria.** A test extracts from a DOM, emits WebMCP, executes the
module against the *same* DOM, and asserts every action resolves and applies.
`jsdom` is already a dev dependency.

### GV-037 — A test asserts on dead code · S2

`TestFlattenFrameTreePreservesTreeOrder`
(`internal/browser/source_test.go:142-177`) covers the dead `flattenFrameTree`
while the production `flattenCompleteFrameTree`, `mergeFrameOwnerOrder`,
`attachOOPIFSessions`, `addUnattachedOOPIF`, `augmentBatch`, and
`frameTraversalNodes` have no unit tests at all. This is false confidence, not
merely a gap.

### GV-038 — Test names overstate their assertions · S3

- `internal/emitter/emitter_test.go:218` — "Runtime" in the name; assertions
  cannot distinguish a working module from a file containing the same
  substrings.
- `internal/emitter/emitter_test.go:180-215` — calls the annotation helpers
  directly rather than through `Emit`, so wiring in `mcp.go:63` /
  `webmcp.go:69` could regress undetected.
- `internal/tir/types_test.go:41-58` — uses `encoding/json.Marshal`, not the
  production `tir.Marshal`, so the canonical path is not what is exercised.
- `client/test/extractor.test.mjs:121-136` — see GV-010.

### GV-039 — CI never requires a browser · S2

`.github/workflows/ci.yml:74` runs `go test ./...` without
`GEOVISOR_REQUIRE_BROWSER=1`. `internal/browser/integration_test.go:216-227` and
`internal/cli/integration_test.go:19-22` skip silently when no Chromium is
found. Skips are not failures, so the entire browser layer can stop being tested
without CI turning red. Locally, `internal/browser` finished in 2.7s — it
skipped.

### GV-040 — `WriteFiles` replace-phase failure untested · S3

`internal/output/writer_test.go:35-61` covers only staging failure. The
replace-phase partial state of GV-024, `MkdirAll` failure, empty-path rejection,
`Chmod` failure, and the OS-specific `replace_windows.go` / `replace_other.go`
split are all uncovered.

### GV-041 — CLI paths untested · S3

No coverage for `ExitGeneric` fallback (`internal/cli/errors.go:77`),
`validateDependencies` (`root.go:121-135`), the browser-config-to-usage remap
(`root.go:328-331`), `validateArtifactName` (`root.go:501-507`), `writeBytes`
short writes (`root.go:528-539`), unknown subcommand (`root.go:145-149`), or
most flag-bound validation (`root.go:258-291`).

### GV-042 — `tir` validation codes unasserted · S3

Defined in `internal/tir/validation.go` but never asserted in
`internal/tir/canonical_test.go`: `unsupported_version`,
`invalid_execution_boundary`, `inconsistent_complete_coverage`,
`inconsistent_unavailable_coverage`, `invalid_coverage_status`,
`duplicate_frame_path`, `duplicate_parameter_name`, `missing_locator_strategy`,
`duplicate_locator_reference`, `duplicate_enum_value`, and the empty-field
checks. A table-driven case per code would suit the go-quality rule.

### GV-043 — Unchecked assertions panic in tests · S4

`internal/integration/pipeline_test.go:146-147`,
`internal/emitter/emitter_test.go:110-111`, `internal/tir/types_test.go:60-70`.
Panics obscure which assertion failed.

---

## H. Tooling, CI, and documentation

### GV-044 — No linter beyond `go vet` · S3

`go vet` does not report unused unexported functions, which is why GV-019 and
GV-020 survived. `staticcheck` (`U1000`) or `golangci-lint` would catch both.

### GV-045 — Docs describe shipped adapters as future work · S3

`client/README.md:16-19` still says frame discovery belongs to "a future browser
source"; `docs/adr/0001-execution-boundary.md:12` and `docs/architecture.md:34`
use the future tense for source adapters. All are implemented
(`internal/browser/types.go:156-169`, `internal/cli/root.go:384-412`).

### GV-046 — Root `GEOVisor Plan` contradicts accepted ADRs · S3

It describes `visor inspect`, `--exploration-budget`, a Go host daemon,
`go-rod/stealth`, `map[string]any` TIR metadata, and Playwright-style locator
strings. Several of these are things ADR 0001 and ADR 0003 explicitly decided
against. Sitting at the repository root, it invites someone to build from it —
which `AGENTS.md` forbids.

### GV-047 — Local gate is stronger than the CI gate · S3

`scripts/check.sh:20-25` runs `npm run check`, `go test`, and
`./scripts/verify-release.sh`. `.github/workflows/ci.yml:55-74` omits
`verify-release`. `CONTRIBUTING.md:64-67` and `docs/releasing.md:3-7` present
`check.sh` as the gate.

### GV-048 — `paths-ignore` can deadlock docs-only PRs · S4

`.github/workflows/ci.yml:18-28` skips `pull_request` runs for `docs/**` and
`**/*.md`. A docs-only PR produces no check run, so if branch protection
requires the `Quality` job it can never be satisfied.

---

## Cross-cutting root cause

GV-001, GV-003, and GV-004 are not three unrelated bugs. The extractor
(`client/src/extractor.ts`) and the generated runtime
(`internal/emitter/webmcp.go`) are **two independent implementations of role
resolution, accessible-name computation, and element addressing**, and no test
runs them against each other. GV-036 is why the divergence was invisible.

Fixing the three individually without closing that loop will let the next
divergence through. **GV-036 is the highest-leverage single item in this
document** — the round-trip test it describes fails on all three today and
prevents the class going forward.

## Suggested sequencing

Ordering reflects dependency and leverage, not just severity.

**Phase 1 — close the feedback loop.** GV-036 first; it is the harness the rest
of Phase 2 is verified against. Add GV-044 in the same pass to sweep GV-019 and
GV-020 automatically. Set `GEOVISOR_REQUIRE_BROWSER=1` in CI (GV-039) so the
browser layer stops being optional.

**Phase 2 — make generated tools work.** GV-001, GV-002, GV-003, GV-004, then
GV-005 and GV-007. Each should land with a case in the GV-036 harness.

**Phase 3 — safety and honesty.** GV-008 and GV-009 together, since the fix for
one determines the shape of the other. Then GV-011, GV-012, GV-013, GV-030.
These change documented guarantees, so `docs/product-spec.md` and `README.md`
move with them.

**Phase 4 — contract integrity.** GV-014 and GV-016 together; both are about
putting validation at the layer that owns the invariant. Then GV-017, GV-018,
GV-015.

**Phase 5 — resilience and performance.** GV-033 and GV-034 as a pair; the
correct frame timeout depends on the extraction budget. Then GV-024, GV-006.

**Phase 6 — hygiene.** Remaining S3/S4 items, GV-042 and GV-041 test backfill,
and the documentation set GV-045 through GV-048.

## Confirmed sound — do not regress

Worth listing explicitly so remediation does not erode it.

- Package boundaries hold. `internal/tir` imports no browser or emitter code;
  `internal/observation` is genuinely library-neutral.
- The canonical marshal path is careful and correctly ordered:
  clone → normalize → canonicalize → validate → marshal
  (`internal/tir/canonical.go:13-27`), operating on a clone so caller data is
  untouched.
- `TestPayloadCompilerEmitterPipeline`
  (`internal/integration/pipeline_test.go:34-63`) reverses input order and
  demands byte-identical output. That is the right shape of determinism test.
- `assertNoSensitiveValues` (`pipeline_test.go:243-250`) checks artifacts for
  sentinel secrets from the corpus. Good instinct, worth extending.
- The committed bundle is reproducibly built and staleness-checked
  (`client/scripts/build.mjs:30-39`) with exactly pinned esbuild.
- Emitted JS escaping is real and tested — the golden fixture carries
  `</script>` and U+2028 payloads.
- Typed, code-carrying errors at every package boundary (`browser.Error`,
  `emitter.Error`, `tir.ValidationError`, `compiler.Error`) with `errors.As`
  helpers.
- `tsconfig.json` enables `strict`, `noUncheckedIndexedAccess`, and
  `exactOptionalPropertyTypes`. No `any` in `client/src`.
- Launch mode is properly isolated: temp profile, `site-per-process` preserved,
  `disable-site-isolation-trials` explicitly deleted, correct defer ordering for
  browser cleanup before profile removal.
- CI is multi-OS with `-race`, `govulncheck`, `npm audit`, and least-privilege
  `permissions: contents: read`.

## Appendix — reproducing the verified findings

Requires `npm ci` in the repository root. Run with `node` from the repository
root against the committed bundle at `internal/payload/extractor.js`.

```js
const root = "<repo-root>";
const { JSDOM } = await import(`file:///${root}/node_modules/jsdom/lib/api.js`);
const bundle = await (await import("node:fs/promises"))
  .readFile(`${root}/internal/payload/extractor.js`, "utf8");

function page(html) {
  const dom = new JSDOM(html, { runScripts: "outside-only", url: "https://example.test/" });
  dom.window.eval(bundle);
  return dom;
}

// GV-002: all three inputs are emitted; only the last is actually visible.
const hidden = page(`
  <div style="display:none"><input aria-label="A"></div>
  <div aria-hidden="true"><input aria-label="B"></div>
  <input aria-label="C">`);
console.log((await hidden.window.__GEOVISOR_EXTRACT__()).interactions.map((i) => i.name));

// GV-001: enum is ["Basic","Pro"], but assigning "Basic" to .value yields "".
const sel = page(`<select aria-label="Plan"><option value="basic-id">Basic</option></select>`);
const batch = await sel.window.__GEOVISOR_EXTRACT__();
const el = sel.window.document.querySelector("select");
el.value = batch.interactions[0].parameters[0].enum[0];
console.log(JSON.stringify(el.value)); // ""  -> runtime throws

// GV-003: index 0 matches two iframes, index 1 matches none.
const frames = page(`<div><iframe name="a"></iframe></div><div><iframe name="b"></iframe></div>`);
for (const i of [0, 1]) {
  const s = `:is(iframe, frame):nth-child(${i + 1} of iframe, frame)`;
  console.log(i, frames.window.document.querySelectorAll(s).length);
}

// GV-004: locator name is "Query (2)"; no such accessible name exists.
const dup = page(`<section role="region" aria-label="F">
  <input aria-label="Query"><input aria-label="Query"></section>`);
const ctls = (await dup.window.__GEOVISOR_EXTRACT__()).interactions
  .filter((i) => i.kind === "control");
console.log(ctls.map((c) => c.locators[0].semantic.name));

// GV-005 / GV-007
console.log((await page(`<div contenteditable="false" aria-label="X">x</div>`)
  .window.__GEOVISOR_EXTRACT__()).interactions.map((i) => `${i.kind}/${i.role}`));
console.log((await page(`<form aria-label="L"><input aria-label="U">
  <button type="submit">Go</button></form>`)
  .window.__GEOVISOR_EXTRACT__()).interactions.map((i) => `${i.kind}:${i.name}`));
```

GV-006 reproduces from a temporary test in `internal/browser`:

```go
func TestScratch(t *testing.T) {
	t.Log(sanitizeText(
		"cannot attach to target: a page was already claimed by another agent",
		"http://localhost:9222/?a=1&user=bob"))
}
```
