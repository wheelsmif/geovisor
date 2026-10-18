# ADR 0004: Browser Safety and Frame Coverage

Status: Accepted

## Context

Exploration can trigger irreversible page behavior, and browser security
boundaries can make some frames inaccessible.

## Decision

Safe exploration only sets `<details open>` and restores the previous `open`
state. It does not consult `safeForExploration`, and it does not navigate or
submit. `unknown`, `navigation`, and `submission` still cannot be marked safe
in the schema. Stealth is opt-in. Frame discovery reports `complete`,
`partial`, or `unavailable`; partial coverage must identify every known
uncovered frame and a reason.

## Consequences

Unknown effects still cannot be marked safe for exploration. Consumers can
distinguish incomplete evidence from an empty page because partial coverage
names every uncovered frame and a reason.
