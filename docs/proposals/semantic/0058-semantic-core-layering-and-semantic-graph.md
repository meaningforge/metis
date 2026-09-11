# RFC-0058: Semantic Core Layering, Lookup, and Manifest-Scoped SemanticGraph

> **Design history:** This RFC preserves its original lifecycle status and rationale.
> Implementation claims describe the recorded design, not a guarantee that every
> proposed API remains current. See [current documentation](../../README.md) for
> supported interfaces. Package paths have been updated for this repository.

- **Status:** Implemented
- **Owners:** Metis maintainers
- **Created:** 2026-08-29
- **Last updated:** 2026-08-30
- **Scope:** `manifest/`, `resolver/`, `query/`, `planner/`, semantic compilation architecture, Ossie document/model composition, terminology, semantic-graph/lookup extraction
- **Supersedes:** None
- **Clarifies:** RFC-0037, RFC-0039, RFC-0040, RFC-0054

## 1. Summary

Metis standardizes Semantic Core around one canonical project-scoped semantic authority while preserving Apache Ossie's native hierarchy.

The semantic packaging hierarchy is:

```text
Metis Project
    -> 1..N semantic documents/sources
        -> 1..N Ossie SemanticModel
            -> 1..N Dataset
                -> physical table/view/query source
```

The important boundary is:

> **`SemanticModel` is the primary semantic-domain boundary. An Ossie document/file is a packaging and source-location boundary, not a semantic namespace.**

Apache Ossie 0.2.0.dev0 defines the document root as `version + semantic_model[]`; each `SemanticModel` is a complete semantic model containing `datasets[]`, optional `relationships[]`, and optional model-level `metrics[]`. A Dataset represents a logical dataset backed by a physical table/view or query. Metis therefore MUST NOT impose a one-file-one-model or one-model-one-table assumption.

A Metis Project MAY load multiple Ossie documents. One Ossie document MAY contain multiple SemanticModels. One SemanticModel MAY contain multiple Datasets and is expected to represent a cohesive semantic/data-mart domain such as sales analytics, including its fact/dimension datasets and relationships.

All documents loaded into one Project contribute to one canonical project `SemanticManifest`. Document boundaries do not create semantic isolation, qualification, or precedence. Conflicting canonical semantic identities MUST fail closed rather than being silently qualified by filename or load order.

The long-term compiler pipeline remains:

```text
Ossie documents / future semantic providers
                |
                v
        SemanticManifest
                |
                v
         SemanticGraph
                |
                v
     SemanticManifestLookup
         /          |          \
        v           v           v
 MetricLookup  DimensionLookup  GraphLookup
         \          |          /
          +----------+---------+
                     |
SemanticQuery -------+
                     |
                     | selected Renderer evidence
                     v
               QueryResolver
                     |
                     v
             SemanticQuerySpec
                     |
                     v
        MetricEvaluationPlanner
                     |
                     v
         MetricEvaluationPlan
                     |
                     v
              SemanticPlan
                     |
                     v
                 Optimizer
                     |
                     v
                  SQLPlan
                     |
                     | Renderer.Render(SQLPlan)
                     v
               SqlRenderResult
```

The central distinction is:

```text
Semantic document       = packaging/source-location boundary
Ossie SemanticModel     = cohesive semantic-domain boundary
Ossie Dataset           = logical dataset backed by table/view/query
SemanticManifest        = canonical project-scoped semantic truth
SemanticGraph           = reusable semantic topology
SemanticManifestLookup  = read-only semantic access facade
QueryResolver           = request-specific semantic decision process
SemanticQuerySpec       = canonical resolved query specification
MetricEvaluationPlan    = metric-domain planning IR
SemanticPlan            = source-aware logical query IR
SQLPlan                 = database-neutral physical SQL-structure IR
Renderer                = dialect-specific SQL lowering/rendering boundary
SqlRenderResult           = rendered execution artifact
```

Metis has one semantic compiler architecture. The former `CompileTarget{Engine, Dialect}` abstraction is not part of the long-term Semantic Core contract.

