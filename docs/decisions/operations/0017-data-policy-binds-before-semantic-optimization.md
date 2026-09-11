# ADR-0017: Data policy binds before semantic optimization

> **Decision record:** Read alongside the [current documentation](../../README.md);
> superseded decisions are retained for their rationale.

- **Status:** Proposed
- **Date:** 2026-09-09

## Decision

Application services own Principal-aware policy evaluation over a canonical
whole-operation workload. They reuse the same prepared metric evaluation
plans, bind one immutable decision, and install typed relation-local constraints
before SemanticPlan optimization. Core packages never receive Principal or
policy adapters. Multi-query workflows preflight all artifacts before execution.

SQLPlan adds a `FilteredTableSource` relation alternative to its stable Renderer
type closure. Renderers must filter that input before joining it, or reject
the form. This is an explicit additive SPI evolution: extension authors must
handle the new alternative, not assume every non-query input is TableSource.
Unrestricted plans and existing conformance fingerprints remain unchanged.

## Consequences

Per-query or renderer-side policy evaluation is rejected because it permits
mixed snapshots and changes authorization authority. Final WHERE injection is
rejected because it changes outer-join semantics. Runner remains artifact-only.
The explicit constant compatibility adapter preserves unrestricted deployments;
nil and configured-policy failures never fall back to it. Compile permission
includes disclosure of policy-bearing physical SQL and parameters.

The policy adapter is experimental embedder API. The current normative behavior
and V1 limitations are in [data policy](../../specs/operations/data-access-policy.md).
