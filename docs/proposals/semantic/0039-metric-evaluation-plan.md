# RFC-0039: Query-Scoped MetricEvaluationPlan and Explicit Semantic Lowering

> **Design history:** This RFC preserves its original lifecycle status and rationale.
> Implementation claims describe the recorded design, not a guarantee that every
> proposed API remains current. See [current documentation](../../README.md) for
> supported interfaces. Package paths have been updated for this repository.

- **Status:** Implemented
- **Owners:** Metis maintainers
- **Created:** 2026-08-23
- **Last updated:** 2026-08-24
- **Scope:** `resolver/`, `planner/`, metric evaluation planning, semantic-plan construction, validation, explainability, fingerprinting, and evaluation-to-plan lowering
- **Supersedes:** RFC-0031's metric-evaluation ownership and naming decision; RFC-0040 subsequently replaced the intermediate stage representation with concrete typed `SemanticPlanNode` values

## Summary

Metis makes metric evaluation planning an explicit query-scoped compiler IR.

The final compilation pipeline is:

```text
ResolvedSemanticQuery
        |
        | BuildMetricEvaluationPlan
        v
MetricEvaluationPlan
        |
        | LowerMetricEvaluationPlan
        v
SemanticPlan
        |
        | semantic optimization
        v
SemanticPlan
        |
        | physical SQL lowering (RFC-0038)
        v
SQLPlan
        |
        | dialect rendering
        v
compiler.SqlStatement
```

The four main objects have deliberately different responsibilities:

```text
ResolvedSemanticQuery
    resolved query references and target-compatible expressions

MetricEvaluationPlan
    which metrics must be evaluated, why they are required,
    what each metric means, and how metric evaluations depend on one another

SemanticPlan
    how those metric semantics are organized as typed plan nodes over datasets,
    relationships, grain, predicates, shared work, and final output

SQLPlan
    how proven semantic work is packed into target-independent SQL
    query blocks / CTEs before dialect rendering
```

`MetricEvaluationPlan`, `SemanticPlan`, and `SQLPlan` are all **static compiler
IR objects**. The processes are the verbs around them: build, validate, lower,
optimize, and render.

This RFC therefore rejects the interpretation that "evaluation" is a runtime or
in-place execution phase that mutates a `SemanticPlan`. Metis does not execute
query data. It constructs a metric-evaluation plan, lowers that plan into a
source-aware semantic plan, then lowers again into SQL structure.

## Implementation closure and RFC-0040 amendment

RFC-0039 was implemented through Phases A-G and closed by
earlier implementation work. The final implementation
is based on the typed-node architecture introduced by RFC-0040:

```text
ResolvedSemanticQuery
        |
        v
MetricEvaluationPlan
        |
        v
SemanticPlan.Nodes []SemanticPlanNode
        |
        v
SQLPlan
```

Phase A originally renamed the old stage-local `SemanticEvaluation` payload to
`SemanticStageEvaluation`. That was a valid migration step at the time, not the
final representation. RFC-0040 subsequently deleted `SemanticStage`,
`SemanticStageEvaluation`, and the stage-operation compatibility envelope.
RFC-0039's final lowering therefore creates concrete typed nodes such as
`SourceAggregateNode`, `PostAggregateNode`, `JoinAggregatesNode`,
`CumulativeWindowNode`, and `SourceSelectionNode` directly.

The closure evidence is:

- exactly one validated `MetricEvaluationPlan` is built for each metric-bearing
  `Planner.Plan` call and reused across predicate planning and semantic lowering;
- metric-free and `distinct_values` queries bypass metric evaluation planning
  and construct a typed `SourceSelectionNode` directly;
- separate metric identities share compatible source aggregation and expose
  reusable outputs without redundant branches;
- all 96 canonical scenarios compile for DuckDB, Doris, and ClickHouse, with
  all 288 SQL fingerprints and all 288 exact-parameter records unchanged;
- DuckDB, ClickHouse, and Doris real-engine conformance passed on final
  implementation head `d5f578274c947092b56251b64b358d6d406a3651` in
  earlier implementation work;
- current specifications, design documentation, ADR-0006, and the RFC-0030,
  RFC-0031, RFC-0037, RFC-0038, and RFC-0040 evolution notes describe the final
  authority and cardinality contracts.

## Normative vocabulary

The vocabulary in this section is normative. New code, tests, RFC updates, and
architecture documentation should use these meanings consistently.

| Term | Meaning | Owns | Does not own |
| --- | --- | --- | --- |
| `ResolvedSemanticQuery` | Resolver output for one query | resolved metric/dimension/filter/order references, target-compatible expression selection, query intent | query-scoped metric dependency authority, plan-node organization, SQL shape |
| `MetricEvaluationPlan` | Static query-scoped plan describing all metric evaluations required by the query | roots, metric-evaluation nodes, metric dependency edges, typed metric kind/spec, evaluation-local semantic evidence | dataset roots, relationship joins, plan-node grain, predicate placement, source sharing, SQL blocks |
| `MetricEvaluationRoot` | A query obligation that makes a metric evaluation externally required | metric reference + role | metric dependency topology or execution shape |
| `MetricEvaluationRole` | Why a root metric is required | `output`, `predicate`, or `order` | how the metric is computed |
| `MetricEvaluationNode` | Exactly one metric's semantic evaluation identity | metric identity, resolved expression, metric inputs, kind, spec, canonical time binding/evaluation-local evidence | source/plan-node organization or physical query grouping |
| `MetricEvaluationInput` | A metric-to-metric dependency edge from a consumer to a prerequisite metric | prerequisite metric identity | dataset/plan-node dependency information |
| `MetricEvaluationKind` | Stable semantic category of one metric evaluation | `source`, `derived`, `cumulative`, `time_offset`, `offset_to_grain`, `conversion`, `semi_additive` | `SemanticPlanNode` shape, join strategy, SQL operator shape |
| `MetricEvaluationSpec` | Typed kind-specific configuration required to preserve one metric's meaning | source-independent metric semantic configuration | source-specific join trees, physical input plans, SQL operators |
| `SemanticPlanNode` | One concrete typed source-aware semantic computation/correctness node inside `SemanticPlan` | node-specific source/grain/predicate evidence, typed inputs, reusable-output evidence | query-scoped metric dependency authority |
| `SourceSelectionNode` | Metric-free typed plan node | grouped/distinct source-selection semantics | metric-evaluation identity or dependency topology |
| `SemanticPlan` | Canonical source-aware semantic planning IR | typed nodes, source roots, joins, grain, predicate placement, sharing proof, reusable outputs, final output contract | physical SQL block structure |
| `SQLPlan` | Physical SQL-structure IR from RFC-0038 | query blocks, CTE structure, compatible physical fusion/reuse | metric semantic dependency authority |

