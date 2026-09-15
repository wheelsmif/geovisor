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
Length-prefixed `framePathKey` uniqueness is enforced by Go `tir.Validate` and
`canonical` only; the JSON Schema does not implement those keys. A document
that passes validation emits on every registered format.

See `docs/adr` for the decisions that establish these boundaries.
