# RFC-0086: Federated Semantic Query Execution

- **Status:** Draft
- **Owners:** Metis maintainers
- **Created:** 2026-09-17
- **Last updated:** 2026-09-17
- **Scope:** Cross-DataSource semantic planning, bounded federation execution, Arrow interchange, embedded DuckDB composition, dialect authority, and optional federation build capability
- **Supersedes:** None

## Summary

This RFC adds true cross-DataSource semantic query execution to Metis while preserving the existing single-DataSource fast path.

The design follows four rules:

> **Metis understands. Databases compute. Arrow moves. DuckDB composes.**
>
> **Push down first; compose only when necessary.**
>
> **Integrate proven engines instead of rebuilding them.**
>
> **Zero additional infrastructure by default.**

Planning is organized as:

```text
Semantic Query
      ↓
Semantic Planner
      ↓
SemanticPlan
      ↓
Physical Planner
      ├── Placement Resolution
      ├── Fragmentation
      ├── Pushdown Analysis
      └── Composition Planning
      ↓
DirectPlan / FederatedPlan
```

Placement Resolution is deterministic. It answers where a semantic dataset physically executes; it is not a separate cost-based optimizer.

A single-source query stays on the current path:

```text
Semantic Query
      ↓
SemanticPlan
      ↓
Physical Planner
      ↓
DirectPlan
      ↓
DataSource -> Backend -> Renderer -> Driver
      ↓
Database
      ↓
Result
```

Only a query whose required datasets span multiple DataSources produces a `FederatedPlan`:

```text
                     FederatedPlan
                          │
          ┌───────────────┼───────────────┐
          ▼               ▼               ▼
    Fragment f0      Fragment f1      Fragment f2
     Doris            ClickHouse        DuckDB DS
          │               │               │
       source SQL      source SQL      source SQL
          │               │               │
          ▼               ▼               ▼
       Result          Result          Result
          └───────────────┬───────────────┘
                          ▼
                     Arrow boundary
                          ↓
                    Embedded DuckDB
                   JOIN / UNION / CALC
                          ↓
                     Final Result
                          ↓
               governed result normalize
                          ↓
                    Agent / User
```

The embedded DuckDB federation runtime is an **optional compile-time capability**. The default Metis build remains CGO-free.

Metis does not add a persistent federation store, worker cluster, distributed shuffle, distributed SQL engine, or mandatory materialization layer.

## Motivation

Metis already allows one Project to apply several DataSources and routes semantic models to configured DataSources. The current governed execution path still resolves one model to one physical route, selects that route's Renderer, compiles one SQL query, and executes it through Runner.

That is multi-source placement, not cross-source analysis.

A normal semantic question may require:

```text
orders      -> Doris
customers   -> ClickHouse
```

for example revenue from `orders` grouped by `customers.region`.

Requiring a separate Trino cluster for every bounded cross-source semantic join adds infrastructure that many deployments do not need. Building a Metis Store or a custom distributed executor would duplicate mature systems and make users pay additional CPU, memory, storage, network, HA, capacity planning, and operations cost.

The missing capability is deliberately narrow:

1. understand semantic meaning;
2. resolve where each required dataset lives;
3. maximize source-local computation;
4. move only bounded intermediate results;
5. delegate remaining local relational composition to embedded DuckDB.

## Design principles

### Single-source execution remains unchanged

Federation is additive.

If all datasets required by a semantic plan resolve to one DataSource, Metis MUST continue to use the existing route:

```text
SemanticQuery
    -> DataSource
    -> Backend
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
- native indexes and materialized views;
- cost-based optimization;
- distributed execution owned by the source.

Metis MUST maximize safe pushdown before data crosses the federation boundary.

### Metis does not become a SQL execution engine

Metis MUST NOT implement its own:

- hash join engine;
- vectorized executor;
- distributed shuffle;
- worker scheduler;
- persistent analytical store;
- mandatory cache/materialization subsystem.

Bounded local JOIN, UNION, GROUP BY, sorting, windows, and expressions are delegated to embedded DuckDB.

### Arrow is interchange, not a DataSource requirement

A source does not need native Arrow support.

```text
existing Driver / native source protocol
              ↓
      bounded source result
              ↓
         Arrow bridge
              ↓
       Arrow RecordBatch
              ↓
       embedded DuckDB