### Short definitions

Use these definitions in code review and design discussion:

```text
MetricEvaluationPlan = WHICH metric evaluations are required and HOW they depend
Root                 = WHY a metric is required by the query
Node                 = WHICH one metric evaluation exists
Input                = WHAT metric it depends on
Kind                 = WHAT TYPE of metric evaluation it is
Spec                 = WHAT TYPED CONFIG defines that metric meaning
SemanticPlan         = HOW metric semantics are organized over data
SQLPlan              = HOW proven semantic work is packed into SQL structure
```

## Why the name is MetricEvaluationPlan

The top-level IR plans **metric evaluation**, not all semantic facts in the
query.

The resolver also resolves dimensions, filters, order expressions, relationships,
time references, and other semantic references. The evaluation IR, however,
materializes only the metric-bearing dependency/computation obligations required
by the query.

For example:

```text
revenue_ytd
    Kind  = cumulative
    Input = revenue

profit_margin
    Kind   = derived
    Inputs = profit, revenue
```

That is a metric-evaluation plan.

The `Plan` suffix is equally important. It states that the value is a static
compiler object, not a runtime action:

```go
evaluationPlan, err := BuildMetricEvaluationPlan(q)
if err != nil {
    return nil, err
}

if err := ValidateMetricEvaluationPlan(evaluationPlan); err != nil {
    return nil, err
}

semanticPlan, err := LowerMetricEvaluationPlan(q, evaluationPlan)
```

The nouns are plans; the verbs are compiler passes.

## Scope today and future semantic-evaluation expansion

RFC-0039 intentionally supports **metric evaluation only**.

This is not a claim that metrics are the only semantic domain Metis may ever
analyze before `SemanticPlan`. It is a claim about current ownership: metrics are
the only domain that currently has a sufficiently independent evaluation model
to justify a dedicated query-scoped plan.

The naming rule is:

> Name an IR for the semantic domain it actually owns today. Do not pre-generalize
> an IR around hypothetical future responsibilities.

Therefore this RFC chooses `MetricEvaluationPlan` instead of a speculative
`SemanticEvaluationPlan`.

Other semantic domains may become independently evaluable in the future. Examples
could include, but are not commitments to introduce:

- temporal/calendar evaluation, if time-spine, fiscal-calendar, alignment, and
  offset semantics form an independent dependency/validation model;
- relationship analysis/evaluation, if path alternatives, fanout constraints,
  ambiguity, and compatibility become a stable pre-plan authority;
- derived-dimension or hierarchy evaluation, if dimension semantics develop
  independent dependency topology and typed evaluation contracts;
- other semantic domains introduced by future Ossie capabilities.

A new domain-specific evaluation IR is justified only when the domain has most of
these properties:

1. an independent dependency graph, topology, or ordering contract;
2. stable typed semantics that can be determined before source-aware plan-node
   construction;
3. a meaningful authority boundary that lower layers must not rediscover;
4. independent validation and fail-closed invariants;
5. independent explainability and deterministic fingerprinting;
6. enough complexity that embedding the logic directly in `SemanticPlan`
   construction would blur ownership.

Until those conditions exist, ordinary semantic facts remain resolver output or
`SemanticPlan` construction concerns rather than receiving speculative
`*EvaluationPlan` types.

The long-term architecture is therefore extensible by **parallel domain-specific
analysis**, not by turning `MetricEvaluationPlan` into a generic container:

```text
                         ResolvedSemanticQuery
                                  |
                 +----------------+----------------+
                 |                                 |
                 v                                 v
       MetricEvaluationPlan             Future domain analysis
          (exists today)                (only when justified)
                 |                                 |
                 +----------------+----------------+
                                  |
                                  v
                             SemanticPlan
                                  |
                                  v
                               SQLPlan
```

If multiple additional semantic evaluation domains become real, a future RFC may
introduce explicit orchestration between them. Such orchestration must reference
well-bounded domain plans; it must not merge unrelated domain vocabulary into one
catch-all `SemanticEvaluationPlan` merely for naming symmetry.

`SemanticPlan` remains the convergence point where resolved/evaluated semantic
facts become source-aware semantic organization.

This gives Metis two desirable properties at once:

- **precision now:** the current IR truthfully says it plans metric evaluation;
- **extension later:** future temporal, relationship, dimension, or other semantic
  analysis can evolve independently without renaming or weakening the metric
  contract.

## Architecture

### Metric-bearing query path

