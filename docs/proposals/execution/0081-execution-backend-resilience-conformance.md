# RFC-0081: Execution Backend Resilience Conformance

> **Design history:** This RFC preserves its original lifecycle status and rationale.
> Implementation claims describe the recorded design, not a guarantee that every
> proposed API remains current. See [current documentation](../../README.md) for
> supported interfaces. Package paths have been updated for this repository.

- **Status:** Implemented
- **Owners:** Metis maintainers
- **Created:** 2026-09-03
- **Last updated:** 2026-09-09
- **Scope:** fault behavior and real-engine conformance for Runner, Driver, DataSourceRuntime, Doris, ClickHouse, and DuckDB execution
- **Supersedes:** None

## Summary

Define a target-neutral resilience conformance suite for production execution
backends. The suite proves fail-closed limits, cancellation, resource cleanup,
secret redaction, lifecycle safety, and consistent error classification under
real engine and injected fault conditions.

## Motivation

Existing real-engine conformance proves successful semantic queries and exact
result normalization. Production confidence also requires evidence for network
failure, cancellation races, partial result streams, oversized results,
connection churn, and shutdown.

## Design

The conformance contract exercises the existing `CompiledQuery -> Runner ->
Driver` boundary. It adds no semantic or Renderer authority and no public raw
SQL execution path.

Every execution backend must demonstrate:

- bounded connection acquisition and query timeout;
- cancellation before acquisition, during execution, and during row reading;
- row and byte limit enforcement, including one oversized row;
- cleanup after decode failure, partial stream failure, and client disconnect;
- deterministic error code and caller action classification;
- secret, endpoint-credential, SQL-parameter, and row-value redaction;
- stable runtime reuse, graceful shutdown, and rejection after close;
- admission fairness and no leaked permits under faults.

Shared behavioral scenarios are target-neutral; fault injection remains local
to each Driver test package rather than becoming a public Driver helper or SPI.
Real Doris and ClickHouse environments validate transport behavior, while
DuckDB validates local lifecycle behavior. Renderer conformance remains a
separate concern.

Metis does not transparently retry analytical queries in this RFC. Safe retry
requires an explicit idempotency and deadline budget and cannot be inferred
from a Driver error. Backend-native retry may remain disabled or separately
documented.

This RFC does not add new warehouses, arbitrary SQL, result caching,
pre-aggregation, query federation, or credential management.

## Alternatives

Happy-path integration tests alone are rejected because they miss lifecycle
leaks. A shared exported fault-injection Driver API is rejected because test
mechanics would expand the stable SPI. Automatic retry is deferred because it
can duplicate expensive work and violate caller deadlines.

## Rollout and migration

1. Define the scenario matrix and stable expected error classes.
2. Add deterministic Runner-level injected faults.
3. Add Driver-local Doris, ClickHouse, and DuckDB cases.
4. Run real-engine fault scenarios in the existing conformance workflow.
5. Publish the backend resilience support matrix.

## Test and acceptance criteria

- All supported execution backends pass the required scenario matrix.
- Race and leak checks cover cancellation, limit failure, and shutdown.
- No fault artifact contains resolved secrets, row values, or SQL parameters.
- Admission permits and result resources return to baseline after every case.
- Error codes are consistent across equivalent backend faults where the public
  caller action is the same.
- No test-only interface becomes part of the public Driver SPI.

## Documentation updates

Implementation must update the execution runtime specification, runtime
observability contract, SQL extension authoring guide, testing architecture,
operations guide, and `tests/benchmarks/CONFORMANCE.md`.