A compile operation selects exactly one SQL dialect before target-compatible semantic binding is required. That dialect resolves exactly one Renderer. The same Renderer provides expression-dialect/capability evidence during semantic resolution and later lowers/renders the final `SQLPlan`. A compilation MUST NOT resolve semantics against one dialect and render SQL with another.

Compile-only uses `compile_sql(... dialect=doris)`. Runtime instead derives Renderer from `DataSource.type`; runtime callers do not provide a second dialect selector.

### 1.1 Status and authority

RFC-0058 is implemented. The current architecture and normative behavior are
owned by [`docs/design/semantic/pipeline.md`](../../design/semantic/pipeline.md),
[`docs/design/operations/runtime-bootstrap.md`](../../design/operations/runtime-bootstrap.md),
[`docs/specs/semantic/compilation-pipeline.md`](../../specs/semantic/compilation-pipeline.md),
and [`docs/specs/operations/runtime-bootstrap.md`](../../specs/operations/runtime-bootstrap.md).
This RFC remains the implementation rationale and migration record.

The implementation now provides one deterministic project-scoped
`SemanticManifest`, its matching manifest-scoped `SemanticGraph` and
`SemanticManifestLookup`, and one canonical planner-facing
`SemanticQuerySpec`. Compile-only and runtime entrypoints select one Renderer
before semantic binding and preserve that exact Renderer identity through
physical rendering.

## 2. Ossie composition and semantic boundaries

### 2.1 Apache Ossie native hierarchy

Metis MUST preserve the Ossie schema's native cardinality rather than flattening it into a one-table semantic-model abstraction:

```text
Ossie document
    1 : N SemanticModel

SemanticModel
    1 : N Dataset
    0 : N Relationship
    0 : N Metric
```

A SemanticModel is a complete semantic model. Its `datasets[]` collection may naturally contain a fact table plus several dimensions, with `relationships[]` describing legal connections. Metrics are model-level and may span datasets.

Therefore Metis MUST NOT assume:

```text
SemanticModel == one physical table
SemanticModel == one YAML file
Project == one YAML file
```

### 2.2 SemanticModel is the primary domain boundary

Metis guidance is semantic-model-first rather than file-first:

```text
cohesive business/data-mart domain
        |
        v
Ossie SemanticModel
        |
        +-- fact dataset(s)
        +-- dimension dataset(s)
        +-- relationships
        `-- metrics
```

For example, a `sales_analytics` SemanticModel may contain orders, customers, products, and date datasets plus their relationships. A separate `marketing_analytics` SemanticModel may represent impressions, clicks, campaigns, and related metrics.

Metis MUST NOT define arbitrary normative table-count limits such as “4-8 datasets” or “at most 10 datasets” as if they came from Ossie. Operational guidance MAY recommend cohesive data-mart-sized models and warn against unbounded graph growth, but such numbers are implementation guidance, not Ossie schema constraints.

### 2.3 Documents are packaging boundaries

One Ossie document may contain one or more SemanticModels:

```yaml
version: 0.2.0.dev0
semantic_model:
  - name: sales_analytics
    datasets:
      # ...
  - name: marketing_analytics
    datasets:
      # ...
```

A Project may also load multiple documents when repository organization, ownership, review boundaries, or file size justify it:

```text
Project commerce
   |
   +-- commerce.ossie.yaml
   |      +-- sales_analytics
   |      `-- customer_analytics
   |
   `-- marketing.ossie.yaml
          `-- marketing_analytics
```

But the document boundary MUST NOT become a semantic namespace. Metis MUST NOT silently turn duplicate names into filename-qualified identities such as:

```text
commerce.sales_analytics
marketing.sales_analytics
```

unless a future RFC explicitly introduces a first-class namespace mechanism.

### 2.4 One Project produces one canonical SemanticManifest

All configured semantic sources for a Project are loaded, validated, normalized, and assembled into one canonical project-scoped `SemanticManifest`:

```text
semantic source A ----+
semantic source B ----+--> load/validate/assemble --> SemanticManifest
semantic source C ----+
```

Assembly MUST be deterministic and independent of filesystem enumeration order.

Document/source location MAY be retained as provenance for diagnostics, source spans, error messages, and tooling. It MUST NOT become semantic identity or alter query meaning.

