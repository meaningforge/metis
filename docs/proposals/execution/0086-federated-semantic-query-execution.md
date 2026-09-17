# RFC-0086: Federated Semantic Query Execution

- **Status:** Draft
- **Owners:** Metis maintainers
- **Created:** 2026-09-17
- **Last updated:** 2026-09-17
- **Scope:** Cross-DataSource semantic planning, bounded federation execution, Arrow interchange, embedded DuckDB composition, and optional build capability
- **Supersedes:** None

## Summary

This RFC adds true cross-DataSource semantic query execution to Metis while preserving the existing single-DataSource execution path.

The design follows four rules:

> **Metis understands. Databases compute. Arrow moves. DuckDB composes.**
>
> **Push down first; compose only when necessary.**
>
> **Integrate proven engines instead of rebuilding them.**
>
> **Zero additional infrastructure by default.**

A query whose required datasets resolve to one DataSource continues to use the current direct path:

```text
Semantic Query
      │
      ▼
    Metis
      │
      ▼
 Source SQL
      │
      ▼
   Database
      │
      ▼
    Result
```

Planning for both direct and federated execution is organized as:

```text
Semantic Query
      ↓
Semantic Planner
      ↓
Physical Planner
      │
      └── Placement Resolution
      ↓
DirectPlan / FederatedPlan
```

Placement Resolution is a deterministic input to physical planning, not a separate optimizer. It answers where each semantic dataset physically executes. The Physical Planner then owns fragmentation, pushdown decisions, and any remaining composition plan.

Only a query whose required datasets span multiple DataSources produces a federated plan:

```text
                 FederatedPlan
                      │
      ┌───────────────┼────────────────┐
      ▼               ▼                ▼
   Doris          ClickHouse        DuckDB
      │               │                │
source SQL A     source SQL B      source SQL C
      │               │                │
      ▼               ▼                ▼
  Result A         Result B          Result C
      │               │                │
      └──────── Arrow boundary ─────────┘
                      │
                      ▼
              Embedded DuckDB
                      │
             JOIN / UNION / CALC
                      │
                      ▼
                   Result
```

The embedded DuckDB runtime is an **optional compile-time capability**. The default Metis build remains CGO-free. A federation build explicitly opts into CGO, DuckDB, and Arrow interoperability.

Metis does not add a persistent federation store, worker cluster, shuffle service, distributed execution engine, or Metis-owned materialization layer.

## Motivation

Metis already allows one Project to apply multiple DataSources and routes semantic models to configured DataSources. The current governed execution path still resolves one model to one DataSource, chooses that route's Renderer, compiles one SQL query, and executes it atomically through Runner.

That is multi-source placement, not cross-source analysis.

Real analytical questions may combine semantic datasets owned by different systems, for example:

```text
orders         -> Doris
customers      -> ClickHouse
country_target -> DuckDB
```

Requiring Trino or another distributed federation service for every bounded semantic join would add infrastructure that many deployments do not need. Building a Metis Store or distributed SQL engine would duplicate mature systems and force users to pay additional CPU, memory, storage, network, HA, and operational cost.

The missing capability is deliberately narrow:

1. understand which semantic work belongs to which DataSource;
2. maximize computation inside each source;
3. move only bounded intermediate results across a stable interchange boundary;
4. use a mature embedded analytical engine for the remaining local composition.

DuckDB and Apache Arrow provide that substrate without requiring another long-running service.

## Design principles

### Single-source execution remains unchanged

Federation is additive.

If all datasets required by a semantic plan resolve to one DataSource, Metis MUST continue to use the existing route:

```text
SemanticQuery
    -> exact DataSource
    -> exact Backend
    -> exact Renderer
    -> CompiledQuery
    -> Runner.ExecuteResolved
    -> ResultSet
```

The direct path MUST NOT initialize the federation Composer, convert results to Arrow, or pass through DuckDB.

### Databases perform database work

Source databases remain responsible for:

- scans;
- source-local joins;
- filtering;
- projection;
- aggregation;
- sorting where useful;
- indexing;
- materialized views and transparent rewrite;
- cost-based optimization;
- source-local parallel and distributed execution.

Metis MUST maximize safe pushdown before crossing a federation boundary.

### Metis does not become a SQL execution engine

Metis MUST NOT implement its own:

- hash join engine;
- vectorized executor;
- spill engine;
- distributed shuffle;
- worker scheduler;
- persistent analytical store;
- mandatory cache/materialization subsystem.

