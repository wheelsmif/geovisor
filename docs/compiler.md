# Deterministic TIR compiler

`internal/observation` is the browser-independent input boundary.
`internal/compiler.Compile` accepts all observation batches for one session and
produces canonical TIR. Batch order and unordered input slices do not affect the
result. Parameter and object-property source order is preserved only when an
explicit `SourceOrder` is present; otherwise names provide canonical order.

Interaction identity consists of interaction kind, frame path, semantic scope,
role, and normalized accessible name. Locator content is also included when a
name must be inferred. This deduplicates repeated observations without merging
equivalent-looking controls in different frames or semantic scopes. IDs use an
ASCII semantic slug plus the first 48 bits of SHA-256 over length-delimited
canonical identity. If those complete IDs collide, identities are sorted and
receive deterministic numeric suffixes.

Evidence is deduplicated by provenance kind and reference, retaining the
highest reported score. Confidence is the noisy-or of the sorted unique scores,
rounded to six decimal places. Missing evidence receives a `0.5` heuristic
fallback. Conflicting side-effect reports select the most conservative class;
safe exploration requires every report to be safe and is always false for
navigation and submission.

Coverage is `unavailable` when no batch reports it, `partial` when any known
frame is inaccessible, and `complete` otherwise. Conflicting accessibility
reports are treated conservatively as uncovered. Every fallback, ambiguity,
and uncovered frame produces a structured warning.

`tir.Marshal` and `tir.Write` deep-clone, normalize, canonically order, and
validate before emitting compact JSON. Both omit a trailing newline and never
mutate caller-owned documents.
