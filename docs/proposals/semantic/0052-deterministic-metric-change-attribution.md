# RFC-0052: Deterministic Metric Change Attribution

> **Design history:** This RFC preserves its original lifecycle status and rationale.
> Implementation claims describe the recorded design, not a guarantee that every
> proposed API remains current. See [current documentation](../../README.md) for
> supported interfaces. Package paths have been updated for this repository.

- **Status:** Implemented
- **Owners:** Metis maintainers
- **Created:** 2026-08-27
- **Last updated:** 2026-08-28
- **Scope:** `catalog/`, `resolver/`, `planner/`, metric decomposition analysis, `SemanticPlan`, `SQLPlan` lowering, explain/fingerprint/conformance
- **Supersedes:** None

## 1. Summary

Metis introduces a deterministic semantic planning model for **metric change attribution**.

Given one governed metric, one explicit time dimension, a baseline period, a current period, zero or more shared filters, and one or more compatible dimensions to investigate, Metis can determine whether the metric admits a mathematically valid decomposition, construct a query-scoped `MetricAttributionPlan`, lower each requested dimension into the canonical typed `SemanticPlan`, and compile deterministic physical queries that return contribution evidence.

The initial strategies are:

1. **additive contribution** for metrics whose segment values can be safely summed back to the total;
2. **ratio decomposition** for supported ratios whose numerator and denominator are themselves additive.

The architecture is:

```text
Agent / caller
    |
    | chooses what change to investigate
    | metric + periods + candidate dimensions
    v
ResolvedMetricAttributionRequest
    |
    | metric dependency analysis
    v
MetricEvaluationPlan
    |
    | derive decomposition semantics
    v
MetricAttributionPlan
    |
    | lower one independent analysis per dimension
    v
SemanticPlan
    |
    | AdditiveAttributionNode
    | or RatioAttributionNode
    v
SQLPlan
    |
    v
SQLRenderResult
    |
    | Metis boundary
    v
Database / DB MCP / CLI / SDK
```

Metis remains compile-only. It does **not** connect to the warehouse, execute generated queries, fetch result rows, or interpret business causality.

This RFC also does **not** add `attribute_metric`, `analyze_metric`, or any other MCP tool. The nine-tool Agent-facing MCP surface established by RFC-0051 remains unchanged.

A later interface RFC may decide whether attribution should be exposed through a new compile-oriented operation, an extension of `compile`, or another bounded interface. That decision must be informed by implementation evidence and AgentBench rather than assumed here.

## 2. Motivation

Metis currently makes ordinary metric queries deterministic:

```text
semantic intent
    ->
metric resolution
    ->
relationship / grain validation
    ->
MetricEvaluationPlan
    ->
SemanticPlan
    ->
SQLPlan
    ->
physical SQL
```

This solves questions such as:

```text
What was revenue by region in Q2?
```

but analytical Agents frequently receive a different class of request:

```text
Why did revenue fall from Q1 to Q2?
```

An Agent can answer this today by orchestrating ordinary queries: query both totals, group both periods by several dimensions, align appeared/disappeared segments, calculate deltas, repeat with narrower filters, and for ratio metrics reason separately about numerator, denominator, mix, and rate changes.

The individual queries may all be semantically valid while the overall analysis is still inconsistent. The Agent can accidentally use different filters between periods, aggregate an already-computed ratio, compare incompatible grains, lose new or disappeared segments, treat missing segments as zero when fill semantics do not permit it, double count a non-additive metric, derive a ratio from different populations, perform unsafe fan-out joins, produce contributions that do not reconcile, or describe numerical association as business causality.

These are deterministic semantic and mathematical concerns. They should not depend on prompt quality or next-token prediction.

The desired responsibility boundary is:

```text
Agent:
    What should I investigate?
    Which dimensions are plausible?
    Should I drill further?
    How should the evidence be explained?

Metis:
    Is this metric decomposable?
    What mathematical strategy is valid?
    Are the dimensions compatible?
    Are both periods using the same semantic contract?
    How are missing segments aligned?
    Do segment contributions reconcile?
    What physical query computes that evidence?

Engine:
    Execute the physical query.
```

