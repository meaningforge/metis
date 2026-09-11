# RFC-0027: Deterministic Semantic Optimizer

> **Design history:** This RFC preserves its original lifecycle status and rationale.
> Implementation claims describe the recorded design, not a guarantee that every
> proposed API remains current. See [current documentation](../../README.md) for
> supported interfaces. Package paths have been updated for this repository.

- **Status:** Implemented
- **Owners:** Metis maintainers
- **Created:** 2026-08-18
- **Last updated:** 2026-08-18
- **Scope:** rule-based semantic optimization determinism
- **Supersedes:** None

## Summary

Phase I5 deepens the semantic optimizer without introducing cost-based optimization. The first invariant is that every rewrite must observe a canonical semantic plan, independent of incidental resolver traversal order for fields whose contract is set-like.

Tracking: #262, #267.

## Motivation

Metis already has deterministic plan fingerprints and a `SemanticSetNormalizationRule`, but normalization currently lives inside the rewrite list. A custom optimizer can omit it, and a rule placed before it can observe non-canonical set ordering or duplicate set members.

That makes rewrite behavior depend on optimizer configuration rather than on the semantic-plan contract. Phase I5.1 promotes canonicalization to an optimizer precondition.

## I5.1 — canonicalization before rewrite — implemented

`Optimizer.Optimize` clones the input plan and canonicalizes semantic set fields before invoking the first configured rewrite rule.

The canonicalized fields are the same fields already covered by `SemanticSetNormalizationRule` and plan fingerprinting:

- `Projection.Datasets`;
- `MetricEvaluationNode.DirectDatasets`;
- `MetricEvaluationNode.RequiredDatasets`;
- `MetricEvaluationNode.SourceRoots`.

Canonicalization de-duplicates and lexicographically sorts these set-like fields. It does not reorder user-visible projections, dependency-first metric evaluation nodes, predicates, joins, grouping keys, or sorts where sequence order is semantically meaningful.

The existing `SemanticSetNormalizationRule` remains available for compatibility and explicit rule pipelines. In the default optimizer it becomes idempotent because the pre-rewrite boundary has already canonicalized the plan.

Canonicalization is deliberately not emitted as a rewrite trace entry in I5.1. `OptimizationTrace` remains a record of configured rewrite rules, while canonicalization is an invariant of entering the rewrite phase. Explainable per-pass rewrite evidence is deferred to later I5 work.

## Determinism contract

For semantically equivalent inputs that differ only in set ordering or duplicate set members:

- the first rewrite observes the same canonical set representation;
- equivalent source contexts compare structurally the same;
- rule outcomes do not depend on resolver traversal order;
- optimized semantic plan fingerprints are stable;
- optimizer input ownership is preserved and the caller's plan is not mutated.

## I5.2 — semantic subplan identity and reuse

### I5.2.1 — semantic source subplan identity — implemented

Reusable source-aggregate work has an explicit deterministic identity derived from:

- root dataset reference;
- canonicalized source-root and required-dataset sets;
- relationship joins;
- semantic output grain;
- pre-aggregation predicates.

Metric name, dependency wiring, and assigned `SourceStage` are excluded because they identify consumers or optimizer output rather than reusable source work.

### I5.2.2 — deterministic source-stage reuse — implemented

`SourceScanFusionRule` groups source aggregates by the semantic subplan identity from I5.2.1 instead of incidental full-structure equality. The optimizer preserves dependency-first evaluation order as the deterministic first-seen group order, assigns stable `source_%03d` stage names, and preserves metric order within each fused stage.

A semantic work difference such as a different output grain produces a different identity and therefore cannot be fused. Existing caller ownership remains intact because optimization occurs on a cloned plan and identity calculation owns its canonicalization inputs.

## I5.3 — deterministic rewrite ordering and fixpoint

### I5.3.1 — deterministic bounded fixpoint — implemented

`Optimizer.Optimize` executes configured rules in declaration order and repeats the same ordered pass until no rule reports a semantic-plan change. Pass execution is bounded to 16 productive attempts; a rule set that keeps reporting changes is rejected explicitly instead of looping indefinitely.

The first pass retains the historical complete rule trace. Later passes are added to `OptimizationTrace` only when they are productive, so the trace remains stable for plans that already converge in one pass while still preserving deterministic evidence for rewrites that require another pass. The final all-no-change convergence probe is intentionally omitted as non-actionable evidence.

Rules that participate in fixpoint execution must report `Changed` idempotently. `SourceScanFusionRule` therefore compares the rebuilt source-stage assignment against the prior working-plan assignment and reports a change only when stage evidence or node assignments actually differ.

### I5.3.2 — safe redundant projection/filter elimination — implemented

`ProjectionDeduplicationRule` removes only structurally identical projection work after the canonicalization boundary, retaining the first occurrence so user-visible projection order stays stable. Canonical-equivalent `Projection.Datasets` sets therefore compare identically, while same-name projections with different semantic fields remain distinct.

The default optimizer executes projection deduplication after metric-projection pruning and semantic-set normalization, before predicate, join, and source-stage rewrites. Exact filter redundancy remains owned by `PredicateDeduplicationRule`. Both rules participate in the I5.3.1 bounded fixpoint and must become no-ops once the same working plan is normalized.

Default-pipeline regression coverage requires the first productive pass to expose explicit rewrite evidence for both projection and predicate deduplication, while re-optimizing the normalized result produces the same semantic-plan fingerprint and no changed rewrite steps.

## I5.4 — predicate placement and differential closure — implemented

