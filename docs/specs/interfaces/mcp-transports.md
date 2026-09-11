# MCP Transport Contract

This specification defines how Metis exposes its Agent-facing MCP server over
local stdio and Streamable HTTP without creating transport-specific semantic
behavior.

## Shared server authority

`app/mcp` MUST construct one production `mcp.Server` registration containing
the complete configured tool set. Streamable HTTP and stdio MUST wrap that same
constructor. They MUST expose identical tool names, input schemas, server
instructions, structured results, domain-error payloads, and MCP observations
for the same runtime configuration and Principal permissions.

Transport adapters MUST NOT call each other and MUST NOT implement semantic
resolution, planning, compilation, or execution policy.

## Streamable HTTP

`metis serve --config <metis.yaml>` exposes the MCP server at `/mcp` on the
shared Gin listener. The shared Bearer middleware authenticates the request and
injects its `Principal` before the MCP handler. The optional
`X-Metis-Project-Id` header may bind Project context; tool arguments and MCP
metadata retain their documented precedence.

## Local stdio

`metis mcp --config <metis.yaml>` starts one persistent MCP stdio session. The
client owns the subprocess lifecycle. The command:

- reads newline-delimited MCP JSON-RPC only from stdin;
- writes MCP protocol frames only to stdout;
- writes every application, authorization, SDK, startup, and shutdown log to
  stderr;
- exits when the client closes stdin, the session fails, or `SIGINT`/`SIGTERM`
  cancels the process;
- closes an initialized execution runtime before returning.

Stdio does not use an HTTP Bearer envelope. The command establishes the bounded
local Principal `tenant=local`, `subject=stdio`, `api_key_id=local-process` with
the wildcard scope. `ScopeProjectAuthorizer` still evaluates every resolved
Project and action. This is an explicit trusted-local adapter, not an
authorization bypass. A process started by an untrusted caller has all access
available to the local Metis deployment and its inherited environment.

## Project context

Both transports use the canonical resolution order:

```text
explicit tool project_id -> MCP project metadata -> transport binding
  -> deployment default_project -> sole registered Project -> PROJECT_REQUIRED
```

Only HTTP supplies the optional header transport binding. A multi-Project stdio
client without `default_project` discovers visible Projects with
`list_projects` and passes `project_id` to subsequent tools.

## CLI compatibility

`metis mcp`, `--config`, and `--shutdown-timeout` are public CLI vocabulary.
Adding another MCP transport MUST NOT change their stdio meaning. HTTP remains
owned by `metis serve`; `serve --stdio` is not an alias.

## Conformance

Tests MUST prove that:

- the stdio runtime exposes the complete production tool set;
- a stdio tool call receives the explicit local Principal and passes through
  Project-action authorization;
- selecting the `mcp` command routes logs to stderr;
- the production HTTP handler and stdio command use the same MCP server
  constructor;
- client disconnect and context cancellation terminate the server and close
  execution resources.
