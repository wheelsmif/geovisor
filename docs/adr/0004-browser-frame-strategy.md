# ADR 0004: Browser Safety and Frame Coverage

Status: Accepted

## Context

Exploration can trigger irreversible page behavior, and browser security
boundaries can make some frames inaccessible.

## Decision

Safe exploration never performs actions classified as navigation or submission.
Stealth is opt-in. Frame discovery reports `complete`, `partial`, or
`unavailable`; partial coverage must identify every known uncovered frame and a
reason.

## Consequences

Unknown effects default to unsafe. Consumers can distinguish incomplete
evidence from an empty page instead of silently accepting partial extraction.