```

Native Arrow or Arrow Flight may be added later as an optional fast path.

### Metis owns semantic types; DuckDB owns federation execution types

Metis owns semantic meaning: metric datatype, entities, relationships, aggregation semantics, grain, timezone intent, and semantic correctness.

Metis MUST NOT create a complete duplicate physical type/coercion system for federation.

At the federation boundary, source values are mapped losslessly to Arrow/DuckDB-compatible physical types. DuckDB owns ordinary physical coercion during composition. Metis rejects mappings that lose required precision or semantic meaning.

### Federation is an optional compiled capability

The default Metis build remains CGO-free. Users who need local cross-source composition explicitly select the federation build flavor.

The existing DuckDB DataSource Backend and the federation Composer are separate capabilities:

- `duckdb`: user-configured DuckDB DataSource through normal Backend/Driver execution;
- `federation_duckdb`: internal embedded DuckDB Composer for cross-DataSource queries.

## Placement Resolution

**Placement is the deterministic binding of a semantic physical dataset to a configured physical DataSource.**

It answers one question:

```text
semantic dataset -> physical DataSource
```

Example:

```text
orders    -> doris-prod
customers -> clickhouse-prod
```

Placement does not decide SQL syntax, dialect, Renderer, Driver, join implementation, or aggregation strategy.

Placement precedence is:

```text
explicit dataset placement
        ↓
model placement
        ↓
sole Project DataSource inference
        ↓
unresolved -> deployment configuration error
```

Conceptually:

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

Physical placement remains deployment-owned. Agent and REST/MCP callers do not choose DataSources.

### Placement Resolution is part of Physical Planning

Metis does not introduce a heavyweight standalone `PlacementPlanner` in v1.

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
            remaining work -> Composer
     ↓
ExecutionPlan
```

A separate cost-based Placement Planner is justified only if a future version has multiple viable physical execution locations for the same dataset or whole plan.

## Two-phase semantic and physical lowering

Cross-source execution cannot bind the entire semantic query to one Renderer before placement is known.

### Phase A: renderer-neutral semantic resolution

Resolve:

- metrics;
- dimensions;
- entities;
- datasets;
- relationships;
- filters;
- grouping requirements;
- aggregation methods;
- required source fields;
- semantic expressions;
- semantic datatypes.

The output is a renderer-neutral `SemanticPlan`.

### Phase B: fragment-specific physical lowering

After placement and fragmentation, every source fragment resolves its exact route:

```text
Fragment f0 -> doris-prod      -> Doris Backend      -> Doris Renderer
Fragment f1 -> clickhouse-prod -> ClickHouse Backend -> ClickHouse Renderer
```

Each fragment is compiled using the Renderer from the same resolved route later used for execution.

## Dialect authority and route resolution

Placement determines a DataSource, **not a SQL dialect**.

Dialect is derived from the exact Backend bound to the resolved DataSource:

```text
Semantic Dataset
      ↓
Placement Resolution
      ↓
DataSource name
      ↓
DataSource Registry
      ↓
DataSource.Type
      ↓
Backend Registry
      ↓
Backend
  ├── Renderer
  │     ├── SQLDialect()
  │     └── ExpressionDialect()
  └── DriverFactory
```

Ownership is explicit:

```text
Placement Resolution owns:
  dataset -> DataSource

Backend owns:
  DataSource type -> Renderer + DriverFactory

Renderer owns:
  physical SQL dialect + source SQL rendering

Composer owns:
  CompositionPlan -> local physical composition
```

