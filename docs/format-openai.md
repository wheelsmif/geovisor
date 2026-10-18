# OpenAI function tools

Format name: `openai`

The emitter uses the unwrapped OpenAI Responses API function-tool array shape
documented on `2026-09-04`, conforming to
`schemas/openai-function-tools.schema.json`.

This fragment is the `read_only` tool from
`internal/emitter/testdata/openai-nonstrict.golden`, with the golden's second
tool, `complex_tool`, omitted:

```json
[{"type":"function","name":"read_only","description":"Inspect a local control.","parameters":{"type":"object","properties":{},"additionalProperties":false},"strict":false}]
```

`name` is the TIR tool id, not a display name with underscores. Compiler
family link tools use `follow-link-` plus the 12-hex-character digest described
in `docs/compiler.md`. The emitter golden uses the fixture ids `read_only` and
`complex_tool`. A compiler golden id of the same shape is `search-2880204d3af8`
in `internal/compiler/testdata/compiler.golden.json`.

When the TIR description is empty or whitespace, the function `description`
falls back to the tool's display name (`tool.Name` in
`internal/emitter/openai.go`). Both tools in the golden have descriptions, so
that fallback is not in the file.

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
`schemas/emitter-bindings.schema.json` at bindings schema version `1.1.0`.
Those recipes are not directly executable. `client/src/apply-runtime.ts` is
the in-repo test interpreter and is not shipped with `geovisor`. An
agent-owned browser executor must interpret the recipes. GEO-Visor does not
add an MCP host or any other execution service. Schema `$id` values under
`https://wheelsmif.github.io/geovisor/schemas/` are identifiers; nothing
publishes those pages. See `docs/format-tir-json.md`.
