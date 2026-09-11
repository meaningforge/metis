# ADR-0008: Metric Attribution Is Compile-Only Independent Evidence

> **Decision record:** Read alongside the [current documentation](../../README.md);
> superseded decisions are retained for their rationale.

**Status:** Superseded by ADR-0012
**Date:** 2026-08-28
**Last reviewed:** 2026-08-28

## Context

The independent-evidence and no-cross-dimension-ranking decisions below remain
historical foundations. ADR-0012 supersedes only this ADR's compile-only public
boundary after RFC-0057 added bounded typed runtime evaluation.

Agents can investigate metric changes by issuing ordinary semantic queries and
performing alignment and decomposition themselves. That approach can drift in
periods, filters, population, fill behavior, ratio algebra, or reconciliation.
Moving the whole investigation into Metis, however, would also risk making the
semantic runtime own Agent reasoning, causal interpretation, or database
execution.

A request may investigate several dimensions. Combining those dimensions into
one attribution grain or ranking their effects together would change the
meaning of each decomposition and suggest unsupported cross-dimension
comparability.

## Decision

Metis owns deterministic compilation of metric change-attribution evidence for
algebra it can prove from the existing typed metric evaluation contract.
Supported strategies are governed additive decomposition and explicit ratios
whose ordered numerator and denominator operands are both additive over the
same population. Unsupported algebra fails closed.

Every requested dimension lowers into its own typed `SemanticPlan`, `SQLPlan`,
physical query, output schema, and reconciliation. A bounded bundle validates
shared metric, exact periods, filters, model, target, and decomposition proof,
and canonicalizes query order. It does not create a combined grain or
cross-dimension ranking.

Attribution remains compile-only. Metis does not execute the physical queries,
fetch results, infer business causality, or add an Agent-facing REST or MCP
operation under this decision. Any public interface requires a separate RFC
informed by Agent evaluation.

## Consequences

- `MetricEvaluationPlan` remains metric-dependency authority;
  `MetricAttributionPlan` adds only decomposition authority.
- `AdditiveAttributionNode` and `RatioAttributionNode` remain ordinary concrete
  nodes in the one canonical semantic DAG architecture.
- Exact period and shared filter semantics are repeated consistently in each
  independent query; reconciliation occurs before result ordering.
- Missing population values use only governed identity fill, while present zero
  denominators remain explicit undefined evidence.
- SQLPlan carries typed formulas and renderers remain syntax-only.
- Multi-dimension work grows linearly and is capped at sixteen queries.
- DuckDB, ClickHouse, and Doris conformance executes planted evidence outside
  the Metis runtime boundary.

## Alternatives considered

**Let Agents perform all decomposition.** This preserves flexibility but cannot
guarantee aligned populations, filters, fill semantics, or reconciliation.

**Compile one SQL query grouped by all requested dimensions.** This changes the
grain and no longer answers each independent attribution question.

**Rank contributions across dimensions.** Effects from overlapping population
partitions are not directly additive or comparable and would imply unsupported
causal semantics.

**Add an attribution MCP tool immediately.** The stable semantic primitive and
its execution evidence must precede an interface decision and Agent benchmark.

## References

- RFC-0052, *Deterministic Metric Change Attribution*
  (`docs/proposals/semantic/0052-deterministic-metric-change-attribution.md`)
- [`../../specs/semantic/metric-change-attribution.md`](../../specs/semantic/metric-change-attribution.md)
- [`0004-semantic-plan-is-the-dag.md`](0004-semantic-plan-is-the-dag.md)
- [`0006-metric-evaluation-plan-is-query-scoped-metric-authority.md`](0006-metric-evaluation-plan-is-query-scoped-metric-authority.md)
- [`../sql/0005-sqlplan-is-the-physical-renderer-boundary.md`](../sql/0005-sqlplan-is-the-physical-renderer-boundary.md)