Metis MUST NOT introduce a second runtime `DialectResolver` for execution.

A source fragment carries placement and a renderer-neutral fragment plan, but MUST NOT carry an independently selected SQL dialect:

```go
type SourceFragment struct {
    ID         FragmentID
    DataSource string
    Plan       FragmentPlan
}
```

Execution resolves the route first:

```go
route, err := runtime.ResolveDataSource(fragment.DataSource)
if err != nil {
    return err
}

renderer := route.Backend.Renderer
compiled, err := compileFragment(fragment.Plan, renderer)
if err != nil {
    return err
}

result, err := runtime.ExecuteResolved(ctx, route, compiled, options)
```

The core invariant is:

> **A SourceFragment MUST NOT carry an independently selected SQL dialect. Its dialect is derived exclusively from the Renderer bound to its resolved Backend.**

Therefore an inconsistent state such as:

```text
DataSource = doris-prod
Dialect    = CLICKHOUSE
Driver     = Doris
```

is structurally impossible rather than merely rejected by convention.

DuckDB composition follows a different authority path. Embedded DuckDB is not a third placement chosen for source data:

```text
CompositionPlan
      ↓
DuckDB Composer
      ↓
controlled DuckDB physical lowering
      ↓
query-scoped composition
```

Source dialect selection and Composer dialect selection are separate concerns and neither is caller-controlled.

## Physical execution plan

The Physical Planner produces one of two plan families.

```go
type ExecutionPlan interface {
    isExecutionPlan()
}

type DirectPlan struct {
    DataSource string
    Plan       FragmentPlan
}

type FederatedPlan struct {
    Fragments   []SourceFragment
    Composition CompositionPlan
}
```

### DirectPlan

Selected when every required dataset resolves to one DataSource. It uses the existing exact Backend/Renderer/Runner path.

### FederatedPlan

Example:

```text
FederatedPlan
│
├── f0 @ doris-prod
│     orders
│     filter last_30_days
│     aggregate revenue by customer_id
│
├── f1 @ clickhouse-prod
│     customers
│     project customer_id, region
│
└── CompositionPlan
      join f0.customer_id = f1.customer_id
      aggregate revenue by region
      order revenue desc
```

## Fragmentation and pushdown

The Physical Planner creates maximal source-local fragments.

A subtree stays source-local when all required datasets resolve to one DataSource and the source Backend/Renderer can lower it safely.

A cross-source edge introduces a federation boundary.

```text
Doris fragment ──────────┐
                         ├── CompositionPlan -> DuckDB
ClickHouse fragment ─────┘
```

The primary optimization objective is:

> **Maximize source pushdown, minimize federation boundary cardinality.**

For example:

```text
Doris orders: 1,000,000,000 rows
        ↓
WHERE last_30_days
        ↓
GROUP BY customer_id
SUM(revenue)
        ↓
42,000 rows
        ↓
Arrow
```

is preferred over shipping one billion raw rows into Metis.

Pushdown may include:

- source-local filters;
- projection;
- source-local joins;
- semantically safe aggregation;
- decomposable metric partials;
- ordering/limits only when global semantics remain correct.

Only fields required by downstream composition cross the boundary.

### Aggregation and fan-out safety

Existing semantic correctness rules remain authoritative.

Pre-aggregation below a federation boundary is allowed only when relationship cardinality, grain, aggregation algebra, and join keys prove semantic equivalence.

Unsafe or ambiguous many-to-many shapes MUST fail closed in v1 rather than return a plausible but incorrect result.

## Source fragment execution

Federation reuses the existing Runner for every source fragment.

```go
route, err := runtime.ResolveDataSource(fragment.DataSource)
if err != nil {
    return err
}

compiled, err := compileFragment(fragment.Plan, route.Backend.Renderer)
if err != nil {
    return err
}

result, err := runtime.ExecuteResolved(ctx, route, compiled, options)
```

