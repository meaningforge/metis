# ADR-0016: HTTP and Stdio Share One MCP Server

> **Decision record:** Read alongside the [current documentation](../../README.md);
> superseded decisions are retained for their rationale.

**Status:** Accepted
**Date:** 2026-09-08
**Last reviewed:** 2026-09-08
**Supersedes:** None

## Context

Streamable HTTP is the right MCP transport for a shared Metis deployment: Gin
provides authentication, routing, and process-level operations. It is needless
friction for a local Agent that already knows how to start and supervise an MCP
stdio subprocess. Supporting both transports creates a risk that tool
registration, authorization, errors, or observations diverge if each transport
assembles its own server.

Stdio also lacks HTTP's Bearer envelope. [Project authorization contract](../../specs/operations/project-action-authorization.md) requires every Project action
to have a Principal and forbids routing or transport from becoming an implicit
authorization grant.

## Decision

`app/mcp` owns one complete production `mcp.Server` constructor. Both the
Streamable HTTP handler and `metis mcp` stdio process wrap that exact server.
Transport adapters own only their connection lifecycle and authentication
envelope; all tools continue to call the same application services.

`metis serve` remains the shared HTTP process. `metis mcp` is a distinct
single-session command owned by its local Agent client. The stdio process
reserves stdout for protocol frames and routes every log to stderr.

The stdio command establishes an explicit wildcard local Principal and submits
it to `ScopeProjectAuthorizer`. It does not use an absent Principal and does not
replace the shared authorizer with an unconditional transport-specific policy.
Possession of the local command and inherited deployment environment is the
trusted boundary.

## Consequences

- Codex, Cursor, and other local MCP clients can use Metis without a daemon,
  port, or local Bearer token.
- Shared deployments retain authenticated Streamable HTTP unchanged.
- Adding or removing a production tool changes both transports together.
- Local stdio callers receive wildcard Project actions and can reach any
  DataSource credentials available to the spawned process; clients must treat
  the command configuration as privileged.
- Per-user restricted stdio scopes would require an explicit future identity
  input rather than transport-specific policy branches.
- Transport-parity and stdout-purity tests become release requirements.

## References

- [RFC-0083](../../proposals/interfaces/0083-local-stdio-mcp-transport.md)
- [MCP transport specification](../../specs/interfaces/mcp-transports.md)
- [Project action authorization](../../specs/operations/project-action-authorization.md)
