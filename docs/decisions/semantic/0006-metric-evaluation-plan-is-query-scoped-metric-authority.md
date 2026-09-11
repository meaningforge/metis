# ADR-0006: MetricEvaluationPlan Is the Query-Scoped Metric Authority

> **Decision record:** Read alongside the [current documentation](../../README.md);
> superseded decisions are retained for their rationale.

**Status:** Accepted (current)
**Date:** 2026-08-24
**Last reviewed:** 2026-08-24

## Context

Resolver already computed a query's metric dependency closure, selected
target-compatible expressions, and attached canonical time bindings. Semantic
plan construction then used that resolver collection together with catalog
dependency state to construct source-aware work.

Without a materialized authority boundary, multiple construction helpers could
rebuild the closure, classify metric kinds from Ossie extensions again, or infer
metric dependencies while also deciding dataset roots, relationship paths,
grain, predicate placement, and semantic node shape. The resulting answers were
usually equal, but equality depended on parallel implementations continuing to
drift in lockstep.

`SemanticPlan` cannot replace this query-scoped metric contract. It owns
source-aware computation and may map a stable derived metric to different
concrete node shapes or share compatible source work across several metrics.
Metric identity cardinality and semantic/physical execution-unit cardinality
are deliberately different.

## Decision

Every metric-bearing query builds exactly one validated
`MetricEvaluationPlan` after resolution and before source-aware semantic
planning. Metric-free queries do not fabricate one and directly construct a
typed `SourceSelectionNode`.

`MetricEvaluationPlan` is the sole query-scoped authority for:

- metric root obligations and their `output`, `predicate`, or `order` roles;
- exactly one `MetricEvaluationNode` identity per required metric;
- metric-to-metric `MetricEvaluationInput` edges and deterministic topological
  order;
- stable metric evaluation kinds and exactly one matching typed spec;
- resolved metric expressions, canonical time bindings, and evaluation-local
  semantic evidence.

The plan deliberately does not own dataset roots, relationship paths, joins,
source/read predicate placement, output grain, concrete `SemanticPlanNode`
shape, source-sharing groups, SQL blocks, CTEs, or runtime strategy.

`ResolvedSemanticQuery.EvaluationMetrics` may be consumed as a collection only
by `BuildMetricEvaluationPlan`. A named, fail-closed lowering pass transfers the
validated metric authority into typed source-aware `SemanticPlan.Nodes`.
Downstream semantic optimization, explanation, fingerprinting, and SQLPlan
lowering consume `SemanticPlan`, not the metric plan.

Source-aware lowering may still consult catalog dataset, field, relationship,
and reachability facts. It may not reopen catalog metric edges or Ossie metric
extensions to derive a competing dependency or kind answer.

## Consequences

Metric dependency and kind errors fail at one bounded validation boundary.
Clone, explanation, and fingerprint contracts make the query-scoped metric
meaning deterministic and independently testable without claiming source or
physical decisions.

A `MetricEvaluationNode` is never treated as a scan, aggregation, semantic
node, SQL block, or CTE. Several metric identities may share compatible
semantic source/aggregation work, and shared work may expose reusable outputs
without duplicating or merging metric identities.

The architecture gains one intentional internal IR and one explicit lowering
pass. Repository guards are required to prevent that bounded plan from becoming
a second downstream semantic authority.

Future temporal, relationship, dimension, or other semantic evaluation domains
must justify their own authority, validation, evidence, and lowering contracts.
They are not added to a generic `SemanticEvaluationPlan` merely to reuse a name.

This decision complements ADR-0004 (`SemanticPlan` is the source-aware semantic
DAG) and ADR-0005 (`SQLPlan` is the physical renderer boundary).

## Alternatives considered

**Keep metric evaluation implicit in SemanticPlan construction.** This avoids
one type but preserves multiple places that can rediscover metric dependency or
kind and makes the authority transfer untestable.

**Store query-scoped metric state inside SemanticPlan.** This creates two
co-equal semantic authorities after lowering and gives optimizer/SQL lowering a
fallback path to stale construction state.

**Use a generic SemanticEvaluationPlan.** The implemented independent contract
is metric-specific. Generalizing around hypothetical domains weakens ownership
and makes unrelated future semantics share a catch-all vocabulary.

**Shape metric evaluation nodes like execution units.** Multi-metric nodes or
one-node-per-CTE rules conflate semantic identity with sharing and physical
packing. SemanticPlan and SQLPlan already own those later decisions.