This preserves existing ownership of:

- secret resolution;
- Driver lifecycle;
- timeout and cancellation;
- row and byte limits;
- per-DataSource concurrency;
- result normalization;
- public error redaction;
- execution observability.

Federation MUST NOT introduce a second Driver stack.

## Federation interchange

V1 keeps the current Driver SPI compatible:

```text
Driver ResultStream
       ↓
Runner normalization
       ↓
bounded fragment ResultSet
       ↓
Arrow Bridge
       ↓
Arrow RecordBatch
       ↓
DuckDB relation
```

This may temporarily double-buffer bounded fragment results, so federation limits are mandatory.

A later additive fast path may expose Arrow batches directly from capable Backends. Native Arrow support remains optional.

## Type behavior

Metis should follow DuckDB logical types for physical federation instead of rebuilding DuckDB's coercion engine.

Examples:

```text
Doris BIGINT       -> Arrow Int64  -> DuckDB BIGINT
ClickHouse UInt64  -> Arrow UInt64 -> DuckDB UBIGINT
String/VARCHAR     -> Arrow UTF8   -> DuckDB VARCHAR
DATE               -> Arrow Date   -> DuckDB DATE
```

Exact Decimal values MUST remain exact. Converting exact numerics to `float64` is prohibited.

Metis still owns semantic-loss validation for:

- Decimal precision/scale;
- signed/unsigned overflow;
- timestamp vs timestamp-with-time-zone meaning;
- wall-clock vs absolute instant semantics;
- incompatible entity-key representations;
- unsupported nested/vendor types required by composition.

## Embedded DuckDB Composer

DuckDB is an internal physical composition engine, not a semantic authority.

It receives only bounded fragment outputs and a Metis-generated `CompositionPlan`.

It may execute:

- JOIN;
- UNION / UNION ALL;
- final aggregation;
- arithmetic/scalar expressions;
- projection;
- ordering;
- bounded window operations;
- final LIMIT.

Fragment Arrow outputs are registered as query-scoped relations such as:

```text
f0 -> __metis_f0
f1 -> __metis_f1
```

The Composer MUST NOT receive:

- source credentials;
- Agent-provided SQL;
- arbitrary DataSource names;
- caller-provided `ATTACH` commands;
- arbitrary filesystem paths;
- caller-provided network endpoints.

Default federation state is ephemeral and query-scoped. No persistent customer data store is required.

## Composer SPI

Metis Core depends on a narrow internal SPI:

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

DuckDB/Arrow concrete dependencies MUST NOT leak into Semantic Core public contracts.

## Compile-time capability model

Default build:

```bash
CGO_ENABLED=0 go build ./cmd/metis
```

Federation build:

```bash
CGO_ENABLED=1 \
  go build \
  -tags='federation_duckdb duckdb_arrow' \
  ./cmd/metis
```

DuckDB DataSource build:

```bash
CGO_ENABLED=1 go build -tags='duckdb' ./cmd/metis
```

Full build:

```bash
CGO_ENABLED=1 \
  go build \
  -tags='duckdb federation_duckdb duckdb_arrow' \
  ./cmd/metis
```

The desired pluggability is compile-time provider composition. This RFC does not use Go's runtime `plugin` package.

## Capability and failure behavior

A default build can host Projects with multiple DataSources and execute every query that produces a `DirectPlan`.

If a query requires federation but no Composer is compiled in, Metis fails **before any source fragment starts**:

```text
FEDERATION_NOT_AVAILABLE

The semantic query requires multiple DataSources, but this Metis build does not include a federation runtime.
```

Capability discovery MUST NOT open Drivers, resolve secrets, or initialize DuckDB.

## Resource guardrails

Federation is bounded single-node composition, not arbitrary remote fact shuffling.

Required controls include:

