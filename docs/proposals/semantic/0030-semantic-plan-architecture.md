# RFC-0030: SemanticPlan Architecture

> **Design history:** This RFC preserves its original lifecycle status and rationale.
> Implementation claims describe the recorded design, not a guarantee that every
> proposed API remains current. See [current documentation](../../README.md) for
> supported interfaces. Package paths have been updated for this repository.

- **Status:** Implemented
- **Owners:** Metis maintainers
- **Created:** 2026-08-18
- **Last updated:** 2026-08-20
- **Scope:** Phase J semantic composition, predicate ownership, lineage, explainability, and lowering convergence
- **Supersedes:** None

## Later architecture

This proposal records the Phase J architecture that existed when it was
implemented. RFC-0037 subsequently made the `SemanticPlan` DAG universal,
RFC-0040 replaced stage envelopes with concrete typed `SemanticPlanNode`
values, and RFC-0039 established `MetricEvaluationPlan` as a permanent,
query-scoped metric-only authority before source-aware semantic lowering.

Accordingly, the stage-graph and "transitional MetricEvaluationPlan" language
below is migration history, not current truth. Current behavior is defined by
the semantic compilation specification and pipeline design.

## Context

RFC-0029 introduced `SemanticStageGraph` as an owned, engine-neutral representation of staged semantic evaluation. Phase J uses that graph as the stage-oriented internal representation of Metis's `SemanticPlan` rather than promoting the graph itself into a second top-level planning IR.

Metis is still early enough to avoid carrying parallel long-lived planning IRs. `SemanticPlan` is the canonical logical semantic planning result. `MetricEvaluationPlan` remains a transitional metric-centric construction/evaluation payload while remaining metric-specific expression and extension data migrates into plan-owned structures; it is not a co-equal long-term planner IR and must not become the home for new planning features.

## Decision

`SemanticPlan` is the canonical semantic planning IR of Metis. `SemanticStageGraph` is its ordered internal graph representation for staged semantic composition, validation, optimization evidence, lineage, explainability, and staged SQL AST lowering. `SemanticStagePlanner` materializes and validates that graph from the canonical plan.

The stage graph preserves resolved metric dependency order and proven semantic evidence, then validates the following invariants before it is accepted for downstream use:

1. stage IDs are unique and dependency stages precede consumers;
2. each dependency edge records exactly the producer stage's output grain;
3. post-aggregate derived stages consume inputs at their own output grain;
4. join-after-aggregate composition consumes compatible aggregate-grain inputs;
5. cross-aggregate composition is valid only for scalar aggregate inputs and scalar output;
6. shared-grain evidence remains fanout-safe and agrees with the stage output grain;
7. every predicate has one explicit semantic scope, one owner model, and typed placement proof;
8. predicate movement across a stage boundary is allowed only when boundary evidence explicitly permits it;
9. Agent-facing lineage and explain evidence is derived from `SemanticPlan` and its stage graph rather than reconstructed from transitional metric-evaluation inputs;
10. staged SQL AST lowering consumes canonical stage structure before metric-specific expression/extension payloads.

These rules encode semantic necessity only. They do not estimate or compare execution cost.

## Boundary classification

Each stage exposes a correctness boundary:

- `source_aggregate` for source aggregation;
- `post_aggregate` for compatible-grain derived computation;
- `aggregate_before_composition` when aggregate results must exist before composition;
- `semantic_isolation` for advanced metric semantics that require a dedicated evaluation boundary.

Boundary classification is deterministic from semantic evaluation kind.

## Predicate placement contract

Predicate ownership is represented independently from predicate movement.

`SemanticPredicateScope` distinguishes `source_read`, `pre_aggregation`, `stage_local`, `post_aggregation`, and `final_output`. Stage predicates identify their exact owner stage and carry a typed placement proof. Final-output predicates are plan-level output semantics represented on the stage graph and cannot claim a stage owner.

Each stage exposes `SemanticPredicateBoundaryEvidence` for downward movement toward its inputs:

- source aggregation allows movement only with dataset-reachability proof;
- post-aggregate, aggregate-composition, and semantic-isolation boundaries block inferred downward movement.