This extends the existing product principle:

> Agents reason. Metis resolves semantics. Engines execute.

into analytical investigation without turning Metis into an Agent or a database platform.

## 3. Attribution is not causal inference

The term **attribution** in this RFC means deterministic decomposition of an observed metric change into mathematically defined segment contributions.

For example:

```text
revenue decreased by 1.57M

region=west:
    delta = -1.02M

category=electronics:
    delta = -0.89M
```

proves that those segments account for corresponding changes under their respective decompositions. It does **not** prove that west caused revenue to fall or that electronics pricing caused the regional decline.

Different dimensions may describe overlapping populations. Their contribution percentages must not be added across dimensions.

Accordingly, public and internal vocabulary should prefer `change`, `contribution`, `decomposition`, `effect`, and `reconciliation`, and avoid `cause`, `root_cause`, or `causal_effect` unless a future explicitly causal subsystem is introduced.

## 4. Relationship to existing algebra

RFC-0033 already defines **aggregation algebra**. That algebra answers questions such as whether an aggregation can survive fan-out, whether it can be rolled up, and what partial state and merge operator are required.

RFC-0052 does not replace or duplicate it.

This RFC introduces a second, narrower derived concept:

```text
MetricDecompositionAlgebra
```

which answers:

```text
Can the change in this metric be decomposed across segments?
If yes, by what exact mathematical strategy?
```

The distinction is:

```text
AggregationAlgebra
    property of aggregate computation

MetricDecompositionAlgebra
    property of metric change decomposition
```

The second consumes evidence from the first. It does not infer safety independently.

## 5. Metric decomposition algebra

The first implementation defines:

```go
type MetricDecompositionKind string

const (
    MetricDecompositionAdditive    MetricDecompositionKind = "additive"
    MetricDecompositionRatio       MetricDecompositionKind = "ratio"
    MetricDecompositionUnsupported MetricDecompositionKind = "unsupported"
)
```

with typed structures rather than a nullable generic payload:

```go
type MetricDecomposition interface {
    Kind() MetricDecompositionKind
    metricDecomposition()
}
```

Conceptually:

```text
AdditiveMetricDecomposition

RatioMetricDecomposition
    numerator
    denominator

Unsupported
    reason
```

The decomposition is **derived**. It is never trusted from an Ossie extension or model-author declaration.

A model author cannot unlock attribution safety with a custom extension such as `additive: true`. Unknown semantics fail closed.

## 6. Additive decomposition

A metric is attribution-additive when values computed independently for a partition of the requested dimension can be merged with numeric addition to reproduce the metric total.

The initial source-aggregate admission rule requires the selected aggregate to have:

```text
rollup merge operator = SUM
finalizer             = identity
required state        = retained by the grouped result
```

Examples include, subject to normal semantic validation:

```text
SUM(...)
COUNT(...)
SUMIF(...)
```

Examples that are **not** directly additive:

```text
MIN(...)
MAX(...)
AVG(...)
COUNT(DISTINCT ...)
median(...)
percentile(...)
unknown target-native aggregate
```

`MIN` and `MAX` may be distributive under their own merge operators, but segment changes do not sum to the total change, so they are not additive for contribution attribution.

`AVG` is algebraic but a finished average cannot be added across segments.

`COUNT(DISTINCT ...)` is holistic and may count the same identity in multiple segments.

### 6.1 Derived additive metrics

The decomposition analyzer may recursively prove simple linear derived metrics additive.

Supported forms may include:

```text
A + B
A - B
constant * A
A * constant
A / nonzero_constant
```

when all referenced metric operands are themselves proven additive.

For example:

```text
total_margin = revenue - cost
```

can remain additive if both `revenue` and `cost` are additive at the requested dimension.

The following are not assumed additive:

```text
A * B
A / B
pow(A, B)
log(A)
CASE expressions whose branches change metric population
arbitrary unknown functions
```

unless a future RFC supplies explicit derivation rules and correctness proofs.