```yaml
federation:
  max_fragments: 8
  max_rows_per_fragment: 100000
  max_bytes_per_fragment: 67108864
  max_total_intermediate_bytes: 268435456
  max_concurrency: 4
  query_timeout: 30s
```

Existing DataSource limits remain authoritative and may be stricter.

When a limit is exceeded, execution fails closed. Metis MUST NOT silently attempt an unbounded fact-to-fact join merely because DuckDB can execute one.

Temporary spill, if added later, must be explicit, bounded, query-scoped, and cleaned up. It MUST NOT evolve into a persistent Metis Store.

## End-to-end query lifecycle

For a normal cross-source semantic query:

```text
1. Agent/User submits semantic intent
2. authorize Project and semantic assets
3. Semantic Planner resolves renderer-neutral SemanticPlan
4. Physical Planner resolves dataset placement
5. Physical Planner fragments the plan and computes pushdown
6. produce DirectPlan or FederatedPlan
7. for each SourceFragment:
      DataSource -> resolved Backend
      Backend -> exact Renderer + DriverFactory
      Renderer -> exact source SQL dialect
      compile source SQL
      Runner executes against that same resolved route
8. bounded source results cross through Arrow
9. DuckDB Composer lowers CompositionPlan and joins/aggregates results
10. Metis normalizes the final result against governed output schema
11. return one QueryMetricsResult through REST/MCP
12. Agent/User receives semantic result, not physical execution details
```

The Agent/User MUST NOT need to know or choose:

- DataSource names;
- SQL dialects;
- Renderer names;
- Driver types;
- Arrow;
- DuckDB;
- credentials.

The final public result does not expose credentials or internal physical routing. A separate governed `EXPLAIN` surface may expose bounded physical diagnostics in the future.

## Concurrency, cancellation, and errors

Independent fragments MAY execute concurrently under both federation-level and per-DataSource limits.

Parent cancellation cancels:

1. source fragments;
2. Arrow conversion still in progress;
3. DuckDB composition.

One fragment failure cancels siblings in v1. Partial federated results are not returned.

Errors should remain bounded and categorized, for example:

- federation unavailable;
- unsupported semantic federation shape;
- fragment limit exceeded;
- fragment execution failed;
- type incompatibility;
- composition failed.

Raw credentials, query values, or arbitrary Driver errors MUST NOT leak to callers.

## Observability

Federated execution should expose low-cardinality observations for:

- direct vs federated plan count;
- fragment count;
- Backend family per fragment;
- fragment execution duration;
- intermediate row/byte totals through typed non-label sinks;
- Arrow conversion duration;
- composition duration;
- federation limit failures;
- federation-unavailable failures.

Metrics MUST NOT label by Project, DataSource name, table name, semantic ref, SQL text, credentials, values, or raw errors.

## Security

Federation does not broaden caller authority.

Placement comes from deployment/model state. Source credentials are resolved only by the existing execution runtime. The embedded Composer receives only controlled intermediate data and a generated composition plan.

Arbitrary DuckDB extension installation/loading, caller SQL, arbitrary `ATTACH`, arbitrary filesystem access, and arbitrary network access are outside this RFC.

## Package boundaries

Target layout:

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

cmd/metis/
  federation_default.go
  federation_duckdb.go
