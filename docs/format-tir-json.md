# Canonical TIR JSON

Format name: `tir-json`

The canonical JSON emitter delegates directly to `tir.Marshal`. It therefore
uses TIR schema version `1.0.0`, canonical collection ordering, compact JSON,
and no trailing newline. It does not produce a companion artifact.

The emitter is pure: it does not mutate TIR, write streams, add timestamps, or
generate identifiers. Invalid TIR is returned as a typed emitter error that
wraps the original TIR validation error.