If bounded local composition requires JOIN, UNION, GROUP BY, sorting, windowing, or expression evaluation, Metis delegates those physical operations to embedded DuckDB.

### Arrow is an interchange format, not a DataSource requirement

DataSources do not need native Arrow support.

```text
Source Driver
     │
     ▼
normalized bounded result
     │
     ▼
Arrow RecordBatch stream
     │
     ▼
Embedded DuckDB
```

A source with native Arrow/Arrow Flight support may later provide a fast path. Existing row-based Drivers remain usable; Metis converts bounded row batches to Arrow inside the federation runtime.

### Metis owns semantic types; DuckDB owns federation execution types

Metis owns semantic meaning such as metrics, dimensions, entities, relationship cardinality, aggregation semantics, time semantics, and semantic datatype intent.

Metis MUST NOT introduce a second complete physical SQL type system solely for federation.

At the federation boundary, source values are mapped into Arrow representations consumable as DuckDB logical types. DuckDB owns ordinary physical coercion and combination casting during composition. Metis still rejects conversions that would violate semantic correctness or required precision.

### Federation is an optional compiled capability

Federation is not a mandatory runtime dependency.

The default Metis build MUST remain CGO-free and MUST NOT require embedded DuckDB federation support.

This follows the same composition model already used by the optional DuckDB execution Backend, but the two capabilities are distinct:

- `duckdb` enables a user-configured DuckDB DataSource through the normal Backend/Driver path;
- `federation_duckdb` enables Metis's internal DuckDB Composer for cross-DataSource queries.

A deployment may enable either capability independently or both together.

## Goals

This RFC enables:

1. one semantic query to use datasets placed on multiple DataSources;
2. deterministic placement of each physical dataset;
3. maximal source-local fragments;
4. source-specific Renderer selection per fragment;
5. concurrent execution of independent fragments;
6. bounded Arrow interchange;
7. in-process DuckDB composition;
8. cross-source JOIN and UNION;
9. bounded final calculations, grouping, ordering, and windowing;
10. compatibility with existing row-stream Drivers;
11. unchanged direct execution for single-source queries;
12. a default CGO-free Metis build;
13. explicit capability discovery and fail-closed behavior when federation is unavailable.

## Non-goals

This RFC does not provide:

- arbitrary federated SQL submitted by callers;
- distributed cross-source execution;
- workers or distributed shuffle;
- persistent Metis federation storage;
- a Metis materialized-view subsystem;
- automatic creation of database MVs;
- an Agent-visible DuckDB SQL surface;
- unbounded raw fact-to-fact federation;
- replacement for Trino;
- replacement for source database optimizers;
- cross-model public query syntax in the first implementation phase.

Large-scale federation should be delegated to a purpose-built engine when one is configured and capable of executing the complete plan.

## Current behavior and required change

Today `QueryMetricsService` resolves one physical route from one semantic model:

```text
Project
  -> Model
  -> DataSourceForModel(project, model)
  -> ResolvedDataSource
  -> route.Backend.Renderer
  -> compileWithRenderer
  -> Runner.ExecuteResolved
```

True federation cannot be implemented only by making Runner execute several SQL statements. Renderer selection currently happens before a multi-source physical plan exists.

Federation therefore requires a planning boundary between semantic resolution and source-specific rendering.

## Dataset-aware placement

**Placement is the deterministic binding of a semantic dataset to its physical DataSource.**

It answers a narrow question:

```text
semantic dataset -> physical DataSource
```

For example:

```text
orders    -> doris-prod
customers -> clickhouse-prod
```

Placement does not decide how to write SQL, whether to aggregate, how to join, or whether DuckDB should compose results. Those are Physical Planner responsibilities.

Existing model-level placement remains backward compatible.

Federation adds dataset-aware placement:

```go
type DatasetExecutionProjectResolver interface {
    ModelExecutionProjectResolver

    DataSourceForDataset(
        project string,
        model string,
        dataset string,
    ) (string, bool)
}
```

Placement precedence is:

```text
explicit dataset placement
        ↓
model placement
        ↓
sole applied DataSource inference
        ↓
unresolved -> deployment configuration error
```

Physical placement remains deployment-owned. Agents do not choose DataSources.

A public semantic query may remain model-first in v1. A single semantic model may contain datasets placed on different physical DataSources.

### Placement Resolution is part of Physical Planning