```

Semantic planning packages MUST NOT import DuckDB/Arrow concrete runtime packages.

## Compatibility

Existing single-source Projects require no API/configuration changes.

Existing model-level multi-DataSource placement remains valid. Dataset placement is additive.

RFC-0063 remains scoped to DuckDB as a user-configured DataSource Backend. The federation Composer MUST NOT implicitly reuse a user's DuckDB database file as scratch storage.

Default builds remain CGO-free and retain existing direct execution behavior.

## Alternatives

### Require Trino for all federation

Rejected as a mandatory dependency for bounded semantic joins. Trino remains a valid future delegated execution target for large federation.

### Build a Metis Store / workers

Rejected because it creates a second data plane and duplicates mature analytical infrastructure.

### Implement relational operators directly in Go

Rejected because it recreates mature vectorized execution, type coercion, memory management, sorting, and spill behavior already provided by DuckDB.

### Route every query through DuckDB

Rejected because it penalizes the common direct path and bypasses source-native optimization.

### Separate runtime DialectResolver

Rejected because Backend already owns the exact Renderer and DriverFactory. A second dialect authority can create inconsistent route/dialect/driver combinations.

### Go dynamic plugins

Rejected for v1. Compile-time provider composition supplies the required optionality with less portability and toolchain complexity.

## Rollout

### Phase 1: planning contracts

- dataset-level placement;
- renderer-neutral `SemanticPlan` boundary;
- Physical Planner with deterministic Placement Resolution;
- Backend-derived Renderer/dialect authority;
- `DirectPlan` and `FederatedPlan`;
- federation capability/error contract;
- no DuckDB composition yet.

### Phase 2: two-source bounded composition

- Composer SPI;
- Arrow bridge;
- `federation_duckdb` build flavor;
- two source fragments;
- safe equi-join;
- final projection/order;
- direct-path regression coverage.

### Phase 3: general bounded composition

- multiple fragments;
- UNION / UNION ALL;
- safe final aggregation;
- calculations;
- broader type conformance;
- bounded concurrent fragment execution;
- observability.

### Phase 4: advanced optimization

Only after correctness evidence exists:

- streaming Arrow bridge;
- native Arrow fast paths;
- more aggressive safe pushdown;
- optional bounded temporary spill;
- optional full-plan delegation to a configured federation-capable Backend such as Trino.

## Test and acceptance criteria

### Direct path

- one-source plans always produce `DirectPlan`;
- direct execution does not initialize Composer;
- direct execution does not convert results to Arrow;
- existing exact Renderer + Runner behavior remains unchanged.

### Placement and authority

- model-level placement remains backward compatible;
- dataset placement overrides model placement deterministically;
- placement to an unapplied DataSource fails bootstrap;
- Agents cannot control physical placement;
- a SourceFragment cannot carry an independently selected SQL dialect;
- source dialect identity equals `resolvedBackend.Renderer.SQLDialect()`;
- compile and execute use the same resolved DataSource route.

### Federation

- Doris + ClickHouse can answer a governed cross-source semantic join;
- source-local filters and safe aggregations are pushed down;
- only required fields cross the federation boundary;
- fragment failure cancels sibling work;
- parent cancellation cancels source and composition work;
- federation limits fail closed;
- final output is normalized against the governed semantic output schema.

### Type conformance

Tests cover at least:

- signed integers;
- signed/unsigned compatibility where lossless;
- exact Decimal values;
- strings;
- booleans;
- dates;
- timestamps/timezones;
- NULL values;
- unsupported precision/type rejection.

No exact numeric conformance test may rely on float conversion.

### Build behavior

- `CGO_ENABLED=0 go build ./cmd/metis` succeeds without federation runtime;
- `CGO_ENABLED=1 -tags='federation_duckdb duckdb_arrow'` builds federation flavor;
- existing `-tags='duckdb'` behavior remains separate;
- enabling both DuckDB DataSource and federation Composer works in one binary.

### Infrastructure behavior

- no DuckDB server is required;
- no persistent Metis Store is created;
- no worker service is required;
- query-scoped resources close on success, failure, timeout, and cancellation;
- deployments that never use federation pay no DuckDB runtime cost in the default build.

## Documentation updates

Implementation must update authoritative documentation for:

- runtime bootstrap and build flavors;
- model/dataset placement;
- semantic query execution architecture;
- capability discovery;
- execution limits and observability;
- federation extension points;
- glossary definitions for fragment, federation boundary, Physical Planner, Placement Resolution, and Composer.

RFC-0063 remains scoped to DuckDB as a user-configured execution Backend and must not be conflated with the internal federation Composer introduced here.
