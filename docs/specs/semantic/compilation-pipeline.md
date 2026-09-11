# Semantic Compilation Pipeline Specification

## Contract

Metis compilation is a six-phase contract:

1. **Renderer Selection**: resolve the requested compile-only SQL dialect to
   exactly one Renderer identity/instance.
2. **Semantic Resolution**: bind and validate semantic intent using that
   selected Renderer's expression dialect and capabilities.
3. **Metric Evaluation Planning**: for a metric-bearing query, build and
   validate exactly one query-scoped `MetricEvaluationPlan`; metric-free queries
   bypass this phase without fabricating metric state.
4. **Semantic Planning**: explicitly lower metric evaluation meaning into the
   source-aware typed `SemanticPlan` DAG, or directly construct a typed
   `SourceSelectionNode` for a metric-free query.
5. **Semantic Plan Optimization**: validate the planner-produced plan, apply typed semantics-preserving query-shape rewrites when an optimizer is configured, then validate the rewritten plan.
6. **Physical Compilation**: invoke typed SQLPlan lowering and ask the same
   selected Renderer to materialize that physical DAG.

Normative flow:

    SemanticQuery + SQLDialect
        -> selected Renderer (exactly once)
        -> SemanticQuerySpec
        -> MetricEvaluationPlan (metric-bearing queries only)
        -> explicit metric-to-semantic lowering
        -> SemanticPlan
        -> semanticplan.ValidateSemanticPlan (pre-optimization)
        -> SemanticPlan optimization
        -> semanticplan.ValidateSemanticPlan (post-optimization)
        -> planner/conversion.BuildSQLPlan
        -> sqlplan.Plan
        -> same Renderer.Render(SQLPlan)
        -> artifact.CompiledQuery{SqlRenderResult + OutputSchema}

`artifact.CompiledQuery` is the atomic compiler/runtime handoff. It contains no
DataSource, Backend, Renderer, Project, or routing state. Package `compiler`
owns both generic one-plan `SemanticPlan -> SQLPlan -> selected Renderer`
orchestration and metric-attribution bundle composition. Those workflows remain
separated by `compiler.go` and `attribution_compiler.go`: bundle
compilation reuses the generic path, selects a Renderer at most once, and passes
that exact instance to every query without adding attribution state to generic
one-plan methods or artifacts.

Metric change attribution is an internal sibling path over an already resolved
request and a proven `MetricEvaluationPlan`. It derives a
`MetricAttributionPlan`, builds one independent typed `SemanticPlan` per
dimension, validates their shared bundle contract, and compiles each through the
same SQLPlan and renderer boundary. It does not alter ordinary `SemanticQuery`
compilation or expose a second SQL compiler. The normative contract is
[`metric-change-attribution.md`](metric-change-attribution.md).

When a planner path intentionally bypasses optimizer rewrites, it MUST still validate the final `SemanticPlan` before SQLPlan lowering. Optimization is a query-shape boundary, not a semantic-resolution boundary. SQLPlan lowering MUST remain valid for an unoptimized semantic plan as well as an optimized one; optimizer configuration does not redefine metric meaning.

## Semantic invariance

SQL dialect/Renderer selection MUST NOT redefine semantic meaning. Given the
same model and SemanticQuery, changing only the dialect MUST NOT change metric
identity, dimension identity, root dataset, semantic dependencies, required
relationships, requested grain, or filter target.

A target MAY select a different declared physical expression and MAY produce different physical SQL syntax.

## Resolver requirements

The Resolver MUST:

- resolve metrics, dimensions, filters and order targets;
- reject missing or ambiguous references;
- validate filter shapes and grain use;
- determine semantic dependencies and relationship paths;
- select a declared target expression, with a validated ANSI fallback where supported;
- return an explicit error when no compatible expression exists.

The Resolver MUST NOT maintain separate semantic algorithms for ClickHouse, Doris, Snowflake, Databricks, or other targets.

