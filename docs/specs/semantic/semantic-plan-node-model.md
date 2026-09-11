# Semantic Plan Node Model

`SemanticPlan` is Metis's deterministic engine-neutral planning envelope between resolved semantic meaning and target-aware SQL lowering. It is also the semantic computation DAG itself: `SemanticPlan.Nodes` are the nodes, each node's `Inputs` are its dependency edges, node order is topological, and validation, explainability, semantic optimization, and SQLPlan construction all read the DAG from there.

Every successful plan owns that DAG. A query with metrics owns the typed evaluation nodes metric construction produced; a metric-free dimension or `distinct_values` query owns a typed source-selection node. There is no query shape that reaches lowering without nodes, and there is no second representation of the DAG beside the plan. See RFC-0037, *Universal SemanticPlan DAG and Unified Lowering*.

`planner/conversion.BuildSQLPlan` chooses a physical shape from the validated plan: **compact** when every node reads one scan that can be expressed as a single query block, **composed** when aggregate boundaries, source composition, windows, or calendar domains require separate blocks. Both strategies read the same plan-owned meaning; neither reinterprets semantics from query-shape fields. The resulting validated `sqlplan.Plan` is the sole dialect-renderer input.

Metric attribution uses specialized plan-owned lowering selected by its concrete semantic node. One independent attribution dimension owns one SemanticPlan and one SQLPlan. Additive attribution lowers period aggregates, population union, delta, total reconciliation, and contribution evidence. Ratio attribution lowers ordered numerator/denominator period aggregates, population union with explicit presence evidence, guarded segment rates and weights, symmetric rate/mix effects, entry/exit effects, full-population reconciliation, and deterministic segment ordering. Neither path performs SemanticManifest lookup or combines independent dimensions.

## Contract

A `SemanticPlan` owns ordered `SemanticPlanNode` values, the requested outputs, a `SemanticOutputContract` carrying output projections, grain, final-output predicates, ordering and limit, and any dense calendar domain required by time-relative semantics. Each concrete node type owns its semantic boundary, dependency inputs and grains, output grain, predicates, and its family-specific source, metric, expression, rollup, calendar, conversion, offset, or semi-additive state.

The plan is the authority. Read paths must not reconstruct a second DAG from query-shape state. Explain, validation and lowering fail closed when a plan owns no nodes, because that is a planning bug rather than a query shape they decline to describe.

Node-specific semantic decisions required by lowering belong to the concrete node type; a node that declares a custom-calendar domain owns it directly, and consumers read it from the node rather than from a plan-level index. Query-wide calendar domains and shared-grain proof belong to plan scope. Query-shape fields such as `Root`, `Joins`, `Projections`, `Predicates`, `Groups` and `Sorts` remain construction inputs, but they are not downstream semantic authority once the DAG is installed.

## Composition boundaries

A node boundary records why the node must remain distinct for semantic correctness:

- `source_aggregate`: semantic aggregation over a resolved source path;
- `post_aggregate`: derived computation over already aggregated compatible-grain inputs;
- `aggregate_before_composition`: independently aggregated inputs must compose only after their aggregate boundary;
- `semantic_isolation`: cumulative, offset, conversion, semi-additive, or metric-attribution semantics require a dedicated evaluation boundary.

These boundary values describe correctness, not expected runtime cost.

Plan DAG validation requires unique node IDs, dependency-before-consumer ordering, exact producer grain on dependency edges, boundary/kind consistency, typed evaluation payload consistency, canonical set-like metadata, reachable required datasets, compatible post-aggregate composition grains, and scalar-only cross-aggregate composition where represented as a cross join. Shared-grain evidence must remain fanout-safe and describe the node output grain. Every relationship on a source-aggregate node must also have aligned, complete typed population-preservation evidence; target-key uniqueness and population preservation are separate claims.

## Predicate ownership and boundaries