Metis does not introduce a heavyweight standalone `PlacementPlanner` in v1.

The Physical Planner consumes a `SemanticPlan` and performs four closely related tasks:

```text
SemanticPlan
     ↓
Physical Planner
     ├── Placement Resolution
     │      dataset -> DataSource
     ├── Fragmentation
     │      split at cross-source boundaries
     ├── Pushdown Analysis
     │      keep maximal safe work at sources
     └── Composition Planning
            remaining work -> local Composer
     ↓
ExecutionPlan
```

Placement Resolution SHOULD remain deterministic. A separate cost-based Placement Planner is only justified later if one semantic dataset or complete plan has multiple viable execution locations and Metis must choose among them.

## Two-phase semantic lowering

### Phase A: renderer-neutral semantic resolution

Resolve:

- metrics;
- dimensions;
- datasets;
- relationships;
- measures;
- aggregations;
- filters;
- grouping;
- required fields;
- semantic datatypes.

Do not permanently bind the whole query to one dialect Renderer.

### Phase B: fragment-specific physical lowering

After placement resolution and fragmentation:

```text
Fragment A -> Doris      -> Doris Renderer      -> CompiledQuery A
Fragment B -> ClickHouse -> ClickHouse Renderer -> CompiledQuery B
```

Compilation and execution for a fragment MUST use the same resolved DataSource route.

## Physical execution plan

The Physical Planner lowers a `SemanticPlan` into one of two execution plan families.

```go
type ExecutionPlan interface {
    isExecutionPlan()
}

type DirectPlan struct {
    DataSource string
    // renderer-neutral physical input
}

type FederatedPlan struct {
    Fragments   []Fragment
    Composition CompositionPlan
}
```

`DirectPlan` preserves the existing path.

`FederatedPlan` describes source fragments plus local composition.

The boundary is intentionally simple:

```text
Semantic Planner
      ↓
SemanticPlan
      ↓
Physical Planner
      │
      └── Placement Resolution
      ↓
DirectPlan / FederatedPlan
```

Example federated plan:

```text
FederatedPlan
│
├── Fragment f0
│     datasource: doris-prod
│     datasets: orders
│
├── Fragment f1
│     datasource: clickhouse-prod
│     datasets: customers
│
└── Composition
      Join(
        left=f0,
        right=f1,
        on=customer_id
      )
```

## Fragmentation

The Physical Planner walks the semantic plan and resolves source placement.

If an operation and all required children can execute on one DataSource, the Physical Planner keeps the operation source-local whenever the Backend can represent it.

If children cross DataSources, Metis creates a federation boundary.

The result MUST be maximal source-local fragments.

```text
Doris Fragment ────────┐
                       ├── DuckDB composition
ClickHouse Fragment ───┘
```

## Pushdown objective

The primary optimization objective is:

> **Maximize source pushdown, minimize federation boundary cardinality.**

For example:

```text
Doris
1,000,000,000 orders
        ↓
WHERE last_30_days
        ↓
GROUP BY customer_id
        ↓
42,000 rows
        ↓
Arrow
```

is preferred over moving one billion raw rows into Metis.

Fragments SHOULD return only required join keys, dimensions, partial metrics, and other fields needed by the composition plan.

## Semantic aggregation safety

Aggregation may only be pushed below a federation boundary when semantic equivalence can be proven.

The Physical Planner uses existing semantic evidence such as relationship cardinality, aggregation algebra, grain, required join keys, and fan-out safety.

A many-to-one lookup may allow source-side pre-aggregation. An ambiguous many-to-many cross-source shape MUST fail closed in v1 rather than silently return a wrong answer.

Correctness takes precedence over federation coverage.

## Direct execution

When the Physical Planner's placement resolution finds exactly one physical DataSource, Metis uses the existing route unchanged:

```text
resolve route
    -> exact Renderer
    -> compile
    -> Runner.ExecuteResolved
    -> ResultSet
```

No Arrow conversion occurs and no Composer is initialized.

## Federated execution

When the Physical Planner's placement resolution finds more than one physical DataSource:

```text
FederatedPlan
      │
      ▼
compile fragments with exact route Renderers
      │
      ▼
execute fragments through existing Runner
      │
      ▼
bounded fragment results
      │
      ▼
Arrow bridge
      │
      ▼
embedded DuckDB Composer
      │
      ▼
final ResultSet
```

Independent fragments SHOULD execute concurrently under bounded concurrency.