The Resolver MUST return expressions as `ResolvedExpression` values. Each
value identifies the selected declared source dialect and physical source.
Resolved metric expressions MUST carry the bound and typed SemanticManifest analysis
for that exact dialect implementation. Target-to-ANSI fallback selection is a
Resolver responsibility and MUST NOT be repeated by Planner.

## Metric evaluation plan contract

For every metric-bearing query, `BuildMetricEvaluationPlan` MUST materialize
one deterministic, query-scoped plan before source-aware semantic construction.
The plan contains:

- `MetricEvaluationRoot` values identifying `output`, `predicate`, and `order`
  obligations;
- exactly one `MetricEvaluationNode` per metric identity in the required
  dependency closure;
- explicit `MetricEvaluationInput` edges in deterministic topological order;
- one stable `MetricEvaluationKind` and exactly one matching typed
  `MetricEvaluationSpec` per node;
- owned resolved metric expressions, metric declarations, canonical time
  bindings, and semantic-critical evaluation-local evidence.

The supported metric kinds are `source`, `derived`, `cumulative`,
`time_offset`, `offset_to_grain`, `conversion`, and `semi_additive`. A derived
metric remains `derived` regardless of whether source-aware lowering produces a
`PostAggregateNode`, `JoinAggregatesNode`, or `CrossJoinAggregatesNode`.

`ValidateMetricEvaluationPlan` MUST fail closed on invalid roots or roles,
duplicate identities or edges, missing or non-topological inputs, unreachable
nodes, unresolved expressions, missing semantic analysis, invalid canonical
time bindings, kind/spec mismatch, invalid typed specs, and dependency contracts
that disagree with the typed advanced-metric spec. Unsupported
semantic-critical metric extensions MUST fail rather than degrade to an untyped
derived metric.

Clone, bounded explanation, and deterministic fingerprint functions MUST cover
all owned metric-evaluation state. Their projections MUST NOT claim dataset-root
selection, relationship joins, predicate placement, semantic-node shape,
sharing groups, SQL blocks, CTEs, or runtime strategy.

Metric identity cardinality is independent from execution-unit cardinality. A
metric evaluation node is not a semantic plan node, source scan, aggregation,
SQL block, or CTE. Multiple metric identities MAY share source-aware semantic
work and physical aggregation after `SemanticPlan` proves compatibility, and
shared work MAY expose reusable metric outputs without creating duplicate
evaluation identities or redundant branches.

## Planner requirements

The Planner boundary MUST own metric evaluation planning, source-aware semantic
query shape, optimization, and SQLPlan construction. Its implementation
responsibilities are separated as follows: `planner/evaluation` owns
`MetricEvaluationPlan`; `planner/builder` constructs source-aware plans;
`planner/semanticplan` owns the typed `SemanticPlan` IR and invariants;
`planner/optimizer` owns proof-gated rewrites; `planner/attribution` owns the
attribution workflow; and `planner/conversion` owns validated
`SemanticPlan -> sqlplan.Plan` conversion and logical output-schema derivation.
`planner/temporal` owns only the built-in grain ordering and temporal literal
operations shared by builder and conversion. Relationship-safety proof remains
builder-owned, and source-scan fusion remains optimizer-owned.
The root `planner` package MUST contain only `Planner`, `New`, `Plan`, `PlanPrepared`, runtime
observation, and top-level builder/optimizer orchestration. It MUST NOT alias or
forward contracts owned by planner subpackages.

Every metric-bearing `Planner.Plan` call MUST
build exactly one validated `MetricEvaluationPlan` before `SemanticPlan`
construction. Only the metric-plan builder may consume
`SemanticQuerySpec.EvaluationMetrics` as a collection.

