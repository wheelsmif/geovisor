# ADR 0003: Structured Locator Candidates

Status: Accepted

## Context

Opaque automation-library selector strings cannot reliably express nested
frames, shadow roots, semantic intent, or ranked fallbacks.

## Decision

TIR stores ordered locator candidates with explicit frame and shadow paths,
semantic scope/role/name, and optional CSS fallbacks. Action bindings reference
candidate IDs rather than embedding executable selectors.

## Consequences

Browser adapters translate the structure to their own APIs. Emitters can retain
semantic intent, and TIR remains independent of any automation library.
