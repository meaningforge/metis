# RFC-0086: Federated Semantic Query Execution

- **Status:** Draft
- **Priority:** P3 / Future capability
- **Implementation target:** Not scheduled; not a near-term delivery goal
- **Owners:** Metis maintainers
- **Created:** 2026-09-17
- **Last updated:** 2026-09-17
- **Scope:** Cross-DataSource semantic planning, bounded federation execution, Arrow interchange, embedded DuckDB composition, dialect authority, and optional federation build capability
- **Supersedes:** None

## Summary

This RFC reserves an architecture for true cross-DataSource semantic query execution while preserving the existing single-DataSource fast path.

**Priority is P3 / Future capability. Cross-source execution is not a near-term Metis implementation target.** The direct single-source semantic execution path remains the product priority. This RFC is architecture reserve, not a delivery commitment; implementation should begin only after the activation criteria in this document are met by concrete workloads.

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

A single-source query continues through the current Backend/Renderer/Runner path. Only a query whose required datasets resolve to multiple DataSources produces a `FederatedPlan`.

The embedded DuckDB federation runtime is optional and compile-time selected. The normal Metis build remains CGO-free. Metis does not add a persistent federation store, worker cluster, distributed shuffle, distributed SQL engine, or mandatory materialization layer.

## Motivation

Metis already supports a Project applying several DataSources and routing each SemanticModel to one applied DataSource. Current governed execution still resolves one model to one physical route, selects that route's Renderer, compiles one SQL query, and executes it through Runner.

That is **multi-DataSource placement**, not cross-source analysis.

A future workload may need one SemanticModel whose datasets physically live on different systems, for example:

```text
orders      -> Doris
customers   -> ClickHouse
```

while the semantic question remains ordinary: revenue grouped by customer region.

Federation should be considered only when normal data modeling, ETL/ELT, co-location, or an existing source-native federation engine is not a better answer. Requiring a separate Trino cluster for every small bounded join is unnecessary, but building a Metis Store or a custom distributed executor would be worse. The narrow missing capability is to push maximum work into each source and compose only bounded intermediate results.

## Terminology and scope

Federation is a **physical execution property**, not a semantic-model property.

Direct and federated execution use the same Ossie concepts:

- `SemanticModel`;
- `Dataset`;
- `Relationship`;
- `Metric`;
- `SemanticQuery`;
- `DataSource`.

Metis MUST NOT create parallel semantic concepts such as `FederatedMetric`, `CrossSourceDataset`, or `FederatedRelationship`.

The only physical distinction is:

```text
same SemanticQuery
      ↓
same semantic meaning
      ↓
Physical Planner
      ├── one DataSource   -> DirectPlan
      └── many DataSources -> FederatedPlan
```

Moving a dataset between DataSources may change execution mode, but MUST NOT require rewriting the metric, relationship, or caller query solely because of physical placement.

Cross-Model semantic composition is explicitly outside this RFC. A query that combines independent SemanticModels requires separate semantics for qualified refs, cross-model relationships/entities, metric ownership, and ambiguity resolution.

## Design principles

### Single-source execution remains unchanged

If all required datasets resolve to one DataSource, Metis MUST continue to use the current route:

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

Because this RFC is P3/Future, Metis MUST NOT refactor the production direct path merely to pre-stage federation.

### Databases perform database work

Source databases remain responsible for scans, source-local joins, filters, projection, aggregation, ordering where safe, indexes/materialized views, cost-based optimization, and their own distributed execution.

Metis MUST maximize safe source pushdown before data crosses the federation boundary.

### Metis does not become a SQL execution engine

Metis MUST NOT implement its own hash-join engine, vectorized executor, shuffle service, worker scheduler, persistent analytical store, or mandatory materialization/cache layer.

Remaining bounded relational composition is delegated to embedded DuckDB.

### Arrow is interchange, not a DataSource requirement

A source does not need native Arrow support. Existing row/native-protocol Drivers remain valid. Native Arrow or Arrow Flight may later be an optimization.

### Metis owns semantics; DuckDB owns bounded physical composition

Metis owns metric semantics, entities, relationships, grain, aggregation algebra, timezone intent, semantic correctness, placement, and plan decomposition.

DuckDB owns physical JOIN/UNION/final aggregation/order/window/scalar execution for bounded intermediate relations. Metis MUST NOT recreate a second complete physical coercion engine.