Predicate placement is an explicit plan/node contract rather than an implicit optimizer choice. `SemanticPredicateScope` distinguishes four semantic locations:

- `source_read`: a predicate owned by source-row acquisition;
- `pre_aggregation`: a predicate applied after source resolution but before semantic aggregation;
- `post_aggregation`: a predicate evaluated over a node's aggregated output;
- `final_output`: a query-level predicate applied only after semantic evaluation is complete, owned by the plan's output contract.

Every `SemanticPlanNodePredicate` carries an owner and typed placement proof. Node-owned predicates must identify their exact `OwnerNodeID`. A predicate representation is fail-closed: exactly one source or post-evaluation representation must be present.

Final-output predicates are stored in the output contract as `PostEvaluationPredicate` values rather than wrapped node predicates. Their scope and proof are fixed by that position, so the contract records the predicate itself; what a caller can get wrong is the output name, and validation checks that the name identifies a produced output.

Each node also carries `SemanticPredicateBoundaryEvidence`, which describes downward predicate movement from the node output toward its inputs. A `source_aggregate` boundary permits movement only with explicit dataset-reachability proof. `post_aggregate`, `aggregate_before_composition`, and `semantic_isolation` boundaries block inferred downward movement because doing so can change metric semantics.

Moving a predicate from ordinary plan predicates into post-evaluation ownership must preserve the predicate multiset. Predicate identity includes field, operator, and value; promotion consumes one matching predicate at a time and fails closed if the promoted predicate cannot be matched. This prevents same-field, same-operator filters with different values from being silently conflated.

The semantic optimizer consumes predicate evidence but does not invent semantic ownership. It may place an ordinary node predicate at a source only when the plan's nodes prove that the source boundary allows movement and the predicate dataset is reachable. When a semantic-isolation node exists, planner-owned placement remains authoritative.

## Concrete node ownership

The concrete `SemanticPlanNode` type is the semantic discriminator and authority. Its family-specific fields carry decisions that lowering is not allowed to derive again.

Examples include custom-calendar mappings for time offsets, rolling cumulative windows, grain-to-date evaluation, conversions, offset-to-grain boundaries, rollup contracts, semi-additive selection state, and attribution operands/period evidence. Lowering consumes these node-owned values rather than reading independent semantic side channels from `SemanticPlan`.

Ratio attribution lowering fails closed unless both ordered operands are governed source aggregates at the exact decomposition grain, carry resolved physical expressions and additive identity-fill authority, and describe the same source population and filter work. Missing segments remain distinguishable from present segments whose denominator is zero. Undefined segment or total ratios produce null decomposition/reconciliation values plus explicit defined-state output; they are never coerced to zero.

Attribution period evidence remains canonical RFC3339 in semantic explain and fingerprints. SQLPlan period predicates materialize the same UTC instant with target-neutral SQL timestamp lexical form; physical formatting is not a second semantic time interpretation.

Dense calendar domains that apply across nodes are plan-owned. Built-in and custom dense-calendar lowering consumes `SemanticPlan.DenseCalendar` and `SemanticPlan.CustomDenseCalendar`. Per-node custom-calendar domains live directly on the declaring concrete node, and consumers that need a query-wide view compute it from the nodes rather than reading a parallel index.

## Semantic optimization

Semantic plan rewrites operate behind the `SemanticPlanOptimizationRule` contract. A semantic plan rule receives only a bounded `SemanticPlanOptimizationState` — the plan's nodes plus the explicitly permitted query-shape and output context — and never the whole plan. Flattening the DAG into `SemanticPlan` did not widen that surface: the state is transient and unexported, it is not a stored IR, and applying a rewrite explicitly writes back to the plan and revalidates it.

Whether a plan composes nodes is derived from the DAG a rule was handed, not read off a marker field. This boundary prevents a semantic rewrite from treating query-shape fields as an alternate source of semantic truth.

