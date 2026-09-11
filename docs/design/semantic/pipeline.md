# Semantic Pipeline Architecture

Metis separates semantic meaning from physical SQL syntax while allowing Agents to consume stable semantic evidence before physical execution.

Ontology candidate resolution is a separate metadata path from the query
pipeline: a generation-owned typed graph and binding index supplies visible
dimension evidence through DiscoveryService. The caller explicitly selects a
canonical ref before ordinary query resolution. Neither ontology discovery nor
resolution invokes the Planner or changes SQL compilation. See the
[current contract](../../specs/semantic/ontology-resolution.md).

```text
Ossie / future semantic model providers
              |
              v
       SemanticManifest
      (parsed, bound, typed
       semantic truth)
              |
              +-- SemanticGraph
              |   manifest-scoped relationship and
              |   metric-dependency topology
              |
              +-- SemanticManifestLookup
              |   read-only metric, dimension, and
              |   graph evidence bound to one digest
              |
      +-------+----------------------+
      |                              |
      v                              v
Semantic Search                 SemanticQuery + SQLDialect
 ranked refs or enumeration                 |
 compact manifest evidence                  v
      |                         Renderer Selection (once)
      |                                      |
      |                                      v
      |                                   Resolver
      |                          bind / validate / dependencies
      |                          relationships / expression selection
      |                    attach SemanticManifest bound/typed analysis
      |                                      |
      |                                      v
      |                              SemanticQuerySpec
      |                              ResolvedExpression
      |                                      |
      |                                      v
      |                          Metric Evaluation Planner
      |                       query-scoped metric obligations
      |                         identity / kind / dependency
      |                                      |
      |                                      v
      |                           MetricEvaluationPlan
      |                        (metric-bearing queries only)
      |                                      |
      |                                      v
      |                              Semantic Lowering
      |                         source-aware typed node DAG
      |                                      |
      |                                      v
      |                                SemanticPlan
      |                                      |
      |                                      v
      |                          Plan Invariant Validator
      |                            pre-optimization gate
      |                                      |
      |                                      v
      |                                  Optimizer
      |                         typed query-shape rewrites
      |                         no semantic reinterpretation
      |                                      |
      |                                      v
      |                          Plan Invariant Validator
      |                           post-optimization gate
      |                                      |
      |                                      v
      |                           optimized SemanticPlan
      |                     +----------------+----------------+
      |                     |                                 |
      v                     v                                 v
Agent Evidence       QueryExplanation                      SQLPlan
                    stable semantic evidence          typed physical DAG
                                                              |
                                                              v
                                                    Same selected Renderer
                                                    Doris | ClickHouse | ...
                                                              |
                                                              v
                                                   Physical SQL + Logical Output Schema
```

The final compilation handoff is `artifact.CompiledQuery`, which atomically
owns the physical query and logical output schema without runtime placement
state. `compiler` owns generic single-plan compilation through the already
selected Renderer and attribution-specific bundle composition on top of that
path. The two workflows remain separated into `compiler.go` and
`attribution_compiler.go`. The execution Runner consumes only the completed
artifact and schema, never a SemanticPlan, SQLPlan, Renderer, or target lookup.

Configured data policy adds an application preflight between resolution and
semantic lowering. It collects the complete multi-query workload using the
same evaluation plans, evaluates one immutable decision, and binds relation
constraints before optimization. Core plan inputs retain only typed bound
constraints and opaque equality scope, never authenticated identity or an
adapter. Physical lowering filters each owning relation before joining it.
See the [current contract](../../specs/operations/data-access-policy.md).

The Planner implementation is split by the IR or workflow each package owns:

```text
planner/evaluation   MetricEvaluationPlan construction and validation
        |
        v
planner/builder      resolved request + evaluation evidence -> SemanticPlan
        |
        +-----> planner/temporal   shared built-in grain/literal operations
        |
        v
planner/semanticplan SemanticPlan IR, typed nodes, validation and ownership
        |
        +-----> planner/optimizer    proof-gated SemanticPlan rewrites
        |
        +-----> planner/attribution  attribution request/proof/bundle workflow
        |
        v
planner/conversion   SemanticPlan -> sqlplan.Plan + logical OutputSchema
        |
        +-----> planner/temporal   shared built-in grain/literal operations
```