## Placement Resolution

Placement is the deterministic binding of a semantic dataset to a configured physical DataSource:

```text
semantic dataset -> physical DataSource
```

Placement answers **where data executes**, not how SQL is written.

The future precedence is:

```text
explicit dataset placement
        ↓
model placement
        ↓
sole Project DataSource inference
        ↓
unresolved -> deployment configuration error
```

Current Metis runtime placement is model-level. Dataset-level placement is proposed by this RFC and is not current behavior.

The future dataset form MUST reuse Ossie `custom_extensions`, not introduce a separate federation DSL. The same `METIS data_source` kind is scoped by where it appears:

```yaml
semantic_model:
  - name: commerce
    custom_extensions:
      - vendor_name: METIS
        data: '{"kind":"data_source","name":"doris-prod"}'

    datasets:
      - name: orders
        source: sales.orders

      - name: customers
        source: crm.customers
        custom_extensions:
          - vendor_name: METIS
            data: '{"kind":"data_source","name":"clickhouse-prod"}'
```

Here `orders` inherits `doris-prod`; `customers` overrides to `clickhouse-prod`. Project-applied DataSources remain the deployment allowlist. A semantic asset cannot expand Project runtime authority by naming an unapplied DataSource.

The `METIS data_source` placement kind MUST NOT be accepted on Metric or Relationship as an execution-placement authority.

Placement Resolution is part of Physical Planning, not a standalone cost-based optimizer. A true cost-based Placement Planner is justified only if the same dataset/plan has multiple viable execution locations and Metis must choose among them.

## Semantic resolution and deferred physical binding

This is a critical implementation constraint.

Current Metis semantic resolution is Renderer-aware: target-specific field/metric expressions are selected using the Renderer expression dialect before planning, and current SemanticPlan nodes carry resolved expressions. A future federation implementation therefore cannot simply remove one Renderer argument and declare the current `SemanticPlan` renderer-neutral.

The durable rule is:

> **Resolve semantic meaning before placement-dependent physical syntax; bind target-specific expressions only after the execution route is known.**

Before physical binding, Metis may resolve information that is independent of one SQL dialect:

- metric/dimension/entity/dataset/relationship identity;
- dependency closure;
- relationship cardinality and grain;
- target-independent aggregation algebra;
- data-access-policy intent and relation constraints;
- required source fields;
- semantic datatypes and governed output intent.

If the current `SemanticPlan` cannot represent that state without a Renderer, the implementation MAY introduce an internal pre-binding representation. That is an internal planning detail, not a new public semantic vocabulary.

After placement and fragmentation, each fragment resolves its route and only then binds target-specific expressions:

```text
fragment semantic intent
        ↓
resolved DataSource route
        ↓
exact Backend Renderer
        ↓
target-specific expression binding
        ↓
fragment physical plan / SQL
```

Source-specific SQL expression strings MUST NOT cross the federation boundary and be reinterpreted by DuckDB. Remaining work is represented as typed composition operations and lowered independently by the Composer.

If a semantic operation cannot be decomposed into equivalent source-local work plus composition work, the federated shape MUST fail closed.

Data-access policy is evaluated once for the governed semantic request. Source-local policy predicates are bound into the corresponding fragment after its route is resolved; federation MUST NOT create a weaker second authorization decision.

## Dialect authority and route resolution

Placement determines a DataSource, **not a SQL dialect**.

Dialect derives from the Backend bound to the resolved DataSource:

```text
Dataset
  ↓
Placement Resolution
  ↓
DataSource
  ↓
DataSource.Type
  ↓
Backend
  ├── Renderer -> SQLDialect / ExpressionDialect
  └── DriverFactory
```

Ownership is fixed:

```text
Placement Resolution: dataset -> DataSource
Backend:              DataSource type -> Renderer + DriverFactory
Renderer:             source SQL dialect + rendering
Composer:             CompositionPlan -> local physical composition
```

Metis MUST NOT add a second runtime `DialectResolver`.

A `SourceFragment` carries placement and a renderer-independent fragment intent, but MUST NOT carry an independently selected SQL dialect:

```go
type SourceFragment struct {
    ID         FragmentID
    DataSource string
    Plan       FragmentPlan
}
```

The route is resolved once and reused for both compile and execution:

```go
route, err := runtime.ResolveDataSource(fragment.DataSource)
if err != nil { return err }

compiled, err := compileFragment(fragment.Plan, route.Backend.Renderer)
if err != nil { return err }

// execution uses the same route
```

This makes inconsistent states such as Doris DataSource + ClickHouse dialect + Doris Driver structurally impossible.

## Physical execution plans

Conceptually:

```go
type ExecutionPlan interface { isExecutionPlan() }

type DirectPlan struct {
    DataSource string
    Plan       FragmentPlan
}

type FederatedPlan struct {
    Fragments   []SourceFragment
    Composition CompositionPlan
}
```

`DirectPlan` preserves current behavior.

A `FederatedPlan` contains maximal source-local fragments plus remaining composition work:

```text
f0 @ Doris
  filter last_30_days
  aggregate revenue by customer_id

f1 @ ClickHouse
  project customer_id, region

Composition
  join f0.customer_id = f1.customer_id
  aggregate revenue by region
  order revenue desc
```

## Fragmentation and semantic correctness

The Physical Planner keeps a subtree source-local when all required datasets resolve to one DataSource and the source Backend/Renderer can lower it safely. A cross-source edge introduces a federation boundary.

Primary objective:

> **Maximize source pushdown, minimize federation boundary cardinality.**

Only fields required by downstream composition cross the boundary.

Existing semantic correctness rules remain authoritative. Federation MUST reuse aggregation/fan-out evidence rather than invent a parallel metric system.

Rules include:

- distributive metrics such as `SUM` may merge partials when grain is preserved;
- `AVG` requires explicit partial state such as `SUM` + `COUNT`; average-of-averages is invalid;
- exact `COUNT DISTINCT` and other holistic metrics remain source-local or fail closed unless exact merge semantics are proven;
- `ORDER BY` / `LIMIT` are not pushed below a federation boundary if doing so can change the global answer;
- cumulative, semi-additive, window, conversion, attribution, and similar shapes remain source-local unless equivalence of decomposition is proven;
- unsafe/ambiguous many-to-many shapes fail closed.

## Source fragment execution and typed intermediate results

Federation SHOULD reuse Runner as the authority for DataSource resolution, secrets, Driver/runtime lifecycle, admission/concurrency, timeout/cancellation, row/byte ceilings, observation, and error redaction. It MUST NOT introduce a second Driver stack.

However, current `Runner.ExecuteResolved` intentionally normalizes results to the stable Agent-facing representation. Decimal values may become text and temporal values may become formatted strings. That representation is correct for the public result contract but MUST NOT become the canonical federation intermediate representation.

A future implementation therefore needs a narrow **type-preserving internal fragment boundary** before public normalization. The exact API is intentionally deferred. It may be a bounded internal Runner mode or result sink, but it should preserve the current Driver SPI where practical and keep Runner's limits/lifecycle authoritative.

Conceptually:

```text
Driver ResultStream
       ↓
Runner-owned admission / timeout / row+byte accounting
       ↓
type-preserving fragment boundary
       ↓
Arrow Bridge
       ↓
Arrow RecordBatch
       ↓
query-scoped DuckDB relation
```

Public Agent-facing normalization happens only after final composition.

## Type behavior

Arrow is the federation interchange format, not a new Metis semantic type system.

The bridge must preserve sufficient physical evidence for exact composition, including:

- Decimal precision/scale;
- integer range and signedness where relevant;
- nullability;
- date/time/timestamp representation;
- timezone meaning;
- entity-key compatibility.

Exact numerics MUST NOT be routed through `float64`.

DuckDB may perform ordinary physical coercion only after Metis proves that the semantic operation is compatible/lossless. Permissive DuckDB casts do not override Metis semantic correctness.

Unsupported high-precision or vendor/nested types required by composition fail closed.

## Embedded DuckDB Composer

DuckDB is an internal physical composition engine, not a semantic authority and not a third placement target for source data.

It receives only bounded fragment outputs and a Metis-generated `CompositionPlan`. It may perform JOIN, UNION, final aggregation, scalar calculations, projection, ordering, bounded windows, and final LIMIT.

The Composer MUST NOT receive source credentials, Agent SQL, arbitrary `ATTACH`, caller filesystem paths, or caller network endpoints.