Conflicting canonical semantic identities across loaded documents MUST fail closed. In particular, duplicate Ossie `semantic_model[].name` values within one Project MUST NOT be resolved by last-write-wins, first-write-wins, filename precedence, or implicit qualification.

The same principle applies to other semantic identities at the scope defined by the Ossie/Metis semantic contract: ambiguity is rejected rather than hidden by packaging order.

### 2.5 Reuse and composition are not invented by Metis v1

Current Ossie core semantics require a SemanticModel to declare its datasets/relationships inline and do not define a stable general-purpose import/ref composition contract for sharing a Dataset definition across SemanticModels.

Metis v1 MUST NOT invent an incompatible implicit cross-SemanticModel Dataset inheritance/import mechanism merely to remove duplication. Future Ossie composition/import capabilities or a separately specified Metis extension may be adopted explicitly when their identity, override, and conflict semantics are deterministic.

This means file splitting is not a substitute for semantic composition. Authors should first choose appropriate SemanticModel boundaries; document splitting is an engineering packaging choice.

## 3. Why Semantic Core layers must be explicit

When this RFC was accepted, Metis already implemented most responsibilities,
but several were fused together:

- `SemanticManifest` owns per-model relationship and metric-dependency indexes;
- `Resolver.Resolve()` performs lookup, dependency closure, source/path selection, ambiguity rejection, filter/order binding, and query-shape validation;
- `ResolvedSemanticQuery` already serves the architectural role of a planner-facing resolved query specification;
- `MetricEvaluationPlan`, `SemanticPlan`, and `SQLPlan` are already separate compiler IRs;
- transitional code still carried `CompileTarget`, semantic-engine selection, and target fields even though Metis had one semantic compiler implementation.

The implementation made the durable seams explicit without introducing a
second semantic engine, a second canonical query IR, duplicated dialect
authority, or document-level semantic namespaces.

## 4. Normative vocabulary

| Term | Lifecycle | Owns | Does not own |
| --- | --- | --- | --- |
| semantic document/source | project configuration / load-time | source location and provenance | semantic namespace or precedence |
| Ossie `SemanticModel` | semantic domain | cohesive datasets, relationships, metrics | Metis Project identity or runtime routing |
| Ossie `Dataset` | semantic model | logical dataset and physical source mapping | cross-model namespace authority |
| `SemanticManifest` | project/version scoped, immutable | canonical assembled semantic definitions, identities, validated declarations, typed analysis and derived semantic facts | request-specific choices, selected paths, SQL structure |
| `SemanticGraph` | manifest scoped, immutable/rebuildable | relationship topology, reachability, legal path evidence, reusable metric dependency topology | independent semantic truth, query-selected path, logical/SQL plan |
| `SemanticManifestLookup` | manifest scoped, read-only | stable lookup facade over one manifest and matching graph | request-specific decisions or query planning |
| `MetricLookup` | manifest scoped | metric identity, analysis, dependency/source facts, compatibility evidence | choosing query output or logical plan shape |
| `DimensionLookup` | manifest scoped | dimension identity, field/grain metadata and compatibility facts | request-specific output/ordering decisions |
| `GraphLookup` | manifest scoped | reachability, candidate paths, path evidence | choosing an ambiguous meaning for the caller |
| `SemanticQuery` | request scoped input | caller's governed semantic intent | resolved semantic identity or planner authority |
| `QueryResolver` | request scoped process | binding, validation, ambiguity rejection, deterministic path selection, filter/order target resolution, Renderer-compatible semantic evidence | metric-evaluation IR, logical optimization, SQL blocks, Renderer selection |
| `SemanticQuerySpec` | request scoped canonical value | fully resolved planner-facing semantic request | global topology, metric-evaluation topology, logical/physical plan shape |
| `MetricEvaluationPlan` | request scoped domain IR | metric roots/roles, metric kinds/specs, metric dependency edges and metric-local evidence | source/join organization, SQL block structure |
| `SemanticPlan` | request scoped logical IR | typed source-aware DAG, source roots, joins, grain, predicates, aggregation/computation boundaries, output contract | global semantic possibility space, physical SQL packing |
| `Optimizer` | request scoped process | semantics-preserving rewrites over `SemanticPlan` | semantic identity resolution, graph/path selection |
| `SQLPlan` | request scoped physical IR | database-neutral SQL blocks/CTEs/subqueries and physical reuse | semantic identity, target selection, rendered SQL strings |
| `Renderer` | process-registered implementation | SQL-dialect-specific lowering/serialization plus expression dialect/capability evidence | semantic truth, DataSource routing, connections, execution |
| `SqlRenderResult` | request scoped compiler artifact | rendered physical query plus canonical output schema | semantic discovery, runtime execution state |

