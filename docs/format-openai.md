# OpenAI function tools

Format name: `openai`

The emitter uses the unwrapped OpenAI Responses API function-tool array shape
documented on `2026-09-04`, conforming to
`schemas/openai-function-tools.schema.json`:

```json
[{"type":"function","name":"follow_link","description":"target is the control's accessible name","parameters":{"type":"object","properties":{"target":{"type":"string","enum":["CSS","HTML"]}},"required":["target"],"additionalProperties":false},"strict":false}]
```

This is not the nested Chat Completions
`{"type":"function","function":{...}}` representation.

Non-strict mode preserves the TIR required list exactly, so optional properties
may be omitted. Strict mode follows the current Structured Outputs
constraints: every property at every object level is listed in `required`,
every object sets `additionalProperties` to `false`, and originally optional
properties add `null` to their `type`. Shape and identifier constraints live
on TIR, so a valid document always emits; the OpenAI emitter still checks the
same function-name pattern as defense in depth.

The standard function definitions contain no browser bindings or GEO-Visor
extensions. Structured locator and action recipes are written separately as
`tools.openai.bindings.json`, conforming to
`schemas/emitter-bindings.schema.json`. Those recipes are not directly
executable. `client/src/apply-runtime.ts` is the in-repo test interpreter and
is not shipped with `geovisor`. An agent-owned browser executor must interpret
the recipes. GEO-Visor does not add an MCP host or any other execution service.