Every federated query MUST have an isolated query-scoped Composer session/connection and relation namespace. Intermediate relations from one query MUST NOT be visible to another. A process-scoped provider may be reused, but query memory, cancellation, relation state, and cleanup are isolated.

Default federation state is ephemeral. No persistent customer-data store is required.

## Cross-source consistency model

Independent databases do not provide Metis a global transaction or shared snapshot.

Each fragment observes the consistency/snapshot semantics of its own DataSource when it executes. Concurrent execution reduces time skew but does not create cross-source snapshot isolation.

The public contract MUST NOT claim point-in-time atomicity across heterogeneous sources. Coordinated `AS OF` semantics would require an explicit capability supported by every participating Backend and is outside the baseline design.

One fragment failure cancels sibling work. Partial federated results are never returned. Retrying the whole query may observe newer source data.

## Security and data-movement governance

Federation adds a new physical action: intermediate data leaves a source database and enters the Metis process. Permission to query a semantic asset MUST NOT automatically imply permission to export arbitrary intermediate rows.

A future implementation MUST define an explicit deployment-owned federation data-movement policy/capability. When the policy is absent or denies movement, execution fails before source fragments start. This is execution/governance configuration, not a new semantic-model concept.

All row/column/data-access constraints that can execute source-locally MUST be applied before data crosses the boundary. Fragments emit only fields required by composition. Intermediate Arrow metadata MUST NOT contain credentials or secret material.

## Resource guardrails

Federation is bounded single-node composition, not arbitrary fact shuffling.

The future runtime must enforce explicit limits such as:

```yaml
federation:
  max_fragments: 8
  max_rows_per_fragment: 100000
  max_bytes_per_fragment: 67108864
  max_total_intermediate_bytes: 268435456
  max_concurrency: 4
  query_timeout: 30s
```

Exact defaults are implementation-time decisions. Existing per-DataSource policy may be stricter and remains authoritative.

Limit breaches fail closed and never return partial results. Temporary spill, if ever added, must be explicitly enabled, bounded, query-scoped, and cleaned up; it MUST NOT become a persistent Metis Store.

## Compile-time capability model

Federation is opt-in. The durable build contract is:

```text
normal Metis build
  -> CGO-free
  -> direct execution only

federation build
  -> explicitly CGO-enabled
  -> embedded DuckDB + Arrow composition available
```

The existing optional DuckDB DataSource Backend remains distinct from the internal federation Composer.

Exact future Go build-tag names and DuckDB Arrow switches are implementation details, not a long-lived public API. Metis does not use Go runtime `.so` plugins for this feature.

A build without federation may host a multi-DataSource Project and execute any direct query. If a query requires federation and no Composer is available, Metis fails before any fragment starts with a stable `FEDERATION_NOT_AVAILABLE` class.

## End-to-end lifecycle

```text
1. Agent/User submits ordinary semantic intent
2. authorize Project and semantic assets
3. resolve semantic identities, dependencies, correctness evidence, and policy intent
4. resolve dataset placement
5. fragment the query and compute safe pushdown
6. produce DirectPlan or FederatedPlan
7. for each SourceFragment:
      resolve DataSource route
      derive exact Backend Renderer + DriverFactory
      bind target-specific expressions
      compile source SQL
      execute under Runner-owned limits
8. convert bounded typed fragment outputs to Arrow
9. execute CompositionPlan in isolated embedded DuckDB session
10. normalize the final result against the governed output schema
11. return one QueryMetricsResult through REST/MCP
```

The Agent/User does not choose or need to know DataSource names, dialects, Renderers, Drivers, Arrow, DuckDB, or credentials. A future governed `EXPLAIN` may expose bounded physical diagnostics separately.

## Observability and errors

Federated execution should expose low-cardinality observations for plan mode, fragment count/backend family, fragment duration, intermediate row/byte totals, Arrow conversion, composition duration, and bounded failure classes.

Metrics MUST NOT label by Project name, DataSource identity, table, semantic ref, SQL text, credentials, values, or arbitrary error text.

Failure classes should distinguish at least federation unavailable, unsupported semantic shape, fragment limit, fragment execution, type incompatibility, policy denial, and composition failure.

## Compatibility and non-goals

Existing single-source Projects and direct queries require no API/configuration changes.

Existing model-level multi-DataSource placement remains valid. Dataset placement is additive and future-only.

