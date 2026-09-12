# GEO-Visor Product Specification

## Purpose

GEO-Visor converts browser-observed interfaces into a canonical, deterministic
Tool Intermediate Representation (TIR). Separate emitters will translate TIR to
WebMCP, MCP, or OpenAI tool formats.

## Product boundaries

- `geovisor` is the primary Go executable.
- Browser execution is agent-owned; GEO-Visor does not host an MCP daemon.
- Launching a URL and attaching to an existing same-session CDP endpoint are
  browser-source adapters.
- Browser orchestration and extraction remain at the source edge; WebMCP, MCP,
  and OpenAI remain emitter edges around canonical TIR.

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
  extraction failures are reported as warnings.

## TIR contract

- TIR is versioned and emitter-agnostic.
- Locator candidates model frame traversal, shadow traversal, semantic scope,
  role, accessible name, and an optional CSS fallback.
- Action bindings carry side-effect classification.
- Confidence, provenance, and warnings remain structured data.
- Core artifacts contain no generated timestamps, random IDs, or run-specific
  metadata by default.

## Determinism

Producers preserve stable source order, derive any IDs from stable content, call
`Normalize` before serialization, and avoid maps in canonical DTOs. All JSON
collections serialize as arrays, including empty collections.
