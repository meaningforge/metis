# RFC-0026: Multi-metric Shared-grain Planning

> **Design history:** This RFC preserves its original lifecycle status and rationale.
> Implementation claims describe the recorded design, not a guarantee that every
> proposed API remains current. See [current documentation](../../README.md) for
> supported interfaces. Package paths have been updated for this repository.

- **Status:** Implemented
- **Owners:** Metis maintainers
- **Created:** 2026-08-17
- **Last updated:** 2026-08-17
- **Scope:** multi-metric semantic planning feasibility
- **Supersedes:** None

## Summary

Phase I4 makes multi-metric correctness explicit before physical staging or SQL lowering. A request containing multiple metrics is feasible only when every metric can be evaluated at one compatible semantic output grain through an unambiguous, fanout-safe relationship traversal.

The contract is intentionally separate from physical strategy. Proving that two metrics can share a grain does not choose whether they should share a source scan, use independent aggregate stages, or be joined after aggregation.

Tracking: #262, #266.

## Motivation

Metis already contains strong local correctness checks:

- source-metric evaluation resolves a root dataset and the relationships needed to reach query grain/filter datasets;
- relationship traversal fails when the target side is not proven unique;
- advanced metric kinds retain explicit evaluation nodes and output grain;
- source-stage fusion and dependency reuse can reduce duplicate work after semantic planning.

Those checks are necessary but do not yet expose one first-class answer to the multi-metric question: can all requested metrics legally produce values at the same requested semantic grain?

Without a shared-grain contract, compatibility can be inferred indirectly from whichever physical staging shape happens to be constructed. Phase I4 makes feasibility an explicit semantic invariant and leaves physical strategy to later planning/lowering.

## I4.1 shared-grain feasibility contract

`SharedGrainCandidate` captures the evidence already established for one metric candidate:

- metric identity;
- source/root dataset;
- semantic output grain;
- selected relationship path;
- number of viable path alternatives;
- per-metric fanout-safety proof.

`ResolveSharedGrain` compares candidates without choosing a SQL strategy. It returns deterministic `SharedGrainResolution` evidence or a typed `SharedGrainError`.

### Canonical grain identity

A grain is identified by its set of grouped semantic dimensions. Each group contributes:

- dataset identity;
- resolved output/group name;
- built-in time grain when present;
- custom-calendar grain when present.

Group ordering is not semantically significant, so the canonical key sorts group identities. No groups is the scalar grain.

I4.1 intentionally requires exact canonical grain equality. It does not silently roll a finer metric up to a coarser grain or invent a drill-across key. Those transformations require explicit semantic proof and belong in later I4 integration if supported.

### Typed failures

The feasibility boundary fails closed with stable typed categories:

- `INCOMPATIBLE_GRAIN`: metric output grains do not identify the same semantic grouping;
- `AMBIGUOUS_RELATIONSHIP_PATH`: more than one viable relationship path remains for a metric;
- `FANOUT_UNSAFE`: the selected traversal is not proven fanout-safe for that metric;
- `INVALID_CANDIDATE`: required candidate identity/root evidence is absent.

An ambiguous path is never resolved by registration order. A fanout-unsafe metric is never made safe merely because another requested metric has a compatible grain.

### Deterministic evidence

Metric evidence is ordered deterministically by metric name. The returned grain slice and relationship-path evidence are owned by the resolution so callers cannot mutate the proof through input slices.

## I4.2 production planner integration

I4.2 wires the I4.1 contract into the production `MetricEvaluationPlan` graph without changing physical staging decisions.

For every requested metric, planner integration derives one shared-grain candidate from the completed evaluation graph:

- the query-level semantic output grain retained by `MetricEvaluationPlan.OutputGrain`;
- the requested metric node's deterministic source-root set;
- relationship paths collected from its source-leaf dependencies;
- the fanout-safe status already proven by `planSourceEvaluation` and `validateNonFanoutJoin` before the candidate is built.

The query-level semantic output grain is intentionally distinct from a node's internal stage `OutputGrain`. Advanced evaluation can refine a source/input stage with hidden grouping keys that are required for correct evaluation but are not part of the user-visible result grain. For example, semi-additive first/last selection can add raw ordering, tie-break, or window-grouping dimensions to its base stage. Those internal keys must not turn an otherwise compatible query into `INCOMPATIBLE_GRAIN`.

Multi-root metrics retain the complete canonical `RootDatasets` set and all deterministic `RelationshipPaths`. The original singular `RootDataset` / `RelationshipPath` evidence remains available for compatibility and simple one-root/one-path cases.

`Planner.Plan` resolves this evidence after evaluation-specific semantic grain/read-range adjustments and before optimizer/staging/lowering strategy is allowed to proceed. Successful evidence is attached to `SemanticPlan.SharedGrain` so later planning and explain/debug consumers do not need to re-derive semantic compatibility from a physical plan shape.