Application data-policy preflight MAY build that one evaluation plan first,
collect the complete workload and bind source-local constraints, then pass it
to `Planner.PlanPrepared`. This path MUST reuse, not rebuild, the validated
evaluation plan. Constraints attach before optimization and are validated on
every owned source input. Core planning receives no Principal or policy adapter.
See [data access policy](../operations/data-access-policy.md).

`MetricEvaluationPlan` MUST be the sole query-scoped authority for metric root
obligations, identities, dependency edges, stable evaluation kinds, typed metric
specs, resolved metric expressions, and canonical time bindings. It MUST NOT own
dataset roots, relationship paths, predicate placement, semantic-node shape,
source-sharing groups, output grain, or SQL structure. Source-aware lowering MAY
consult SemanticManifest dataset, field, and relationship facts, but MUST NOT rediscover
metric dependency edges or metric kinds.

Metric-free dimension and `distinct_values` queries MUST materialize no fake
metric plan. They proceed through typed `SourceSelectionNode` construction.

The explicit `MetricEvaluationPlan -> SemanticPlan` lowering boundary MUST
validate its supplied metric plan and fail closed when a metric-bearing query
reaches it without one.

`planner/conversion.BuildSQLPlan` is the typed physical-producer entry point.
The compilation orchestrator invokes it directly with the already-selected Renderer during
physical compilation. Conversion MUST consume resolved semantic objects
rather than re-reading raw model expressions. No consumer may mutate the
returned plan using `SemanticPlan` authority.

`planner/conversion` MUST NOT resolve or construct a Renderer registry, import
a concrete Renderer, or call `Renderer.Render`. It MAY read expression-dialect
and capability evidence from the exact selected Renderer instance supplied by
the orchestrator. That same instance MUST continue to final rendering.

The planned representation MUST contain enough structure that neither the optimizer nor renderer needs to rediscover semantic identity, relationship meaning, grouping intent, filter targets, aliases, or semantic dependencies.

`SemanticPlan` MUST be the only canonical source-aware semantic IR consumed by
optimization, explain, fingerprinting, and SQLPlan lowering. It MUST own its
computation DAG directly: `SemanticPlan.Nodes` are ordered topologically and
each node's `NodeBase().Inputs` are its dependency edges. Every successful
`Planner.Plan` result MUST own typed nodes, including metric-free dimension and
`distinct_values` queries, which MUST use a `SourceSelectionNode` rather than an
empty or fabricated metric payload.

`planner/conversion.BuildSQLPlan` MUST have exactly one `SemanticPlan` entry. It MUST NOT
branch on the presence of a separate graph value and MUST NOT interpret
query-shape fields as an alternative semantic authority. It MAY select a
compact single-select shape or a composed multi-block shape, derived from the
validated nodes and their boundaries; that selection is a physical-shape
decision only and MUST NOT reinterpret semantic meaning. A selected strategy
that cannot lower a validated plan MUST fail explicitly rather than fall back to
another interpretation.

Query-shape fields such as `Root`, `Joins`, `Projections`, `Predicates`,
`Groups`, and `Sorts` MAY remain construction inputs for source and node
planning. Once the DAG is installed they MUST NOT be downstream semantic
authority, and mutating them MUST NOT silently change generated SQL, explain
output, or fingerprints.

Post-evaluation metric predicates MUST bind to the relation that materializes
the filtered metric. A metric referenced only by a predicate is a constraint on
the visible output domain; joining that hidden metric MUST NOT introduce grain
keys that are absent from every projected or sorted metric.

Planner MUST reject unresolved expression values. Core projections,
predicates, groupings, sorts, and metric-evaluation nodes MUST retain the
`ResolvedExpression` value until physical SQLPlan lowering; empty strings are
not a valid resolution signal.

`semanticplan.ValidateSemanticPlan` is independent of individual optimizer rules. It MUST fail closed on malformed planner state that would make optimization or conversion unsafe, including missing or unordered node dependencies, unreachable required datasets, malformed join reachability, unresolved required expressions, inconsistent concrete node semantics, broken source-node sharing membership, and invalid post-evaluation predicate targets.