A fragment failure cancels sibling fragment work and local composition.

## Reuse the existing Runner

Federation SHOULD reuse the current Runner for source execution rather than creating a second Driver framework.

For each fragment:

```go
route := runtime.ResolveDataSource(fragment.DataSource)
compiled := compileFragment(fragment, route.Backend.Renderer)
result := runtime.ExecuteResolved(ctx, route, compiled, options)
```

This preserves existing ownership of:

- secret resolution;
- Driver lifecycle;
- admission and concurrency limits;
- query timeout;
- max rows/max bytes;
- result normalization;
- execution observability.

## Driver compatibility

Federation v1 MUST NOT require a breaking Driver SPI change.

Existing row-based `ResultStream` implementations remain valid.

The first implementation may convert bounded normalized fragment results into Arrow after Runner returns them.

A later additive streaming interface may allow direct batch/Arrow production for sources where it materially improves performance, but native Arrow support is never an admission requirement.

## Arrow bridge

The Arrow bridge is internal to the federation capability.

Responsibilities:

1. map governed fragment schema to Arrow fields;
2. append normalized source values in batches;
3. preserve nullability;
4. preserve exact Decimal values;
5. reject unsupported/lossy mappings;
6. register Arrow streams as DuckDB relations/views.

Semantic Core SHOULD NOT expose Arrow-specific types in its public planning contracts.

## DuckDB Composer

DuckDB is an internal physical composition engine, not a semantic authority.

It is responsible for:

- JOIN;
- UNION/UNION ALL;
- GROUP BY required after federation;
- ORDER BY;
- window operators;
- arithmetic and scalar expressions;
- ordinary DuckDB logical type coercion.

It MUST NOT receive:

- source credentials;
- arbitrary Agent SQL;
- arbitrary DataSource names;
- arbitrary `ATTACH` commands;
- filesystem paths from callers;
- network endpoints from callers.

DuckDB only consumes controlled relations created from fragment results and a Metis-generated composition plan.

## Composer SPI

Federation uses a narrow pluggable interface rather than spreading DuckDB dependencies through semantic packages.

Conceptually:

```go
type Composer interface {
    Name() string
    Available() bool

    Compose(
        ctx context.Context,
        plan CompositionPlan,
        inputs []FragmentResult,
        limits Limits,
    ) (*ResultSet, error)

    Close() error
}
```

The interface intentionally does not expose DuckDB or Arrow types.

The v1 provider is `duckdbcomposer`.

This is an architecture plugin with compile-time provider selection, not Go's runtime `plugin` package.

## Build composition

Default build:

```bash
CGO_ENABLED=0 go build ./cmd/metis
```

Properties:

```text
✓ semantic planning
✓ existing single-source execution
✓ Doris / ClickHouse backends as currently assembled
✗ embedded federation Composer
✗ federation DuckDB/Arrow runtime cost
```

Federation build:

```bash
CGO_ENABLED=1 \
  go build \
  -tags='federation_duckdb duckdb_arrow' \
  ./cmd/metis
```

The exact tag wiring may reuse current build composition helpers, but `federation_duckdb` MUST remain logically distinct from RFC-0063's `duckdb` DataSource Backend capability.

Example build assembly:

```go
//go:build !federation_duckdb

func federationComposer() federation.Composer {
    return federation.UnavailableComposer()
}
```

```go
//go:build federation_duckdb

func federationComposer() federation.Composer {
    return duckdbcomposer.New()
}
```

The default source tree may contain DuckDB modules as dependencies for the optional build flavor, but the default binary MUST not require CGO or initialize the federation runtime.

## Capability discovery and errors

A build without federation MAY still parse and plan multi-source semantics, but it MUST reject execution before starting source fragments if no Composer is available.

The error should be stable and actionable, for example:

```text
FEDERATION_NOT_AVAILABLE

This semantic query spans multiple DataSources, but this Metis build does not include a federation Composer.
```

Project/runtime capability discovery should distinguish:

```json
{
  "query_metrics": true,
  "federation": {
    "enabled": true,
    "engine": "duckdb",
    "interchange": "arrow"
  }
}
```

from a default build where federation is disabled.

Capability inspection MUST NOT open Drivers, resolve secrets, or initialize DuckDB.

## Resource guardrails

Federation is intentionally bounded single-node composition.

The runtime needs independent limits such as:

```yaml
federation:
  max_fragments: 8
  max_rows_per_fragment: 100000
  max_bytes_per_fragment: 64MB
  max_total_bytes: 256MB
  max_concurrency: 4
  timeout: 30s
```

Exact defaults are implementation decisions, but all limits MUST be explicit and enforceable.

Metis MUST NOT silently attempt an unbounded fact-to-fact join merely because DuckDB can execute a SQL join.

If a plan is too large for bounded local composition, execution fails clearly. A future capability may delegate the complete plan to a configured federation engine such as Trino or another capable Backend.

## Memory, spill, and lifecycle

The initial implementation SHOULD prefer in-memory query-scoped composition and conservative limits.

If temporary DuckDB spill is later enabled, it MUST be explicitly configured and bounded by disk quota and lifecycle rules. It MUST NOT evolve into persistent Metis-owned analytical storage.

All query-scoped resources must close on success, failure, timeout, and cancellation.

## Type behavior

Metis follows DuckDB logical types for physical federation where possible rather than recreating DuckDB's coercion system.

Examples include:

```text
Doris BIGINT        -> DuckDB BIGINT
ClickHouse UInt64   -> DuckDB UBIGINT
cross-source compare -> DuckDB combination casting when lossless
```

The Arrow bridge preserves the corresponding physical representation.

Special attention is required for:

- Decimal precision/scale;
- signed vs unsigned integer ranges;
- timestamp vs timestamp-with-time-zone semantics;
- NULL behavior;
- unsupported high-precision source-native numeric types;
- nested types where source/DuckDB mappings are incomplete.

Metis validates semantic meaning; DuckDB performs ordinary physical execution coercion.

## Observability

Federated execution adds bounded low-cardinality telemetry, including:

- direct vs federated plan count;
- fragment count;
- Backend family per fragment;
- fragment execution duration;
- intermediate row/byte totals through typed non-label sinks;
- Arrow conversion duration;
- composition duration;
- federation limit failures;
- federation unavailable failures.

Metrics MUST NOT label by Project name, DataSource name, table name, semantic refs, SQL, values, credentials, or arbitrary error text.

## Security

Federation does not broaden caller authority.

The Agent still submits one governed semantic operation. Placement is resolved from deployment/model state.

The Agent cannot provide source credentials, physical DataSource names, arbitrary SQL, DuckDB extensions, `ATTACH`, filesystem paths, or Composer network endpoints.

Source secrets are resolved only by the existing Runner. The Composer receives only intermediate data and a controlled composition plan.

Arbitrary DuckDB extension installation/loading, arbitrary filesystem access, and arbitrary network access are outside this RFC.

## Package boundaries

A target layout is:

```text
execution/
  federation/
    plan.go
    physical_planner.go
    executor.go
    limits.go
    composer.go

    arrowbridge/
      schema.go
      convert.go

    duckdbcomposer/
      composer.go
      relation.go
      runtime.go
```

Build-specific assembly stays at the application composition boundary, for example:

```text
cmd/metis/
  federation_default.go
  federation_duckdb.go
```

DuckDB/Arrow imports MUST NOT leak into semantic planning packages.

## Compatibility

### Existing single-source Projects

No API or configuration changes are required.

### Existing multi-DataSource Projects

Existing model-level placement remains valid. Dataset-level placement is additive and needed only when one semantic model intentionally spans multiple physical DataSources.

### Existing DuckDB DataSource support

RFC-0063 remains valid. A DuckDB DataSource is a normal execution Backend selected by placement.

The federation Composer is an internal runtime and MUST NOT implicitly reuse a user DuckDB DataSource file as scratch storage.

### Default builds

Default builds remain CGO-free and continue to execute existing single-source workloads.

A default build MUST detect a required federated plan and fail before starting any source fragment if no Composer is available.

## Alternatives

### Require Trino for all cross-source analysis

Rejected as the default because it introduces mandatory external infrastructure for bounded semantic joins that can be safely composed in-process. Trino remains a valid future delegated execution target.

### Build a Metis Store and workers

Rejected because it introduces storage, compute, HA, networking, lifecycle, and operational cost that duplicate mature analytical systems.

### Implement joins and aggregation directly in Go

Rejected because it would recreate vectorized execution, type coercion, memory management, spill, sorting, and relational operators already provided by DuckDB.

### Require native Arrow support from every DataSource

Rejected because Arrow is the federation interchange format, not a database admission requirement.

### Route every query through DuckDB