The semantic optimizer consumes this contract instead of deciding from evaluation-kind heuristics alone. Unknown scopes, missing owners, missing proofs, ambiguous predicate representations, and inconsistent boundary evidence fail closed.

## Lineage and explainability

`SemanticPlan` owns Agent-facing semantic explainability. `ExplainSemanticStageGraph` is the internal stage projection used to expose deterministic, bounded stage evidence without creating another planning path.

The explanation contains:

- stage dependencies, stage boundary and output grain;
- metric and dimension lineage through dependency stages to source datasets;
- relationship-path and fanout-safety evidence already proven by planning;
- predicate ownership, placement proof and boundary evidence;
- typed extension evidence without opaque source payloads;
- declarative metric constraints carried by the canonical plan, such as fill and metric-definition filters.

REST and MCP explain paths consume this same `SemanticPlan` evidence and its stage projection. They must not walk `MetricEvaluationPlan` directly or invent an independent semantic planning path. Backend `EXPLAIN`, runtime operators, statistics and execution metadata are outside this contract.

## Sharing

Existing source-stage fusion remains the migration source for already-proven reusable source work. Phase J exposes proven membership through `SemanticStage.ShareGroup`; staged lowering reconstructs source-sharing groups from that canonical stage evidence instead of trusting stale transitional lowering metadata.

A share group means only that semantic source work was proven reusable. It does not require the backend to materialize a CTE, choose a cache, or execute the work once.

## Deterministic fingerprints

`FingerprintSemanticStageGraph` is the regression and equivalence fingerprint for the stage-oriented semantic composition structure inside `SemanticPlan`. Set-like source roots and required datasets are canonicalized, while stage and dependency order remain meaningful. Predicate ownership, placement proof, and boundary evidence are included so a semantically meaningful placement change changes the fingerprint.

The fingerprint is not a physical-plan identity and must never become a cost or execution decision input.

## SQL AST lowering boundary

J6 converges staged SQL AST lowering on canonical `SemanticPlan` stage structure.

`BuildSQLAST` routes staged plans through one staged lowering boundary. That boundary materializes and validates the `SemanticStageGraph`, then reconstructs stage order, dependency edges, output grain, source roots and required datasets, joins, predicate ownership, source-sharing groups, and final predicates from canonical stage evidence before invoking metric-specific SQL AST materialization.

`MetricEvaluationPlan` is retained only as a transitional carrier for metric-specific expression and semantic-extension payloads that existing lowerers still consume. It is not authoritative for staged composition structure. If canonical stage identity or evaluation kind disagrees with transitional payload metadata, lowering fails closed.

Feature-specific transforms such as cumulative, offset-to-grain, dense-calendar, custom-calendar, conversion, and semi-additive handling remain implementation details behind this single staged lowering boundary. They must not reintroduce independent top-level semantic planning paths.

Physical dialect renderers remain responsible only for target syntax and capability translation. No cost, statistics, cardinality, physical join ordering, access-path selection, runtime strategy, materialization policy, or database execution is introduced by this migration.

## Migration

`MetricEvaluationPlan` is a transitional construction/evaluation representation used to preserve working metric-specific payloads while consumers move onto canonical `SemanticPlan` structures. It is not a permanent second IR.

Migration rules are explicit:

- new semantic planning capabilities land on `SemanticPlan` and, when stage-oriented, its `SemanticStageGraph`;
- optimizer decisions consume plan-owned stage invariants and evidence;
- Agent-facing explain/lineage consumes only canonical plan evidence;
- composition conformance is expressed through shared semantic-plan/result contracts;
- staged SQL AST lowering consumes canonical stage structure and uses transitional metric-evaluation payloads only for metric-specific expression/extension details not yet migrated;
- once plan construction and lowering no longer require those transitional payloads, `MetricEvaluationPlan` may be removed or reduced without compatibility promises.

The migration should prefer direct cleanup over preserving unused abstractions because Metis has no released customer contract requiring a dual-IR architecture.

## Permanent non-goals

Metis does not collect statistics, estimate cardinality, model cost, search physical join order, select access paths, select runtime distribution, choose materialization policy, or execute queries. Backend engines own those responsibilities.
