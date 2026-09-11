# ADR-0009: Execution Runtime Is Optional, Driver-Factory-Based, and Orchestrated

> **Decision record:** Read alongside the [current documentation](../../README.md);
> superseded decisions are retained for their rationale.

**Status:** Superseded by ADR-0010
**Date:** 2026-08-29
**Last reviewed:** 2026-08-29

## Context

ADR-0010 supersedes this decision's `ExecutionBinding` routing, user-selected
Driver, and independent compile/runtime target authority. The four-layer split,
optional Execution Runtime, orchestration boundary, and canonical
`SqlRenderResult` handoff remain durable context.

Metis currently compiles governed semantic requests into physical queries and
leaves database connectivity, execution, result fetching, and analytical
evaluation to external Agents and runtimes. That boundary keeps the semantic
compiler deterministic, but it also forces every caller to reproduce database
routing, bounded execution, multi-query coordination, and deterministic
post-query mathematics.

Production deployments may need several database families and several concrete
instances of the same family. Treating endpoints or credentials as semantic
placement would couple Ossie assets and `ExecutionBinding` to deployment details.
Teaching orchestration or analytical code about each database would duplicate
driver behavior and prevent a stable execution-extension boundary.

Cube provides useful user-facing vocabulary for this problem: a named
`data_source` identifies the source selected by a semantic model, while
`driverFactory({ dataSource })` resolves the database driver used for that
source. Metis adopts the same durable concepts while retaining an explicit
engine-neutral `Executor` contract and deployment routing required by its
compiler-only mode and multi-project runtime.

Metis also reserves `Catalog` for actual physical metadata authorities such as
AWS Glue, Apache Gravitino, Iceberg catalogs, or warehouse-native catalogs.
Those concepts must remain distinct from executable data sources.

## Decision

Metis adopts four explicit runtime layers:

```text
                    Orchestration Layer
                     /       |       \
                    v        v        v
             Semantic Core  Execution  Analytics
                              Runtime   Runtime
```

Semantic Core remains deterministic and database-independent. It reads one
immutable `SemanticManifest`, resolves governed semantics, plans, and compiles a
canonical `SqlRenderResult`. It does not open database connections or depend on
live result rows.

Execution Runtime is optional. It consumes already-compiled `SqlRenderResult`
artifacts and exposes one engine-neutral `Executor` contract.

The execution vocabulary is:

```text
DataSource
    one named, deployment-configured database or warehouse instance

Driver
    the database family/protocol selected by a DataSource
    examples: duckdb, doris, snowflake

DriverFactory
    registered Go implementation for one Driver
    validates driver-owned configuration and opens an Executor

Executor
    engine-neutral contract that executes canonical SqlRenderResult artifacts
```

A deployment-scoped `DataSource` selects exactly one `driver`. The registered
`DriverFactory` for that driver validates the DataSource configuration, resolves
its driver-specific connection behavior, and opens an `Executor`. Multiple
DataSources may use the same driver and DriverFactory, and one deployment may
combine multiple driver families.

`ExecutionBinding` remains logical project-scoped placement metadata. A root
deployment manifest explicitly routes a resolved project and binding to one
DataSource. Engine or dialect compatibility validates that route; it never
selects an instance heuristically. Endpoints, credentials, driver configuration,
pool settings, and execution policy do not enter Ossie, project manifests, or
`ExecutionBinding`.

The transport-neutral Orchestration Layer owns capability dispatch, context and
authorization propagation, explicit DataSource routing, bounded fan-out,
cancellation, budgets, and result correlation. It delegates semantic planning to
Semantic Core, physical execution to Execution Runtime, and domain mathematics
to Analytics Runtime. REST and MCP remain thin adapters over the same service
behavior.

`Catalog` is reserved for external physical metadata authorities and their
adapters. `SemanticManifest` is the immutable semantic read model. Validated
Catalog evidence may participate in building a new manifest, but request-time
semantic resolution does not call a mutable external Catalog after the manifest
becomes current.

## Transition rule

This decision accepts the target architecture; it does not claim that execution
is implemented. Until the corresponding code, specifications, designs, and
working contract are updated, Metis remains compile-only and the existing public
`compile` capability remains current behavior.

ADR-0008 remains the current attribution decision during this migration.
Attribution stays compile-only until RFC-0057 and its implementation explicitly
supersede ADR-0008's execution and public-interface boundary while preserving its
deterministic attribution semantics.

No implementation change may cross an existing compile-only red line by citing
this ADR alone. The delivery that crosses a boundary must update its owning
current contract and add tests at that boundary.

## Consequences

- Compiler-only deployments remain first-class and require no warehouse
  credentials.
- The user-facing and configuration vocabulary aligns with Cube around
  DataSource, Driver, and DriverFactory rather than introducing a separate
  Connector term for database execution.
- Adding another instance of a supported database is configuration-only; adding
  another database family requires a registered DriverFactory rather than
  semantic or orchestration branches.
- Runtime-enabled deployments require explicit root-manifest DataSources and
  project/binding routes, including single-project deployments.
- Semantic compilation and runtime execution share the same `SqlRenderResult`
  artifact; there is no second SQL-building path.
- Credential resolution, live connections, result normalization, limits, and
  execution telemetry stay outside Semantic Core.
- Analytics Runtime can reuse execution without directly invoking database
  drivers or choosing DataSources.
- External Catalog integration remains a metadata concern distinct from an
  executable DataSource.
- Implementation requires staged vocabulary, contract, runtime, transport, and
  attribution migrations rather than one compatibility-breaking change.

## Alternatives considered

**Keep execution permanently outside Metis.** This preserves the smallest trust
boundary but leaves every Agent or embedding application to reproduce routing,
multi-query coordination, limits, and deterministic analytical evaluation.

**Keep `Connector` as the primary database-execution term.** This is workable and
matches Trino, but it is less direct for semantic-layer users and overlaps with
future uses of connector terminology for metadata and external-system adapters.
Cube's `data_source` / driver / `driverFactory` vocabulary more directly matches
Metis' product surface while the explicit `Executor` contract preserves the
runtime abstraction Metis needs.

**Put connection details in `ExecutionBinding`.** This makes semantic projects
deployment-specific, weakens project portability, and mixes logical placement
with secrets and runtime behavior.

**Use engine or dialect as the database-instance selector.** Several instances
may share both values, so selection would be ambiguous and load-order dependent.

**Let each analytical capability call database drivers directly.** This
duplicates routing and execution policy and prevents ordinary metric queries and
future analytical workflows from sharing one runtime contract.

**Call configured execution instances Catalogs.** Trino and Doris use that term,
but Metis needs `Catalog` for genuine physical metadata authorities. `DataSource`
keeps execution configuration distinct from Catalog metadata and live connection
handles.

## References

- [RFC-0054: Execution Runtime and Analytics Runtime Architecture](../../proposals/execution/0054-execution-analytics-runtime-architecture.md)
- [ADR-0008: Metric Attribution Is Compile-Only Independent Evidence](../semantic/0008-metric-attribution-is-compile-only-independent-evidence.md)
- [RFC-0056: Runtime Metric Query Execution](../../proposals/execution/0056-runtime-metric-query-execution.md)
- [Runtime Bootstrap specification](../../specs/operations/runtime-bootstrap.md)
- [Semantic compilation pipeline](../../design/semantic/pipeline.md)
- [Cube data sources](https://docs.cube.dev/configuration/data-sources)
- [Cube `data_source` model property](https://docs.cube.dev/reference/data-modeling/cube)
- [Cube dynamic `driverFactory` example](https://docs.cube.dev/docs/data-modeling/dynamic/javascript)