```text
Semantic Query
      |
      v
Resolver
      |
      | resolve names / types / target-compatible expressions
      v
ResolvedSemanticQuery
      |
      | BuildMetricEvaluationPlan
      v
MetricEvaluationPlan
      |
      | ValidateMetricEvaluationPlan
      | ExplainMetricEvaluationPlan
      | FingerprintMetricEvaluationPlan
      v
LowerMetricEvaluationPlan
      |
      v
SemanticPlan
      |
      | ValidateSemanticPlan
      | semantic optimizer
      v
Optimized SemanticPlan
      |
      | physical SQL lowering
      v
SQLPlan
      |
      v
Dialect Renderer
      |
      v
compiler.SqlStatement
```

### Metric-free query path

Metric-free queries such as `distinct_values` do not fabricate a fake metric
plan:

```text
ResolvedSemanticQuery
        |
        | no metric-evaluation obligations
        v
source-selection SemanticPlan construction
        |
        v
SemanticPlan
        |
        v
SQLPlan
        |
        v
Dialect Renderer
```

This bypass is intentional. `MetricEvaluationPlan` exists only when the query
requires metric evaluation.

### Authority transfer

Each lowering boundary transfers authority instead of creating co-equal sources
of truth:

```text
resolver/catalog state
        |
        v
ResolvedSemanticQuery
        |
        v
MetricEvaluationPlan
        X  no metric dependency / metric kind rediscovery below this line
        |
        v
SemanticPlan
        X  no source-aware semantic rediscovery by SQL lowering
        |
        v
SQLPlan
        |
        v
Dialect Renderer
```

After `MetricEvaluationPlan` has been validated, lower layers may not reopen the
catalog dependency graph or metric extension definitions to derive competing
metric dependency/kind answers.

After `SemanticPlan` has been validated, SQL lowering may not use the metric
plan as an alternate semantic authority.

## MetricEvaluationPlan data model

The conceptual shape is:

```go
type MetricEvaluationPlan struct {
    Roots []MetricEvaluationRoot
    Nodes []MetricEvaluationNode // deterministic topological order
}

type MetricEvaluationRole string

const (
    MetricEvaluationRoleOutput    MetricEvaluationRole = "output"
    MetricEvaluationRolePredicate MetricEvaluationRole = "predicate"
    MetricEvaluationRoleOrder     MetricEvaluationRole = "order"
)

type MetricEvaluationRoot struct {
    Metric string
    Role   MetricEvaluationRole
}

type MetricEvaluationNode struct {
    ID          string
    Inputs      []MetricEvaluationInput
    Metric      *ossie.Metric
    Expression  expression.ResolvedExpression
    TimeBinding *ossie.MetricTimeBindingSpec

    Kind MetricEvaluationKind
    Spec MetricEvaluationSpec
}

type MetricEvaluationInput struct {
    Metric string
}
```

The field spelling is implementation-private; the ownership rules are the
durable contract.

### Roots mean query obligation, not dependency

A root explains why a metric entered the query-scoped plan.

For a query that outputs `revenue`, filters on `conversion_rate`, and orders by
`customer_count`:

```text
MetricEvaluationPlan.Roots
|-- revenue         [output]
|-- conversion_rate [predicate]
`-- customer_count  [order]
```

Dependency metrics appear as nodes but are not roots unless the query itself
independently requires them.

A metric may legitimately have multiple root roles. Identical `(metric, role)`
entries are canonicalized.

### Nodes mean one metric semantic identity

Each `MetricEvaluationNode` represents exactly one metric's evaluation meaning.
It is not a source scan, `SemanticPlanNode`, aggregation, query block, or CTE.

The stable kinds are:

```go
type MetricEvaluationKind string

const (
    MetricEvaluationSource        MetricEvaluationKind = "source"
    MetricEvaluationDerived       MetricEvaluationKind = "derived"
    MetricEvaluationCumulative    MetricEvaluationKind = "cumulative"
    MetricEvaluationTimeOffset    MetricEvaluationKind = "time_offset"
    MetricEvaluationOffsetToGrain MetricEvaluationKind = "offset_to_grain"
    MetricEvaluationConversion    MetricEvaluationKind = "conversion"
    MetricEvaluationSemiAdditive  MetricEvaluationKind = "semi_additive"
)
```

There is deliberately no metric-evaluation kind for:

```text
post_aggregate
join_aggregates
cross_join_aggregates
source_selection
```

The first three are typed `SemanticPlanNode` shapes. `source_selection` is
metric-free and lowers directly to `SourceSelectionNode`.

### Typed specs preserve metric meaning only

```go
type MetricEvaluationSpec struct {
    Source        *SourceMetricEvaluationSpec
    Derived       *DerivedMetricEvaluationSpec
    Cumulative    *CumulativeMetricEvaluationSpec
    TimeOffset    *TimeOffsetMetricEvaluationSpec
    OffsetToGrain *OffsetToGrainMetricEvaluationSpec
    Conversion    *ConversionMetricEvaluationSpec
    SemiAdditive  *SemiAdditiveMetricEvaluationSpec
}
```

A node owns exactly one typed spec matching its kind.

The metric plan may carry semantic-critical evaluation-local constraints required
to preserve metric meaning, but it does not own source/plan-node-enriched values such
as:

- dataset root selection;
- relationship path or source join tree;
- source/read predicate placement;
- typed semantic node boundaries;
- node output grain;
- source share group;
- conversion physical input plan;
- custom-calendar physical source mappings;
- SQL query blocks or CTEs.

Those facts are created only after lowering into `SemanticPlan` or `SQLPlan`.

## Worked example: derived metric dependency

For:

```text
net_revenue = revenue - refunds
```

and a query requesting `net_revenue`:

```text
MetricEvaluationPlan
|
|-- Roots
|     `-- net_revenue [output]
|
`-- Nodes (topological order)
      |-- revenue
      |     Kind = source
      |
      |-- refunds
      |     Kind = source
      |
      `-- net_revenue
            Kind   = derived
            Inputs = [revenue, refunds]
```

