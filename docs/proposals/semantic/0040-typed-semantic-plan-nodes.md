# RFC-0040: Typed SemanticPlan Nodes

> **Design history:** This RFC preserves its original lifecycle status and rationale.
> Implementation claims describe the recorded design, not a guarantee that every
> proposed API remains current. See [current documentation](../../README.md) for
> supported interfaces. Package paths have been updated for this repository.

- **Status:** Implemented
- **Owners:** Metis maintainers
- **Created:** 2026-08-23
- **Last updated:** 2026-08-25
- **Scope:** `planner/` SemanticPlan node representation, validation, cloning, explain/fingerprint projection, optimizer consumers, and SemanticPlan -> SQLPlan lowering
- **Supersedes:** the earlier RFC-0040 `SemanticStage + SemanticStageOperation` final model, and the stage-local `SemanticStage.Kind + SemanticEvaluation` representation established by RFC-0031/RFC-0037

## Summary

Metis represents `SemanticPlan` directly as a DAG of typed planner-owned nodes.

The canonical abstraction is:

```go
type SemanticPlanNode interface {
    NodeBase() SemanticPlanNodeBase
    Kind() SemanticPlanNodeKind
    semanticPlanNode()
}
```

with concrete node types:

```text
SourceAggregateNode
PostAggregateNode
JoinAggregatesNode
CrossJoinAggregatesNode
CumulativeWindowNode
TimeOffsetNode
OffsetToGrainNode
ConversionNode
SemiAdditiveNode
SourceSelectionNode
```

`SemanticPlan` stores typed nodes directly. The concrete node type is the authoritative statement of the semantic role of that DAG node.

The final representation contains neither the old `Kind + SemanticEvaluation` nullable tagged union nor the transitional `SemanticStageOperation` envelope.

## Final architecture

```text
ResolvedSemanticQuery
        |
        v
MetricEvaluationPlan
        |
        | source-aware semantic lowering
        v
SemanticPlan
        |
        +-- SemanticPlanNode
        |       +-- SourceAggregateNode
        |       +-- PostAggregateNode
        |       +-- JoinAggregatesNode
        |       +-- CrossJoinAggregatesNode
        |       +-- CumulativeWindowNode
        |       +-- TimeOffsetNode
        |       +-- OffsetToGrainNode
        |       +-- ConversionNode
        |       +-- SemiAdditiveNode
        |       `-- SourceSelectionNode
        |
        v
SQLPlan
```

The authority boundaries are:

```text
MetricEvaluationPlan
    = query-scoped metric identity, metric kind,
      typed metric semantics, canonical time binding,
      and metric dependency authority

SemanticPlanNode concrete type
    = typed source-aware semantic-node authority

SemanticPlan
    = semantic DAG authority

SQLPlan
    = physical SQL-structure authority