RFC-0063 remains scoped to DuckDB as a user-configured DataSource Backend. Federation MUST NOT reuse a user's DuckDB database file as implicit scratch storage.

This RFC does not provide:

- cross-Model semantic composition;
- arbitrary caller-provided federated SQL;
- distributed federation workers/shuffle;
- persistent Metis federation storage;
- automatic creation of source materializations/MVs;
- unbounded raw fact-to-fact federation;
- replacement for Trino or source-native federation engines.

## Alternatives

### Require Trino for all federation

Not selected as a mandatory dependency for bounded local composition. A configured federation engine remains a valid future full-plan delegation target.

### Build a Metis Store / workers

Rejected because it creates a second data plane and duplicates mature analytical infrastructure.

### Implement relational operators directly in Go

Rejected because it recreates vectorized execution, type coercion, sorting, memory management, and spill behavior already supplied by DuckDB.

### Route every query through DuckDB

Rejected because it penalizes the common direct path and bypasses source-native optimization.

### Separate runtime DialectResolver

Rejected because Backend already owns the exact Renderer and DriverFactory. A second dialect authority can create inconsistent route/dialect/driver combinations.

## Activation criteria

This RFC is P3 / Future capability. Implementation is intentionally deferred. Work should start only when concrete product/customer evidence satisfies all of the following:

1. a real governed query requires datasets on multiple physical DataSources and cannot reasonably be solved by normal modeling, ETL/ELT, co-location, or an already-available source-native federation engine;
2. required semantics fit within one Ossie `SemanticModel` and do not depend on unresolved cross-Model composition;
3. expected intermediate data is demonstrably bounded for single-node local composition;
4. deployment governance explicitly permits intermediate data movement into the Metis process;
5. the workload justifies the added CGO/DuckDB/Arrow operational surface;
6. the direct semantic execution roadmap is not displaced by higher-value correctness, dialect, governance, or reliability work.

Absent this evidence, the correct implementation state is **not implemented**. Existing multi-DataSource Project placement continues through independent direct queries.

## Future implementation outline (non-commitment)

These stages describe dependency order, not scheduled milestones.

### Stage 1: correctness contracts

- dataset-level placement;
- separation of semantic identity/correctness from placement-dependent expression binding;
- Physical Planner + Placement Resolution;
- Backend-derived Renderer/dialect authority;
- data-movement policy and consistency contracts;
- DirectPlan/FederatedPlan definitions;
- no DuckDB composition yet.

### Stage 2: bounded two-source composition

- type-preserving fragment result boundary;
- Composer SPI with query-scoped isolation;
- Arrow bridge;
- opt-in DuckDB federation build;
- safe two-source equi-join;
- direct-path regression coverage.

### Stage 3: broader bounded composition

- multiple fragments;
- UNION / UNION ALL;
- proven-safe final aggregation/calculations;
- broader type conformance;
- bounded concurrency and observability.

### Stage 4: advanced optimization

Only after correctness evidence exists: streaming Arrow, native Arrow fast paths, more aggressive proven-safe pushdown, explicitly bounded temporary spill, or full-plan delegation to a configured federation engine.

## Future implementation gates

These are gates for a future implementation, not current-quarter deliverables.

- one-source plans preserve direct execution and never initialize Composer;
- model-level placement remains backward compatible;
- dataset placement is deterministic and cannot name an unapplied DataSource;
- Agent callers cannot control physical placement/dialect/Renderer;
- fragment dialect equals `resolvedBackend.Renderer.SQLDialect()` and compile/execute share the same route;
- public Agent-facing normalization is not the canonical typed intermediate representation;
- source-local filters and proven-safe aggregations are pushed down;
- only required fields cross the federation boundary;
- data-movement denial prevents all fragment execution;
- heterogeneous-source consistency limitations are documented;
- fragment/parent cancellation and limits fail closed without partial results;
- exact Decimal/integer/temporal semantics survive the Arrow/DuckDB boundary;
- default build remains CGO-free;
- no DuckDB server, persistent Metis Store, or worker service is required.

## Documentation updates

Any future implementation must update authoritative documentation for runtime bootstrap/build flavor, dataset placement, semantic execution architecture, data-movement governance, consistency semantics, limits/observability, federation extension points, and glossary terms.

RFC-0063 remains scoped to DuckDB as a user-configured execution Backend and must not be conflated with the internal federation Composer described here.
