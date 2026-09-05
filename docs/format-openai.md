# OpenAI function tools

Format name: `openai`

The emitter uses the unwrapped OpenAI Responses API function-tool array shape
documented on `2026-09-04`:

```json
[{"type":"function","name":"example","parameters":{"type":"object","properties":{},"additionalProperties":false},"strict":false}]
```

This is not the nested Chat Completions
`{"type":"function","function":{...}}` representation.

Non-strict mode preserves the TIR required list exactly, so optional properties
may be omitted. Strict mode follows the current Structured Outputs
constraints: every property at every object level is listed in `required`,
every object sets `additionalProperties` to `false`, and originally optional
properties add `null` to their `type`. Shapes that cannot be represented by
the supported subset are rejected with a typed error rather than weakened.
Function names must match `^[A-Za-z0-9_-]+$` and contain at most 64 characters.

The standard function definitions contain no browser bindings or GEO-Visor
extensions. Structured locator and action recipes are written separately as
`tools.openai.bindings.json`, conforming to
`schemas/emitter-bindings.schema.json`. OpenAI does not execute those recipes;
an agent-owned browser executor must interpret them. GEO-Visor does not add an
MCP host or any other execution service.
