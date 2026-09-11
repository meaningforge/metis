# RFC-0017: Semantic Optimizer Safety and Query Shape Quality

> **Design history:** This RFC preserves its original lifecycle status and rationale.
> Implementation claims describe the recorded design, not a guarantee that every
> proposed API remains current. See [current documentation](../../README.md) for
> supported interfaces. Package paths have been updated for this repository.

- **Status:** Implemented
- **Owners:** Metis maintainers
- **Created:** 2026-08-16
- **Last updated:** 2026-08-16
- **Scope:** `planner`, semantic-plan validation/lowering, optimizer conformance, Doris/ClickHouse execution evidence
- **Supersedes:** None

## Summary

Metis has a deterministic rule-based semantic-plan optimizer between semantic planning and SQL AST lowering. The current default rule set performs metric projection pruning, semantic-set normalization, predicate deduplication and pushdown, join deduplication and pruning, and compatible source-scan fusion.

Phase D hardened the existing optimizer as an explicit semantics-preserving contract and improved query shape only where equivalence can be proven.

The implemented flow is:

```text
ResolvedSemanticQuery
  -> Planner semantic construction
  -> pre-optimization invariant validation
  -> rule-based SemanticPlan optimization
  -> post-optimization invariant validation
  -> SQL AST lowering
  -> dialect rendering
```

The central requirement is stronger than "the optimized SQL looks smaller": for the same resolved semantic query, optimization MUST preserve observable semantic behavior while allowing the physical query shape to become smaller or simpler.

## Motivation

Phase B established executable semantic correctness for advanced metric semantics. Phase C established deterministic Agent-facing context and explanation over the same semantic truth. The next risk was query-shape quality: correct plans could still carry redundant joins, duplicate source stages, unnecessary evaluation nodes, or predicates at unnecessarily late stages.

Phase D closed those gaps before expanding the optimizer or multiplying physical targets.

## Goals

Phase D MUST:

1. make the existing optimizer boundary and safety invariants normative;
2. validate semantic-plan invariants before and after optimization;
3. require deterministic and idempotent optimizer behavior;
4. make rule preconditions explicit and fail closed when equivalence cannot be proven;
5. improve relational and metric-evaluation shape without changing semantic meaning;
6. tighten predicate placement around aggregation, time, fill, semi-additive, conversion, and composition boundaries;
7. reduce duplicate compatible source/evaluation work where sharing is provably safe;
8. prove optimized/unoptimized equivalence with canonical and real-engine differential tests.

## Non-goals

Phase D does not add:

- warehouse statistics or cost-based optimization;
- database connectivity or query execution inside Metis;
- cache, materialization, or pre-aggregation management;
- natural-language planning or LLM-directed rewrites;
- arbitrary rendered-SQL rewriting;
- a new semantic model beside Ossie;
- new SQL dialects merely to exercise optimizer work.

A later RFC may introduce physical cost information, but it MUST NOT be required for Phase D correctness.

## Existing baseline

The current `optimizer.Optimizer` clones its input and applies an ordered list of typed `OptimizationRule` values. The default optimizer includes these rule names:

- `metric_projection_pruning`
- `semantic_set_normalization`
- `predicate_deduplication`
- `predicate_pushdown`
- `join_deduplication`
- `unused_join_elimination`
- `source_scan_fusion`

`OptimizationTrace` remains planner evidence. It is not an Agent-facing semantic contract and MUST NOT become required REST/MCP output merely because the optimizer records it internally.

## Semantic invariants

For a valid plan, an optimization MUST preserve all observable semantic properties applicable to the query, including:

- project/model and compile target identity;
- root semantic dataset unless a future rule has an explicitly proven equivalent root rewrite contract;
- metric and dimension identity;
- projection identity and user-visible projection order;
- declared output schema identity, role, datatype, and requested grain;
- grouping and ordering semantics;
- limit semantics;
- metric dependency closure and evaluation meaning;
- relationship reachability and fanout safety;
- definition-filter and query-filter meaning;
- conversion base/candidate population semantics;
- cumulative and grain-to-date windows;
- time-offset coordinate alignment;
- fill timing and values;
- semi-additive selection and composability rules;
- public Explain Query semantic evidence and stable failure class.

Physical SQL text, CTE names, source-stage grouping, redundant projection materialization, and other non-semantic physical shape MAY change.

If a rule cannot prove its rewrite preserves the relevant invariants, it MUST leave the plan unchanged. It MUST NOT guess based on rendered SQL text or target-specific heuristics.

## Plan invariant validation

Metis uses an explicit semantic-plan invariant validator around optimization.

The validator is independent of individual optimization rules and detects malformed plan states such as:

- unresolved required expressions;
- projection/evaluation references to missing metric nodes;
- broken dependency closure or dependency order;
- source stages referencing missing or incompatible evaluation nodes;
- joins whose path cannot satisfy the datasets claimed by their consumer;
- predicates attached to a stage that cannot evaluate their dataset/expression;
- invalid output-grain or semantic-transform state that existing planner contracts require.

