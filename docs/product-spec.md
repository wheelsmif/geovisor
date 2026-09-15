# GEO-Visor Product Specification

## Purpose

GEO-Visor converts browser-observed interfaces into a canonical, deterministic
Tool Intermediate Representation (TIR). Separate emitters translate TIR to MCP
or OpenAI tool formats.

## Product boundaries

- `geovisor` is the primary Go executable.
- Browser execution is agent-owned; GEO-Visor does not host an MCP daemon.
- Launching a URL and attaching to an existing same-session CDP endpoint are
  browser-source adapters.
- Browser orchestration and extraction remain at the source edge; MCP and
  OpenAI remain emitter edges around canonical TIR.

## Safety

- Safe exploration may inspect and interact only when an action is classified
  safe; it must never navigate or submit. Bindings classified `unknown` cannot
  be marked safe to explore.
- When enabled, safe exploration may open `<details>` elements, yields so
  reveal handlers can run, and restores each element's prior `open` state
  before returning. `--depth 0` performs no exploration.
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
  browsing context. `--frame-timeout` is independent of `--exploration-timeout`
  and defaults to the exploration budget plus a selector-generation allowance.

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
  role, accessible name, and an optional CSS fallback. Selector generation is
  time-bounded and degrades to a simple unique selector when the budget is
  exhausted.
- Action bindings carry side-effect classification.
- Confidence, provenance, and warnings remain structured data.
- Core artifacts contain no generated timestamps, random IDs, or run-specific
  metadata by default.
- Page URLs in TIR are origin+path only. Query strings and fragments are
  stripped from source URLs, frame coverage URLs, and frame `src` values.
- A form tool is fill-only. Submit controls are standalone actions.
- Same-role, same-action families in one frame compile as one parameterized
  TIR tool with a required `target` parameter. Links always use that family
  shape. Unique buttons and submit/reset/image-submit stay standalone.
- Low-value citation, DOI/RFC, and same-document `#cite` / `#fn` actions are
  omitted. Collapsed link tools keep `sideEffect.class = navigation` and
  `safeForExploration = false`.

## Determinism

Producers preserve stable source order, derive any IDs from stable content, call
`Normalize` before serialization, and avoid maps in canonical DTOs. Document and
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