A validation failure before optimization, after optimization, or on an explicitly unoptimized pre-lowering path MUST surface as a planning failure. Invalid plan state MUST NOT continue to SQLPlan lowering.

## Semantic optimizer contract

The default Planner uses a typed rule-based `optimizer.Optimizer`. The optimizer clones its input plan before applying rules, so optimization MUST NOT mutate the builder-produced input value.

The current default rule sequence is:

1. `metric_projection_pruning`;
2. `semantic_set_normalization`;
3. `predicate_deduplication`;
4. `predicate_pushdown`;
5. `join_deduplication`;
6. `unused_join_elimination`;
7. `source_scan_fusion`.

These rules may remove redundant evaluation work, canonicalize set-valued semantic metadata, consume proven node-owned source predicates, remove redundant/unneeded relationship edges, and share compatible source nodes. They MUST operate on typed semantic-plan state rather than rendered SQL text.

Optimization MUST NOT select metric definitions, change metric or dimension identity, invent relationship paths, reinterpret target expressions, change public projection order, or silently cross a semantic boundary whose equivalence has not been established.

If an optimization rule returns an error, Planner MUST fail the request as a planning failure rather than lowering a partially rewritten plan.

For the same input plan and default rule set, optimization MUST be deterministic. Applying the default optimizer to an already optimized plan MUST reach the same structural plan shape; `OptimizationTrace` may describe whether rules changed the current pass and is excluded from fixed-point shape equality.

Requirement-driven pruning MUST account for projections, groups, predicates, sorts, hidden/requested metric dependency closure, and semantics-bearing transformations. A relationship edge or evaluation node may be removed only when no surviving typed consumer requires it. Requirement proof is conservative: incomplete evidence retains work rather than guessing.

Predicate placement is Planner-owned semantic state. For advanced semantic evaluation, the optimizer MAY remove a redundant top-level predicate only when equivalent source ownership has already been proven by the planned source nodes. It MUST NOT invent a move across conversion, cumulative, grain-to-date, time-offset, dense-calendar/fill, semi-additive, or post-evaluation boundaries when equivalence is not established.

Source-node fusion requires compatible semantic source context, including root/source identity, semantic source roots, required datasets/relationship context, grain, joins, and pre-aggregation predicates. Compatible source nodes use deterministic naming and membership. Pruning invalidates stale sharing membership before fusion is regenerated, common dependency evaluation is reused, and stale intermediate columns from pruned metrics MUST NOT survive lowering.

`OptimizationTrace` is internal planner evidence about rule application. It is not part of the REST/MCP semantic contract and MUST NOT be required to interpret a successful Agent-facing response.

## Optimized/unoptimized equivalence

Optimization MUST preserve observable semantics even when it changes physical query shape. For the same resolved semantic query, repository conformance compares optimized and unoptimized paths at multiple layers:

- logical `OutputSchema` equality;
- Agent-facing `QueryExplanation` equality where applicable;
- deterministic structural query-shape expectations demonstrating actual reduction;
- normalized Doris and ClickHouse execution-result equality for representative advanced semantic scenarios.

Rendered SQL text is not required to be equal. The unoptimized planner path used by differential tests is a verification mechanism, not a separate public semantic mode.

## Explain Query orchestration

`CompileService.Explain` consumes the same `CompileRequest` as `Compile` and MUST
reuse the same target-resolution, Resolver, validation, and Planner stages up to
semantic planning. It MUST NOT independently reconstruct semantic decisions.

Normative shared flow:

    CompileRequest
      -> SQLDialect -> selected Renderer (once)
      -> Resolver
      -> Planner
      -> SemanticPlan
      -> invariant validation
      -> configured semantic optimization
      -> invariant validation
      +-> Explain -> QueryExplanation
      +-> Compile -> SQLPlan -> SqlRenderResult + OutputSchema

