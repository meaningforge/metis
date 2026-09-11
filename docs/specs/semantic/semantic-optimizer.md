# Semantic Optimizer Contract

The Metis semantic optimizer is rule-based and deterministic. It does not use statistics, cardinality estimates, cost models, or join-order search.

## Canonicalization boundary

Before any configured rewrite rule executes, optimizer entry clones caller-owned state and canonicalizes fields whose semantic contract is set-like. The canonical set metadata is plan `Requested` and node `Metrics`, `Dimensions`, `SourceRoots`, and `RequiredDatasets`. One rule applies to every plan; there is no separate normalization behavior for a query that happens to lower compactly.

Canonicalization removes duplicate members and sorts the remaining values lexicographically. Rules therefore cannot observe resolver traversal order for set-like fields.

The boundary must preserve sequence-sensitive fields. In particular, it must not reorder user-visible projections, dependency-first semantic nodes, node inputs, joins, predicates, groupings, or sorts solely for canonicalization.

## Rewrite contract

Configured rules execute in the order supplied to the optimizer. The same ordered rule pass repeats until no rule reports a change. Fixpoint execution is bounded to 16 passes; a rule set that never converges fails explicitly rather than looping forever. Rules are therefore required to report `Changed` idempotently for an already-normalized working plan.

Semantic plan optimization is sealed behind `SemanticPlanOptimizationRule`. A semantic plan rule receives a bounded `SemanticPlanOptimizationState` — the plan's nodes plus only the explicitly permitted query-shape and output context needed by safe rewrites — and never the whole plan. A rule that implements only `OptimizationRule.Apply(*SemanticPlan)` is rejected on the semantic plan path rather than being given access to the full plan.

Whether a plan composes nodes is derived from the DAG the rule was handed, not read from a routing field. A rule body shared with the plan-level path therefore asks one question and gets an answer that survives any field being removed.

`OptimizationTrace` preserves the historical complete first-pass trace. A later pass is appended only when at least one rule in that pass changes the plan; the final no-change convergence probe is omitted. This keeps one-pass traces stable while retaining deterministic evidence for productive follow-up passes.

`SemanticSetNormalizationRule` remains a valid explicit rule for compatibility, but normal optimizer entry already guarantees equivalent set normalization before the first rewrite.

The optimizer must not mutate caller-owned input state.

## Agent-facing explain invariance

Agent-facing semantic explanation is a projection of semantic and logical evidence from the plan's own nodes. It must remain invariant under semantic-preserving optimization.

Optimizer-only annotations such as `SourceAggregateNode.MetricState.ShareGroup` are therefore excluded from `SemanticPlanExplanation`. If optimizer observability is exposed to users, it must use a separate optimization trace or optimization explain surface rather than changing the semantic explanation contract.

## Canonical plan DAG gate

`SemanticPlan` is the single semantic IR consumed by validation, optimization, explain, and lowering. Permanent regression coverage must keep those surfaces aligned rather than allowing each consumer to reconstruct or normalize a different semantic representation.

The gate enforces four distinctions:

- plan DAG validation rejects non-canonical set metadata and invalid dependency, boundary, predicate, reachability, or typed-evaluation evidence, for every plan rather than only for plans that lower as composed blocks;
- `FingerprintSemanticPlan` normalizes every canonical set-like metadata field while preserving sequence-sensitive node and dependency ordering;
- semantic explain excludes optimizer-only annotations and therefore remains stable across semantic-preserving rewrites;
- structural plan fingerprinting may observe optimizer annotations such as `ShareGroup`, because the fingerprint is an internal regression identity for plan structure rather than the Agent-facing semantic meaning of a query.

RFC-0037 closed the IR boundary by making the plan the only DAG. Future semantic correctness work should add scenarios and result evidence without creating a second semantic planning representation.

## Redundant semantic work

The default optimizer removes only redundancy whose semantic equivalence is explicit.

`ProjectionDeduplicationRule` runs after metric-projection pruning and semantic-set normalization, before predicate, join, and source-node rewrites. It removes structurally identical projections while retaining the first occurrence, so user-visible projection order is stable. Because projection dataset sets are canonicalized before rewrites, set ordering or duplicate set members cannot prevent otherwise-identical projections from matching. A shared projection name alone is never sufficient for elimination.

Exact filter redundancy remains owned by `PredicateDeduplicationRule`, which retains the first occurrence and therefore stable parameter ordering. Neither projection nor filter deduplication may broaden predicates, merge merely similar expressions, or infer equivalence from dialect-specific behavior.

Both deduplication rules participate in bounded fixpoint execution. Once the working plan is normalized, another optimizer invocation must report no changed rewrite step and must preserve the semantic-plan fingerprint.

## Predicate placement

Predicate movement is constrained by semantic ownership.

For ordinary semantic composition, `PredicatePushdownRule` may move a top-level predicate to source aggregates only when its dataset is present in every source aggregate's `RequiredDatasets`. If any source cannot reach the predicate dataset, optimization fails closed rather than dropping the predicate or attaching it to invalid source work.

Advanced semantic nodes — cumulative windows, time offsets, offset-to-grain evaluation, conversions, and semi-additive evaluation — own predicate placement in Planner. The optimizer must not infer or invent source placement across those node boundaries. It may consume a top-level predicate only when Planner has already placed the same predicate on every relevant source aggregate. If that proof is missing, the top-level predicate remains unchanged.

This contract participates in the deterministic fixpoint. Exact duplicate predicates may be normalized first, but a normalized advanced semantic plan must preserve the same source placement and semantic-plan fingerprint when optimized again, with no changed rewrite steps.

## Semantic subplan reuse

Reusable source-aggregate work is identified by deterministic semantic source equivalence. The identity includes:

- root dataset reference;
- canonicalized source-root and required-dataset sets;
- joins;
- output grain;
- node-owned predicates.

Metric name, dependency wiring, and assigned `ShareGroup` are consumer or optimizer metadata and therefore do not participate in the identity.

`SourceScanFusionRule` groups equivalent source `SemanticPlanNode` values and assigns deterministic `source_%03d` share groups in first-seen reusable-group order. A difference in semantic source work, including output grain or predicate placement, must produce a distinct identity and must not be fused. Re-running source fusion on the same working graph must report no change once the same share groups are already present.

## Fingerprint contract

`FingerprintSemanticPlan` canonicalizes plan `Requested` and node `Metrics`, `Dimensions`, `SourceRoots`, and `RequiredDatasets` before hashing. Sequence-sensitive node and input ordering remains represented. The function operates on an owned clone and must not mutate caller state.

`FingerprintSemanticPlan` canonicalizes its declared plan-level set fields and excludes optimization trace state. Both fingerprints are internal regression identities for structure; neither is a cache key, authorization token, semantic API identifier, or externally stable identifier.

Because optimizer annotations are structural state, plan fingerprints may differ before and after a semantic-preserving optimization even though `SemanticPlanExplanation` must remain equal.

## Optimization boundaries

Semantic optimization may perform deterministic rule-based simplification and safe reuse. It must not introduce:

- statistics or cardinality estimation;
- cost-based optimization;
- join-order search;
- database execution;
- dialect-specific semantic branches.