## 7. Ratio decomposition

A metric is ratio-decomposable when its metric evaluation semantics can be proven equivalent to:

```text
R = A / B
```

where:

- `A` is an additive metric;
- `B` is an additive metric;
- both operands are evaluated over a compatible population and grain;
- their missing-segment fill semantics permit alignment;
- no unsupported nonlinear transformation changes the ratio semantics.

Typical examples include:

```text
average_order_value = revenue / order_count
conversion_rate = conversions / visits
```

A metric is not classified as ratio merely because its name or description contains words such as `rate`, `average`, or `percentage`. The proof comes from typed metric dependency and expression structure.

A source aggregate written directly as `AVG(x)` is not automatically rewritten into a ratio in v1. Supporting such a transformation requires a later correctness proof that the original aggregate can be reconstructed from appropriate retained partial state.

## 8. Fill and population requirements

Change attribution requires aligning the segment domain of two periods.

For an additive metric:

```text
baseline:
    north = 100
    west  = 200

current:
    north = 120
    east  = 50
```

the aligned population is:

```text
north = 100 -> 120
west  = 200 ->   0
east  =   0 ->  50
```

Treating an absent segment as zero is only legal if the metric's semantic fill policy admits the additive identity.

RFC-0014 fill semantics remain authoritative. RFC-0052 must not invent a second missing-value rule.

If a metric or dependency cannot safely use identity fill during period alignment, attribution fails closed rather than coercing absence to zero. The same rule applies independently to ratio numerator and denominator metrics.

## 9. Attribution request

The internal semantic request is conceptually:

```go
type MetricAttributionRequest struct {
    ProjectID        string
    MetricRef        string
    TimeDimensionRef string

    Baseline TimeRange
    Current  TimeRange

    Dimensions []string
    Filters    []Filter
}
```

where both ranges use half-open semantics:

```text
[start, end)
```

The request uses canonical semantic identities.

Natural-language period interpretation such as `last quarter`, `previous month`, or `since launch` remains an Agent/application responsibility.

The resolver validates project, metric identity, time dimension identity, time-dimension compatibility, dimension compatibility, filter identity and value typing, relationship reachability, fan-out safety, and explicit period validity.

Shared filters apply identically to baseline and current periods. A caller cannot silently use different non-time filters for the two periods in one attribution request. If the caller wants to compare different populations, that is a different analytical operation and not metric change attribution under this RFC.

## 10. Query-scoped `MetricAttributionPlan`

RFC-0039 intentionally requires domain-specific IRs to be named for the semantic domain they actually own.

RFC-0052 therefore introduces:

```text
MetricAttributionPlan
```

not `AnalysisPlan`, `SemanticAnalysisPlan`, or another pre-generalized container.

The plan is a static compiler IR. It does not execute data.

Conceptually:

```go
type MetricAttributionPlan struct {
    Metric        CanonicalMetricRef
    TimeDimension CanonicalDimensionRef

    Baseline TimeRange
    Current  TimeRange

    Filters    []ResolvedFilter
    Dimensions []AttributionDimension

    Decomposition MetricDecomposition
}
```

It owns the target metric, the two comparison periods, the exact time coordinate, shared filter semantics, decomposition proof, requested independent attribution dimensions, and attribution-specific validation evidence.

It does not own natural-language interpretation, semantic asset discovery, source credentials, database connections, physical execution, or causal interpretation.

## 11. Interaction with `MetricEvaluationPlan`

`MetricAttributionPlan` does not replace `MetricEvaluationPlan`.

The latter remains the sole query-scoped authority for metric identity, dependencies, metric kind, and typed evaluation semantics.

The pipeline becomes:

```text
ResolvedMetricAttributionRequest
        |
        | build / reuse metric dependency authority
        v
MetricEvaluationPlan
        |
        | derive decomposition proof
        v
MetricAttributionPlan
        |
        | source-aware lowering
        v
SemanticPlan
```

For a ratio such as `average_order_value = revenue / order_count`, `MetricEvaluationPlan` owns the dependency topology. `MetricAttributionPlan` owns the conclusion that the topology admits ratio decomposition for this request.

