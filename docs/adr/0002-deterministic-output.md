# ADR 0002: Deterministic Canonical TIR

Status: Accepted

## Context

Artifacts need stable diffs, reproducible tests, and consistent emitter input.
Wall-clock values, random IDs, null collections, and unordered maps undermine
those properties.

## Decision

Canonical TIR excludes generated timestamps and random IDs. DTOs use ordered
slices, and producers normalize every collection to a non-null array before
serialization. Stable IDs, when needed, must be derived from stable input.

## Consequences

Run metadata belongs in a separate envelope if introduced later. Producers own
stable ordering; normalization does not sort or invent identity.
