# RFC-0069: Runtime Capability and Result Diagnostics

> **Design history:** This RFC preserves its original lifecycle status and rationale.
> Implementation claims describe the recorded design, not a guarantee that every
> proposed API remains current. See [current documentation](../../README.md) for
> supported interfaces. Package paths have been updated for this repository.

- **Status:** Implemented
- **Owners:** Metis maintainers
- **Created:** 2026-09-02
- **Last updated:** 2026-09-02
- **Scope:** Agent capability discovery, result-contract diagnostics, ClickHouse execution, and bounded execution telemetry
- **Supersedes:** None

## Summary

Make executable Project capability discoverable before an Agent chooses a
terminal tool, distinguish result-schema mismatches from generic execution
failures, add an official ClickHouse Backend, and expose low-cardinality
physical-execution health metrics.

## Motivation

Registering `query_metrics` in compile-only deployments gives Agents a stable
surface but previously required a failed call to learn that a Project was not
executable. A Runner normalization failure likewise collapsed to a generic
execution error, so an Agent could retry the same request without learning that
the compiled schema and decoded result disagreed. Finally, ClickHouse had real
SQL conformance but no production Runtime path, while Runner lifecycle events
were not connected to operator metrics.

## Design

`list_projects` returns a closed, sorted capability list. `compile_sql` is
present when the shared compile service and Project resolver are available;
`query_metrics` is present only when the caller has execution scope and the
Project resolves to a DataSource whose Backend is available through Runner.
Capability inspection performs no Driver
open, secret resolution, or connectivity probe and exposes no placement name.

Runner classifies lossless normalization failures as
`RESULT_CONTRACT_MISMATCH`. The shared service projects this as
`QUERY_RESULT_SCHEMA_MISMATCH` with `stage`, semantic `column`,
`expected_datatype`, a closed `observed_family`, and `retryable: false`. Raw
values, concrete Go types, Driver errors, and credentials are never retained or
returned.

`execution/backend/clickhouse` binds DataSource type `clickhouse` to the exact
ClickHouse Renderer and official Go Driver over HTTP(S). Its flat configuration
accepts `scheme`, `address`, `database`, `username`, and a SecretRef-only
`password`. The default CGO-free binary composes ClickHouse and Doris; the
DuckDB build flavor adds DuckDB.

Runner observations feed three Prometheus families labeled only by the closed
Backend family, success/error result, and closed internal execution code. Row
and byte counts remain available only to typed non-label sinks. Query IDs,
Project, DataSource identity, semantic refs, SQL, values, and raw errors are
excluded.

## Alternatives

- Omitting `query_metrics` from `tools/list` was rejected because capability is
  Project-specific and the stable public operation should not vary by route
  registration.
- Connectivity probing during discovery was rejected because discovery must
  not open Drivers or resolve secrets.
- Returning `%T`, raw values, or Driver messages was rejected because those are
  unbounded and may disclose sensitive implementation or customer data.
- Keeping ClickHouse tests on the direct HTTP fixture adapter was rejected
  because that does not exercise bootstrap, Backend selection, Runner, or
  `QueryMetricsService`.

## Rollout and migration

The new `capabilities` field is additive. Existing query requests are
unchanged. Deployments configured with ClickHouse now resolve the official
Backend instead of failing bootstrap. Operators enabling metrics receive three
additional bounded families and should update dashboards intentionally.

## Test and acceptance criteria

- compile-only and executable Projects advertise different capability sets;
- capability inspection does not open a Driver;
- result-contract diagnostics are structured and cannot leak the offending
  value or internal error message;
- Doris and ClickHouse execute the shared production `query_metrics` workflow;
- execution metric label products are exhaustively locked;
- `go test ./...`, `go vet ./...`, and repository diagnostics pass.

## Documentation updates

Update Agent discovery, Runtime bootstrap, Runtime observability, and MCP and
bootstrap design documents.
