# RFC-0038: Typed SQLPlan and Physical SQL Lowering

> **Design history:** This RFC preserves its original lifecycle status and rationale.
> Implementation claims describe the recorded design, not a guarantee that every
> proposed API remains current. See [current documentation](../../README.md) for
> supported interfaces. Package paths have been updated for this repository.

- **Status:** Implemented
- **Owners:** Metis maintainers
- **Created:** 2026-08-22
- **Last updated:** 2026-08-23
- **Scope:** `planner/`, `sqlplan/`, `sql/`, `sql/render/`, `engine/native/`, compiler and real-engine conformance
- **Supersedes:** None

## Later architecture

The implemented SQLPlan boundary remains current. RFC-0039 subsequently fixed
the upstream metric-only IR name and ownership as `MetricEvaluationPlan`, and
RFC-0040 made `SemanticPlan.Nodes` the typed source-aware DAG. The current
pipeline is therefore:

```text
ResolvedSemanticQuery
    -> MetricEvaluationPlan (metric-bearing queries only)
    -> SemanticPlan
    -> SQLPlan
    -> Dialect Renderer
```

The `SemanticEvaluation` placeholder used below is historical terminology, not
the current IR name.

## Summary

Metis should replace the current bare `*sqlast.Select` compiler boundary with a
typed, validated `SQLPlan`. In Go, the canonical type is `sqlplan.Plan`.

This is not a package rename. The current `sqlast` package is a syntax tree: its
root is a `Select`, CTE dependencies are implicit through nested pointers and
names, validation is distributed across builders and renderers, and several
physical rewrites still require both the SQL AST and `SemanticPlan` after the
nominal lowering boundary.

The proposed `SQLPlan` is the SQL-specific physical planning IR between
`SemanticPlan` and a concrete dialect renderer. It directly owns:

- a deterministic root query block;
- ordered query blocks and their explicit dependency edges;
- typed relations, joins, projections, predicates, grouping, ordering,
  windows, and limits;
- selected physical SQL expression leaves;
- the compile target required to validate renderer selection.

The long-term pipeline is:

```text
Semantic Query
      |
      v
SemanticEvaluation
      |
      v
SemanticPlan
      |
      v
SQLPlan
      |
      v
Dialect Renderer
      |
      v
compiler.SqlRenderResult
```

`SemanticEvaluation` is the proposed future query-scoped metric-evaluation DAG
described as long-term direction in
[RFC-0037](../semantic/0037-unified-semantic-lowering.md). It is not implemented by
this RFC and is not a dependency for SQLPlan construction. RFC-0038 depends only
on the RFC-0037 end state in which every query lowers from one canonical
`SemanticPlan`.

After migration, `planner/conversion.BuildSQLPlan` produces a validated `*sqlplan.Plan`,
`sql.Dialect.Render` consumes only that plan, and `sqlast` is deleted. Generated
SQL, parameter order and values, output schema, and real-engine results remain
unchanged unless a separately approved correctness fix documents otherwise.

## Implementation outcome

Implemented across Phases A–E. `planner/conversion.BuildSQLPlan` is the only
semantic-to-physical SQL producer, `sql.Dialect.Render` consumes only
`*sqlplan.Plan`, and the former shared SQL AST package, builder, aliases,
adapter, post-plan semantic mutations, and migration-only differential tests
have been deleted. Repository guards scan production and test Go files to keep
that boundary deleted. The frozen 96-scenario by three-target SQL and parameter
fingerprints remained unchanged through the cutover.

The durable decision is recorded in ADR-0005. Current behavior is specified in
the semantic compilation and SQL dialect-rendering specifications; this RFC is
retained as implementation history.

## Motivation

### The current boundary is an AST value, not a plan contract

The current entry point returns:

```go
func BuildSQLAST(plan *SemanticPlan) (*sqlast.Select, error)
```

and the renderer boundary is:

```go
type Dialect interface {
    Name() string
    Render(*sqlast.Select) (string, []compiler.QueryParameter, error)
}
```

`sqlast.Select` describes `WITH`, `SELECT`, `FROM`, joins, predicates,
grouping, ordering, and limit. This is useful typed syntax, but it does not
establish plan-wide identity, dependency, ownership, or validation.

In particular:

- the root is implied by whichever `Select` pointer is passed to a renderer;
- CTE identity and dependency are represented by string names and nested
  pointers rather than explicit plan edges;
- deterministic ordering is convention rather than a plan-level invariant;
- malformed state is often discovered only while rendering;
- there is no canonical clone, validation, explanation, or explicit structural
  projection for the complete physical SQL shape;
- query-block sharing and reachability cannot be stated independently of a
  particular rendered `WITH` tree.

The current code is correct for its covered queries. The problem is that the
boundary is too weak for the architecture Metis is building around it.

### Physical rewrites still cross the nominal lowering boundary

`engine/native.Compile` currently performs this sequence:

```text
SemanticPlan
    -> planner.BuildSQLAST
    -> ApplySemiAdditiveComposability(AST, SemanticPlan)
    -> NormalizeSemiAdditiveComposableInputs(AST, SemanticPlan)
    -> ApplyPostEvaluationPredicates(AST, SemanticPlan)
    -> ApplyMetricFillPolicies(AST, SemanticPlan)
    -> Dialect.Render(AST)
```

That means `BuildSQLAST` is not actually the complete physical-lowering
boundary. The native compiler still coordinates semantic-aware AST mutation,
and a consumer cannot understand the final renderable structure by inspecting
the value returned by `BuildSQLAST` alone.

`SQLPlan` must close this gap. Every correctness-affecting transformation that
requires `SemanticPlan` belongs inside `SemanticPlan -> SQLPlan` lowering. Once
the plan is returned, neither `engine/native` nor a renderer may consult
`SemanticPlan` to complete it.

### Renderer-local lowering needs an immutable input contract

Concrete renderers currently lower some typed expressions before text
rendering because supported databases differ in what they can express directly.
That responsibility remains physical and dialect-owned. However, a renderer
must not mutate the shared plan or use the absence of validation as permission
to guess.

A validated immutable-input contract lets a renderer:

- clone or visit a plan into private renderer state;
- make explicit target-specific physical choices;
- reject an unsupported typed operation;
- render deterministic SQL and parameters;
- avoid rediscovering semantic meaning.

### Developer vocabulary should match architectural responsibility

The intended layer is no longer merely “some SQL-shaped nodes.” It is the
complete, renderable SQL physical plan for one compile target. Naming that
layer `SQLPlan` makes the long-term phase sequence legible:

```text
SemanticEvaluation -> SemanticPlan -> SQLPlan -> SqlRenderResult
```

The name is justified only if the implementation gains the plan contract in
this RFC. Renaming `sqlast` to `sqlplan` while retaining a naked `Select` root
would make the vocabulary less precise, not more.

### MetricFlow prior art

MetricFlow models its SQL plan as a DAG with a render node and typed SQL plan
nodes such as tables, select statements, CTEs, literal select text, and CTAS.
Its dataflow-to-SQL conversion produces that plan before SQL rendering:

