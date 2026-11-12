# Architecture

## Data flow

The intended flow is:

1. An agent selects a launch or same-session CDP attach `BrowserSource`.
2. The source observes one browser session and reports frame coverage.
3. The embedded browser payload produces browser-independent observations.
4. The compiler produces normalized TIR.
5. TIR is validated and serialized as the canonical artifact.
6. A selected emitter translates TIR without changing its meaning.

## Package boundaries

- `cmd/geovisor`: process entry point.
- `internal/cli`: argument shell, exit-code policy, and stream ownership.
- `internal/browser`: URL launch and CDP attach source adapters. Sources
  assemble `compiler.Input` (`observation.Source` plus batches).
- `internal/pageurl`: origin+path URL normalization; no browser or emitter
  imports.
- `internal/payload`: committed embedded browser extraction payload.
- `internal/observation`: browser-independent extraction facts.
- `internal/compiler`: deterministic observation-to-TIR aggregation.
- `internal/tir`: canonical DTOs, normalization, and cross-field validation.
- `internal/emitter`: TIR, MCP, OpenAI, and binding artifacts.
- `internal/output`: per-file atomic replace with call-scoped rollback if a
  later replace, chmod, or parent-directory sync fails.
- `internal/version`: development default and release version injection point.
- `schemas`: language-neutral JSON contract.

Browser sources assemble `compiler.Input` and depend on observations. The
compiler depends on observations and TIR, and emitters depend only on TIR. TIR
does not import browser automation, protocol, or emitter-specific types.

## Process and streams

Browser execution belongs to the calling agent and its session. GEO-Visor
accepts browser access through explicit source adapters; it does not expose a
long-running host MCP daemon. Standard output is reserved for artifacts.
Human-readable help, version output, and diagnostics use standard error.

## Contract invariants

Canonical DTOs use ordered slices instead of maps. `NewDocument` and
`Document.Normalize` ensure non-null collections. `Document.Validate` and
`schemas/tir.schema.json` share shape, tool-ID pattern, coverage, and
unsafe-exploration constraints: type-consistent parameter shapes, tool IDs that
match `^[A-Za-z0-9_-]{1,64}$`, explicit partial frame coverage, and the
prohibition on safe exploration of navigation, submission, or unknown.
`schemas/tir.schema.json` checks shape. Cross-field uniqueness and references
are enforced by Go `tir.Validate` and `canonical` only: duplicate tool ids,
duplicate parameter names, duplicate property names, globally unique locator
ids, `inputParameter` and `locatorCandidateIds` referencing the same tool,
`warnings[].toolId` referencing an existing tool, and length-prefixed
`framePathKey` uniqueness. That uniqueness key includes the frame name. Tool
identity does not: it uses the frame index plus `pageurl.PageURL` of the frame
source. `PageURL` is idempotent for `data:`, `blob:`, `mailto:`, and
`javascript:`. A whitespace-only `cssFallback` is rejected by both
`tir.Validate` and the schema. A document that passes `tir.Validate` emits on
every registered format.
