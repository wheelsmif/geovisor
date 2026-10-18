# Canonical TIR JSON

Format name: `tir-json`

The CLI accepts `--format tir` as an alias for this format name.

The canonical JSON emitter delegates directly to `tir.Marshal`. Documents use
TIR schema version `1.2.0`, the `SchemaVersion` constant in
`internal/tir/types.go` and the `schemaVersion` const in
`schemas/tir.schema.json`. Binding companions use schema version `1.1.0`
(`bindingSchemaVersion` in `internal/emitter/bindings.go` and
`schemas/emitter-bindings.schema.json`).

`tir.Canonical` and `tir.Marshal` clone, normalize, deduplicate, and validate.
They do not sort tools, parameters, actions, or enum values. The compiler
emits those collections in source order, and producers own that order
(ADR 0002). A select enum therefore stays `Small`, `Medium`, `Large`. Output
is compact JSON with no trailing newline. Empty optional `description`,
`enum`, and `properties` are omitted; required document and tool collections
remain arrays. This format does not produce a companion artifact.

`internal/emitter/testdata/tir.golden` begins with `"schemaVersion":"1.2.0"`.
The rest of that file is a two-tool fixture and is not repeated here.

The `$id` values in `schemas/*.json` use the prefix
`https://wheelsmif.github.io/geovisor/schemas/`. Those URLs are identifiers.
Nothing publishes those pages.

The emitter is pure: it does not mutate TIR, write streams, add timestamps, or
generate identifiers. Invalid TIR is returned as a typed emitter error that
wraps the original TIR validation error.