No lower layer reparses raw model YAML to rediscover that relationship.

## 12. SemanticPlan representation

Attribution remains semantic computation. It must not be hidden as dialect-specific SQL string construction.

RFC-0040 remains authoritative: concrete typed `SemanticPlanNode` values express semantic DAG roles.

RFC-0052 adds two planner-owned node types:

```text
AdditiveAttributionNode
RatioAttributionNode
```

### 12.1 `AdditiveAttributionNode`

Conceptually consumes baseline and current grouped metric branches and owns period alignment, missing-segment identity-fill proof, per-segment delta, total delta, contribution percentage, reconciliation semantics, and deterministic ranking projection.

### 12.2 `RatioAttributionNode`

Conceptually consumes baseline/current branches containing numerator and denominator and owns period alignment, segment numerator/denominator values, global ratio values, per-segment rates, denominator-derived weights, rate effect, mix effect, entry/exit treatment, and reconciliation semantics.

The node family remains closed. Adding either node requires the normal RFC-0040 obligations: construction, validation, deep clone, deterministic kind projection, explain, fingerprint, optimizer handling, SQLPlan lowering, and conformance coverage.

## 13. Additive attribution mathematics

For one requested dimension with segments `i`:

```text
B_i = baseline metric value for segment i
C_i = current metric value for segment i

delta_i = C_i - B_i
```

Global values are:

```text
B = Σ B_i
C = Σ C_i

total_delta = C - B
```

The fundamental invariant is:

```text
Σ delta_i = total_delta
```

subject only to the metric's declared numeric precision behavior.

When `total_delta != 0`, segment contribution percentage is:

```text
contribution_pct_i = delta_i / total_delta * 100
```

A negative contribution percentage is valid. A contribution percentage greater than 100% is also valid when positive and negative segment changes offset one another. Metis must not clamp values into `[0, 100]`.

If `total_delta = 0`, `contribution_pct` is undefined and is returned as null rather than fabricated.

## 14. Ratio attribution mathematics

For ratio:

```text
R = A / B
```

and segment `i`, define for each period `p`:

```text
a[p,i] = numerator
b[p,i] = denominator

r[p,i] = a[p,i] / b[p,i]
w[p,i] = b[p,i] / Σ b[p,j]
```

so that:

```text
R[p] = Σ w[p,i] * r[p,i]
```

For a segment present with a defined denominator in both periods, use a symmetric two-factor decomposition:

```text
rate_effect_i =
    0.5
    * (w[baseline,i] + w[current,i])
    * (r[current,i] - r[baseline,i])
```

and:

```text
mix_effect_i =
    0.5
    * (r[baseline,i] + r[current,i])
    * (w[current,i] - w[baseline,i])
```

This avoids making the answer depend on whether the implementation applies rate changes before mix changes or vice versa.

For continuing segments:

```text
segment_effect_i = rate_effect_i + mix_effect_i
```

### 14.1 Entry and exit

A segment absent in baseline but present in current is an entry:

```text
entry_effect_i = w[current,i] * r[current,i]
```

A segment present in baseline but absent in current is an exit:

```text
exit_effect_i = -w[baseline,i] * r[baseline,i]
```

Metis does not invent a baseline rate for a newly appearing segment or a current rate for a disappearing segment.

### 14.2 Reconciliation

The required invariant is:

```text
R[current] - R[baseline]
    =
Σ rate_effect
  + Σ mix_effect
  + Σ entry_effect
  + Σ exit_effect
```

within the deterministic numeric tolerance defined for the selected result type.

### 14.3 Undefined ratios

If a total denominator is zero, the ratio decomposition is undefined.

The compiled evidence query must surface this explicitly rather than divide by zero or silently substitute a value. Segment-level zero denominators similarly produce explicit defined/undefined evidence.

No `COALESCE(..., 0)` may turn an undefined ratio into a valid zero rate unless the metric's existing semantics explicitly define that behavior.

## 15. Independent dimensions, not multidimensional causality

Given:

