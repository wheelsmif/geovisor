# GEO-Visor Agent Guide

## Intent

Build a deterministic browser-to-contract pipeline around canonical TIR. Keep
browser sources and output emitters at the edges.

## Boundaries

- Browser execution is owned by the calling agent and session.
- Do not add a host MCP daemon.
- Keep TIR independent of browser libraries and emitter formats.
- Treat URL launch and same-session CDP attach as source adapters.
- Treat MCP and OpenAI as emitters.
- Use structured locator candidates: frame path, shadow path, semantic
  scope/role/name, and optional CSS fallback.
- Do not consult archived or superseded implementation trees.

## Safety and determinism

- Safe exploration must not submit or navigate.
- Stealth behavior must be explicit and opt-in.
- Report inaccessible frames as partial coverage, including uncovered paths
  and reasons; never hide gaps.
- Do not add generated timestamps, random IDs, or unordered maps to core TIR.
- Normalize TIR collections before serialization.
- Reserve stdout for artifacts and stderr for diagnostics.

## Quality

- Prefer the smallest interface that preserves these contracts.
- Use typed errors at package boundaries.
- Add tests for contract and safety invariants.
- Run `gofmt`, `go test ./...`, and `go vet ./...` for Go changes.
- Run `scripts/check.ps1` or `scripts/check.sh`.
- After client changes, run `npm run build` and commit
  `internal/payload/extractor.js`.
- Keep `schemas/*.json` in sync with the Go DTOs.

Keep changes aligned with `docs/product-spec.md`.