## 5. SemanticManifest and SemanticGraph

`SemanticManifest` remains the sole semantic authority after configured semantic sources have been assembled.

For a fixed manifest version/digest:

```text
SemanticGraph = BuildSemanticGraph(SemanticManifest)
```

must be deterministic. The graph is discardable and rebuildable and MUST NOT introduce semantic facts not proven by the manifest.

The implemented graph composes existing proven relationship and
metric-dependency topology rather than inventing a parallel graph engine.

There remains no `SemanticPlan.Graph`. `SemanticPlan` directly owns the one query-scoped computation DAG used by validation, optimization, explain, fingerprinting, and SQL lowering.

## 6. SemanticManifestLookup and specialized lookups

Metis exposes an explicit read-only `SemanticManifestLookup` facade rather than
allowing query-time code to depend pervasively on manifest implementation
internals.

Conceptually:

```text
SemanticManifestLookup
    |- MetricLookup
    |- DimensionLookup
    `- GraphLookup
```

The durable rule is:

```text
Lookup = facts/evidence
Resolver = decisions
```

Lookup may answer whether a metric/dimension exists, metric dependencies, source datasets, dataset reachability, and candidate relationship paths. It MUST NOT choose ambiguous query meaning or logical plan shape.

## 7. Dialect selection, Renderer selection, and QueryResolver

### 7.1 Compile-only mode

`compile_sql` explicitly receives an SQL dialect:

```text
compile_sql(... dialect=doris)
    |
    v
RendererRegistry.LookupDialect("doris")
    |
    v
DorisRenderer
```

The dialect is compiler input only. It MUST NOT imply a DataSource, connection, endpoint, credential set, or runtime route.

Conceptually:

```go
type SQLDialect string

type Renderer interface {
    Dialect() SQLDialect
    ExpressionDialect() ossie.Dialect
    Capabilities() RendererCapabilities
    Render(ctx context.Context, plan *SQLPlan) (compiler.SqlRenderResult, error)
}
```

### 7.2 Execution-enabled mode

Execution Runtime does not accept an independent dialect selector. Orchestration resolves Project Registration -> DataSource -> `DataSource.type` -> Backend -> Renderer:

```text
project
   -> ProjectRegistration.data_source
   -> DataSourceRegistry
   -> DataSource.type=doris
   -> BackendRegistry[doris]
   -> DorisRenderer
```

Runtime has only one database-family authority: `DataSource.type`.

### 7.3 Resolver consumes Renderer evidence but does not select Renderer

The semantic request is resolved using immutable semantic lookup plus target-compatible evidence supplied by the already selected Renderer:

```text
SemanticQuery
    + SemanticManifestLookup
    + selected Renderer expression dialect/capabilities
        |
        v
    QueryResolver
        |
        v
SemanticQuerySpec
```

The Resolver MUST NOT select another Renderer, SQL dialect, execution DataSource, or database family.

There is no `CompileTarget{Engine, Dialect}` hop between these stages.

## 8. SemanticQuerySpec

`SemanticQuerySpec` is the one canonical planner-facing query specification after resolution. It owns request-specific facts downstream planning must not rediscover, including canonical project/model identity, resolved metric/dimension identities, filter/order targets, requested grain, selected source/path evidence, and Renderer-compatible expression evidence.

It MUST NOT own the global semantic graph, DataSource identity, runtime type, a second dialect/target authority, logical plan organization, optimizer decisions, or rendered SQL.

`ResolvedSemanticQuery` remains only as a source-compatible alias of
`SemanticQuerySpec`; it is not a peer canonical query IR.

> **One resolved query role, one canonical query-spec type.**

## 9. MetricEvaluationPlan and SemanticPlan

The planning split remains:

```text
SemanticQuerySpec
        |
        v