This plan deliberately says nothing about whether `revenue` and `refunds` share
a dataset, what joins are needed, what grain they aggregate at, or whether
`net_revenue` is implemented as post-aggregate or aggregate-join work.

Those are `SemanticPlan` questions.

## Metric identity versus shared computation

This is the central cardinality contract:

```text
                 MetricEvaluationPlan
                           |
          +----------------+----------------+
          v                v                v
 MetricEvaluation   MetricEvaluation   MetricEvaluation
    Node A              Node B              Node C
          |                |                |
          +-----------+----+----------------+
                      |
                      | LowerMetricEvaluationPlan
                      v
                 SemanticPlan
                      |
             compatibility proof
                      |
              shared computation
                      |
                      v
                   SQLPlan
                      |
              one query / CTE
                      |
        +-------------+-------------+
        v             v             v
     Metric A      Metric B      Metric C
```

The normative invariant is:

```text
1 MetricEvaluationNode
    != 1 SemanticPlanNode
    != 1 source scan
    != 1 aggregation
    != 1 SQL query block / CTE
```

Equivalently:

```text
metric identity cardinality != execution-unit cardinality
```

`MetricEvaluationNode` is deliberately one metric per node. That is a semantic
identity rule, not an execution-shape rule.

### SemanticPlan proves sharing compatibility

Multiple metric-evaluation nodes may share semantic work only after
`SemanticPlan` can prove relevant compatibility, including as applicable:

- compatible source roots and relationship paths;
- compatible fanout guarantees;
- compatible output grain;
- compatible source/read predicates and predicate boundaries;
- compatible time binding and calendar semantics;
- compatible aggregation and extension boundaries;
- no ambiguous alias or output-name collision.

The metric plan cannot prove those facts because source-aware plan-node organization is
not its responsibility.

### SQLPlan chooses physical packing

Once `SemanticPlan` owns the semantic proof, `SQLPlan` may pack compatible work
into one physical SQL query block or CTE.

Valid mappings include:

```text
one MetricEvaluationNode       -> one SemanticPlanNode
one MetricEvaluationNode       -> multiple SemanticPlanNodes
multiple MetricEvaluationNodes -> shared SemanticPlan work
multiple SemanticPlanNodes     -> one compatible SQLPlan query block
```

No validation rule may infer equality between these cardinalities.

### Shared computation may expose reusable metric outputs

A proven shared semantic or physical computation may expose more than one metric
output.

Two important forms are:

1. **co-computation** — `revenue` and `orders` share one compatible source
   aggregation;
2. **passthrough/reuse** — a derived computation already materializes an input
   metric such as `bookings`, and that metric is also required by the query.

The contract is:

```text
MetricEvaluationNode
    owns exactly one metric's semantic identity

SemanticPlan / SQLPlan computation
    may satisfy one or more MetricEvaluationNodes
    may expose one or more reusable metric outputs
```

A requested metric already available from compatible shared work must be
eligible for reuse without creating a redundant branch merely to preserve
one-node-per-metric identity.

## SemanticPlan boundary

`LowerMetricEvaluationPlan` converts stable metric meaning into source-aware
semantic organization.

Conceptually:

```go
func (p *Planner) Plan(q *resolver.ResolvedSemanticQuery) (*SemanticPlan, error) {
    evaluationPlan, err := BuildMetricEvaluationPlan(q)
    if err != nil {
        return nil, err
    }
    if err := ValidateMetricEvaluationPlan(evaluationPlan); err != nil {
        return nil, err
    }

    plan, err := LowerMetricEvaluationPlan(q, evaluationPlan)
    if err != nil {
        return nil, err
    }
    if err := ValidateSemanticPlan(plan); err != nil {
        return nil, err
    }
    return plan, nil
}
```

For metric-free queries, the planner bypasses metric evaluation planning and
constructs typed source-selection `SemanticPlan` state directly.

`LowerMetricEvaluationPlan` may resolve or construct:

- source roots;
- relationship paths and joins;
- concrete typed semantic nodes and node boundaries;
- typed node dependency inputs;
- source/read and post-evaluation predicate placement;
- node/output grain;
- source-work sharing and reusable-output evidence;
- source/plan-node-enriched extension implementation state;
- final output contract.

It may not rediscover metric dependency or metric kind from catalog/Ossie state
once the metric plan exists.

## Final typed-node representation

The stage-local `planner.SemanticEvaluation` that existed when this RFC was
drafted was **not** the query-scoped metric plan. Phase A renamed it to
`SemanticStageEvaluation` without a compatibility alias so the new
`MetricEvaluationPlan` vocabulary could be introduced unambiguously.

RFC-0040 later completed the representation migration and deleted both
`SemanticStage` and `SemanticStageEvaluation`. The final contract is:

```go
type SemanticPlan struct {
    // ...
    Nodes []SemanticPlanNode
}

type SemanticPlanNode interface {
    // implemented by concrete typed nodes such as SourceAggregateNode,
    // PostAggregateNode, JoinAggregatesNode, CumulativeWindowNode,
    // and SourceSelectionNode
}
```

Consequently:

- `MetricEvaluationPlan` remains the query-scoped metric authority;
- lowering transfers metric meaning into concrete typed plan nodes;
- node type is the discriminator; there is no tagged operation envelope;
- metric-free source selection is a `SourceSelectionNode`, not a metric-plan
  node or stage payload;
- no compatibility alias keeps `SemanticEvaluation`, `SemanticStageEvaluation`,
  or `SemanticStage` alive.

## Resolver boundary

RFC-0039 does not redesign resolver expression resolution.

`ResolvedSemanticQuery.EvaluationMetrics` remains resolver output because the
resolver uses the metric dependency closure to select and analyze
target-compatible expressions.

At the implemented boundary:

- downstream of resolver construction, only `BuildMetricEvaluationPlan` may
  consume `EvaluationMetrics` as a whole;
- source-aware plan-node construction consumes `MetricEvaluationPlan` instead;
- no source-aware helper may use `EvaluationMetrics` as an alternate metric
  dependency or metric-kind authority;
- no SQL lowering code may consume it.

Removing or reshaping `EvaluationMetrics` is permitted after target-aware
expression resolution has an equally explicit replacement, but it is not
required for RFC-0039 closure.

## Catalog dependency boundary

`BuildMetricEvaluationPlan` is the last planning layer allowed to convert the
model-scoped dependency index into query-scoped metric dependency truth.

```text
catalog.MetricDependencyGraph
          |
          v
MetricEvaluationPlan
          X  no metric dependency rediscovery below this line
          |
          v
SemanticPlan construction
```

`semanticConstructionInput.Dependencies` was made redundant and deleted.

Source planning may still read dataset metadata, relationship definitions, and
other source-specific information that it genuinely owns.

## Validation

`ValidateMetricEvaluationPlan` is a first-class fail-closed boundary. It proves
at least:

1. root metric names are non-empty and root roles are supported;
2. identical `(metric, role)` roots are canonicalized and a metric may retain
   distinct roles;
3. every root references an existing node;
4. node IDs are unique;
5. node order is topological and every input references an earlier node;
6. every node is reachable from at least one root;
7. every node carries exactly one typed spec matching its kind;
8. source evaluations have no metric inputs;
9. cumulative, time-offset, offset-to-grain, and semi-additive dependencies
   exactly match their declared base metric;
10. conversion dependencies exactly match declared base and conversion metrics;
11. derived dependencies exactly match canonical metric dependency captured when
    the plan was built;
12. target-compatible resolved expressions required by evaluation are present;
13. time-relative metrics retain valid canonical time binding and query-level
    compatibility already proven by resolution;
14. owned slices, pointer-backed specs, expressions, and evidence do not alias
    mutable construction input;
15. unsupported semantic-critical metric extensions fail closed rather than
    becoming an untyped derived node.

Catalog/model validation may detect cycles earlier, but the query-scoped
validator independently rejects an invalid hand-constructed plan.

`ValidateMetricEvaluationPlan` deliberately does **not** validate source sharing
or physical query-block coalescing. Those facts belong to later layers.

## Determinism, ownership, explanation, and fingerprinting

RFC-0039 adds these first-class contracts:

- `ValidateMetricEvaluationPlan`;
- `CloneMetricEvaluationPlan` or equivalent deep ownership helper;
- bounded `ExplainMetricEvaluationPlan`;
- `FingerprintMetricEvaluationPlan`.

The canonical fingerprint includes:

- root metric and role;
- node identity;
- dependency inputs;
- metric evaluation kind;
- resolved expression identity;
- canonical time binding;
- typed evaluation spec;
- semantic-critical evaluation-local evidence.

It excludes:

- dataset/source choices;
- relationship joins;
- typed semantic-node shape;
- shared-computation grouping;
- SQL blocks/CTEs;
- physical cost/runtime strategy.

Set-like fields are canonicalized. Dependency order is canonical unless a metric
semantic contract makes order meaningful. Topological tie-breaking is
deterministic.

Tests verify determinism, canonicalization, sensitivity, deep ownership, and
lowering consistency. Literal hash values are not a public semantic identity
contract.

Example explain output:

```text
roots:
  net_revenue [output]

nodes:
  revenue [source]
  refunds [source]
  net_revenue [derived]
    <- revenue
    <- refunds
```

Explain output must not claim source join order, shared scan shape, CTE shape,
SQL operators, or physical cost.

## Optimization boundary

The normal semantic optimizer consumes `SemanticPlan`, not
`MetricEvaluationPlan`.

This is intentional:

```text
MetricEvaluationPlan
    stable metric identity / dependency semantics
        |
        v
SemanticPlan
    source-aware organization with the evidence needed for safe rewrites
        |
        v
Semantic Optimizer
```

Source sharing, compatible aggregation sharing, reusable node outputs,
predicate movement, join pruning, and source-scan fusion require source/grain/
predicate evidence owned by `SemanticPlan`.

RFC-0039 does not introduce evaluation-plan optimizer rewrites. If future
metric-evaluation-level rewrites become useful, they require their own proof,
validation, explain, fingerprint, and differential contracts.

## Interaction with RFC-0038

RFC-0039 and RFC-0038 define adjacent but independent boundaries:

```text
MetricEvaluationPlan --semantic lowering--> SemanticPlan
SemanticPlan         --physical lowering--> SQLPlan
```

`SemanticPlan` proves semantic compatibility and reusable outputs. `SQLPlan`
chooses physical SQL query blocks, CTE structure, and compatible fusion without
reopening metric meaning.

A many-to-one physical mapping is valid:

```text
MetricEvaluationNode(revenue) --\
                                +--> compatible SemanticPlan work
MetricEvaluationNode(orders)  --/              |
                                               v
                                      one SQLPlan query block
```

No `SQLPlan` node, renderer, or dialect package may depend directly on
`MetricEvaluationPlan`.

## Relationship to MetricFlow

MetricFlow uses the name `MetricEvaluationPlan` for its recursive metric
evaluation layer. Metis intentionally adopts the same high-level term because it
accurately describes this IR boundary.

The internal ownership model is deliberately different:

```text
MetricFlow                              Metis

MetricFlowQuerySpec                     ResolvedSemanticQuery
        |                                       |
        v                                       v
MetricEvaluationPlan                    MetricEvaluationPlan
        |                                       |
        |-- TopLevelQueryNode                    |-- MetricEvaluationRoot
        |-- MetricQueryNode*                     |-- MetricEvaluationNode*
        |    (may output multiple metrics)       |    (exactly one metric identity)
        |                                       |
        v                                       v
DataflowPlan                            SemanticPlan
        |                                       |
        |-- query/dataflow sharing               |-- source/grain/predicate proof
        |                                       |-- shared/reusable metric outputs
        v                                       v
SQL Plan                                SQLPlan
```