Predicate placement stays split by semantic ownership rather than by optimizer convenience.

For ordinary staged composition, `PredicatePushdownRule` may move a top-level predicate only when every source aggregate proves the predicate dataset is reachable through its `RequiredDatasets`. An unreachable predicate fails closed instead of being silently dropped or attached to an invalid source.

For advanced semantic stages — cumulative windows, time offsets, offset-to-grain, conversions, and semi-additive evaluation — Planner owns source placement. The optimizer may consume the corresponding top-level predicate only when the same predicate is already present on every source aggregate as `PreAggregationPredicates`. Without that proof the optimizer leaves the top-level predicate unchanged and never invents source placement across an advanced semantic boundary.

Differential closure verifies that this placement contract is stable under the default optimizer fixpoint: exact predicate redundancy is normalized, planner-owned source placement is preserved, a second optimization produces the same semantic-plan fingerprint, and caller-owned input remains unchanged.

## Sequencing

### I5.1 — pre-rewrite canonicalization — implemented

- make canonicalization an optimizer precondition;
- prove custom first rules see canonical set fields;
- prove equivalent set permutations produce stable rewrite/fingerprint results;
- preserve meaningful sequence order and input ownership.

### I5.2 — semantic subplan identity and reuse — implemented

- define semantic subplan identity independent of incidental plan shape;
- reuse equivalent safe subplans without changing semantic ownership;
- keep reuse deterministic and explainable.

### I5.3 — deterministic rewrite ordering and fixpoint — implemented

- define explicit pass ordering/fixpoint behavior — I5.3.1 implemented;
- eliminate redundant projections/filters only under semantic equivalence — I5.3.2 implemented;
- retain stable rewrite evidence.

### I5.4 — predicate placement and differential closure — implemented

- harden safe predicate placement across ordinary and advanced semantic stages;
- prove optimization preserves predicate ownership, fingerprints, and caller-owned input under differential/fixpoint coverage.

## Non-goals

Phase I5 does not introduce:

- statistics;
- cardinality estimation;
- cost models;
- cost-based plan selection;
- join-order search;
- database execution;
- dialect-specific semantic rewrite branches.

## Acceptance criteria for I5.1

I5.1 is complete when:

- every configured optimizer rule observes canonical set-like plan fields, including custom rule lists that omit `SemanticSetNormalizationRule`;
- canonicalization does not mutate the caller-owned input plan;
- equivalent set orderings and duplicate members produce equivalent rewrite results and identical semantic-plan fingerprints;
- existing default optimizer rule traces remain stable;
- meaningful sequence ordering is not normalized away;
- existing correctness, E2E, and real-engine gates remain green.

## Acceptance criteria for I5.2

I5.2 is complete when:

- reusable source-aggregate work has a deterministic semantic identity independent of metric consumer metadata;
- source-stage fusion uses that identity as its grouping contract;
- equivalent canonical source work fuses even when consumer metadata differs;
- semantic work differences do not fuse;
- source-stage names and metric evidence are deterministic from evaluation order;
- optimizer input ownership remains preserved;
- no CBO, statistics, cardinality estimation, cost model, join-order search, database execution, or dialect-specific semantic rewrite branch is introduced;
- existing correctness, E2E, and applicable real-engine gates remain green.

## Acceptance criteria for I5.3.1

I5.3.1 is complete when:

- configured rules repeat in stable declaration order until a no-change pass is reached;
- a second productive pass can consume work exposed by a later rule in the preceding pass;
- non-converging rule sets fail after a deterministic bounded number of passes;
- `SourceScanFusionRule` is idempotent under repeated execution;
- the historical single-pass trace remains unchanged for plans that need no additional productive pass;
- caller-owned input remains unchanged;
- no CBO, statistics, cardinality estimation, cost model, join-order search, database execution, or dialect-specific semantic rewrite branch is introduced;
- existing correctness, E2E, and applicable real-engine gates remain green.

## Acceptance criteria for I5.3.2

I5.3.2 is complete when:

- structurally identical projections are eliminated in the default optimizer without reordering surviving projections;
- canonical-equivalent projection dataset sets are treated as the same semantic work;
- same-name projections with different semantics remain distinct;
- exact duplicate filters are eliminated by the existing predicate rule;
- projection and filter deduplication are idempotent under bounded fixpoint execution;
- re-optimizing an already-normalized plan produces the same semantic-plan fingerprint and no changed rewrite steps;
- caller-owned input remains unchanged;
- no CBO, statistics, cardinality estimation, cost model, join-order search, database execution, or dialect-specific semantic rewrite branch is introduced;
- existing correctness, E2E, and applicable real-engine gates remain green.

## Acceptance criteria for I5.4

I5.4 is complete when:

- ordinary staged predicate pushdown is allowed only for predicates reachable from every source aggregate and fails closed otherwise;
- cumulative, time-offset, offset-to-grain, conversion, and semi-additive stages never receive optimizer-invented source predicate placement;
- the optimizer consumes an advanced-stage top-level predicate only after Planner-owned source placement is explicitly present on every source aggregate;
- advanced predicate placement remains unchanged across default optimizer fixpoint execution;
- a normalized advanced plan produces the same semantic-plan fingerprint when optimized again and reports no changed rewrite steps;
- caller-owned plans remain unchanged on both successful and failed placement paths;
- no CBO, statistics, cardinality estimation, cost model, join-order search, database execution, or dialect-specific semantic rewrite branch is introduced;
- existing correctness, E2E, and applicable real-engine gates remain green.