- [MetricFlow `SqlPlan`](https://github.com/dbt-labs/metricflow/blob/main/metricflow/sql/sql_plan.py)
- [MetricFlow dataflow-to-SQL-plan conversion](https://github.com/dbt-labs/metricflow/blob/main/metricflow/plan_conversion/to_sql_plan/dataflow_to_sql.py)

Metis adopts the phase boundary and explicit DAG ownership, not MetricFlow's
Python class hierarchy. Metis remains Go-native, SELECT-focused for current
compilation, concrete-dialect-driven, and independent of MetricFlow internal
APIs.

## Goals

1. Introduce `sqlplan.Plan` as the sole canonical SQL physical planning IR.
2. Make query-block identity, dependency, reachability, and root ownership
   explicit and deterministic.
3. Complete every semantic-aware physical rewrite before returning SQLPlan.
4. Give SQLPlan one validation, clone, explanation, and explicit structural
   projection contract.
5. Make every concrete dialect renderer consume only validated SQLPlan.
6. Preserve SQL bytes, parameter order and values, output schema, and execution
   results through the migration.
7. Delete `sqlast`, `BuildSQLAST`, compatibility adapters, and renderer AST
   aliases after cutover.

## Non-goals

This RFC does not introduce:

- metric dependency resolution or the future `SemanticEvaluation` IR;
- a second semantic optimizer or reinterpretation of `SemanticPlan`;
- statistics, cardinality estimation, costing, or physical join-order search;
- a generic physical algebra shared by SQL and non-SQL engines;
- query execution, connections, credentials, caching, or pre-aggregation;
- a public ANSI or portable SQL target;
- parsing arbitrary SQL strings into typed expressions;
- DDL, DML, CTAS, federation, or multi-statement plans;
- an Agent-facing raw SQLPlan API;
- a permanent AST compatibility layer;
- a change to `compiler.SqlRenderResult` or `compiler.OutputSchema` contracts.

## Design

### Boundary and package ownership

The normative flow becomes:

```text
planner.SemanticPlan
        |
        v
planner/conversion.BuildSQLPlan
        |
        v
sqlplan.Validate
        |
        v
sqlplan.Plan
        |
        v
sql.DialectRegistry
        |
        v
sql.Dialect.Render
        |
        v
compiler.SqlRenderResult
```

Package responsibilities are:

```text
planner/       SemanticPlan -> SQLPlan lowering
sqlplan/       plan types, validation, cloning, projection, internal explain
sql/           dialect registry and renderer interface
sql/render/    concrete target rendering and target-owned physical lowering
engine/native/ orchestration only
compiler/      final SqlRenderResult and OutputSchema contracts
```

Dependencies flow downward:

```text
planner -------> sqlplan
sql -----------> sqlplan + compiler
sql/render ----> sqlplan + compiler
engine/native -> planner + sqlplan + sql + compiler
```

`sqlplan` must not import `planner`, `sql`, `sql/render`, `engine`, application
packages, or any dialect implementation.

### Conceptual Go types

The exact field spelling may evolve during implementation, but the ownership
and dependency shape are normative:

```go
package sqlplan

type Plan struct {
    Target compiler.CompileTarget
    Root   QueryBlockID
    Blocks []QueryBlock // deterministic topological order
}

type QueryBlockID string

type QueryBlock struct {
    ID          QueryBlockID
    Inputs      []QueryInput
    From        RelationRef
    Projections []Projection
    Joins       []Join
    Predicates  []Predicate
    GroupBy     []Expr
    OrderBy     []Order
    Limit       *int
}

type QueryInput struct {
    Alias string
    Block QueryBlockID
    Mode  QueryInputMode // CTE or derived table
}

type RelationRef struct {
    Source *TableSource
    Input  *InputRef
    Alias  string
}
```

Every query-block dependency is an explicit `QueryInput`. External database
sources are typed `TableSource` values, not query blocks. The root identifies
the single block rendered as the final statement. Plan block order is
topological and deterministic; renderer-generated display aliases are not used
as structural identity.

The initial implementation supports a single SELECT statement whose reachable
blocks may render as CTEs or derived tables. Future statement kinds require a
new proposal rather than an untyped string field.

### Expressions remain typed

Current typed SQL expression concepts migrate from `sqlast` into `sqlplan`,
including:

- column references;
- binary and logical expressions;
- function calls;
- time-grain and calendar-shift operations;
- window, row-number, latest-value, and earliest-value operations;
- typed predicates and ordering.

Ossie permits target-selected physical SQL expression sources. SQLPlan may
therefore retain a narrowly scoped opaque expression leaf for a resolver-selected
expression fragment. It must carry enough target evidence to prove that the
fragment was selected for the plan target, and it must be non-empty. It is not
permission to introduce whole-query templates or silently parse and rewrite
arbitrary SQL.

Every newly supported physical concept that affects SQL structure should use a
typed plan field or expression variant. `RawExpr` must not become the fallback
for a missing plan design.

### Target ownership

`Plan.Target` records the exact compile target for which resolver expression
selection and semantic lowering occurred. `engine/native` must reject a plan
whose target differs from the requested target before renderer invocation.

SQLPlan remains target-oriented but renderer-neutral:

- it may contain target-selected physical expression leaves;
- it may contain abstract operations whose spelling differs by dialect;
- it does not quote identifiers, select placeholder syntax, or spell
  target-specific functions and clauses;
- it does not claim portability across registered dialects.

### Validation contract

`sqlplan.Validate` is the sole canonical plan validation entry. At minimum it
must prove:

1. the compile target and dialect are present;
2. block IDs are non-empty and unique;
3. the root block exists;
4. every input identifies a topologically earlier block;
5. all rendered blocks are reachable from the root and cycles are impossible;
6. input aliases are unique within their consumer block;
7. every relation references exactly one external source or declared input;
8. every projection has a non-empty deterministic output alias;
9. typed expressions are structurally complete;
10. predicate value cardinality matches its operator;
11. ordering and grouping expressions reference available inputs;
12. selected opaque expressions are non-empty and target-compatible;
13. nested slices, maps, expression values, and pointers are plan-owned and do
    not alias mutable construction input;
14. no query block contains a pointer back to `SemanticPlan` or another
    semantic authority.

Validation occurs after SQLPlan construction and after any typed plan rewrite.
A renderer may perform additional target capability checks, but it must not
accept structurally invalid state that the shared validator rejects.

### Clone, projection, fingerprint, and explanation

`sqlplan.Clone` returns an owned value. Renderers and test transforms must not
mutate the input plan.

SQLPlan receives an explicit typed canonical projection. It must not serialize
the Go object graph or use reflection as a fallback. Every SQL- or parameter-
affecting field has a documented projection disposition and a per-field
movement gate.

`FingerprintSQLPlan` is regression identity for the complete physical plan,
including parameter values that change the returned `compiler.SqlRenderResult`. It is
not a cache key or semantic identity. Sensitive values must never be printed as
part of fingerprint diagnostics.

An internal `ExplainSQLPlan` read model may expose block IDs, dependency edges,
operation kinds, aliases, and target information for tests and developer
diagnostics. It must redact predicate values and must not become part of REST,
MCP, or the stable Agent-facing `QueryExplanation` contract in this RFC.

### Complete lowering before renderer dispatch

`planner/conversion.BuildSQLPlan` owns all transformations that need semantic meaning.
The existing post-`BuildSQLAST` helpers for semi-additive composability,
semi-additive input normalization, post-evaluation predicates, and metric fill
must either:

- move inside SQLPlan construction; or
- become typed SQLPlan rewrites invoked inside `BuildSQLPlan` while the
  `SemanticPlan` input is still explicitly in scope.

After `BuildSQLPlan` returns successfully, its result is complete. The native
engine may validate target equality, resolve a renderer, and render. It must not
apply a rewrite that also reads `SemanticPlan`.

This creates a strict boundary:

```text
semantic evidence allowed         semantic evidence forbidden
          |                                  |
SemanticPlan -> BuildSQLPlan -> validated SQLPlan -> Renderer
```

### Renderer contract

The renderer interface becomes conceptually:

```go
type Dialect interface {
    Name() string
    Render(*sqlplan.Plan) (compiler.SqlRenderResult, error)
}
```

A renderer owns:

- concrete identifier and source quoting;
- function, operator, cast, interval, window, and time-grain syntax;
- parameter placeholder order and materialization;
- target-specific physical lowering for typed SQLPlan concepts;
- required statement settings and suffixes;
- explicit unsupported-operation errors.

A renderer does not own:

- metric, dimension, or relationship resolution;
- metric dependency traversal;
- source, join, predicate, grain, or output ownership decisions;
- semantic fill or semi-additive correctness decisions;
- execution placement, credentials, or query execution.

Renderer-local lowering must treat SQLPlan as immutable. It may produce private
renderer state or an owned transformed plan. It must not mutate the caller's
plan, and a second render of the same plan for the same target must return the
same SQL and parameters.

### Parameters and output schema

Typed predicate values remain owned by SQLPlan until rendering. The renderer
chooses placeholder syntax and returns `compiler.SqlRenderResult.Parameters` in exact
placeholder order. SQLPlan fingerprinting observes value changes, while
explanation redacts values.

`compiler.OutputSchema` continues to derive from `SemanticPlan` and remains
outside SQLPlan. SQLPlan projection order must agree with the output schema,
but Metis must not reconstruct semantic roles, Ossie datatypes, or grains by
inspecting SQLPlan or rendered SQL.

### Error behavior

Malformed SQLPlan state is an internal planning invariant failure. Unsupported
typed physical operations are explicit target compilation failures. Unknown or
missing dialects continue through the existing target/dialect error contract.

Production compilation must not catch a SQLPlan validation or render failure
and retry through legacy `sqlast`. Compatibility adapters are migration-only
test or temporary implementation mechanisms and are deleted at cutover.

## Alternatives

### Rename `sqlast` to `sqlplan`

Rejected. A naked `Select` syntax-tree root would remain a syntax tree. The
rename would promise plan-wide ownership and validation that the implementation
does not provide.

### Wrap `*sqlast.Select` in `SQLPlan`

Rejected as the final architecture. A wrapper could carry target metadata but
would leave CTE dependencies, ownership, validation, and physical rewrite
completion implicit. A wrapper may exist only as a short-lived migration
adapter with a deletion gate.

### Keep `sqlast` as a permanent internal layer below SQLPlan

Rejected for the current SELECT-only scope. Maintaining SQLPlan and a second
isomorphic SQL AST would duplicate expression and query-block concepts and make
every new physical feature cross two internal representations.

If a future renderer genuinely requires a private syntax tree, it may construct
one internally. That renderer-private value is not another shared compiler IR.

### Lower `SemanticPlan` directly in each renderer

Rejected. This would duplicate source composition, predicate placement, fill,
calendar, and stage semantics across concrete dialects and violate the dialect
extension contract.

### Introduce a generic physical relational algebra

Rejected. Metis currently produces SQL through its native semantic engine. A
backend-neutral algebra for SQL and hypothetical non-SQL engines would broaden
the product without evidence and duplicate responsibilities already separated
between `SemanticPlan` and SQLPlan.

### Expose SQLPlan to Agents

Rejected. Agents need semantic explanation and the final physical query, not
compiler-private query-block machinery. No REST or MCP contract changes in this
RFC.

### Adopt MetricFlow SQL plan types

Rejected. MetricFlow is valuable prior art, not an interoperability boundary.
Metis remains Go-native and owns a smaller type system matched to Ossie-first,
adapter-driven compilation.

## Rollout and migration

Implementation lands in reviewable phases. Structural migration phases are
byte-stable; correctness changes require separate review and evidence.

### Phase A: Inventory and freeze

1. Land RFC-0037's universal `SemanticPlan` lowering boundary or pin the exact
   compatible revision used by this migration.
2. Archive all 96-by-3 compiled SQL fingerprints.
3. Archive parameter count, order, names, values, and types for every scenario.
4. Inventory every `sqlast` constructor, mutation helper, renderer alias, and
   validation check.
5. Classify every post-`BuildSQLAST` mutation by its required semantic and
   physical inputs.
6. Add exact repository scans for production `BuildSQLAST` and `sqlast`
   consumers so the deletion scope cannot silently grow.

### Phase B: Introduce SQLPlan foundations

1. Add `sqlplan` plan, query-block, relation, expression, and input-edge types.
2. Add validation, owned clone, redacted explanation, and explicit canonical
   projection.
3. Add mutation gates proving every projected SQL- or parameter-affecting field
   moves `FingerprintSQLPlan`.
4. Add a temporary, bounded `SQLPlan -> sqlast` adapter for differential
   migration only.
5. Prove the adapter preserves query-block scope, SQL bytes, and parameters.

The adapter must be marked with the RFC phase and its deletion acceptance item.
It must not become a public API or renderer extension point.

### Phase C: Make SQLPlan the producer boundary

1. Add `planner/conversion.BuildSQLPlan` and construct explicit blocks/edges from the
   canonical `SemanticPlan`.
2. Move semi-additive, post-evaluation predicate, fill, calendar, conversion,
   and other semantic-aware SQL shaping into complete SQLPlan construction.
3. Make `engine/native` call `BuildSQLPlan`, validate it, then temporarily use
   the compatibility adapter with existing renderers.
4. Differentially compare legacy `BuildSQLAST` with SQLPlan-derived AST for the
   full corpus.
5. Prove `BuildSQLPlan` does not mutate `SemanticPlan` and returns fully owned
   state.

At the end of this phase, production semantics are owned by SQLPlan even though
the final renderer adapter still targets the old AST API.

### Phase D: Migrate renderers

1. Change `sql.Dialect.Render` to consume `*sqlplan.Plan`.
2. Migrate DuckDB, Doris, and ClickHouse to typed SQLPlan visitors/helpers.
3. Preserve the existing fail-closed dialect override boundary from RFC-0035.
4. Move renderer-only lowering into immutable-input renderer state.
5. Delete the SQLPlan-to-AST compatibility adapter.
6. Require identical SQL bytes and parameters before and after renderer
   migration for every target.

### Phase E: Delete the AST architecture

1. Delete `sqlast/` and `planner.BuildSQLAST`.
2. Delete `sql/render/ast.go` aliases and every compatibility constructor.
3. Delete native-engine post-plan mutations that consume `SemanticPlan`.
4. Update package comments and dependency guards.
5. Update current design, specifications, glossary, tests, and conformance
   documentation.
6. Deliberately update repository source-scan gates to require zero production
   `sqlast` imports.

### Rollback

Before Phase D cutover, rollback reverts the current migration phase while the
legacy renderer remains covered by differential tests. After cutover, rollback
is a normal revert of the cutover PR.

Metis will not retain a permanent feature flag, AST fallback, dual renderer
interface, or silent retry path as rollback infrastructure.

## Test and acceptance criteria

RFC-0038 may become `Implemented` only when all of the following are true:

1. Every successful native SQL compilation constructs one validated
   `sqlplan.Plan` from the canonical `SemanticPlan`.
2. Every plan has one existing root block, unique deterministic block IDs,
   explicit topologically ordered input edges, and no unreachable rendered
   blocks.
3. `planner/conversion.BuildSQLPlan` is the complete boundary for every transformation
   that requires `SemanticPlan`.
4. `engine/native` performs no semantic-aware SQLPlan mutation after
   `BuildSQLPlan` returns.
5. `sql.Dialect.Render` accepts only `*sqlplan.Plan`; concrete renderers do not
   import `planner` or consume `SemanticPlan`.
6. SQLPlan validation fails closed on cycles, missing inputs, duplicate IDs or
   aliases, unreachable blocks, invalid relations, malformed expressions,
   predicate cardinality errors, unresolved target expression leaves, and
   target mismatch.
7. Clone and ownership tests cover blocks, inputs, expressions, predicate
   values, nested slices/maps, and pointer-backed state.
8. The canonical SQLPlan projection is explicit and typed, has no reflection
   fallback, and every included field is covered by a fingerprint movement
   test.
9. SQLPlan explanation is deterministic and redacts parameter values.
10. Rendering the same plan twice is deterministic and does not mutate it.
11. All 96-by-3 generated SQL outputs remain byte-identical to the Phase A
    baseline unless a separately approved correctness fix records a changed
    fingerprint.
12. Parameter count, order, names, values, and types remain identical for the
    full corpus.
13. Optimized and unoptimized semantic plans retain their established output
    schema and execution equivalence after SQLPlan lowering.
14. DuckDB, Doris, and ClickHouse real-engine conformance passes.
15. Target mismatch and unsupported typed operations fail explicitly without a
    legacy retry.
16. `sqlast/`, `BuildSQLAST`, renderer AST aliases, adapters, and permanent
    compatibility fields no longer exist.
17. Repository source scans prove there are no production or test imports of
    `github.com/meaningforge/metis/sqlast`.
18. `compiler.SqlRenderResult`, `compiler.SqlRenderResult`, `compiler.OutputSchema`,
    execution binding behavior, REST, and MCP contracts remain unchanged.
19. Adding a concrete dialect still follows the registry and renderer workflow
    without modifying semantic resolution or creating a new SQLPlan builder.
20. `go mod tidy`, module diff checks, Ossie sync, `go test ./...`,
    `go vet ./...`, compiler conformance, and all required CI jobs pass.

## Documentation updates

Implementation must update:

- `AGENTS.md` package responsibilities and the SQL dialect extension workflow;
- `docs/design/semantic/pipeline.md` with the SQLPlan phase;
- `docs/specs/semantic/compilation-pipeline.md` with the complete
  `SemanticPlan -> SQLPlan` boundary;
- `docs/specs/sql/dialect-rendering.md` with `Render(*sqlplan.Plan)` and renderer
  immutability;
- `docs/specs/glossary.md` with `SemanticEvaluation`, `SemanticPlan`, SQLPlan,
  and SqlRenderResult distinctions;
- `docs/specs/testing/architecture.md` and
  `tests/benchmarks/CONFORMANCE.md` with structural and byte-stability gates;
- `sqlplan/doc.go` with the package contract;
- a durable ADR recording replacement of the shared SQL AST IR with SQLPlan.