The key distinction is:

```text
MetricFlow MetricQueryNode
    = query-shaped evaluation unit
    = may compute / passthrough multiple metrics

Metis MetricEvaluationNode
    = exactly one metric's semantic evaluation identity
    = never decides plan-node/SQL sharing
```

Metis obtains the same useful multi-metric execution outcome later through
correctness-proven sharing in `SemanticPlan` and physical fusion in `SQLPlan`.

The naming correspondence is intentional; the node semantics are not copied.

## Naming alternatives considered

This section is normative rationale intended to prevent future vocabulary drift.

### `MetricEvaluationPlan`

**Accepted.**

It identifies both the subject (`MetricEvaluation`) and the fact that the value
is a static plan/IR. It naturally contains `MetricEvaluationNode`s and avoids
implying runtime execution.

It also preserves future extension space: choosing a precise domain name now
does not prevent Metis from introducing other domain-specific analysis or
evaluation IRs later.

### `SemanticEvaluation` / `SemanticEvaluationPlan`

Rejected as the top-level name for RFC-0039.

They are broader than the actual IR. The query contains dimensions, filters,
relationships, temporal semantics, source selection, and other semantic facts
that this object does not evaluate or own.

Using the generic name today would create a false ownership claim and make future
semantic domains appear as if they must be inserted into one existing container.
Metis instead keeps the current contract metric-specific and will add future
domain-specific IRs only when their own invariants justify them.

### `MetricEvaluation`

Rejected as the top-level name.

The singular phrase naturally reads as one metric's evaluation. That conceptual
unit is `MetricEvaluationNode`.

### `MetricsEvaluation`

Rejected.

Plural noun modifiers are less idiomatic in this technical compound and the term
can read as "evaluation metrics" rather than evaluation of semantic metrics.

### `MetricEvaluationGraph`

Rejected as the canonical domain name.

The object is structurally a DAG, but `Graph` describes representation rather
than its compiler role. The graph invariant is explicit in the plan contract.

### A second generic `LogicalPlan`

Rejected.

`SemanticPlan` already owns Metis's source-aware semantic logical planning
boundary. A generic `LogicalPlan` name would hide the distinction between metric
evaluation dependency planning and source-aware semantic organization.

## Goals

1. Introduce one query-scoped `MetricEvaluationPlan` for every metric-bearing
   query.
2. Complete the intermediate stage-local `SemanticEvaluation` to
   `SemanticStageEvaluation` rename without an alias, then preserve the same
   ownership boundary when RFC-0040 replaces stages with concrete typed nodes.
3. Make roots, metric dependency edges, evaluation kind, and typed specs
   explicit, validated, deterministic, explainable, clone-safe, and
   fingerprintable.
4. Make `MetricEvaluationPlan -> SemanticPlan` an explicit deterministic
   lowering pass.
5. Stop source-aware plan-node construction from rediscovering metric dependency
   or metric kind from Ossie/catalog state.
6. Keep source roots, relationship joins, source predicate placement, output
   grain, typed-node composition, sharing proof, and final output exclusively
   in `SemanticPlan` construction.
7. Preserve `SemanticPlan` as the only semantic authority consumed by the normal
   semantic optimizer and SQL lowering.
8. Preserve all current query behavior and compiled SQL bytes through structural
   migration unless an independently reviewed correctness or approved sharing
   optimization change requires otherwise.
9. Keep metric-free queries out of metric evaluation planning.
10. Preserve multi-metric shared computation and passthrough/reuse without
    merging metric identities.
11. Keep RFC-0039 explicitly metric-scoped while documenting a non-breaking path
    for future domain-specific semantic evaluation/analysis IRs.

## Non-goals

RFC-0039 does not introduce:

- runtime metric evaluation or database execution;
- statistics, cost estimation, physical join ordering, or access-path selection;
- a replacement for `SemanticPlan` or its concrete typed nodes;
- dataset-root, relationship-path, or join-order ownership in
  `MetricEvaluationPlan`;
- source predicate placement or final output grain in `MetricEvaluationPlan`;
- SQL query-block/CTE shape in `MetricEvaluationPlan`;
- a metric plan for metric-free queries;
- query-shaped multi-metric nodes inside `MetricEvaluationPlan`;
- a compatibility alias retaining the old stage-local `SemanticEvaluation`
  meaning;
- immediate redesign of resolver target-expression selection;
- evaluation-plan cost-based optimization;
- a generic `SemanticEvaluationPlan` umbrella;
- speculative `TemporalEvaluationPlan`, `RelationshipEvaluationPlan`,
  `DimensionEvaluationPlan`, or other domain IRs before concrete independent
  contracts require them.

## Migration plan

### Phase A — Rename stage-local ownership (historical intermediate step)

**Goal:** name the existing stage payload correctly with no behavior change.

1. Rename stage-local `SemanticEvaluation` to `SemanticStageEvaluation`.
2. Rename stage-specific clone/validation helpers.
3. Rename the stage-local source file where useful.
4. Preserve every typed variant and stage-kind validation rule.
5. Add no alias, compatibility field, or duplicate payload.
6. Prove compiled SQL, parameters, `SemanticPlan` fingerprints, and real-engine
   results are unchanged.

### Phase B — Materialize MetricEvaluationPlan in shadow mode

**Goal:** prove the plan describes current behavior before it becomes authority.