Typed `SharedGrainError` failures are mapped to the existing stable `METRIC_EVALUATION_PLANNING_FAILED` boundary with the typed shared-grain failure category and metric retained in error details. The mapping does not downgrade existing path/fanout failures: source path ambiguity and non-unique relationship traversal still fail earlier at their existing semantic-planning owners.

## Boundary with existing planner behavior

Existing `planSourceEvaluation` remains responsible for resolving concrete dataset paths and validating every join used by one source metric. I4 does not duplicate Catalog path search or uniqueness logic; it defines the common feasibility result those proofs feed into.

Existing `MetricEvaluationPlan` remains the engine-neutral representation for ordinary, cumulative/window, semi-additive, conversion, and derived metrics. I4.2 integrates shared-grain evidence with that evaluation graph so requested metric kinds are checked through one compatibility boundary before physical strategy.

Source-stage fusion, hidden evaluation grouping keys, CTE reuse, and join-after-aggregate decisions are physical/structural concerns applied only after or underneath shared-grain feasibility; they do not redefine the query's semantic output grain.

## Sequencing

### I4.1 — shared-grain feasibility contract — implemented

- define canonical semantic grain identity;
- define per-metric root/path/fanout evidence;
- return deterministic shared-grain evidence for compatible candidates;
- fail closed on incompatible grain, ambiguous path, fanout-unsafe traversal, or incomplete candidate evidence;
- keep the contract independent from staging and SQL lowering.

### I4.2 — planner integration — implemented

- derive candidates from production metric-evaluation nodes and resolved relationship evidence;
- keep query semantic output grain separate from advanced/internal stage grain;
- retain deterministic multi-root and multi-path evidence for derived metrics;
- attach shared-grain evidence to `SemanticPlan` before optimization/lowering;
- map typed feasibility failures into stable metric-evaluation planning diagnostics;
- preserve existing per-metric fanout and dataset-path checks rather than weakening them.

### I4.3 — advanced metric and lowering regression — implemented

- cover ordinary, derived multi-root, and conversion requests through the production planner shared-grain boundary;
- cover cumulative/window, time-offset, offset-to-grain, conversion, and semi-additive internal stage grains at the centralized shared-grain integration boundary;
- prove hidden internal stage grouping keys cannot redefine the query-level semantic output grain;
- prove compatible independent multi-source metrics retain shared-grain evidence through full-outer drill-across SQL AST lowering;
- keep typed incompatible, ambiguous-path, and fanout failures owned by the existing shared-grain/path validation contracts;
- add no dialect, physical strategy, cost model, or database execution behavior.

## Non-goals

Phase I4 does not:

- introduce cost-based optimization or statistics;
- search join orders;
- infer uniqueness not declared by the semantic model;
- silently roll metrics between grains;
- add database execution;
- make physical source-stage reuse part of semantic feasibility.

## Acceptance criteria for I4.1

I4.1 is complete when:

- equivalent group sets resolve to one deterministic canonical grain regardless of group ordering;
- metric evidence is deterministic and caller-owned input slices cannot mutate the returned proof;
- incompatible built-in/custom/dimension grains fail as `INCOMPATIBLE_GRAIN`;
- ambiguous relationship paths fail as `AMBIGUOUS_RELATIONSHIP_PATH`;
- a fanout-unsafe metric fails independently as `FANOUT_UNSAFE`;
- invalid candidates fail explicitly rather than being skipped;
- no physical staging or SQL strategy is selected by the contract;
- existing correctness and real-engine gates remain green.

## Acceptance criteria for I4.2

I4.2 is complete when:

- production requested metrics derive shared-grain candidates from the completed evaluation graph rather than from SQL/lowering shape;
- shared-grain comparison uses the query-level semantic output grain and does not mistake hidden advanced/internal stage grouping keys for user-visible grain;
- compatible multi-root metrics retain deterministic root-set and relationship-path evidence on `SemanticPlan.SharedGrain`;
- requested metric evidence order remains deterministic;
- typed shared-grain failures map to stable metric-evaluation planning diagnostics with failure category and metric details;
- existing per-source path ambiguity and fanout checks still fail at their existing owners before shared-grain integration;
- shared-grain resolution runs before optimizer/lowering strategy;
- no CBO, statistics, join-order search, dialect expansion, or database execution is introduced;
- existing correctness, E2E, Doris, and ClickHouse gates remain green.

## Acceptance criteria for I4.3

I4.3 is complete when:

- ordinary, derived, and conversion production plans expose `SharedGrain` equal to `MetricEvaluationPlan.OutputGrain`;
- cumulative/window, time-offset, offset-to-grain, conversion, and semi-additive internal stage keys cannot alter semantic shared-grain evidence;
- compatible independent multi-source metrics preserve deterministic metric evidence and lower through the existing full-outer drill-across path;
- SQL AST lowering does not mutate the semantic shared-grain proof;
- existing incompatible-grain, path-ambiguity, and fanout-safety typed failure regressions remain green;
- no dialect expansion, CBO/statistics/join-order search, or database execution is introduced;
- required correctness, E2E, Doris, and ClickHouse gates remain green.