The root `planner` package owns only top-level construction/optimization
orchestration and runtime observation. Physical compilation imports
`planner/conversion` directly; the conversion package
consumes the already selected Renderer only for expression compatibility and
capability evidence. It neither resolves a Renderer nor calls `Render`.

Relationship-cardinality and population-preservation proofs are builder-owned
construction logic. Source-scan fusion is an optimizer rule. They do not have
separate `planner/join` or `planner/source_scan` packages; those helper-sized
packages would widen APIs without creating an independent authority boundary.

## Explicit metric authority, one semantic DAG

Metric change attribution reuses these authorities rather than branching around
them. After metric evaluation proves additive or explicit ratio algebra, an
attribution planner creates one typed semantic DAG for each requested
dimension. A bundle contract proves that those DAGs share metric, periods,
filters, model, and target, while the DAGs and physical queries remain
independent. A `channel` decomposition and a `region` decomposition therefore
never become one mixed grain or one cross-dimension ranking. The current
normative contract is
[`metric-change-attribution.md`](../../specs/semantic/metric-change-attribution.md).
The compiled bundle remains internal; the optional runtime executes entries
sequentially and `analytics/attribution` converts complete normalized evidence
into one typed public result.

Metric comparison reuses the ordinary query path rather than adding a new plan
shape. `CompareMetricsService` constructs baseline/current SemanticQuery values
with identical refs and shared filters, compiles both with the exact
Backend-selected Renderer, and executes the two completed artifacts through the
same Runner. `analytics/comparison` then validates and aligns normalized tuple
evidence and computes exact change. No comparison node, SQL builder, or generic
analytical IR is part of Semantic Core.

`SemanticPlan` is not a container that sometimes holds a computation graph. It
is the graph: `Nodes` are ordered topologically, each node's
`NodeBase().Inputs` are its dependency edges, and the output contract carries the final
projections, grain, predicates, ordering and limit.

Metric-bearing queries first build exactly one validated, query-scoped
`MetricEvaluationPlan`. It owns metric root obligations, identities, dependency
edges, stable kinds, typed specs, resolved expressions, and canonical time
bindings. A named lowering pass transfers those facts into source-aware typed
`SemanticPlanNode` values; dataset roots, relationships, grain, predicate
placement, node shape, population-preservation evidence, and sharing proof are decided only on the semantic-plan
side of that seam.

Metric-free queries bypass metric evaluation planning and directly construct a
typed `SourceSelectionNode`. Every successful plan therefore owns one typed DAG,
but not every query fabricates metric evaluation state.

Physical conversion therefore has one entry and no semantic branch.
`planner/conversion.BuildSQLPlan` validates the plan and
picks a physical shape from what the plan owns:

- **compact** — every node reads one scan over the same root, at the same
  grain, with the same predicates as the query, so the whole thing is one
  select;
- **composed** — aggregate boundaries, multiple source roots, windowed or
  offset semantics, calendar domains, or a final-output predicate require
  separate blocks.

The choice affects SQL shape only. It cannot redo semantic resolution,
predicate ownership, join safety, source sharing, extension interpretation, or
grain compatibility: those are settled in the plan it receives. A strategy that
cannot lower a validated plan fails explicitly rather than falling back.

This replaced a real split. Before RFC-0037, 37 of 96 conformance scenarios
reached physical lowering without a materialized graph and used a second
interpreter reading flat plan fields. Both interpreters were correct, and that
was the problem: a semantic feature could work on one and be absent or subtly
different on the other, and scenario counting could detect a routing change but
not drift inside either branch.

## Why this boundary

The architecture borrows two proven ideas from the semantic-layer ecosystem:

- **MetricFlow-style phase separation**: resolve semantic intent and produce a plan before warehouse SQL is materialized.
- **Cube-style semantic graph stability**: measures/dimensions/joins define semantic meaning; dialect-specific SQL generation does not own that graph.

Metis deliberately keeps its architecture smaller. It does not introduce a universal relational IR merely to mirror another project, and it does not translate every provider into MetricFlow or Cube source syntax.

The key rule is:

> Target-aware expression selection is allowed; target-dependent semantic truth is not.

`ResolvedExpression` is the enforced Resolver/Planner value boundary. It
records the selected declared source dialect and source text; metric instances
also retain the SemanticManifest `BoundExpression` and `TypedExpression` produced during
ingestion. Core `SemanticPlan` nodes do not accept raw expression strings.
Planner rejects unresolved expressions and metric expressions that have lost
their semantic analysis before it constructs query shape or SQLPlan.

This lets a metric use `sumIf(...)` on ClickHouse and `SUM(CASE WHEN ...)` on Doris while still representing the same metric, dependencies and relationship graph.

## Semantic-plan optimization

Metis has a rule-based optimizer inside the Planner boundary. The default Planner constructs typed `semanticplan.SemanticPlan` state, validates structural invariants, applies `optimizer.Optimizer`, then validates the rewritten plan again before SQLPlan conversion. The optimizer clones its input and works only on semantic-plan fields; it does not parse or rewrite rendered SQL.

The invariant validator is deliberately independent of individual optimization rules. It checks planner-owned structural assumptions such as node dependency closure/order, source-node sharing membership, required-dataset reachability, resolved expressions, concrete node semantics, rooted join reachability, and post-evaluation predicate targets. Optimizer rules still own their local rewrite preconditions; the validator is the surrounding fail-closed safety net.

Fan-out safety separates aggregation algebra from population shape. SemanticManifest
derives duplicate sensitivity, while semantic lowering derives relationship-
scoped population-preservation obligations from join semantics, filter
placement, grouping, and expression dataset references. The plan carries and
validates that evidence, and Explain projects it unchanged. A target unique key
proves at-most-one matching row but does not prove that an inner join preserves
every source row; absent referential-totality evidence, that obligation remains
unproven. The first narrow relaxation treats a null-rejecting joined-side filter
on a single relationship as an existential-membership definition: unmatched
source rows are outside the requested population, and repeated matches are safe
only for a single duplicate-invariant aggregate whose expression and grouping
remain source-owned. The Resolver carries the selected expression's derived
aggregation properties into planning; the Planner combines them with typed
population evidence, records any fan-out admission on the source node, and
otherwise fails with `UNSUPPORTED_RELATIONSHIP_FANOUT`.

A planner path that intentionally does not run optimizer rewrites must still validate the final semantic plan before lowering. In particular, the conversion path that suppresses raw predicates preserves its intentional optimizer bypass but performs a `pre-lowering` invariant check. Configuring Planner without an optimizer likewise does not disable plan validity checks.

The default optimizer performs metric-evaluation pruning, deterministic normalization of set-valued metadata, predicate deduplication/consumption, join deduplication/elimination, and compatible source-scan fusion. For the same input, the default sequence is deterministic, input-immutable, and reaches the same structural plan shape on a second pass. Per-pass `OptimizationTrace` bookkeeping is not part of structural fixed-point equality.

Requirement closure is explicit and typed. Projections, groups, filters, sorts, hidden/requested metric dependencies, and semantics-bearing transformations contribute dataset/evaluation requirements before pruning. The optimizer may remove only work that is outside that closure; incomplete proof retains work rather than guessing.

Predicate placement remains semantic-planner-owned. Source-node predicates are consumed by the optimizer only when the Planner has already established equivalent ownership on the relevant source aggregates. Advanced boundaries such as conversion, cumulative/grain-to-date, time offset, dense calendar/fill, post-evaluation filters, and semi-additive selection are not crossed by heuristic optimizer movement.