```text
dimensions = [
    customers.region,
    products.category
]
```

Metis computes two independent decompositions: revenue change by region and revenue change by category. Each decomposition reconciles independently to the same global metric change.

Therefore:

```text
west contribution = 65%
electronics contribution = 57%
```

does not imply `65% + 57% = 122% of the cause`.

The two dimensions may describe overlapping rows.

RFC-0052 does not introduce a universal ranking algorithm that claims one dimension explains more of the change than another. There is no mathematically canonical cross-dimension explanation score.

The Agent may inspect the strongest segment evidence and decide where to drill next. Metis calculates evidence; the Agent chooses the investigation path.

## 16. Deterministic ordering

Within one dimension, results are ordered by:

```text
ABS(segment_effect) DESC
```

with a deterministic typed-value tie breaker.

For additive attribution, `segment_effect = delta`.

For ratio attribution:

```text
segment_effect =
    rate_effect
  + mix_effect
  + entry_effect
  + exit_effect
```

Ordering is a presentation projection over deterministic evidence. It is not a causal score.

High-cardinality result limiting is deferred to the eventual interface contract. Any future top-K interface must calculate reconciliation over the full segment population before truncating returned evidence.

## 17. Reconciliation is mandatory

Attribution without reconciliation is not considered valid evidence.

Every compiled attribution query must make it possible to observe expected total change, decomposed total change, and reconciliation residual.

For additive attribution:

```text
residual = total_delta - Σ segment_delta
```

For ratio attribution:

```text
residual = ratio_delta - Σ all decomposition effects
```

Reconciliation fields must survive SemanticPlan explain, output schema, SQLPlan lowering, and real-engine conformance.

If a strategy cannot provide a mathematically defined reconciliation contract, it is not admitted as an attribution strategy.

## 18. Cross-dataset dimensions

Attribution does not bypass ordinary relationship planning.

For a metric on `orders` and a dimension on `customers`, the same relationship, cardinality, fan-out, population-preservation, and temporal-validity rules used by ordinary compilation remain authoritative.

An attribution request cannot use an otherwise-invalid dimension merely because the result is being used for analysis rather than presentation.

The rule is:

```text
if ordinary semantic grouping is not proven safe,
attribution grouping is not proven safe.
```

RFC-0033 fan-out evidence remains authoritative. RFC-0052 adds no special analytical-exploration escape hatch.

## 19. Filters

All non-period filters are shared across both periods.

For example:

```text
metric = revenue
filter = status = 'completed'
baseline = Q1
current  = Q2
```

means Q1 completed revenue versus Q2 completed revenue.

The request cannot encode Q1 completed orders versus Q2 all orders as one ordinary attribution comparison. Different populations require an explicitly different analysis contract.

Filters introduced during drill-down remain ordinary semantic filters, followed by another attribution request. Metis does not communicate drill-down state through raw SQL snippets. Canonical semantic filter structure remains the durable contract.

## 20. Compilation boundary

This RFC does not change Metis ownership of execution.

Metis may internally produce a bounded attribution compilation bundle:

```text
MetricAttributionCompilation
    |
    +-- dimension region
    |      `-- SQLRenderResult
    |
    +-- dimension category
           `-- SQLRenderResult
```

but the physical queries are still executed elsewhere.

Metis does not acquire database URLs, usernames, passwords, OAuth tokens, connection pools, query sessions, retries, result streaming, warehouse routing, or result caches.

The architectural boundary remains:

```text
Metis
    |
    | SQLRenderResult
    v
=========================
    |
DB MCP / CLI / SDK
    |
    v
Analytical engine
```

## 21. Agent-facing interface

RFC-0051 established the primary MCP surface:

```text
list_projects
list_models
get_model
list_metrics
get_metric
get_dimensions
get_dimension
get_relationships
compile
```

RFC-0052 changes none of them.

In particular, this RFC does not add `attribute_metric`, `explain_change`, `analyze_metric`, `compare_periods`, or `root_cause`.

