# ADR 0001: Agent-Owned Browser Execution

Status: Accepted

## Context

GEO-Visor needs browser observations but should not own an unrelated persistent
service or browser lifecycle.

## Decision

Browser execution is agent-owned. Future `BrowserSource` adapters may launch a
URL or attach through CDP only within the caller's existing session. GEO-Visor
will not provide a host MCP daemon.

## Consequences

Authentication and browser lifecycle remain with the caller. Sources must make
session assumptions explicit, and downstream packages remain process-agnostic.
