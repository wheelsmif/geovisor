# MCP tools/list

Format name: `mcp`

The emitter targets the Model Context Protocol revision `2026-07-28` and
produces the standard `ListToolsResult` object, conforming to
`schemas/mcp-tools-list-2026-07-28.schema.json`.

This fragment is the `read_only` tool from
`internal/emitter/testdata/mcp.golden`, with the golden's second tool,
`complex_tool`, omitted:

```json
{"tools":[{"name":"read_only","title":"Read only","description":"Inspect a local control.","inputSchema":{"type":"object","properties":{},"additionalProperties":false},"annotations":{"readOnlyHint":false,"destructiveHint":false,"idempotentHint":false,"openWorldHint":false}}]}
```

`read_only` is a click with side-effect class `none`. In the same golden,
`complex_tool` includes a `submission` action, and its annotations set
`destructiveHint` and `openWorldHint` to true. `name` is the TIR tool id and
`title` is the display name. The golden's `read_only` and `complex_tool` are
fixture ids. Compiler-emitted ids use a slug plus a 12-character digest, as in
`search-2880204d3af8`, and a link family id looks like `follow-link-` plus
that digest.
A form with no fillable controls is skipped, and `tir.Validate` rejects a tool
with zero actions, so catalogs do not contain an empty tool.

The artifact is a static tools/list result, not a JSON-RPC response and not an
MCP server. GEO-Visor deliberately provides no MCP host runtime and no
`tools/call`. It omits `outputSchema` because TIR does not define tool output
contracts.

Input schemas use JSON Schema 2020-12 semantics (the default dialect for this
MCP revision). Object schemas recursively set `additionalProperties` to
`false` and preserve TIR property order. `Options.Strict` is honored the same
way as the OpenAI lane: every property becomes required, and originally
optional properties add `null` to their `type`. Non-strict emission retains
the TIR required list exactly.

Annotations are conservative aggregates over every action. A tool is
destructive when any action is:

- `readOnlyHint` is false for every tool `tir.Validate` accepts, because a
  tool must contain at least one action and every action clears the hint.
- `destructiveHint` is false for side-effect classes `none` and `local_state`,
  and true for `navigation`, `submission`, `network`, and `unknown`.
- `idempotentHint` follows `readOnlyHint`, so it is false on every valid tool.
- `openWorldHint` is true for network, navigation, submission, or unknown
  effects.

The standard tool definitions contain no GEO-Visor extension fields. Browser
locator and action recipes are written separately as
`tools.mcp.bindings.json`, conforming to
`schemas/emitter-bindings.schema.json` at bindings schema version `1.1.0`.
Those recipes require an agent-owned browser executor and are not directly
executable. `client/src/apply-runtime.ts` is the in-repo test interpreter and
is not shipped with `geovisor`. Schema `$id` values under
`https://wheelsmif.github.io/geovisor/schemas/` are identifiers; nothing
publishes those pages. See `docs/format-tir-json.md`.