1. Add `MetricEvaluationPlan`, `MetricEvaluationRoot`, `MetricEvaluationRole`,
   `MetricEvaluationNode`, `MetricEvaluationInput`, `MetricEvaluationKind`, and
   `MetricEvaluationSpec`.
2. Build the plan from resolver-owned evaluation closure used today.
3. Add deep clone/ownership tests.
4. Add `ValidateMetricEvaluationPlan`.
5. Add canonical explain and fingerprint projections.
6. Build the plan for every metric-bearing conformance scenario while existing
   stage construction still controls SQL.
7. Differentially compare metric kind/dependencies against current behavior
   where a one-to-one stage correspondence happens to exist.
8. Prove the API does not encode `node count == stage count`.

### Phase C — Make metric-evaluation semantics authoritative

**Goal:** move metric meaning out of stage-construction control flow.

1. Move metric extension classification into metric-plan construction.
2. Move base/conversion dependency-contract validation into
   `ValidateMetricEvaluationPlan`.
3. Move evaluation-local time semantics and typed `MetricEvaluationSpec`
   construction to evaluation nodes.
4. Make metric stage construction accept validated `MetricEvaluationPlan`.
5. Delete code that re-decides metric evaluation kind after a node exists.

### Phase D — Replace dependency side state with plan inputs

**Goal:** make the authority seam structural.

1. Replace `semanticConstructionInput.Dependencies` consumers with
   `MetricEvaluationInput`s.
2. Delete the construction-only dependency map.
3. Change source-aware enrichment helpers to accept evaluation nodes or explicit
   metric inputs.
4. Remove lower `MetricDependency` / `MetricDependencyGraph` reads used to
   rediscover metric edges.
5. Keep genuine source-planning metadata lookups such as datasets and
   relationships.
6. Add repository-level audit coverage against dependency rediscovery.

### Phase E1 — Make MetricEvaluationPlan -> SemanticPlan lowering explicit

**Goal:** create a named authority transfer.

1. Replace procedural `metricEvaluationConstruction` ownership with
   `LowerMetricEvaluationPlan` or equivalent planner-private pass.
2. Lower source metric evaluations into source-aware semantic work.
3. Lower derived evaluations into post-aggregate, aggregate-join, or scalar
   cross-join work according to source/grain requirements.
4. Enrich cumulative, offset, conversion, and semi-additive typed nodes with
   source-specific plans only during lowering.
5. Preserve source-work sharing and shared-grain proof as `SemanticPlan`
   responsibilities.
6. Validate `SemanticPlan` before optimizer or SQL lowering sees it.

### Phase E2 — Prove shared computation and reuse

**Goal:** ensure one-metric-per-node never becomes one-metric-per-execution-unit.

1. Add explicit compatibility proof for coalescing work across multiple
   `MetricEvaluationNode`s.
2. Allow compatible metric evaluations to share source scans, aggregation, or
   semantic computation while retaining separate identities.
3. Allow shared computation to expose reusable metric outputs.
4. Add conformance proving multi-node shared source/aggregation work.
5. Add conformance proving passthrough/reuse avoids a redundant branch.
6. If physical fusion remains RFC-0038 work, preserve sufficient
   `SemanticPlan` evidence for `SQLPlan` to prove the merge.

### Phase F — Cut over all planner consumers

**Goal:** leave one metric-evaluation authority and one semantic-plan authority.

1. Every metric-bearing `Planner.Plan` builds and validates
   `MetricEvaluationPlan` first.
2. Source-aware plan-node construction reads metric dependency and kind only from the
   validated plan.
3. Direct `ResolvedSemanticQuery.EvaluationMetrics` consumption is restricted to
   the metric-plan builder.
4. `SemanticPlan`, optimizer, explain, fingerprint, SQL lowering, and `SQLPlan`
   construction have no fallback access to metric-plan state.
5. Preserve many-to-shared computation capability.
6. Delete transitional adapters and duplicate constructors.

### Phase G — Closure audit and documentation

Audit for:

- final-production uses of the old `SemanticEvaluation`,
  `SemanticStageEvaluation`, or `SemanticStage` names;
- stale query-scoped `SemanticEvaluation`, `MetricEvaluation`,
  `MetricsEvaluation`, or `MetricEvaluationGraph` terminology for this IR;
- speculative generic `SemanticEvaluationPlan` ownership introduced without a
  separate accepted RFC;
- stale `SemanticMetricEvaluation*` / `SemanticEvaluationRoot*` names;
- lower-layer metric dependency rediscovery;
- `EvaluationMetrics` consumers outside metric-plan construction;
- query-scoped `MetricEvaluationPlan` embedded in `SemanticPlan`;
- assumptions that evaluation node count equals plan-node/scan/aggregation/query/CTE
  count;
- optimizations that merge metric identity instead of sharing computation;
- redundant branches where compatible shared work already exposes the metric;
- SQL lowering or renderer access to metric-plan state;
- stale RFC-0030/RFC-0031/RFC-0037/RFC-0038 vocabulary.

Update durable architecture documentation after implementation closure.

## Acceptance

RFC-0039 is implemented only when all of the following hold:

1. The final vocabulary matches the normative glossary in this RFC.
2. The intermediate `SemanticStageEvaluation` rename introduced no compatibility
   alias, and the final RFC-0040 representation contains only concrete typed
   `SemanticPlanNode` values; `SemanticEvaluation`, `SemanticStageEvaluation`,
   and `SemanticStage` are absent from production authority.
3. Every successful metric-bearing query materializes exactly one validated
   query-scoped `MetricEvaluationPlan` before `SemanticPlan` construction.
4. Metric-free and `distinct_values` queries materialize no fake metric plan and
   continue through typed source-selection planning.
5. Roots are represented by `MetricEvaluationRoot` + `MetricEvaluationRole` and
   cover output, metric predicate, and metric order obligations.
