# ADR-0002: Error Codes and Classes Are Transport-neutral Contracts

> **Decision record:** Read alongside the [current documentation](../../README.md);
> superseded decisions are retained for their rationale.

**Status:** Superseded by [ADR-0003](0003-caller-action-and-transport-projection.md)
**Date:** 2026-08-20
**Last reviewed:** 2026-08-21

> This ADR describes behavior that no longer exists. `ErrorClass` was removed by
> RFC-0034; remediation is now domain-owned and HTTP status is a REST-owned
> projection of the code. Kept as history for the reasoning it records.

## Context

Metis already exposes stable semantic error codes through REST and MCP. REST
used code-name substring checks to choose HTTP status, coupling protocol
behavior to incidental identifier spelling and leaving MCP without the same
coarse classification evidence.

## Decision

`serrors` owns both the precise error code and its coarse error class.
Classes are transport-neutral and centrally derived from codes. REST maps the
class to HTTP status; MCP serializes the same payload. Transports must not infer
behavior from code-name patterns or maintain private code classification maps.

Unknown errors and unregistered typed codes are classified as `InternalError`.
This fail-closed default prevents a new code from gaining client-visible
protocol semantics until its classification is reviewed explicitly.

## Consequences

- Error-code strings remain the precise compatibility contract.
- Clients may use a bounded class vocabulary for coarse handling.
- Adding a stable error code requires an explicit classification decision.
- HTTP behavior remains outside the semantic error package.
- The payload gains one additive `class` field.