MetricEvaluationPlanner
        |
        v
MetricEvaluationPlan
        |
        v
SemanticPlan
```

`MetricEvaluationPlan` owns metric-domain evaluation semantics and dependencies. `SemanticPlan` owns the source-aware logical DAG over data.

Metric-free queries may bypass `MetricEvaluationPlan` and directly construct the appropriate typed source-selection `SemanticPlan` node.

## 10. Optimizer, SQLPlan, and Renderer boundary

The optimizer operates only after semantic meaning has been selected:

```text
SemanticQuerySpec
    -> MetricEvaluationPlan
    -> SemanticPlan
    -> Optimizer
    -> optimized SemanticPlan
    -> SQLPlan
    -> Renderer
    -> SqlRenderResult
```

The optimizer MAY prune/fuse/rewrite already-proven work, but MUST NOT resolve ambiguous refs, change metric identity/kind/dependency meaning, invent relationships, or select a different Renderer/dialect.

`SQLPlan` is database-neutral structured SQL intent. It MUST NOT know about a concrete database family, Renderer registry, selected DataSource type, or concrete Renderer.

Renderer owns target-native lowering and serialization:

```text
renderer.Render(SQLPlan) -> SqlRenderResult
```

The dependency direction MUST NOT be inverted into `SQLPlan.Convert(renderer.Dialect())` or equivalent target-aware behavior on `SQLPlan`.

## 11. MetricFlow mapping

The intended structural mapping remains:

| MetricFlow | Metis |
| --- | --- |
| `SemanticManifest` | `SemanticManifest` |
| `SemanticGraph` | `SemanticGraph` |
| `SemanticManifestLookup` | `SemanticManifestLookup` |
| `MetricLookup` | `MetricLookup` |
| query resolver | `QueryResolver` |
| `MetricFlowQuerySpec` | `SemanticQuerySpec` |
| `MetricEvaluationPlanner` | `MetricEvaluationPlanner` |
| `MetricEvaluationPlan` | `MetricEvaluationPlan` |
| `DataflowPlan` | `SemanticPlan` |
| SQL Plan | `SQLPlan` |
| dialect-specific lowering/rendering | `Renderer` |
| SQL | `SqlRenderResult` |

Metis intentionally keeps `SemanticPlan` instead of renaming it to `DataflowPlan`.

A structural difference is important: Metis' Ossie input model MUST preserve Ossie's ability for one SemanticModel to contain multiple Datasets. Metis MUST NOT import MetricFlow's one-primary-relation modeling assumption into the Ossie domain model.

## 12. Implementation closure

All phases are implemented and covered by package, bootstrap, application, and
conformance tests. In particular:

- project bootstrap deterministically assembles multiple semantic documents,
  rejects duplicate canonical identities, and builds the matching graph;
- `manifest.SemanticGraph` and `manifest.SemanticManifestLookup` expose
  immutable, digest-bound topology and lookup evidence;
- `resolver.SemanticQuerySpec` is the canonical planner-facing resolved query;
  `ResolvedSemanticQuery` is only a source-compatible alias and not a second IR;
- `CompileTarget`, semantic Engine selection, target-bearing plan state, and
  duplicate Renderer lookup have been removed;
- compiler/conformance tests cover deterministic planning, optimizer
  equivalence, one-Renderer identity, and DuckDB, Doris, and ClickHouse output.

### Phase A — semantic source assembly + SemanticGraph — implemented

- support one Project loading one or more configured semantic documents/sources;
- preserve Ossie's `document -> semantic_model[] -> datasets[]` hierarchy;
- assemble one deterministic project-scoped SemanticManifest;
- reject duplicate SemanticModel identities across all loaded documents;
- retain document/source provenance for diagnostics without using it as semantic namespace;
- establish one deterministic manifest-scoped graph boundary;
- preserve existing ambiguity/reachability behavior.

### Phase B — SemanticManifestLookup — implemented

- introduce one read-only lookup facade bound to one manifest digest + matching graph;
- expose metric/dimension/graph lookup capabilities;
- keep lookup deterministic and side-effect free.

### Phase C — QueryResolver / SemanticQuerySpec — implemented

- evolve current `ResolvedSemanticQuery` into the one query-spec role;
- migrate planner/lowering/explain consumers directly;
- preserve semantic behavior across supported dialects.

### Phase D — Renderer cleanup — implemented

- remove `CompileTarget`;
- remove semantic-engine selection/registry from the compile path;
- remove target identity from `SemanticPlan` and compiler artifacts where it duplicates Renderer authority;
- introduce Renderer lookup keyed by SQL dialect for compile-only;
- make runtime select Renderer only from resolved DataSource type mapping;
- preserve one selected Renderer throughout semantic binding and physical rendering.

### Phase E — conformance closure — implemented

Closure covers:

- one document containing multiple SemanticModels loads correctly;
- one SemanticModel containing multiple Datasets/relationships loads correctly;
- multiple documents under one Project assemble deterministically;
- duplicate SemanticModel names across documents fail closed;
- document filename/path does not qualify or change semantic identity;
- no load-order precedence changes semantic meaning;
- unchanged canonical compiler corpus behavior unless a separately documented correctness fix applies;
- deterministic plan fingerprints for unchanged semantic requests;
- optimized/unoptimized equivalence gates;
- Doris, DuckDB, and ClickHouse real-engine conformance;
- no alternate SQL lowering path;
- no duplicate canonical query IR;
- no duplicate runtime dialect authority.

## 13. Non-goals

This RFC does not:

- impose an arbitrary maximum number of Datasets per SemanticModel;
- require one Ossie document per Project;
- require one SemanticModel per Ossie document;
- require one Dataset/physical table per SemanticModel;
- treat document/file boundaries as semantic namespaces;
- invent filename-qualified semantic identities;
- define implicit first/last-write-wins merge behavior;
- invent cross-SemanticModel Dataset import/inheritance semantics not defined by the adopted Ossie contract;
- make `SemanticGraph` user-authored;
- create a second semantic truth beside `SemanticManifest`;
- move NL interpretation into Semantic Core;
- create `SemanticQuerySpec` beside `ResolvedSemanticQuery` as a second canonical IR;
- make DataSource configuration part of Semantic Core;
- allow runtime callers to override the dialect derived from DataSource type.

## 14. Final decision

Metis' semantic containment model is:

```text
Tenant / deployment scope
        |
        v
