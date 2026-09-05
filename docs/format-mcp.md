# MCP tools/list

Format name: `mcp`

The emitter targets the Model Context Protocol revision `2026-07-28` and
produces the standard `ListToolsResult` object:

```json
{"tools":[{"name":"example","title":"Example","inputSchema":{"type":"object","properties":{},"additionalProperties":false},"annotations":{"readOnlyHint":true,"destructiveHint":false,"idempotentHint":true,"openWorldHint":false}}]}
```

The artifact is a static tools/list result, not a JSON-RPC response and not an
MCP server. GEO-Visor deliberately provides no MCP host runtime. It omits
`outputSchema` because TIR does not define tool output contracts.

Input schemas use JSON Schema 2020-12 semantics (the default dialect for this
MCP revision). Object schemas recursively set `additionalProperties` to
`false`, preserve TIR property order, and retain exact required fields.

Annotations are conservative aggregates over every action:

- `readOnlyHint` is true only when every action has side effect `none`.
- `destructiveHint` is true for any non-read-only tool.
- `idempotentHint` is true only for read-only tools.
- `openWorldHint` is true for network, navigation, submission, or unknown
  effects.

The standard tool definitions contain no GEO-Visor extension fields. Browser
locator and action recipes are written separately as
`tools.mcp.bindings.json`, conforming to
`schemas/emitter-bindings.schema.json`. Those recipes require an agent-owned
browser executor and are not directly executable.
