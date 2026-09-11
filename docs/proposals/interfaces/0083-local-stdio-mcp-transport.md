# RFC-0083: Local Stdio MCP Transport

> **Design history:** This RFC preserves its original lifecycle status and rationale.
> Implementation claims describe the recorded design, not a guarantee that every
> proposed API remains current. See [current documentation](../../README.md) for
> supported interfaces. Package paths have been updated for this repository.

- **Status:** Implemented
- **Owners:** Metis maintainers
- **Created:** 2026-09-08
- **Last updated:** 2026-09-08
- **Scope:** `cmd/metis`, `app/mcp`, local Agent integration, MCP transport and authentication boundaries
- **Supersedes:** None

## Summary

Add `metis mcp --config <metis.yaml>` as a native local stdio MCP entrypoint.
It exposes the same production MCP server as Streamable HTTP while allowing
Codex, Cursor, and other local MCP clients to own the process lifecycle without
starting a listener or configuring a local Bearer token.

## Motivation

Metis currently requires every Agent integration to start `metis serve`, choose
a port, and configure an HTTP Bearer token even when the Agent and Metis run for
one user on the same machine. Local MCP clients already manage command-based
stdio servers. Native stdio makes the simplest local setup match that lifecycle
without replacing the authenticated multi-user HTTP deployment.

## Design

`app/mcp` exports one complete transport-neutral production server
constructor. The existing Streamable HTTP handler and the new stdio command
both wrap that constructor, so the transport choice cannot select another tool
surface or service implementation.

The CLI split is explicit:

```text
metis mcp --config <metis.yaml>    local single-session stdio
metis serve --config <metis.yaml>  shared authenticated HTTP listener
```

The stdio command uses the official SDK `StdioTransport`. Stdout is reserved
for newline-delimited MCP JSON-RPC; all structured logs go to stderr. Client
disconnect, `SIGINT`, and `SIGTERM` terminate the session and close any
execution runtime.

Stdio has no HTTP authentication envelope. The local command creates a bounded
Principal with `tenant=local`, `subject=stdio`, `api_key_id=local-process`, and
the wildcard scope. The production `ScopeProjectAuthorizer` still evaluates
that Principal for every resolved Project and action. This models the command
as a deliberate trusted-local adapter instead of silently selecting
`AllAccessProjectAuthorizer` or skipping [Project authorization contract](../../specs/operations/project-action-authorization.md).

Project selection remains an MCP concern. Explicit tool arguments, MCP
metadata, deployment `default_project`, and sole-Project resolution work as
before. HTTP additionally supports its existing Project header. A stdio Agent
against an unbound multi-Project deployment calls `list_projects` and passes
the selected `project_id` to later tools.

## Alternatives

**Keep HTTP only.** Rejected because it imposes daemon, port, and local-secret
setup on command-oriented local Agent clients.

**Ship a stdio-to-HTTP proxy.** Rejected because it preserves the daemon and
authentication setup, adds another lifecycle boundary, and can drift from
native MCP error and observation behavior.

**Add `serve --stdio`.** Rejected because `serve` owns a multi-route HTTP
listener while stdio owns one client-managed session. Separate commands make
their lifecycle and output contracts unambiguous.

**Bypass authorization for stdio.** Rejected because transport must not decide
semantic access. An explicit wildcard local Principal preserves the shared
authorization and audit path.

## Rollout and migration

The change is additive. Existing `metis serve`, REST, and Streamable HTTP MCP
configuration remain unchanged. Local users may replace their URL-based MCP
entry with a command-based entry at their own pace. Removing either transport
requires a future compatibility decision.

## Test and acceptance criteria

- The stdio command exposes the full production MCP tool set.
- A real tool call receives the explicit local Principal and passes through the
  [Project authorizer](../../specs/operations/project-action-authorization.md).
- Stdio logs cannot contaminate stdout.
- HTTP and stdio use the same server constructor.
- Client disconnect and cancellation stop the session cleanly.
- Codex and Cursor configuration examples use absolute executable and manifest
  paths.

## Documentation updates

- `README.md`
- `AGENTS.md`
- `docs/design/interfaces/agent-semantic-mcp.md`
- `docs/specs/interfaces/mcp-transports.md`
- `docs/specs/public-contract.md`
- ADR-0016
