# RFC-0020: Semantic Plan Quality Foundations

> **Design history:** This RFC preserves its original lifecycle status and rationale.
> Implementation claims describe the recorded design, not a guarantee that every
> proposed API remains current. See [current documentation](../../README.md) for
> supported interfaces. Package paths have been updated for this repository.

- **Status:** Implemented
- **Owners:** Metis maintainers
- **Created:** 2026-08-17
- **Last updated:** 2026-08-17
- **Scope:** `planner`, semantic optimizer correctness, plan-quality conformance
- **Supersedes:** None

## Summary

Metis will make semantic query-shape quality measurable and regression-testable while keeping cost-based physical query optimization permanently outside Metis Core.

The current semantic optimizer is deterministic and rule based. It can prove semantics-preserving reductions such as pruning, deduplication, predicate pushdown, unused-join elimination, and source-scan fusion. That is the intended long-term optimizer boundary for Metis: optimize semantic query shape where equivalence can be proven from typed semantic state, then delegate physical cost-based optimization to the target database.

Phase G therefore establishes **structural plan-quality foundations**, not foundations for a future cost-based optimizer. Metis will define a canonical structural summary and fingerprint for `SemanticPlan`, prove that default optimizer rewrites do not regress that structural quality contract, and add a representative plan-shape regression corpus.

## Architectural boundary

Metis is a semantic compiler, not a database optimizer.

Metis owns:

- semantic resolution;
- relationship correctness and required-path planning;
- typed semantic query shape;
- semantics-preserving structural rewrites whose equivalence can be proven without runtime statistics;
- target-neutral plan validation and structural regression evidence.

The target database owns:

- cardinality and selectivity estimation;
- statistics-driven join ordering;
- physical operator selection;
- index/access-path selection;
- distribution, exchange, and locality planning;
- runtime adaptive optimization;
- engine-specific execution-cost decisions.

This separation is a durable architecture rule rather than a temporary Phase G limitation.

## Motivation

Semantic correctness is already protected by planner invariants and optimized-versus-unoptimized differential execution. What is still missing is a stable way to answer engineering questions such as:

- did a planner change unexpectedly add another source scan or staged evaluation;
- did a rewrite reduce joins while accidentally increasing another expensive semantic stage;
- did two equivalent requests produce the same canonical plan shape;
- did a new advanced metric feature cause query-shape explosion even though results remain correct.

Rendered SQL is a poor regression boundary because dialect lowering may legitimately change syntax while preserving the same semantic plan. Database runtime cost is intentionally not modeled by Metis Core because doing so would duplicate responsibilities of the target optimizer and couple semantic planning to engine statistics.

Phase G adds a typed structural quality boundary without introducing a cost model.

## Structural plan summary

Metis SHOULD derive a deterministic `PlanShapeSummary` from a validated `SemanticPlan`.

The summary is structural evidence, not a runtime cost estimate. It may include stable counts such as:

```text
root datasets
joins
predicates
projections
groups
sorts
metric evaluation nodes
source aggregate nodes
source stages
advanced evaluation stages
calendar/densification stages
```

The exact public Go representation is implementation-defined during Phase G, but its fields MUST describe typed plan structure rather than rendered SQL text.

Summary generation MUST be deterministic for the same validated plan and MUST NOT mutate the plan.

## Canonical plan fingerprint

Metis SHOULD derive a canonical fingerprint from semantic-plan structure after removing incidental runtime-only state such as optimizer trace entries.

The fingerprint is intended for repository regression tests and diagnostics. It is not a semantic identity, cache key, authorization token, or Agent-facing compatibility promise.

Two plans that differ only in map iteration order or equivalent set ordering MUST NOT produce different fingerprints after canonicalization. User-authored observable ordering MUST remain represented where it is semantically relevant.

## Structural quality ordering

Phase G MAY define a deterministic structural quality vector for comparing a plan before and after a semantics-preserving optimizer pass.

The vector MUST NOT claim to estimate database execution time. It SHOULD prefer reductions in duplicated semantic work, including source scans/stages, staged evaluation nodes, and unnecessary joins, while preserving correctness and observable ordering.

A default optimizer rewrite MUST NOT be accepted merely because one count decreases if it introduces an unproven semantic transformation. RFC-0017 invariant validation remains authoritative before and after optimization.

If structural fields are incomparable for a rule, correctness wins: Metis retains the semantically proven plan rather than forcing a lower structural score.

## Relationship to database optimizers

Metis MUST NOT implement a statistics-driven cost-based optimizer.

The following remain outside Metis Core by design:

- statistics-driven join reordering;
- cardinality estimation;
- selectivity estimation;
- physical operator choice;
- index/access-path selection;
- distribution or exchange planning;
- runtime adaptive optimization;
- engine-specific cost formulas.

Metis MAY carry semantic facts needed to generate correct SQL, but those facts MUST NOT evolve into a shadow physical optimizer. Any target-specific execution optimization should be expressed through supported SQL/engine capabilities and left for the database optimizer to evaluate.

## Regression corpus

Phase G will add representative structural regression cases covering simple metrics and advanced staged semantics. Cases SHOULD freeze semantic-plan shape at a stable typed boundary rather than snapshot full rendered SQL.

The corpus MUST coexist with existing correctness gates:

- semantic-plan invariant validation;
- optimized versus unoptimized structural checks;
- canonical semantic conformance;
- Doris and ClickHouse differential execution where relevant.

A structural regression is not automatically a semantic regression, but it MUST be intentional and reviewable rather than accidental.

## Agent-facing boundary

`PlanShapeSummary`, fingerprints, and optimizer structural quality evidence are internal/compiler engineering contracts in Phase G.

They MUST NOT be added to `SemanticQuery`, Ossie model identity, or required Agent-facing `QueryExplanation`. Existing Agent explainability remains semantic rather than optimizer-internal.

## Non-goals

Phase G and future Metis Core development do not add:

- database execution to Metis Core;
- row-count/NDV/selectivity statistics to Ossie core schema for optimizer decisions;
- a cost-model statistics provider or statistics persistence service;
- cost-based relationship-path selection;
- physical join reordering based on estimated cost;
- silent resolution of ambiguous relationships;
- target-specific semantic planning;
- SQL-text snapshotting as the primary optimizer regression contract;
- new SQL dialects solely for optimizer work.

## Acceptance

RFC-0020 is complete when:

- validated semantic plans have a deterministic structural summary;
- canonical plan fingerprints are available for regression tests;
- default optimizer behavior has executable structural non-regression checks in addition to semantic invariants;
- representative advanced-semantic plan shapes are covered by a stable regression corpus;
- optimized/unoptimized real-engine differential correctness remains green;
- current semantic pipeline specifications describe the structural-quality boundary;
- repository architecture documentation makes cost-based physical optimization permanently a target-database responsibility.