Optimization annotations such as `SourceAggregateNode.MetricState.ShareGroup` describe proven structural reuse opportunities. They do not change semantic meaning and must not leak into Agent-facing semantic explanation. Optimization observability belongs to a separate optimization trace rather than changing the semantic explanation contract.

## Lineage and explain evidence

`ExplainSemanticPlan` is the single semantic explain entry point for Agent-facing consumers. It normalizes node boundary and predicate-boundary evidence so the explanation reports the boundaries lowering will use, validates the plan's DAG, and projects it; it does not rematerialize semantic state. Every query shape is explainable, including the compact ones, so a query's real source boundary and predicate/join evidence are visible without inventing CTEs or physical execution steps.

The explanation contains:

- requested semantic outputs;
- ordered nodes and dependency IDs;
- transitive metric and dimension lineage to source datasets;
- output grain and semantic boundary evidence;
- relationship paths and fanout-safety proof for shared-grain composition;
- relationship-scoped population-preservation obligations and their derived result;
- predicate scope, owner and placement proof;
- typed extension evidence already carried by the node;
- declarative metric constraints carried by the canonical plan, including fill and metric-definition filters.

Agent-facing semantic explanation describes what the query means. It deliberately ignores optimizer-only annotations when those annotations preserve semantic meaning. Backend runtime plans, cost estimates, cardinality estimates, physical join choices, execution statistics, optimizer trace details, and opaque extension payloads are outside this contract.

REST and MCP surfaces reuse this planner-owned semantic evidence instead of reconstructing semantics independently.

## Safe node sharing

When semantic planning or optimization has proven source work equivalent, `SourceAggregateNode.MetricState.ShareGroup` records that structural membership. The field does not infer physical materialization, CTE reuse, backend execution strategy, or Agent-visible semantic identity.

Sharing rules must preserve plan DAG invariants and semantic results. They remain semantic rewrites only to the extent that they are independent of runtime statistics and backend physical cost.

## Determinism and fingerprinting

`FingerprintSemanticPlan` is the sole full-plan semantic fingerprint. It owns a cloned plan and never mutates caller data.

The projection is explicit and typed. It never serializes or hashes the Go object graph, and reflection is not a fallback for a new semantic field: every field of every plan-shaping type must be declared either projected or deliberately excluded with its reason, and every field declared projected must be shown to move the fingerprint when it changes.

Set-like metadata is canonicalized before hashing, including plan `Requested` and node `Metrics`, `Dimensions`, `SourceRoots`, and `RequiredDatasets`. Meaningful node ordering and dependency-input ordering remain represented. Predicate scope, owner, placement proof, boundary evidence, typed semantic payloads, and structural optimization annotations may contribute to the structural fingerprint. Derived `ExtensionEvidence` does not contribute directly: an extension that affects compilation must materialize that effect into a resolved expression or another explicit typed semantic payload before fingerprinting.

This is intentionally distinct from semantic explanation: an optimizer-only structural annotation may change a plan fingerprint while leaving Agent-facing semantic explanation unchanged.

The plan fingerprint is an engineering regression contract only. It is not a cache identity, authorization token, physical-plan key, public semantic version, or Agent-facing identifier.

## SQL parameter evidence

Core's `renderer/sql.SqlRenderResult` keeps SQL placeholders and parameters separate.
Offline exports preserve both values; engine verification binds the parameters
through drivers. Consumers MUST NOT reimplement generic SQL string interpolation.
See the [CLI overview](../../../README.md) for query export.

## Semantic versus physical responsibility

Node boundaries express computations required for semantic correctness. They may require aggregate-before-composition, isolated cumulative/conversion/semi-additive evaluation, a dense calendar domain, or a proven relationship path. They never encode cost or runtime preferences.

Metis permanently leaves statistics, cardinality estimation, cost modeling, physical join ordering, access-path choice, CTE materialization policy, distribution strategy, parallelism, and execution to the backend engine.
