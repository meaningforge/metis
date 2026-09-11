# RFC-0037: Universal SemanticPlan DAG and Unified Lowering

> **Design history:** This RFC preserves its original lifecycle status and rationale.
> Implementation claims describe the recorded design, not a guarantee that every
> proposed API remains current. See [current documentation](../../README.md) for
> supported interfaces. Package paths have been updated for this repository.

- **Status:** Implemented
- **Owners:** Metis maintainers
- **Created:** 2026-08-22
- **Last updated:** 2026-08-23
- **Scope:** `resolver/`, `planner/`, `sqlast/`, semantic optimization, explainability, and compiler conformance
- **Supersedes:** None

## Later architecture

RFC-0037's durable result remains one universal `SemanticPlan` DAG and one
semantic-to-physical lowering entry. Later RFCs refined the representations on
both sides of that decision: RFC-0039 materialized the query-scoped
`MetricEvaluationPlan` authority anticipated below, and RFC-0040 replaced
`SemanticStage` envelopes with concrete typed `SemanticPlanNode` values stored
in `SemanticPlan.Nodes`.

References below to a future query-scoped evaluation IR or to
`SemanticPlan.Stages` describe the migration state at the time; they are not
the current type contract.

## Summary

`SemanticPlan` is the single universal semantic DAG IR for every supported
query. It directly owns ordered `SemanticStage` nodes; each stage's typed
`SemanticStageInput` values identify its dependency edges.

Before this RFC, `SemanticPlan` was canonical for every query, but staged
computation was stored behind an optional `Graph *SemanticGraph`.
`planner.BuildSQLAST` consequently had two top-level paths:

1. staged queries lowered from `SemanticGraph`;
2. non-staged queries lowered directly from plan-level projections, joins,
   predicates, groups, sorts, and limit.

The implementation removed both the lowering split and the one-to-one
`SemanticGraph` wrapper. `SemanticPlan` directly owns stages, requested outputs,
the final output contract, shared semantic domains, and optimization evidence.
There is one canonical lowering plan, one plan-owned DAG, one plan validation
entry, one plan explain entry, one plan fingerprint, and one SQL AST lowering
boundary.

That statement applies to the canonical IR consumed by query lowering; it does
not collapse metric-evaluation planning into source/stage planning. The
long-term pipeline retains two explicit responsibilities:

```text
Semantic Query
      |
      v
semantic evaluation dependency planning
      |
      v
SemanticPlan construction
      |
      v
SQL AST
```

A future query-scoped `SemanticEvaluation` may make the first result an
independently materialized metric-dependency DAG, analogous to MetricFlow's
`MetricEvaluationPlan`. RFC-0037 establishes and protects that lowering
boundary, but does not require the additional stored IR as part of the
direct/staged cutover.

Metric-bearing queries construct typed `SemanticStage` / `SemanticEvaluation`
values before source/stage lowering. Metric-free dimension and
`distinct_values` queries use a typed source-selection stage rather than a dummy
metric stage.

SQL lowering may choose a compact inline SQL shape for a one-stage plan or a
composed CTE/subquery shape for a multi-stage plan. Both are physical-shape
strategies over the same validated `SemanticPlan`; neither may reinterpret
semantic meaning from compatibility fields.

## Implementation outcome

RFC-0037 is implemented. The final closeout landed through PR #433, and
ADR-0004 records the durable one-plan-one-DAG decision independently of this
proposal. All 19 acceptance criteria are satisfied. The 96 scenarios compiled
for three targets retained all 288 compiled-SQL fingerprints byte-for-byte
through the migration, while DuckDB, Doris, and ClickHouse real-engine
conformance remained green.

The implemented `SemanticPlan` deliberately does **not** add a plan-level
`Intent query.QueryIntent` field. Query-shape distinctions that remain semantic
authority after construction live in typed stage payloads. In particular,
metric-free grouped-value and `distinct_values` queries differ through the
source-selection stage's typed `Mode`. Adding a plan-level intent field would
store the same fact twice for source-selection queries while being inert for
metric-bearing queries, creating an avoidable synchronization invariant.

