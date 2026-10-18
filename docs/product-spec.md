# GEO-Visor Product Specification

## Purpose

GEO-Visor is a compiler with one `inspect` command. It converts
browser-observed interfaces into a canonical, deterministic Tool Intermediate
Representation (TIR). Separate emitters translate TIR to static MCP or OpenAI
tool catalogs. GEO-Visor does not host an MCP server and does not ship an
executor.

## Product boundaries

- `geovisor` is the primary Go executable.
- Browser execution is agent-owned; GEO-Visor does not host an MCP daemon.
- Launching a URL and attaching to an existing same-session CDP endpoint are
  browser-source adapters.
- Browser orchestration and extraction remain at the source edge; MCP and
  OpenAI remain emitter edges around canonical TIR.

## Safety

- Safe exploration, when enabled, sets `<details open>`, yields so reveal
  handlers can run, and restores each element's prior `open` state. It does
  not consult `safeForExploration`. It never navigates or submits. `--depth 0`
  and `--max-operations 0` are sent as JSON zeros and perform no exploration.
  Bindings classified `unknown` still cannot be marked safe: the schema
  requires `safeForExploration` to be false for `navigation`, `submission`,
  and `unknown`.
- Observation-side JavaScript runs in an isolated world, not the page's main
  world.
- Attach mode attaches debugger sessions only to the selected target and its
  descendant frames. `--target active` requires a single top-level HTTP(S)
  page or an explicit `id:` / `url:` selector.
- Stealth behavior is disabled unless explicitly requested.
- Missing or inaccessible frames produce `partial` coverage plus explicit
  uncovered-frame records. Closed shadow roots and suppressed element-level
  extraction failures are reported as warnings. A frame that exceeds its
  extraction deadline is uncovered for that reason, not reported as a lost
  browsing context. When `--frame-timeout` is omitted, the default is the
  exploration budget plus a 2 second selector allowance plus 2 seconds of
  frame overhead. The default exploration budget is 1 second, so the default
  frame deadline is 5 seconds. An explicit value must be at least the
  exploration timeout.

## TIR contract

- TIR is versioned and emitter-agnostic.
- `tir.Validate` and `schemas/tir.schema.json` are the single validity gate
  for shape and identifier constraints: every document they accept emits on
  every registered format.
- Parameter shapes are type-consistent: arrays have items, objects do not carry
  string enums or items, and primitives do not carry items, properties, or
  string enums.
- Tool IDs match `^[A-Za-z0-9_-]{1,64}$` so every emitter can use them as
  function names.
- Locator candidates model frame traversal, shadow traversal, semantic scope,
  role, accessible name, and an optional CSS fallback. CSS is not part of tool
  identity. Frame identity is the frame index plus a normalized page URL, not
  the frame name. `data:` and `blob:` contribute the scheme only.
  `about:srcdoc` stays `about:srcdoc`. An element id becomes a `#id` fallback
  and does not call `@medv/finder`. Otherwise finder runs with `timeoutMs`
  Infinity and a cap of 5000 path checks. There is no wall-clock abort. When
  that search does not return a unique match, the fallback is the tag, with
  `:nth-of-type` when the tag is repeated among siblings.
- Action bindings carry side-effect classification and `locatorSelection`.
  `ranked` means the locator candidates are ordered fallbacks for one element.
  `by_target_name` means the parameter value selects one candidate, and the
  candidates line up with the enum in document order. The apply test harness
  branches on that field, not on the parameter name.
- Confidence, provenance, and warnings remain structured data.
- Core artifacts contain no generated timestamps, random IDs, or run-specific
  metadata by default.
- Page URLs in TIR go through `pageurl.PageURL`. Hierarchical URLs keep
  scheme, host, and path. Query strings and fragments are stripped from
  source URLs, frame coverage URLs, and frame `src` values. `data:` and
  `blob:` keep the scheme only. `about:srcdoc` and `about:blank` stay those
  tokens.
- A form tool is fill-only. Submit controls are standalone actions.
- Same-role, same-action families in one frame compile as one parameterized
  TIR tool with a required `target` parameter, in document order. The first
  control keeps the bare accessible name; later duplicates are `Name (2)`,
  `Name (3)`. Descriptions are `Follow one of this page's links`,
  `Toggle one of this page's controls`, or `Click one of this page's controls`.
  Parameter descriptions can still list the target names. Links always use
  that family shape. Unique buttons and submit/reset/image-submit stay
  standalone. A form with no fillable controls is skipped.
- Low-value actions are omitted using the `citationHref` fact and input
  `inputType`, not a regular expression over CSS. A button id of `fn-save` is
  kept. Citation-mark names, DOI/RFC tokens, and unnamed links are still
  omitted. Collapsed link tools keep `sideEffect.class = navigation` and
  `safeForExploration = false`.

## Determinism

Producers preserve stable source order, derive any IDs from stable content, call
`Normalize` before serialization, and avoid maps in canonical DTOs.
`tir.Canonical` does not sort tools or enums. Document and
tool required collections (`tools`, `warnings`, `locatorCandidates`,
`actionBindings`, `parameters`, and coverage arrays) serialize as arrays,
including when empty. Empty optional `description`, `enum`, and `properties`
fields are omitted. `Normalize` still replaces nil slices with empty slices in
memory. Tool IDs are stable across unrelated earlier DOM edits: positional
fallback names, display name ordinals, and CSS selectors are not part of
identity. Family-tool IDs digest family identity, not each member's href.

## Artifact writes

`--output` and `--format all` stage every artifact before replacing
destinations. Each file is replaced atomically; a mid-call replace failure
restores files this invocation already replaced so a directory is not left
holding a mix of old and new artifacts.
