# ADR-0012: Metric Attribution Is a Typed Runtime Workflow

> **Decision record:** Read alongside the [current documentation](../../README.md);
> superseded decisions are retained for their rationale.

**Status:** Accepted (current)
**Date:** 2026-09-01
**Supersedes:** ADR-0008's compile-only public-boundary decision

## Context

RFC-0052 established deterministic independent additive and ratio evidence
queries. RFC-0053 showed that asking an Agent to execute, align, and reconcile
that bundle was materially less reliable than one governed capability. The
bounded Execution Runtime now supplies exact Backend/Renderer placement,
normalization, cancellation, and deployment limits.

## Decision

Metis exposes `attribute_metric` as one closed analytical runtime workflow.
`AttributeMetricService` resolves Project/DataSource/Backend once, resolves
each decomposition dimension independently, compiles the canonical RFC-0052
bundle with the exact Backend Renderer, executes entries sequentially under one
timeout and cumulative row/byte budgets, and returns a result only after every
entry succeeds.

`analytics/attribution` consumes complete normalized schema/row evidence. It
accepts Integer/Decimal numerical evidence, uses arbitrary-precision rational
arithmetic, verifies additive reconciliation exactly and ratio reconciliation
within `1e-8`, and returns the closed typed `AttributeMetricResult`. SQL,
parameters, DataSource identity, and raw bundle results remain internal.

Independent dimensions are never combined or ranked. Undefined ratios remain
explicit typed evidence. Metis does not select investigations, infer causality,
or expose arbitrary SQL. Compile-only deployment and `compile_sql` remain
supported; Projects without executable DataSources fail explicitly.

## Consequences

- REST `POST /v1/attribute-metric` and MCP `attribute_metric` share one service.
- `semantic:execute` is required.
- Any compile, execution, limit, normalization, or evaluation failure returns
  no partial result.
- `UNSUPPORTED_METRIC_ATTRIBUTION` is caller-repairable;
  `INCONSISTENT_ATTRIBUTION_RESULT` is a reportable invariant violation.
- The RFC-0052 compiled bundle remains an internal compiler boundary.
- `query_metrics` remains the only generic execution capability.

## References

- RFC-0057, *Production Metric Attribution Runtime*
- RFC-0052, *Deterministic Metric Change Attribution*
- ADR-0008, *Metric Attribution Is Compile-Only Independent Evidence*
- ADR-0010, *Project DataSource and Backend Define Runtime Placement*