`QueryExplanation` is an Agent-facing semantic read model. It MAY expose stable
Renderer/dialect selection and output-schema facts, but it MUST translate planning state
into stable semantic evidence and MUST NOT expose raw `SemanticPlan`, optimizer
trace, SQLPlan, renderer-private structures, or physical database execution
details.

A semantic failure encountered before or during planning MUST have the same
stable semantic error class for Explain and Compile. Explain MUST fail closed
rather than selecting a relationship path or transformation that Compile would
reject.

## Compiler/renderer requirements

The physical compiler MUST render a planned AST with the one Renderer selected
before semantic resolution. That same Renderer identity/instance MUST supply
`ExpressionDialect()`, `Capabilities()`, and `Render(SQLPlan)`. Resolver and
physical compilation MUST NOT independently look up Renderers, even when a
second lookup would return the same dialect name.

The Renderer MAY perform physical source lowering, identifier quoting, target
function/operator syntax, capability checks, and parameter collection.

It MUST NOT select metric semantics, discover semantic relationships, or silently reinterpret unsupported expressions.

SemanticPlan, SQLPlan, and compiler artifacts MUST NOT establish a second
Renderer or target authority. During migration, any retained target evidence
must agree with the selected Renderer and is removable validation evidence only.

## Output schema contract

Every successful Agent-facing compilation MUST return an `OutputSchema`
alongside the physical query. Its columns MUST follow physical result order and
MUST be derived from resolved `SemanticPlan.Projections`, never from parsing
rendered SQL.

Each output column MUST contain:

- the logical projection name used as the physical result alias;
- a semantic role of `dimension` or `metric`;
- the declared Ossie datatype when one exists;
- the requested built-in or custom time grain for a grained dimension.

The output schema MUST be invariant when only the SQL dialect/Renderer changes. A
dialect MAY expose a compatible physical wire type with a different database
type name, but it MUST NOT change logical column identity, order, role,
datatype, or grain.

The root physical projection MUST honor the declared output datatype rather
than relying on a Driver or Runtime to repair a widened value. Decimal metrics
MUST pass directly through the `DECIMAL(38,18)` exact output boundary. This is
required for operations such as division and `AVG` on engines that otherwise
return binary floating point for Decimal inputs, while retaining 20 integer
digits for large metric values. The same rule applies to Decimal operand
evidence exposed by closed attribution output schemas, not only ordinary query
projections. The narrower `DECIMAL(20,12)` cast is reserved for ratio
intermediates whose arithmetic scale must be bounded. A dialect MUST NOT
silently select a different precision or scale.

Metis MUST preserve an omitted Ossie datatype as unspecified. It MUST NOT guess
one from expression text, rendered SQL, or target behavior. Nullability is not
part of the current output schema contract.

## Expression contract

Dialect-specific expressions are physical implementations of one semantic definition. They MUST be semantically equivalent with respect to declared semantic dependencies.

Selection order:

1. exact target expression;
2. compatible ANSI expression;
3. explicit unsupported-expression error.

Metis MUST NOT rely on heuristic regex rewriting of arbitrary SQL to make an expression compatible with another target.

## Statement-level features

Target statement features are not expression strings. PREWHERE, ARRAY JOIN, SAMPLE, FINAL, WITH TOTALS, LIMIT BY, SETTINGS, GLOBAL JOIN, ASOF JOIN and comparable features MUST be represented through typed plan/AST capabilities when supported.

## Extension rule

A new SQL target SHOULD be implemented by adding target capabilities and rendering/source-lowering behavior. It SHOULD NOT require a fork of semantic resolution or semantic optimization.

This specification follows the same broad separation used by mature semantic layers such as MetricFlow and Cube: semantic objects and relationships are resolved before physical SQL generation, while target-specific SQL remains a physical concern. Metis intentionally does not adopt either project's internal IR as a compatibility requirement.