This is an implementation clarification of the single-owner rule, not a new
semantic behavior: a fact that affects lowering, validation, explain, or
fingerprinting has one canonical owner.

## Motivation

### The current split is observable architecture

At the time of this proposal, 37 of the 96 core conformance scenarios reached
`BuildSQLAST` without a materialized graph. The split included ordinary shapes
such as simple metrics, aggregation variants, same-source multiple metrics,
one-hop joins, and temporal joins.

Those queries were valid and their SQL was correct. The defect was not that a
compact query lacked a DAG-shaped SQL rendering. The architectural risk was
that the two lowering paths could independently interpret:

- resolved projections and expressions;
- relationship joins and temporal join semantics;
- predicate ownership and placement;
- grouping and time grain;
- output ordering and limits;
- target capabilities and extension-derived behavior.

A new semantic feature could therefore work on staged queries while being absent
or subtly different on direct queries. Scenario-count coverage measured this
split but could not prevent the two implementations from drifting.

### The optional graph wrapper has completed its purpose

`SemanticGraph` originally separated staged semantic composition from the flat
plan used by ordinary queries. That boundary made sense while graph ownership
was optional and staged consumers were migrating.

Under the implemented architecture every successful plan has exactly one
semantic DAG. The plan and graph would otherwise have the same construction
point, owner, lifetime, validation boundary, clone boundary, optimizer boundary,
explain boundary, fingerprint boundary, and lowering boundary. Keeping a
mandatory one-to-one wrapper would preserve two names and two API surfaces
without preserving two architectural responsibilities.

The DAG did not disappear. It became the direct structure of `SemanticPlan`:

- `SemanticPlan.Stages` are the nodes;
- `SemanticStage.Inputs` are the edges;
- stage order remains topological and meaningful;
- graph-wide requested-output and semantic-domain fields became plan fields.

### Phase K makes convergence practical

Phase K established typed `SemanticEvaluation` payloads and owned stage state
for every supported metric-evaluation kind. The canonical metric construction
path already built source-aggregate stages for simple metrics; the non-staged
branch discarded that staged structure as a lowering authority after using a
temporary projection for shared-grain analysis.

Universal plan ownership therefore reused semantic facts that already existed.
It did not manufacture an empty stage merely to satisfy a coverage counter.

Two prerequisites landed while this RFC was being drafted:

- earlier implementation work made the direct/staged
  boundary an exact-set gate on all three compiler targets and rejected
  target-dependent classification;
- earlier implementation work made fusion, validation,
  source-subplan identity, and lowering consume one typed definition of source
  scan work.

This RFC preserves those gates. It does not describe them as unfinished work.

### MetricFlow prior art

MetricFlow separates two planning questions. Its query-scoped
`MetricEvaluationPlan` is a metric-dependency graph; `DataflowPlanBuilder`
traverses that graph and converts each metric-query node into a dataflow branch.
The resulting `DataflowPlan` is itself a DAG whose typed nodes represent the
relational/data-processing work required to produce the metrics. Metric and
distinct-value builders produce a dataflow plan before conversion to an SQL
plan:

- [MetricFlow `MetricEvaluationPlan`](https://github.com/dbt-labs/metricflow/blob/main/metricflow/metric_evaluation/plan/me_plan.py)
- [MetricFlow DFS evaluation planner](https://github.com/dbt-labs/metricflow/blob/main/metricflow/metric_evaluation/dfs_me_planner.py)
- [MetricFlow evaluation-to-dataflow conversion](https://github.com/dbt-labs/metricflow/blob/main/metricflow/dataflow/builder/dataflow_plan_builder.py)
- [MetricFlow engine query-to-plan path](https://github.com/dbt-labs/metricflow/blob/main/metricflow/engine/metricflow_engine.py#L614-L668)
- [MetricFlow `DataflowPlan` DAG](https://github.com/dbt-labs/metricflow/blob/main/metricflow/dataflow/dataflow_plan.py)
- [MetricFlow dataflow-to-SQL conversion](https://github.com/dbt-labs/metricflow/blob/main/metricflow/plan_conversion/to_sql_plan/dataflow_to_sql.py)

Metis adopts two conceptual properties, not MetricFlow's vocabulary or Python
types:

1. metric dependency/evaluation planning is complete before source, join,
   aggregation, and SQL-shape planning reinterpret it;
2. the logical plan consumed by lowering directly owns its DAG.

Metis uses `SemanticPlan` because it owns deterministic semantic meaning and
proof. It uses `SemanticStage` because a node represents a semantically
required evaluation boundary, not an arbitrary relational or physical dataflow
operator.

## Goals

1. Every successful `Planner.Plan` result is one validated `SemanticPlan` DAG
   with directly owned `SemanticStage` nodes.
2. `SemanticPlan` is the only canonical semantic IR; no public one-to-one graph
   wrapper competes with it.
3. `BuildSQLAST` reads semantic lowering decisions only from the validated
   plan.
4. Compact and composed SQL generation are strategies over the same plan, not
   separate semantic implementations.
5. Metric-free queries have an honest typed semantic stage.
6. Explain, fingerprinting, validation, optimization, and lowering observe the
   same plan-owned meaning.
7. Existing SQL and result behavior remains stable unless a separately
   documented correctness fix requires a change.
8. Metric dependency and typed evaluation semantics are determined before
   source/stage planning; neither `SemanticPlan` lowering nor SQL AST lowering
   recursively rediscovers them from Ossie definitions.

## Relationship to Agent benchmark v0

earlier implementation work remains useful for
measuring whether Agent workflows benefit from Metis discovery, context,
diagnostics, and compilation. earlier implementation work
landed its frozen scenario, prompt, oracle, and run-loop frame.

The benchmark is independent evidence, not an implementation gate for this RFC.
RFC-0037 addresses a measured internal architecture split: two canonical
lowering authorities could interpret the same semantic query differently, and
37 of 96 conformance scenarios exercised the direct authority. The ownership,
differential, fingerprint, and real-engine gates in this RFC are the acceptance
evidence for removing that split.

Benchmark runs may pin any suitable Metis revision and may inform later
Agent-facing product work. Their scheduling, model access, or budget did not
block RFC-0037 design, implementation, cutover, or lifecycle closure.

## Non-goals

This RFC does not introduce:

- statistics, cardinality estimation, or a cost model;
- physical join-order or access-path search;
- engine placement, distribution, or materialization decisions;
- query execution, connections, caching, or pre-aggregation;
- a generic relational algebra intended to replace `sqlast`;
- a `DataflowPlan`, `SemanticDataflow`, or second canonical lowering plan that
  competes with `SemanticPlan`;
- dialect-specific branches in semantic planning;
- a generic `SemanticNode` hierarchy or nodes for every SQL keyword;
- implementation of a separately materialized `SemanticEvaluation` in this
  RFC;
- replacement of `sqlast` with the `SQLPlan` proposed separately by
  [RFC-0038](../sql/0038-typed-sql-plan.md);
- future federation/composite-plan behavior.

## Design

### `SemanticPlan` is the DAG IR

Within this RFC's lowering scope, `SemanticPlan` is the only canonical planning
IR and directly owns its DAG. The conceptual type shape is:

```go
type SemanticPlan struct {
    Model  ModelRef
    Target compiler.CompileTarget

    Requested []string
    Stages    []SemanticStage
    Output    SemanticOutputContract

    DenseCalendar       *DenseCalendarPlan
    CustomDenseCalendar *CustomDenseCalendarPlan
    SharedGrain         *SharedGrainResolution

    OptimizationTrace []OptimizationStep
}

type SemanticOutputContract struct {
    Projections []Projection
    Grain       []GroupBy
    Predicates  []PostEvaluationPredicate
    OrderBy     []Sort
    Limit       *int
}
```

The exact field spelling may differ from this conceptual shape, but the
ownership boundaries are normative:

- plan envelope fields identify the model and target;
- semantic query-shape distinctions that survive construction are owned by the
  relevant typed stage payload, not duplicated as plan-level intent metadata;
- `Stages` plus stage `Inputs` are the semantic computation DAG;
- `Output` is the final result/sink contract;
- plan-level calendar/shared-grain domains apply across stages;
- optimization trace remains diagnostic state, not semantic meaning.

The pipeline is:

```text
ResolvedSemanticQuery
        |
        v
metric evaluation planning
  |-- dependency closure and order
  |-- typed evaluation kind/payload
  `-- metric-local filters and time semantics
        |
        v
SemanticPlan
  |-- envelope
  |-- Stages + Inputs (DAG)
  |-- Output contract
  `-- shared semantic domains
        |
        v
unified SemanticPlan lowering
        |
        v
SQL AST
        |
        v
dialect renderer
```

### Long-term evaluation-to-plan boundary

The long-term conceptual alignment is:

```text
             MetricFlow                         Metis

QuerySpec                              Semantic Query
   |                                        |
   v                                        v
MetricEvaluationPlan                 SemanticEvaluation
   |                                        |
   v                                        v
DataflowPlan                          SemanticPlan
   |                                        |
   v                                        v
SQL Plan                              SQLPlan
   |                                        |
   v                                        v
Rendered SQL                         compiler.SQLRenderResult
```

The future plan-level `SemanticEvaluation` is a query-scoped metric evaluation
dependency DAG, not the model-scoped `catalog.MetricDependencyGraph` or the
`SemanticGraph` wrapper removed by this RFC. The current type also named
`SemanticEvaluation` is only one stage's tagged payload; a focused follow-up RFC
must rename that payload (for example, to `MetricEvaluation`) before the
plan-level name is introduced. The plan-level value would answer which metrics
must be evaluated, how they depend on one another, and which typed metric
semantics apply. It would not own dataset roots, relationship join paths, source
predicates, relational aggregation branches, or SQL shape.

`SemanticPlan` answers how resolved semantic work is organized into source and
composition stages that can be validated, optimized, explained, and lowered.
It owns source selection, joins, predicate placement, aggregation/output grain,
stage composition, and the final output contract.

The conversion need not be one-to-one. One evaluation node may lower to one or
more semantic stages, and multiple compatible evaluations may share proven
source work. The conversion must be deterministic and must not rediscover metric
dependency or metric kind.

RFC-0037 does not materialize plan-level `SemanticEvaluation` because current
`SemanticStage` construction already combines the evaluation result with stage
planning, and immediately adding another stored DAG would mostly duplicate
`ID`, `Kind`, `Inputs`, and typed payload. This RFC instead makes the phase
boundary normative and prevents `SemanticPlan`/SQL lowering from absorbing
evaluation planning. A follow-up RFC may extract the standalone IR when it has
independent consumers, optimization/validation needs, one-to-many lowering, or
interchange value.

Metric-free and `distinct_values` queries do not fabricate an evaluation plan.
They enter the universal `SemanticPlan` through typed source-selection stages.
Thus universal `SemanticPlan` coverage and universal metric-evaluation coverage
are deliberately different claims.

The intended evolution sequence remains:

1. RFC-0037 makes `SemanticPlan` universal, deletes the direct lowering path,
   and establishes a tested evaluation-to-stage construction seam.
2. A follow-up proposal evaluates and, if justified, materializes
   `SemanticEvaluation` with its own typed metric evaluations, dependency
   validation, explanation, and deterministic projection.
3. `SemanticEvaluation -> SemanticPlan` becomes an explicit lowering pass;
   source/stage planning consumes resolved evaluation nodes and never traverses
   Ossie metric dependencies itself.
4. Independently, RFC-0038 replaces the current SQL AST boundary with
   `SemanticPlan -> SQLPlan -> dialect renderer`; SQLPlan remains unaffected by
   whether evaluation planning is internally materialized.

### Why `SemanticStage`, not `SemanticNode`

`SemanticStage` continues to represent one semantically required evaluation
boundary. Its kind, output grain, predicate-boundary proof, source requirements,
joins, and typed evaluation payload define that boundary.

Renaming it to `SemanticNode` would add no capability and would encourage a
generic scan/filter/project/join operator hierarchy that duplicates `sqlast`.
A stage is already a DAG node because it has an ID and typed input edges; its
name describes its semantic responsibility, not its container shape.

### Plan coverage by query shape

#### Metric-bearing queries

The planner retains the `SemanticStage` values produced by canonical metric
evaluation construction regardless of the historical `RequiresMetricStaging`
classification.

- A simple metric is represented by its source-aggregate stage.
- Multiple same-source metrics are represented by their source stages and
  proven share-group evidence.
- Derived, cumulative, time-offset, offset-to-grain, conversion, and
  semi-additive metrics retain their typed stages.
- Multi-source and post-aggregate composition retain their dependency edges and
  grain proofs.

These are real semantic evaluations. They are not wrapper or compatibility
nodes.

#### Metric-free and distinct-value queries

A query with dimensions and no metric has no metric evaluation to put into a
`SourceMetricEvaluation`. The plan therefore uses a typed source-selection stage
kind, conceptually:

```go
const EvaluationSourceSelection SemanticStageKind = "source_selection"

type SourceSelectionMode string

const (
    SourceSelectionGroupedValues  SourceSelectionMode = "grouped_values"
    SourceSelectionDistinctValues SourceSelectionMode = "distinct_values"
)

type SourceSelectionEvaluation struct {
    Mode SourceSelectionMode
}
```

The stage owns the resolved root, relationship joins, selected dimensions,
grouping/grain, source predicates, and predicate-boundary evidence. A normal
dimension-only query and a `distinct_values` query differ through one typed
mode, not through an empty metric payload, a plan-level intent copy, or an
unvalidated boolean outside the stage.

Every stage carries exactly one typed evaluation variant. Metric-free queries
do not pretend to evaluate a metric.

### Single-owner plan contract

Every semantic field that can change planning, explain output, or generated SQL
appears exactly once in canonical `SemanticPlan` ownership:

- stage-local computation, joins, source predicates, and proof live on a stage;
- requested output order, final predicates, ordering, and limit live in
  `SemanticOutputContract`;
- target lives in the plan envelope;
- query-shape distinctions that survive construction live in typed stage
  payloads such as source-selection `Mode`, not duplicated plan-level intent;
- cross-stage domains and shared-grain proof live at plan scope.

Flat query-shape fields may remain as construction inputs where source/stage
planning still requires them, but after canonical stage materialization they are
not downstream authority. Mutation of construction-only state must not silently
change SQL, explain output, or fingerprints.

The canonical plan projection remains explicit and typed. It must not serialize
or hash the Go object graph, and it must not use reflection as a fallback for a
new semantic field.

### Validation

`ValidateSemanticPlan` is the only public canonical validation boundary. It
proves:

1. stage IDs are unique and inputs identify existing, topologically earlier
   producers;
2. every stage carries exactly one evaluation variant matching its kind;
3. source-selection stages carry no metric-only payload;
4. metric stages retain the existing dependency, grain, predicate, ownership,
   extension, and sharing invariants;
5. every requested output is produced by a reachable stage;
6. output ordering and final predicates reference produced outputs;
7. plan-owned slices, maps, and pointer-backed values do not alias mutable
   construction input;
8. typed stage mode/evaluation payload, stage shape, and output contract agree.

`ValidateSemanticGraph` and independent graph validation disappeared after
migration. Internal helpers such as `validateSemanticStages` may keep graph
algorithms focused, but they do not define another stored IR or public
validation contract.

Unsupported stage kinds, unknown semantic-critical extension effects,
incomplete output ownership, and missing required stages fail closed.

### Unified lowering boundary

`BuildSQLAST` no longer branches on `plan.Graph != nil` and no longer interprets
legacy flat fields as downstream semantic authority. Its shape is conceptually:

```go
func BuildSQLAST(plan *SemanticPlan) (*sqlast.Select, error) {
    if err := ValidateSemanticPlan(plan); err != nil {
        return nil, err
    }
    return lowerSemanticPlan(plan)
}
```

`lowerSemanticPlan` may choose an internal physical-shape strategy from the
validated stages and boundaries:

- **compact** for a source-selection or source-aggregate plan that can be
  expressed as one select;
- **composed** for plans requiring aggregate boundaries, source composition,
  windows, calendars, or other semantic isolation.

The strategy choice affects SQL shape only. It cannot redo semantic resolution,
predicate ownership, join safety, source sharing, extension interpretation, or
grain compatibility.

Both strategies reuse the same helpers for expressions, joins, predicates,
grouping, output projection, ordering, and limits. A strategy that cannot lower
a validated plan fails explicitly; it must not fall back to the old direct
interpreter.

### Optimizer and explainability

Semantic optimization runs against the plan-owned stages for every query. Rules
remain proof-gated and engine-neutral. A plan's small size is not a reason to
bypass normalization or validation, although a rule may naturally make no
change.

Removing `SemanticGraph` did not broaden optimizer authority. A rule receives
only a bounded, owned stage-DAG rewrite state and the explicitly permitted
output/grain context. That state is transient and unexported; it is neither a
stored plan nor a downstream lowering input. Applying a rewrite explicitly
updates and revalidates the canonical `SemanticPlan`.

Agent-facing explain and lineage consume `SemanticPlan` only. Compact queries
therefore expose their real source boundary and predicate/join evidence without
inventing CTEs or physical execution steps.

Optimization trace remains separate from semantic explanation. A compact SQL
strategy choice is physical-shape diagnostics, not semantic meaning, and must
not alter semantic explain output.

### Fingerprints and identity

`FingerprintSemanticPlan` is the sole full-plan semantic fingerprint. It
projects the plan envelope, stages/edges, output contract, and semantic domains.
There is no nil-graph branch and no independent `FingerprintSemanticGraph`
contract after migration.

Set-like fields are canonicalized; ordering that changes output or evaluation
remains sequence-sensitive. Fingerprints remain regression evidence. They are
not cache keys, physical-plan identities, or cost decisions.

Two fingerprint contracts were intentionally different during this migration:

- the 96-by-3 compiled fingerprints hash generated SQL and remained byte stable
  unless an independently approved correctness fix changed SQL;
- `FingerprintSemanticPlan` describes semantic IR structure and was expected to
  move when ownership moved from compatibility/graph fields into the direct
  plan, even when generated SQL did not change.

A semantic-plan fingerprint change required an explicit projection disposition,
the per-field movement gate, and review of why ownership changed. It was not a
failure of the compiled-SQL byte-stability acceptance criterion.

Since PR #407, `sourceEvaluationSubplanIdentity`, fusion, validation, and
lowering consume the same typed source-scan-work definition. RFC-0037 treats
that constructional unification and its partition gate as a prerequisite that
must remain true; it does not reimplement it.

### `RequiresMetricStaging`

`RequiresMetricStaging` is no longer an ownership or lowering-path switch. It
was retained temporarily as construction evidence during migration, then
removed from the durable resolver/planner routing contract once no consumer
treated it as semantic authority.

Compact versus composed lowering is derived from validated plan stages and
their boundaries.

### Extension behavior

Unknown vendor extensions continue to survive ingestion. Semantic-critical
extension effects must be resolved into typed expressions, typed evaluation
payloads, or other explicit plan-owned fields before lowering.

The universal plan does not require an extension to expose an arbitrary
canonical hash form. Capability/evidence values that do not affect planning or
SQL remain excluded from lowering identity. Unsupported semantic-critical
effects fail closed.

## Alternatives

### Keep the permanent direct/staged split

This would minimize migration work, but leave two semantic interpreters and make
every future feature prove parity twice. Count-based conformance detects routing
changes, not semantic drift inside either branch.

Rejected as the long-term architecture.

### Keep a mandatory `SemanticGraph` wrapper

This would preserve graph-focused APIs, but every plan would own exactly one
graph with the same lifetime and authority. Validation, clone, fingerprint,
explain, optimizer, and lowering would continue to expose paired plan/graph
entry points even though only one canonical object exists.

Rejected. The graph structure remains directly represented by plan stages and
input edges; the redundant ownership wrapper does not.

### Rename the graph to `SemanticDataflow` or add `DataflowPlan`

This would resemble MetricFlow terminology but add no structural capability.
It would also suggest generic data movement or relational operators rather than
semantic evaluation boundaries.

Rejected. Metis aligns on one-plan-DAG structure while retaining
`SemanticPlan` / `SemanticStage` vocabulary.

### Materialize plan-level `SemanticEvaluation` in this RFC

This would make the long-term metric-evaluation boundary concrete immediately,
but the current stage construction already contains both the evaluation node
shape and the planned source boundary. Splitting it during the direct/staged
cutover would add a second migration axis and initially create a mostly
one-to-one graph copy.

Deferred to a focused follow-up RFC. RFC-0037 preserves the boundary and adds
acceptance gates that prevent evaluation planning from leaking downstream, so
later extraction does not require another semantic-lowering redesign.

### Put every query through the existing staged lowerer unchanged

This would raise graph coverage quickly, but metric-free queries would need an
empty or fake metric evaluation, and compact queries could inherit unnecessary
CTE shape.

Rejected. A universal plan DAG does not require universal staged SQL shape.

### Share helper functions but retain two top-level semantic inputs

Common helpers would reduce code duplication, but the direct path would still
read flat state while the staged path reads graph state. Ownership, validation,
optimizer, explain, and fingerprint behavior could still diverge.

Useful as a migration technique, insufficient as the final contract.

### Model every relational operation as a plan node

A generic scan/filter/project/join/aggregate/sort algebra could make SQL
rewriting more conventional, but it would duplicate `sqlast`, invite physical
planning responsibilities, and weaken the semantic-stage boundary.

Rejected for this RFC. New stage kinds are added only when a distinct semantic
evaluation boundary cannot be represented honestly by existing stages.

## Rollout and migration

### Phase A: Baseline and shadow construction

1. Preserve PR #405's exact direct/staged set on all three compiler targets as
   the pre-migration boundary.
2. Archive the existing 288-entry compiled-SQL fingerprint output as the
   byte-stability baseline. Record semantic-plan fingerprints separately as
   structural diagnostics, not as bytes required to survive ownership changes.
3. Retain and validate existing metric stages for currently direct
   metric-bearing queries without changing production lowering.
4. Separate metric evaluation construction from source/stage enrichment in
   code and tests, without requiring a separately stored evaluation DAG.
5. Differentially compare the proposed direct plan ownership with legacy flat
   and graph fields.

### Phase B: Make `SemanticPlan` the universal DAG

1. Add the typed source-selection stage.
2. Construct stages for dimension-only and `distinct_values` queries.
3. Move `SemanticGraph.Stages`, requested outputs, graph-wide domains, and final
   output semantics into direct `SemanticPlan` ownership.
4. Introduce the single `SemanticOutputContract` and eliminate duplicate output
   authority.
5. Add clone/ownership/disposition gates for every newly plan-owned field.

### Phase C: Migrate canonical consumers

1. Make validation, optimization, explain, lineage, and semantic fingerprinting
   consume `SemanticPlan` as their sole canonical input.
2. Preserve the optimizer's bounded rewrite-state contract without retaining a
   public graph IR.
3. Extract shared typed lowering primitives.
4. Implement compact and composed strategies over `SemanticPlan`.
5. For the direct corpus, generate legacy and plan-derived SQL in tests and
   require byte equality.
6. Compare optimized/unoptimized semantics and execution results.

Differential mode was a test and migration mechanism, not a permanent runtime
fallback. Production never silently retries through the legacy path after a
plan-lowering failure.

### Phase D: Cutover and deletion

1. Route every supported query through `SemanticPlan` lowering.
2. Remove the `plan.Graph == nil` branch and direct interpreter from
   `BuildSQLAST`.
3. Delete `SemanticGraph`, `SemanticPlan.Graph`, graph clone/accessor APIs,
   `ValidateSemanticGraph`, and `FingerprintSemanticGraph` after their consumers
   migrate.
4. Remove `RequiresMetricStaging` as a downstream routing contract.
5. Delete flat/direct compatibility fields once no construction code requires
   them.
6. Deliberately update PR #405's exact-set conformance gate from the measured
   59/37 semantic routing split to universal plan coverage; compact/composed
   remains only a physical-shape distinction.

Each phase was independently reviewable. A SQL change discovered during
migration had to be fixed behind its own documented correctness test or the
migration remained byte-stable; the RFC was not blanket authorization for query
shape churn.

### Rollback

Before cutover, rollback meant reverting the current migration phase while the
legacy path remained covered by differential tests. After cutover, rollback is a
normal code revert of the cutover commit or PR.

Metis does not retain a permanent feature flag, hidden direct fallback, graph
wrapper, or dual semantic authority as rollback infrastructure.

## Test and acceptance criteria

RFC-0037 is `Implemented`; all of the following acceptance criteria are met:

1. Every supported query returned by `Planner.Plan` is a validated
   `SemanticPlan` with directly owned stages, input edges, and output contract.
2. `SemanticGraph` and `SemanticPlan.Graph` no longer exist as production types
   or fields; the DAG is represented directly by `SemanticPlan.Stages` and
   `SemanticStage.Inputs`.
3. Metric-bearing simple queries use their real typed source-aggregate stages;
   metric-free queries use a typed source-selection stage; no dummy payload is
   accepted.
4. `BuildSQLAST` has one `SemanticPlan` entry and no nil-graph or legacy-flat
   semantic dispatch.
5. `RequiresMetricStaging` no longer controls plan ownership or lowering
   dispatch.
6. Every compiler target constructs the universal semantic plan; compact versus
   composed classification is physical-shape diagnostics rather than a second
   semantic authority.
7. The existing 288 compiled-SQL fingerprints remained byte-identical through
   the RFC-0037 migration.
8. DuckDB, Doris, and ClickHouse real-engine execution conformance passes.
9. Dimension-only and `distinct_values` queries have explicit planner,
   lowering, result, and real-engine conformance coverage.
10. Legacy-vs-plan differential tests covered projections, joins, temporal
    joins, predicates, grouping/grain, sorting, limits, parameters, calendars,
    and semantic-critical extensions during migration.
11. Optimized and unoptimized plans remain semantically equivalent, with
    explain/lineage invariance and deterministic fingerprints.
12. Ownership tests prove that stages, inputs, output fields, typed evaluations,
    extension evidence, and nested mutable storage do not alias construction
    inputs.
13. Optimizer rules remain confined to bounded, owned stage-DAG rewrite state;
    flattening the DAG into `SemanticPlan` does not grant arbitrary plan access.
14. PR #407's shared typed source-scan-work definition and biconditional
    fusion/validation partition gate remain green; source-sharing behavior and
    SQL output stay independently gated.
15. No dialect renderer, execution binding behavior, public transport contract,
    or engine execution responsibility moves into `SemanticPlan`.
16. The old direct semantic interpreter and every permanent fallback are
    deleted.
17. `go test ./...`, `go vet ./...`, Ossie sync checks, compiler conformance,
    and all required CI jobs pass.
18. For every metric-bearing query, dependency closure, order, metric kind,
    typed evaluation payload, and metric-local semantics are fixed before
    source/stage enrichment; `SemanticPlan` lowering and SQL AST lowering do not
    recurse through Ossie metric definitions to rediscover them.
19. Every requested metric is traceable through the plan to its complete
    evaluation dependency closure; metric-free and `distinct_values` queries
    are explicitly exempt from metric-evaluation coverage while still required
    to produce a universal `SemanticPlan`.

## Documentation updates

Implementation updated:

- `docs/design/semantic/pipeline.md` with the universal `SemanticPlan` DAG and
  unified lowering flow;
- `docs/specs/semantic/compilation-pipeline.md` with the normative plan-based
  lowering boundary;
- `docs/specs/semantic/semantic-plan-node-model.md` with direct plan ownership,
  source-selection, and removal of the graph wrapper;
- `docs/specs/semantic/semantic-optimizer.md` with all-query plan dispatch and
  bounded optimizer rewrite state;
- `docs/specs/glossary.md` so `SemanticPlan` is the canonical DAG term and
  `SemanticGraph` is removed from current vocabulary;
- `docs/specs/testing/semantic-plan-quality-conformance.md` and
  `tests/benchmarks/CONFORMANCE.md` with universal coverage and differential
  gates;
- `docs/decisions/semantic/0004-semantic-plan-is-the-dag.md`, which records the
  durable one-plan-DAG decision and removal of the `SemanticGraph` wrapper.