A plan that was valid before optimization MUST remain valid after optimization. Optimizer failures are planning failures; invalid rewritten state MUST NOT continue to SQL AST lowering. Paths that intentionally bypass optimizer rewrites still validate before lowering.

## Determinism and idempotence

For the same input `SemanticPlan` and rule set, optimization MUST be deterministic.

Applying the default optimizer to an already optimized plan MUST produce an equivalent plan. User-authored ordering MUST be preserved. Collections whose contract is a set MAY be canonicalized deterministically. Optimization MUST NOT mutate the input plan.

## Relational requirement closure

Join and projection pruning are driven by explicit consumer requirements rather than by SQL-string inspection.

Consumers include projections, groups, filters, sorts, metric source/evaluation nodes, hidden/requested metric dependency closure, and semantics-bearing transformations. A relationship edge may be removed only when no surviving consumer requires the edge or any descendant reached through it.

Metric-only sorts, hidden filter metrics, staged metric evaluation, and semantic transforms preserve their typed dataset requirements without forcing retention of unrelated joins.

## Predicate placement

Predicate movement is allowed only across semantic boundaries where equivalence is known.

The optimizer distinguishes the effective boundaries represented by current Metis planning, including source/pre-aggregation evaluation, aggregate evaluation, post-evaluation metric filtering, composition, time alignment/offset, dense-calendar/fill, semi-additive selection, and final query shaping.

Planner-owned staged placement is treated as proof. The optimizer consumes redundant top-level predicates only when source ownership is established; it does not invent predicate movement across advanced semantic boundaries. When proof is incomplete, the predicate remains in place.

## Shared evaluation and source stages

Metis shares source scans or evaluation stages only when semantics-bearing inputs are equivalent. Compatibility includes root/source dataset, semantic source roots, required relationship path, grain, pre-aggregation predicates, and other typed source context represented by the plan.

Compatible source stages use deterministic naming and membership. Pruning invalidates stale stage membership before source-stage shaping is regenerated. Common dependency evaluation is reused instead of being duplicated per derived consumer, and lowering does not retain stale intermediate columns from pruned metrics.

Sharing MUST NOT merge metrics merely because their rendered SQL happens to look similar.

## Differential correctness contract

Metis has first-class differential evidence between an unoptimized planner and the default optimized planner.

For the same resolved semantic query, tests compare:

1. stable logical output schema;
2. public semantic explanation evidence where applicable;
3. normalized execution results on representative real-engine cases;
4. deterministic optimized plan/query shape.

The canonical differential layer proves output-schema and Explain invariance while also requiring an actual query-shape reduction. The real-engine differential harness compiles representative canonical scenarios both with the default optimizer and with optimization disabled, executes both forms on Doris and ClickHouse, normalizes the result sets, and compares them under the scenario's ordered/unordered result contract.

Representative coverage includes relationships, derived and multi-source composition, cumulative/time-offset semantics, metric filters, conversion, semi-additive evaluation, and final filter/order/limit shaping.

Physical SQL equality is not required. Result and semantic-contract equality are required.

## Query-shape quality evidence

Correctness remains the primary gate. Representative optimization fixtures assert structural improvements such as fewer unnecessary joins, aggregate CTEs, duplicated compatible stages, or unused evaluation nodes.

These are deterministic structural assertions, not warehouse cost estimates. Phase D does not claim runtime speedup solely from reduced node counts.

## Compatibility

The optimizer remains inside the planner/compiler boundary. No REST or MCP request shape changes are required.

Compile may emit different but semantically equivalent SQL as optimization improves. Explain continues to expose semantic decisions rather than optimizer internals.

Disabling the optimizer for differential tests is a verification mechanism, not a separate supported semantic mode.

## Implementation evidence

Phase D was delivered through the optimizer-safety PR sequence ending with canonical and real-engine differential conformance. Current normative behavior is reflected in `docs/specs/semantic/compilation-pipeline.md`, `docs/design/semantic/pipeline.md`, and `docs/specs/testing/architecture.md`.

The repository correctness gates include Go formatting, `make check`, canonical conformance, and relevant Doris/ClickHouse real-engine execution. The Phase D active plan is removed after lifecycle closure because implemented behavior belongs in current specs/design rather than a completed roadmap.

## Acceptance criteria

RFC-0017 is `Implemented` because:

- optimizer invariants are normative in current specs/design;
- pre/post optimization validation prevents invalid rewritten plans from lowering;
- default optimization is deterministic, idempotent, and input-immutable;
- pruning and predicate-placement rules fail closed when equivalence is not proven;
- shared evaluation preserves current advanced metric semantics;
- canonical differential tests compare optimized and unoptimized semantic contracts;
- representative Doris and ClickHouse execution cases prove normalized result equivalence;
- query-shape fixtures prove intended structural reductions;
- normal repository correctness and real-engine gates enforce the delivered guarantees.