Source-node sharing requires compatible semantic context, not merely similar SQL. Fusion compares physical root/source plus semantic source roots, required datasets/relationships, joins, output grain, and pre-aggregation predicates. Sharing membership is deterministic. Metric pruning invalidates stale source-node state before fusion is regenerated, common dependencies are evaluated once, and stale intermediate columns do not survive lowering.

The optimizer is intentionally below semantic resolution: it may make the plan smaller or more canonical, but it does not choose metric meaning, resolve new relationships, or select target expressions. SQLPlan lowering remains capable of consuming a semantically valid unoptimized plan.

`OptimizationTrace` is internal planner evidence. Agent-facing contracts do not depend on rule names or whether a particular optimization fired.

## Differential safety evidence

Optimizer safety is executable rather than inferred from SQL appearance. The repository compares the same resolved query through optimized and unoptimized Planner paths.

The in-process differential contract requires identical logical output schema and Agent-facing Explain evidence while also demonstrating a real structural query-shape reduction. The real-engine differential contract compiles representative canonical scenarios with and without optimization, executes both forms against Doris and ClickHouse, normalizes physical result metadata/values into the shared result model, and compares them using each scenario's ordered or unordered semantic contract.

Representative differential coverage includes derived and multi-source composition, relationship/filter/order/limit shaping, cumulative and time-offset metrics, metric filters, conversion, and semi-additive evaluation. Rendered SQL is allowed to differ; observable semantic results are not.

This differential path is test infrastructure, not a second public Planner mode.

## Agent-facing semantic evidence

The primary Agent surface is deliberately two operations:

```text
search_semantics  ranked refs, or enumeration when query is absent
compile           explicit SemanticQuery to physical query
```

Search uses SemanticManifest-derived lexical evidence but does not perform
natural-language planning or mutate a query. Without query text it enumerates
the bounded kind/model scope deterministically. The internal core `Resolver`
remains authoritative only after the Agent submits an explicit
`SemanticQuery`.

Semantic Context is a focused read model over SemanticManifest and discovery compatibility evidence. It does not parse or reinterpret metric expressions independently; dependencies, source datasets, constraints, and relationship-path evidence come from the same semantic truth already used by resolution.

Explain Query branches from the same Renderer Selection -> Resolver -> Planner
orchestration used by Compile. The explanation layer converts resolved/planned
semantic decisions into stable Agent-facing step kinds and evidence. It is not
an alternate planner and does not expose raw `SemanticPlan`, optimizer trace,
SQLPlan, or renderer-private state.

The selected Renderer is one per-compilation identity/instance. Resolver uses
its expression dialect and capabilities, and final physical compilation calls
that same instance's `Render(SQLPlan)`. An equal dialect name obtained through a
second lookup is not the same authority. CompileTarget and semantic Engine
wiring have been removed.

Because Planner's default path includes validated semantic-plan optimization, optimization is required to preserve the semantic evidence from which Explain is produced. An optimization may alter internal query shape, but it must not manufacture or remove an Agent-visible semantic decision. An invariant failure therefore closes the shared Planner path before either Explain or Compile can consume malformed plan state.

This split gives Agents two deterministic read models without moving Metis's execution boundary:

- **Semantic Context** answers what the selected semantic assets mean and whether dimensions are compatible.
- **QueryExplanation** answers how a concrete semantic query resolved and which semantic transformations Planner applied.

Both remain transport-neutral service outputs; REST and MCP only encode them.

`SemanticPlan.Projections` also defines the Agent-facing output contract.
Planner derives a target-neutral schema containing column order, semantic role,
declared Ossie datatype, and requested grain. Renderers materialize the physical
query but do not redefine that schema. REST, MCP, and offline compilation
therefore expose the same logical result shape even when physical SQL syntax or
database wire types differ.

See [`../../specs/semantic/compilation-pipeline.md`](../../specs/semantic/compilation-pipeline.md), [`../../specs/testing/architecture.md`](../../specs/testing/architecture.md), and [`../../proposals/semantic/0017-semantic-optimizer-safety-query-shape.md`](../../proposals/semantic/0017-semantic-optimizer-safety-query-shape.md) for the current contract and implementation record.
