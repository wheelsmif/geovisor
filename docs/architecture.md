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
- `internal/browser`: URL launch and CDP attach source adapters.
- `internal/payload`: committed embedded browser extraction payload.
- `internal/observation`: browser-independent extraction facts.
- `internal/compiler`: deterministic observation-to-TIR aggregation.
- `internal/tir`: canonical DTOs, normalization, and cross-field validation.
- `internal/emitter`: TIR, WebMCP, MCP, OpenAI, and binding artifacts.
- `internal/output`: staged file replacement.
- `internal/version`: development default and release version injection point.
- `schemas`: language-neutral JSON contract.

Browser sources depend on observations, the compiler depends on observations
and TIR, and emitters depend only on TIR. TIR does not import browser
automation, protocol, or emitter-specific types.

## Process and streams

Browser execution belongs to the calling agent and its session. GEO-Visor will
accept browser access through explicit source adapters; it will not expose a
long-running host MCP daemon. Standard output is reserved for artifacts.
Human-readable help, version output, and diagnostics use standard error.

## Contract invariants

Canonical DTOs use ordered slices instead of maps. `NewDocument` and
`Document.Normalize` ensure non-null collections. `Document.Validate` enforces
invariants that JSON Schema cannot represent, including explicit partial frame
coverage and the prohibition on safe exploration of navigation or submission.

See `docs/adr` for the decisions that establish these boundaries.