The reason is architectural sequencing. First prove that attribution is a stable deterministic semantic primitive. Then benchmark how an Agent should consume it. Only after that should Metis decide whether the public interface should be a `compile_attribution` operation, an `intent: query | attribution` extension to `compile`, or a separate REST/SDK analysis compile surface without a primary MCP tool.

That decision belongs to a later interface RFC. The core must not be shaped around a prematurely chosen tool name.

## 22. Error and fail-closed contract

Existing semantic errors remain authoritative wherever they already describe the failure.

RFC-0052 introduces attribution-specific errors only where a genuinely new semantic condition exists.

Candidate stable errors include:

```text
UNSUPPORTED_METRIC_DECOMPOSITION
INVALID_ATTRIBUTION_PERIOD
INVALID_ATTRIBUTION_TIME_DIMENSION
INVALID_ATTRIBUTION_ALIGNMENT
```

Examples:

```text
COUNT(DISTINCT customer_id)
    ->
UNSUPPORTED_METRIC_DECOMPOSITION
```

```text
metric uses unsupported nonlinear derived expression
    ->
UNSUPPORTED_METRIC_DECOMPOSITION
```

```text
dimension missing-value alignment requires an unsupported fill
    ->
INVALID_ATTRIBUTION_ALIGNMENT
```

Runtime data conditions that cannot be known at compile time, such as an observed zero denominator, must be represented explicitly in the compiled evidence result rather than misreported as compile-time semantic errors.

Every new error must use the existing caller-action contract from RFC-0034.

## 23. Explainability and fingerprints

`MetricAttributionPlan` and attribution SemanticPlan nodes must have deterministic projections.

Explain output includes semantic facts such as metric ref, time-dimension ref, baseline/current periods, dimension ref, decomposition kind, numerator/denominator refs, fill/alignment evidence, and reconciliation strategy.

It does not expose internal Go type names, raw Ossie extensions, or arbitrary SQL fragments as semantic authority.

Fingerprinting includes all facts that can change attribution meaning. Two attribution plans that differ in metric, time coordinate, periods, shared filters, dimension, decomposition strategy, ratio operands, or alignment semantics must not have the same semantic fingerprint.

Physical alias changes that do not alter semantics remain outside the semantic fingerprint.

## 24. Optimizer contract

The optimizer may simplify attribution plans only under the same proof-gated model as ordinary SemanticPlan optimization.

Potentially safe future transformations include sharing the same baseline source aggregation across multiple compatible outputs, sharing current-period source work, eliminating redundant projections, and predicate pushdown already proven safe by existing semantics.

The optimizer must not change the comparison population, change baseline/current period predicates, transform a ratio into an average of segment ratios, drop segments required for reconciliation, perform top-K filtering before total reconciliation, or merge independently attributed dimensions into one causal score.

Optimization remains semantics-preserving rather than heuristic.

## 25. Non-goals

RFC-0052 does not introduce:

- causal inference;
- automatic dimension selection;
- natural-language analysis;
- anomaly detection;
- forecasting;
- statistical significance;
- Shapley attribution across arbitrary dimensions;
- warehouse execution;
- public MCP expansion.

## 26. Alternatives

### 26.1 Let the Agent do everything with ordinary `compile`

Rejected as the canonical solution. An Agent may still orchestrate ordinary queries, but repeated decomposition logic would remain probabilistic and duplicated across every Agent implementation. The deterministic parts belong in shared semantic infrastructure.

### 26.2 Put attribution instructions in the MCP server prompt

Rejected. A prompt cannot provide the same correctness authority as typed plans, validation, fingerprints, and reconciliation.

### 26.3 Add `attribute_metric` directly to MCP

Rejected for this RFC. It couples public tool design to an unproven core abstraction and immediately expands the carefully bounded RFC-0051 surface.

### 26.4 Extend `MetricEvaluationPlan` with attribution

Rejected. `MetricEvaluationPlan` answers which metric evaluations exist and how metrics depend on one another. Attribution answers how one metric's change across two periods is decomposed over a requested analytical dimension. They are separate semantic domains.

### 26.5 Introduce a generic `AnalysisPlan`

