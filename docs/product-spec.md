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
  safe; it must never navigate or submit.
- Stealth behavior is disabled unless explicitly requested.
- Missing or inaccessible frames produce `partial` coverage plus explicit
  uncovered-frame records.

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
