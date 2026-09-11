# RFC-0063: DuckDB Execution Backend

> **Design history:** This RFC preserves its original lifecycle status and rationale.
> Implementation claims describe the recorded design, not a guarantee that every
> proposed API remains current. See [current documentation](../../README.md) for
> supported interfaces. Package paths have been updated for this repository.

- **Status:** Implemented
- **Owners:** Metis maintainers
- **Created:** 2026-09-01
- **Last updated:** 2026-09-01
- **Scope:** DuckDB execution Backend, build composition, and production-path evidence
- **Supersedes:** None

## Summary

Add an executable DuckDB Backend that lets a Project wired directly to a
file-backed DuckDB DataSource use the existing governed `query_metrics`
operation. The Backend uses the existing DuckDB Renderer and the official
DuckDB Go client; it adds no target selection, semantic behavior, or arbitrary
SQL surface.

## Motivation

DuckDB was a first-class compile-only Renderer and real-engine conformance
target, but `cmd/metis` could execute only Doris. This left the self-contained
engine used for repository correctness unavailable through the production
Runner and made local governed execution require a separately provisioned
warehouse.

## Design

`execution/backend/duckdb` implements the stable Backend and Driver SPI:

```text
DataSource{type: duckdb, config.path}
    -> DuckDB Backend
    -> exact DuckDB Renderer
    -> DuckDB DriverFactory
    -> one read-only, process-scoped embedded database handle
    -> request-scoped Executor
    -> ResultStream
```

The v1 Driver configuration contains exactly one required trimmed `path`.
DuckDB DSN query and fragment components are rejected so deployment data cannot
override or hide the Backend-owned `access_mode=read_only`. The factory validates configuration at
bootstrap, opens lazily on first execution, and fails if the database is absent
or unusable. It accepts no credentials, extensions, `ATTACH`, arbitrary DuckDB
settings, or write mode.

The ResultStream converts DuckDB's lossless Decimal and huge-integer wrapper
values to text before Runner normalization. Runner continues to own timeouts,
admission, row and byte limits, complete-result atomicity, and public error
redaction.

The official DuckDB client requires CGO. DuckDB execution is therefore an
explicit build flavor selected with `CGO_ENABLED=1 -tags duckdb`; that flavor
composes DuckDB and Doris. The default source build, production container, and
cross-platform release archives remain CGO-free and compose Doris only. A
default binary rejects a configured DuckDB DataSource during bootstrap rather
than silently substituting the compile-only Renderer. Selecting the `duckdb`
tag without CGO is a build error.

## Alternatives

- Reusing the test-only DuckDB CLI wrapper was rejected because it buffers
  subprocess output outside Runner's streaming limits and owns no reusable
  embedded database handle.
- Adding DuckDB branches to Runner was rejected because DataSource-specific
  configuration and execution belong behind the stable Backend and Driver SPI.
- Enabling write mode or arbitrary connection settings was rejected because
  `query_metrics` is a governed read-only semantic execution boundary.

## Rollout and migration

Existing configurations are unchanged. Build the opt-in binary with:

```bash
CGO_ENABLED=1 go build -tags duckdb -o metis-duckdb ./cmd/metis
```

That deployment may add a DataSource such as:

```yaml
duckdb-local:
  type: duckdb
  config:
    path: /var/lib/metis/analytics.duckdb
  policy:
    query_timeout: 30s
    max_rows: 100000
    max_bytes: 67108864
    max_concurrency: 4
```

Operators should select conservative concurrency for embedded databases. The
database file and its parent directory must be readable by the Metis process.

## Test and acceptance criteria

- Backend registration binds type `duckdb` to the exact DuckDB Renderer and
  DriverFactory.
- Strict config validation rejects missing paths, unknown fields, and DSN
  option injection.
- A real embedded database executes through Driver and production
  `query_metrics` paths with exact Decimal text.
- The database is opened read-only; write attempts fail.
- cancellation and bounded Runtime close semantics are covered.
- the default CGO-free build and existing Doris behavior remain valid;
- `make test-duckdb-backend` exercises the explicit build flavor, production
  Backend, complete shared scenario corpus, and AgentBench embedded fixture.
- the former CLI download runner and `duckdbsupport` subprocess adapter are
  removed, leaving one DuckDB execution authority.
- `go test ./...`, `go vet ./...`, and embedded DuckDB conformance pass.

## Documentation updates

Update runtime bootstrap design/specification and extension authoring support
evidence. Keep compile-only Renderer evidence distinct from executable Backend
availability.