6. Every `MetricEvaluationNode` owns exactly one metric identity, explicit
   `MetricEvaluationInput`s, one `MetricEvaluationKind`, and exactly one matching
   `MetricEvaluationSpec` variant.
7. Evaluation node order and plan fingerprinting are deterministic.
8. `ValidateMetricEvaluationPlan`, deep ownership checks,
   `ExplainMetricEvaluationPlan`, and `FingerprintMetricEvaluationPlan` exist.
9. Source-aware plan-node construction does not rediscover metric dependency or metric kind
   after the metric plan is built.
10. `semanticConstructionInput.Dependencies` is deleted.
11. `MetricEvaluationPlan -> SemanticPlan` is an explicit named lowering
    boundary.
12. Derived metric kind remains stable regardless of post-aggregate,
    aggregate-join, or scalar cross-join typed-node shape.
13. `MetricEvaluationPlan` owns no dataset root, relationship join tree, source
    predicate placement, node output grain, share group, shared-query grouping,
    or SQL shape.
14. `SemanticPlan` remains the only semantic IR consumed by the normal optimizer
    and SQL lowering; there is no fallback to metric-plan state.
15. One-to-many and many-to-shared-node lowering are permitted; no validation or
    fingerprint assumes one evaluation node per `SemanticPlanNode`.
16. Evaluation cardinality does not constrain source-scan, aggregation, SQL
    query-block, or CTE cardinality.
17. Two or more compatible `MetricEvaluationNode`s can share proven source work
    or aggregation without merging metric identities.
18. Shared semantic/physical computation can expose multiple reusable metric
    outputs.
19. At least one conformance test proves separate evaluation nodes lower to
    shared source/aggregation work with correct outputs.
20. At least one conformance test proves passthrough/reuse can satisfy a query
    obligation without a redundant independent branch.
21. Downstream of resolver construction,
    `ResolvedSemanticQuery.EvaluationMetrics`, if retained, is consumed as a
    whole only by metric-plan construction.
22. RFC-0039 remains metric-scoped: no unrelated semantic domain is added to
    `MetricEvaluationPlan` merely to avoid introducing an independently justified
    analysis/evaluation boundary.
23. Any future non-metric evaluation IR must be introduced by a separate design
    decision with its own ownership, validation, explainability, fingerprinting,
    and authority-transfer contract.
24. All 96 core scenarios compile successfully for DuckDB, Doris, and ClickHouse.
25. All 288 compiled-SQL fingerprints and exact parameter evidence remain
    byte-for-byte unchanged during structural migration unless an independently
    reviewed correctness or approved sharing change documents the reason and new
    evidence.
26. DuckDB, Doris, and ClickHouse real-engine execution conformance remains
    green.
27. `make check`, ownership tests, deterministic metric-plan tests,
    semantic-plan differential tests, sharing/reuse tests, and documentation
    checks are green.
28. RFC-0030, RFC-0031, RFC-0037, RFC-0038, and RFC-0040 are aligned to the final
    `ResolvedSemanticQuery -> MetricEvaluationPlan -> SemanticPlan -> SQLPlan`
    vocabulary and cardinality contract during closure.

## Suggested PR sequence

This table records the migration sequence proposed before RFC-0040 removed the
intermediate stage representation. The implemented terminal representation is
the typed-node architecture described above.

| PR | Phase | Primary change | Required behavior gate |
| --- | --- | --- | --- |
| 1 | A | rename stage payload to `SemanticStageEvaluation` | 288 SQL fingerprints unchanged |
| 2 | B | add query-scoped `MetricEvaluationPlan` types / builder / validation | shadow differential only |
| 3 | B | add clone / explain / fingerprint | determinism + ownership |
| 4 | C | move typed metric classification into metric plan | stage/plan differential |
| 5 | D | replace dependency side map with metric-plan inputs | no lower dependency rediscovery |
| 6 | E1 | explicit `MetricEvaluationPlan -> SemanticPlan` lowering | plan differential + SQL bytes |
| 7 | E2 | prove multi-metric shared computation / passthrough | sharing + reuse conformance |
| 8 | F | cut over all metric-bearing planner construction | 96 x 3 + three engines |
| 9 | G | delete transitional seams and close documentation | repository closure audit |

Keep PRs narrow. Do not combine a broad naming migration, authority cutover, and
behavior-changing sharing optimization in one diff.

## Final invariants

With RFC-0039 and RFC-0038 complete:

```text
ResolvedSemanticQuery
        |
        v
MetricEvaluationPlan   # metric obligations, identities, kinds, dependencies
        |
        v
SemanticPlan           # source-aware semantic organization and sharing proof
        |
        v
SQLPlan                # physical SQL structure and compatible block fusion
        |
        v
Dialect Renderer       # target syntax only
```

The scope invariant is:

```text
MetricEvaluationPlan
    owns metric evaluation only

future semantic evaluation/analysis
    gets a domain-specific boundary only when independently justified

SemanticPlan
    remains the convergence point for source-aware semantic organization
```

The vocabulary invariant is:

```text
MetricEvaluationPlan
    whole query-scoped metric-evaluation plan

MetricEvaluation*
    concepts inside that plan for individual metric obligations / identities

SemanticPlanNode
    concrete typed source-aware node inside SemanticPlan

SourceSelectionNode
    metric-free source-selection node that bypasses MetricEvaluationPlan
```

The authority invariant is:

```text
metric dependency authority -> MetricEvaluationPlan
source-aware semantic authority -> SemanticPlan
physical SQL structure authority -> SQLPlan
```

The cardinality invariant is:

```text
metric identity cardinality != execution-unit cardinality
```

A `MetricEvaluationNode` is never duplicated or merged merely to match physical
query shape, and physical sharing is never prohibited merely because metric
identities remain distinct.