Rejected. RFC-0039 explicitly rejects pre-generalizing IR names around hypothetical future domains. The real domain introduced here is metric attribution, so its IR is named `MetricAttributionPlan`.

### 26.6 Store additivity or attribution strategy in Ossie YAML

Rejected. Where semantics can be derived, Metis derives them. Model-authored custom extensions cannot assert their own correctness proof.

### 26.7 Execute queries inside Metis

Rejected. It would turn a semantic runtime into a data-serving platform and introduce connection, credential, execution, and result-lifecycle responsibilities unrelated to semantic compilation.

### 26.8 Rank candidate dimensions automatically

Rejected for v1. There is no universal semantic meaning for "dimension X explains the change better than dimension Y," especially when dimensions overlap. The Agent chooses the investigation path.

## 27. Rollout and migration

Implementation is split into independently reviewable phases.

### Phase A — decomposition algebra only

Introduce derived metric decomposition analysis.

Support and test:

```text
additive
ratio
unsupported
```

No planner behavior change. No SQL change. No public interface change.

This phase must explicitly prove interaction with RFC-0033 aggregation algebra and RFC-0014 fill semantics.

### Phase B — additive `MetricAttributionPlan`

Introduce `ResolvedMetricAttributionRequest` and `MetricAttributionPlan` with additive metrics only.

Add `AdditiveAttributionNode` and deterministic plan validation, explain, clone, and fingerprint support.

Compile additive attribution for one dimension through the ordinary SemanticPlan -> SQLPlan boundary.

No MCP changes.

### Phase C — additive multi-dimension bundle

Permit an attribution request to contain at most 16 dimensions. Duplicate and empty dimensions fail closed. Each dimension remains an independent decomposition and lowers into an independent semantic root/query; bundle entries use canonical dimension-ref ordering and share one resolved metric, time dimension, baseline/current pair, filter set, semantic model, execution binding, and compile target. No cross-dimension ranking is introduced.

### Phase D — ratio decomposition

Reuse the Phase A `RatioMetricDecomposition` catalog proof and add `RatioAttributionNode` with exact symmetric mix/rate decomposition and entry/exit semantics. Zero-denominator behavior must be explicit and tested.

Phase D1 closes the typed semantic-node contract, including construction, validation, clone, explain, fingerprint, and optimizer handling, without SQL changes. Phase D2 adds physical SQLPlan lowering and extends the independent-query bundle to ratio evidence. The D2 lowering uses typed conditional/null-test expressions, preserves explicit period-population presence, guards every ratio denominator, and reconciles the complete segment population before deterministic ordering.

### Phase E — real-engine conformance

Execute attribution compilation fixtures against DuckDB, ClickHouse, and Doris using existing test infrastructure. Fixtures must include deliberately planted changes whose expected contributions are known independently of the implementation.

Implemented evidence uses the same internal test-only bundle on all three
engines. It independently compiles `channel` and `segment`, applies one shared
cohort filter to both periods, includes continuing/entry/exit signals, and
checks planted symmetric effects and reconciliation. Each engine also executes
present segment-denominator-zero and total-denominator-zero cases and verifies
null values plus explicit undefined-state output.

### Phase F — Agent evaluation

Only after the core contract is stable, add an experimental AgentBench arm or scenario set comparing Agent + ordinary semantic compile loops versus Agent + deterministic attribution compilation.

Measure at minimum correctness, tool calls, token/context cost, reconciliation failures, and final-answer consistency.

Phase F does not automatically authorize an MCP tool. A separate interface RFC is required for that decision.

Phase F remains follow-on product evaluation rather than an implementation
precondition for this semantic and physical compile contract. Any public
interface remains blocked on a separate RFC.

## 28. Compatibility

Ordinary query compilation must remain unchanged.

The current canonical compilation pipeline and existing SemanticPlan scenarios must produce the same results unless an independently discovered correctness defect requires an explicitly reviewed fix.

In particular, existing canonical scenarios, SemanticPlan fingerprints, SQL/parameter corpus, and RFC-0051 MCP vocabulary must not change merely because attribution support exists.

