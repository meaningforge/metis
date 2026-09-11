# ADR-0013: Analytical Workflows Remain Vertical-First

> **Decision record:** Read alongside the [current documentation](../../README.md);
> superseded decisions are retained for their rationale.

**Status:** Accepted (current)
**Date:** 2026-09-01

## Context

RFC-0054 required at least two real analytical workflows to reuse one Execution
Runtime before Metis considered generalized analytical primitives.
`attribute_metric` became the first closed vertical. RFC-0064 adds
`compare_metrics`: a distinct multi-metric, shared-grain, two-period workflow.

The two workflows expose some superficial repetition—request orchestration,
bounded sequential execution, and deterministic evaluation—but have different
semantic proofs, query shapes, result contracts, failure invariants, and
mathematics. Generalizing from one workflow would have been speculative;
generalizing automatically from two would still make reuse, rather than a
stable semantic contract, the authority.

## Decision

Metis implements analytical capabilities as explicit typed verticals unless a
later RFC proves a stable shared abstraction from concrete compatibility and
migration evidence.

`compare_metrics` compiles exactly two ordinary SemanticQuery values with one
Backend-selected Renderer, executes their completed artifacts through the same
Runner used by `query_metrics` and `attribute_metric`, and delegates only
normalized evidence alignment and arithmetic to `analytics/comparison`.

Metis does not introduce `AnalysisPlan`, a generic analytical bundle, a generic
evaluator interface, or an arbitrary analytical executor. Execution Runtime
continues to consume only atomic `CompiledQuery` values. `query_metrics` remains
the only generic public execution capability.

## Consequences

- RFC-0054's two-workflow gate is satisfied by `attribute_metric` and
  `compare_metrics`.
- A future shared analytical primitive requires its own compatibility decision;
  two implementations are evidence to study, not permission to abstract.
- REST and MCP expose the same `CompareMetricsService` behavior.
- Any compile, execution, budget, normalization, or evaluation failure returns
  no partial comparison result.
- Absence, null, and zero remain explicit comparison states.
- New warehouses extend Renderer/Backend/Driver boundaries and do not change
  comparison arithmetic.

## References

- RFC-0054, *Execution Runtime and Analytics Runtime Architecture*
- RFC-0057, *Production Metric Attribution Runtime*
- RFC-0064, *Production Metric Comparison Runtime*
- ADR-0010, *Renderer and DataSource Type Are the Sole Compile/Runtime Authorities*
- ADR-0012, *Metric Attribution Is a Typed Runtime Workflow*
