# RFC-0029: Semantic Logical Stage Model

> **Design history:** This RFC preserves its original lifecycle status and rationale.
> Implementation claims describe the recorded design, not a guarantee that every
> proposed API remains current. See [current documentation](../../README.md) for
> supported interfaces. Package paths have been updated for this repository.

- **Status:** Implemented
- **Owners:** Metis maintainers
- **Created:** 2026-08-18
- **Last updated:** 2026-08-20
- **Scope:** Phase J semantic logical-stage model
- **Supersedes:** None

## Context

Phase I proved deep semantic correctness across multi-metric shared grain, deterministic semantic rewrites, advanced metrics, and semantic-critical extension lowering. The remaining architectural risk was composition: those capabilities were represented by multiple planner-specific structures and feature-owned lowering paths.

Phase J introduced one engine-neutral logical-stage contract and then progressively made that contract authoritative for staged composition, validation, explainability, and SQL AST lowering.

## Permanent architecture boundary

Metis owns semantic resolution, semantic validation, deterministic logical planning, semantic-preserving rewrites, lineage/evidence, and target-aware lowering.

Metis will not implement cost-based optimization, statistics collection, cardinality estimation, cost models, physical join-order search, access-path selection, physical runtime strategy, or database execution. Those responsibilities belong to the selected backend engine.

A semantic rewrite is allowed only when correctness is independent of runtime cost.

## Canonical stage contract

`SemanticPlan` is the canonical top-level semantic planning IR. `SemanticStageGraph` is its owned, stage-oriented internal representation for staged semantic composition. It is not a second top-level IR and it is no longer merely a compatibility view used only for inspection.

A stage records:

- stable stage identity and semantic evaluation kind;
- dependency edges with each input stage's output grain;
- output metric/dimension identity and output grain;
- source roots, required datasets, root source and relationship joins;
- explicit predicate ownership and typed placement proof;
- shared-grain relationship/fanout evidence when available;
- typed semantic-critical extension evidence;
- source-work sharing membership when sharing has already been proven safe.

`SemanticStagePlanner` materializes and validates the graph from `SemanticPlan`. The graph owns all exposed slices, preserves semantic dependency order, and fails closed when stage identity, dependency grain, predicate ownership, or composition evidence is inconsistent.

`MetricEvaluationPlan` remains a transitional construction/evaluation payload while existing metric-specific expression and extension data is migrated. It is not a permanent peer IR and must not be treated as the source of truth for stage order, dependency edges, output grain, source requirements, joins, predicate ownership, or source-sharing structure.

## Predicate contract

The complete predicate scopes are `source_read`, `pre_aggregation`, `stage_local`, `post_aggregation`, and `final_output`.

Stage-owned predicates identify an exact owner stage and carry typed placement proof. Final-output predicates are plan-level output semantics and cannot claim a stage owner. Movement toward inputs is allowed only when the stage boundary explicitly permits it with proof; aggregate-composition and semantic-isolation boundaries block inferred downward movement.

## SQL AST lowering

Staged SQL AST materialization consumes the validated `SemanticStageGraph` before metric-specific lowering occurs.

The lowering boundary reconstructs stage order, dependency edges, output grain, source roots and required datasets, joins, predicate ownership, source-sharing groups, and final predicates from canonical stage evidence. Transitional `MetricEvaluationPlan` payloads are used only for metric-specific expression or extension details that have not yet moved into plan-owned structures.

If a transitional payload disagrees with canonical stage identity or evaluation kind, lowering fails closed. Feature-specific transforms remain behind the single staged lowering boundary and do not become top-level alternative planning paths.

Dialect renderers remain responsible only for target syntax and capability translation. The logical-stage model never chooses physical join order, runtime distribution, materialization policy, access paths, or execution strategy.

## Ownership and determinism

Dependency order is semantically meaningful and remains stable. Set-like source requirements are canonicalized where their contract permits it. Stage boundaries, predicate evidence, and sharing evidence participate in deterministic semantic fingerprints and explain output.

The graph is an internal representation of `SemanticPlan`; callers should consume plan-owned APIs rather than constructing a parallel planning path around the graph.

## Non-goals

The logical-stage model does not choose CTE materialization, join algorithms, join order, broadcast/shuffle strategy, indexes, partitions, parallelism, or any other physical execution strategy.
