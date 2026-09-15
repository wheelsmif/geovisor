# Canonical TIR JSON

Format name: `tir-json`

The CLI accepts `--format tir` as an alias for this format name.

The canonical JSON emitter delegates directly to `tir.Marshal`. It therefore
uses TIR schema version `1.1.0` as defined by `schemas/tir.schema.json`,
canonical collection ordering, compact JSON, and no trailing newline. Empty
optional `description`, `enum`, and `properties` are omitted; required document
and tool collections remain arrays. It does not produce a companion artifact.

The emitter is pure: it does not mutate TIR, write streams, add timestamps, or
generate identifiers. Invalid TIR is returned as a typed emitter error that
wraps the original TIR validation error.