Rejected because it penalizes the common single-source path and bypasses source-native optimization.

### Use Go runtime plugins

Rejected for v1 because compile-time provider composition provides the required optionality without `.so` portability and toolchain-coupling costs.

### Create a complete Metis canonical physical type system

Rejected because DuckDB already provides a mature logical type/coercion system. Metis should model semantic meaning and add only the boundary validation needed for correctness.

## Rollout and migration

Implementation should proceed in phases.

### Phase 1: planning contracts

- dataset-level placement;
- Physical Planner with deterministic Placement Resolution;
- `DirectPlan` and `FederatedPlan`;
- existing execution remains `DirectPlan`;
- federation capability/error behavior;
- no DuckDB composition yet.

### Phase 2: two-source bounded composition

- Composer SPI;
- Arrow bridge;
- `federation_duckdb` build flavor;
- two source fragments;
- semantically safe equi-join;
- final projection/order;
- direct-path regression coverage.

### Phase 3: general bounded relational composition

- multiple fragments;
- UNION / UNION ALL;
- safe final aggregation;
- calculations;
- broader tested DuckDB-compatible types;
- concurrent fragment execution;
- federation observability.

### Phase 4: advanced optimization

Only after correctness evidence exists:

- streaming Arrow bridge;
- native Arrow source fast paths;
- more aggressive safe pushdown;
- explicit bounded temporary spill;
- optional full-plan delegation to configured federation-capable Backends.

## Test and acceptance criteria

### Build composition

- `CGO_ENABLED=0 go build ./cmd/metis` succeeds without the federation runtime;
- the default binary still executes existing Doris/ClickHouse single-source scenarios;
- `CGO_ENABLED=1 -tags='federation_duckdb duckdb_arrow'` builds the federation flavor;
- selecting the federation flavor without required CGO support fails clearly;
- existing `-tags='duckdb'` DataSource Backend behavior remains valid and separate;
- enabling both DuckDB DataSource support and federation Composer works in one binary.

### Placement and planning

- Placement is a deterministic dataset-to-DataSource binding, not a standalone cost-based planner in v1;
- model-level placement remains backward compatible;
- dataset placement overrides model placement deterministically;
- placement to an unapplied DataSource fails bootstrap;
- a one-source semantic plan always produces `DirectPlan`;
- a multi-source semantic plan produces deterministic `FederatedPlan` fragments;
- Agents cannot control physical placement.

### Direct execution

- direct queries do not initialize the Composer;
- direct queries do not convert results to Arrow;
- direct queries continue through the existing exact Renderer plus Runner path;
- existing result/error contracts remain unchanged.

### Federation execution

- Doris + ClickHouse can answer a semantic query requiring a cross-source join;
- Doris + DuckDB + ClickHouse can execute a three-fragment semantic query in the full test flavor;
- source-local filters and safe aggregations are pushed down;
- only required columns cross the federation boundary;
- every fragment uses its exact resolved Backend Renderer;
- fragment failure cancels sibling work;
- parent cancellation cancels source work and local composition;
- federation limit overflow fails closed;
- final output is normalized against the governed semantic output schema.

### Type conformance

Cross-source tests cover at least:

- signed integers;
- signed/unsigned compatibility where lossless;
- exact Decimal values;
- strings;
- booleans;
- dates;
- timestamps;
- NULL values;
- unsupported precision/type rejection.

No exact numeric conformance test may rely on float conversion.

### Semantic correctness

- many-to-one safe pre-aggregation is covered;
- unsafe many-to-many aggregation is rejected or planned without semantic loss;
- filters are pushed only when source-local and semantics-preserving;
- final ordering and limits remain globally correct;
- federated results are compared with an equivalent single-engine oracle for shared fixtures.

### Infrastructure behavior

- no DuckDB server is required;
- no persistent Metis Store is created;
- no worker service is required;
- query-scoped resources close on success, failure, timeout, and cancellation;
- deployments that never execute federated queries pay no DuckDB runtime cost in the default build.

## Documentation updates

Implementation must update authoritative documentation for:

- runtime bootstrap and build flavors;
- model/dataset placement;
- semantic query execution architecture;
- capability discovery;
- execution limits and observability;
- federation extension points;
- glossary definitions for fragment, federation boundary, and Composer.

RFC-0063 remains scoped to DuckDB as a user-configured execution Backend and must not be conflated with the internal federation Composer introduced here.