```

The vocabularies are intentionally not one-to-one. A `MetricEvaluationKind=derived` may lower to `PostAggregateNode`, `JoinAggregatesNode`, or `CrossJoinAggregatesNode`; one metric evaluation may lower to multiple semantic nodes and multiple metric evaluations may share semantic work.

## Node contract

`SemanticPlanNodeBase` contains only state semantically valid for every node, including node identity, graph inputs, output grain, predicate ownership, boundary evidence, dimensions, and extension evidence.

Shared-but-not-universal state uses focused composition rather than nullable fields on the universal base. Source-aware nodes own source state; metric-bearing nodes own metric state; metric-free nodes such as `SourceSelectionNode` do not carry fake metric state.

`SemanticPlanNodeKind` is a deterministic projection derived from the concrete node type. It is retained for explain output, fingerprints, diagnostics, tests, and bounded observability labels. It is not an independently mutable discriminator.

## Validation and ownership

Generic graph validation operates through `SemanticPlanNode` and `SemanticPlanNodeBase` and checks node identity, input existence, DAG acyclicity, grain integrity, predicate ownership, and extension ownership.

Node-specific validation dispatches on the concrete type. Unknown internal node implementations fail closed.

Deep clone/ownership support is exhaustive over the closed node family and nested mutable semantic state. Adding a new node kind requires coordinated construction, validation, cloning, explain, fingerprint, optimizer, lowering, and conformance coverage.

## Optimizer and lowering contract

Generic optimizer work consumes the typed node/base contract. Rules that require node-specific semantics dispatch on concrete node type; they do not reconstruct parallel operation-kind or evaluation-kind authority.

SemanticPlan -> SQLPlan lowering consumes typed semantic nodes. It does not reopen legacy evaluation state and does not rediscover semantic-node meaning from model/catalog definitions.

Typed SemanticPlan nodes remain semantic IR. They do not absorb SQLPlan query-block, CTE, physical join, or dialect-rendering responsibilities.

## Migration history

RFC-0040 was completed in explicit representation-only phases:

- **Phase A/B:** establish and freeze the transitional operation-envelope representation as migration evidence.
- **Phase B:** introduce the complete typed `SemanticPlanNode` family in shadow form and prove equivalence.
- **Phase C:** construct concrete typed nodes directly.
- **Phase D:** make `SemanticPlan.Nodes []SemanticPlanNode` canonical and cut production consumers to typed nodes.
- **Phase E:** delete `SemanticStageOperation`, `SemanticEvaluation`, and the compatibility envelope.
- **Phase F:** close canonical `Stage` -> `Node` vocabulary and remove remaining stage-shaped authority seams.
- **Phase G:** repository authority/docs closure audit and lifecycle finalization.

The temporary operation representation was migration scaffolding only and is not part of the final architecture.

## Closure evidence

The migration was required to preserve semantic behavior. Closure is backed by:

- `make check` and deployable E2E correctness gates;
- deterministic SemanticPlan fingerprint coverage;
- the frozen 96-scenario × DuckDB/Doris/ClickHouse compile corpus (288 SQL/parameter projections);
- applicable DuckDB, Doris, and ClickHouse real-engine execution conformance;
- typed-node ownership and exhaustiveness coverage;
- repository-wide authority checks showing no canonical `SemanticStageOperation` or `SemanticEvaluation` path remains.

The Phase G closure additionally removes retired Stage vocabulary from current
semantic-pipeline helpers, diagnostics, Explain JSON, fingerprints, and generated
semi-additive helper aliases. A repository guard rejects its reintroduction while
explicitly preserving Ossie's metric definition-filter `stage` domain field.

Phase F exact-head CI completed successfully before merge, including required correctness, E2E, DuckDB, and Doris; ClickHouse was skipped by the repository's impact detector for that representation-only diff.

earlier implementation work closed the remaining
Agent-facing Explain and semantic-plan fingerprint vocabulary leak. Its exact
head passed required correctness plus DuckDB, ClickHouse, and Doris real-engine
execution conformance before merge as `9f63963621d1e210759fb1eb628ed8f5659281f1`.
earlier implementation work completes the repository
vocabulary guard and current production helper/diagnostic cleanup.

## Acceptance criteria

RFC-0040 is implemented with the following invariants:

1. `SemanticPlanNode` is the canonical planner-owned abstraction for one SemanticPlan DAG node.
2. `SemanticPlan` stores typed nodes directly and has no universal semantic stage envelope.
3. The closed concrete node family covers all currently supported semantic roles.
4. Each concrete node owns its node-specific semantic state.
5. `SemanticStageOperation` and `SemanticEvaluation` are not canonical or compatibility semantic authorities.
6. No node stores an independent mutable kind discriminator.
7. `SemanticPlanNodeKind` is deterministic and derived from concrete node type.
8. `SemanticPlanNodeBase` contains only universally valid node state.
9. Shared-but-not-universal state uses explicit composition.
10. `SourceSelectionNode` is structurally metric-free.
11. Generic graph algorithms operate through the node/base contract.
12. Node-specific validation dispatches on concrete node type and unknown implementations fail closed.
13. Deep clone/ownership is exhaustive over the node family.
14. Explain and fingerprint projections remain deterministic and use stable semantic vocabulary rather than Go type names.
15. Optimizer and SemanticPlan -> SQLPlan lowering consume typed nodes and do not recreate legacy semantic discriminators.
16. `MetricEvaluationPlan` remains the sole authority for query-scoped metric identity, kind, typed spec, canonical time binding, and metric dependencies.
17. No `MetricEvaluationNode == SemanticPlanNode` cardinality assumption is introduced.
18. SQLPlan remains the sole physical SQL-structure authority.
19. Canonical SemanticPlan APIs use node vocabulary rather than stage vocabulary.
20. The 96 × 3 semantic results and parameter corpus remain unchanged for the representation migration; any pre-release physical-alias vocabulary cleanup is recorded explicitly and revalidated across applicable engines.

## Non-goals

RFC-0040 does not change metric definitions, metric dependency semantics, product semantic features, cost-based optimization, SQL block/CTE policy, dialect rendering, or external plugin extensibility. It does not require inheritance or expose concrete Go type names as public API contracts.

## Final invariant

```text
concrete SemanticPlanNode type
    =
the single authoritative statement
of what semantic DAG node this is
```

There is no separate stored node-kind discriminator, no nullable stage-evaluation union, and no permanent stage-operation envelope. The SemanticPlan DAG itself is typed.