Attribution is additive product capability, not a rewrite of ordinary metric compilation.

## 29. Test and acceptance criteria

RFC-0052 may become `Implemented` only when all of the following are true.

### Algebra

- `SUM` is admitted as additive when fill and grouping semantics permit.
- `COUNT` is admitted as additive where grouped values safely merge through `SUM`.
- `SUMIF` is admitted under its existing predicate semantics.
- `MIN` and `MAX` are not classified additive for change contribution.
- source `AVG` is not silently classified as a ratio.
- `COUNT(DISTINCT ...)` fails closed.
- unknown aggregations fail closed.
- a supported linear derived metric such as `revenue - cost` is derived additive.
- arbitrary nonlinear expressions fail closed.
- an explicit `revenue / order_count` metric is derived as ratio.

### Alignment

- a segment present in both periods is aligned once;
- new/disappeared additive segments receive legal identity fill only when permitted;
- unsupported fill semantics fail closed;
- filter semantics are identical across baseline/current except for the period predicate.

### Additive attribution

- segment deltas reconcile exactly or within the numeric contract to total delta;
- contribution percentage is null when total delta is zero;
- contribution percentages are not clamped;
- positive and negative offsetting contributions are preserved.

### Ratio attribution

- global baseline/current ratios match ordinary metric semantics;
- symmetric rate/mix decomposition reconciles;
- result does not depend on rate-first versus mix-first ordering;
- entry and exit segments reconcile without invented rates;
- zero-denominator behavior is explicit;
- undefined ratios never silently become zero.

### Semantic safety

- incompatible dimensions fail through existing compatibility rules;
- unsafe relationship fan-out remains rejected;
- temporal relationship evidence remains enforced;
- semi-additive and unsupported metric kinds fail closed unless explicitly proven by implemented rules.

### Plan architecture

- `MetricEvaluationPlan` remains metric-dependency authority;
- `MetricAttributionPlan` remains attribution authority;
- `SemanticPlan` remains source-aware semantic DAG authority;
- `SQLPlan` remains physical SQL-structure authority;
- attribution nodes satisfy all typed-node exhaustiveness requirements;
- no raw model reparsing is introduced below resolver/evaluation boundaries.

### Determinism

- attribution explain output is deterministic;
- attribution semantic fingerprints are stable;
- baseline/current period, filter, dimension, or ratio operand changes alter the fingerprint appropriately.

### Regression protection

- existing ordinary compile correctness corpus remains green;
- existing real-engine query conformance remains green;
- Agent-facing MCP vocabulary remains exactly the RFC-0051 nine-tool surface;
- no database credential or query-execution ownership enters Metis.

### Real-engine evidence

At least one deterministic fixture must contain planted signals such as one region losing volume and one category changing ratio/rate, and DuckDB, ClickHouse, and Doris must reproduce expected attribution evidence from independently calculated ground truth.

## 30. Documentation updates

Implementation must update:

```text
docs/proposals/README.md
docs/design/semantic/pipeline.md
docs/specs/semantic/compilation-pipeline.md
docs/specs/semantic/aggregation-algebra-and-fanout-safety.md
docs/specs/semantic/semantic-plan-node-model.md
docs/specs/glossary.md
README.md
```

and add a dedicated current specification such as:

```text
docs/specs/semantic/metric-change-attribution.md
```

The specification must clearly distinguish aggregation algebra, metric decomposition algebra, metric attribution, and causal inference, and document that the RFC-0051 Agent-facing MCP surface is unchanged.

## 31. Final invariant

The durable boundary introduced by this RFC is:

```text
Agent
    decides what to investigate

Metis
    proves how the metric may be decomposed
    and compiles deterministic evidence

Engine
    executes the evidence query

Agent
    reasons over the returned evidence
```

Or, more compactly:

> **Agents investigate. Metis decomposes semantics. Engines compute evidence.**

This extends Metis from deterministic Semantic-to-SQL compilation toward deterministic **Semantic-to-Evidence** without making Metis an Agent, a causal inference system, or a database execution platform.