Metis Project
        |
        +-- semantic document A
        |      +-- SemanticModel sales
        |      |      +-- Dataset orders
        |      |      +-- Dataset customers
        |      |      `-- Dataset products
        |      `-- SemanticModel marketing
        |
        `-- semantic document B
               `-- SemanticModel fulfillment
                        +-- Dataset shipments
                        `-- Dataset warehouses

all configured documents
        |
        v
one canonical project SemanticManifest
        |
        v
SemanticGraph -> Lookup -> Resolver -> planning -> Renderer
```

The durable semantic packaging rules are:

> **SemanticModel is the primary cohesive business/data-mart semantic boundary. A SemanticModel may contain multiple Datasets and relationships.**

> **A Project may load multiple Ossie documents, and a document may contain multiple SemanticModels. Document boundaries are packaging/provenance boundaries, not semantic namespaces.**

> **All semantic documents in one Project assemble into one deterministic SemanticManifest. Duplicate canonical semantic identities fail closed; filename and load order never resolve conflicts.**

> **Metis does not invent arbitrary Dataset-count limits or implicit cross-model import semantics. Authors choose SemanticModel boundaries first and split files only for engineering organization when useful.**

The compiler lifecycle remains:

```text
SemanticManifest
 -> SemanticGraph
 -> SemanticManifestLookup
 -> QueryResolver
 -> SemanticQuerySpec
 -> MetricEvaluationPlan
 -> SemanticPlan
 -> Optimizer
 -> SQLPlan
 -> Renderer
 -> SqlRenderResult
```
