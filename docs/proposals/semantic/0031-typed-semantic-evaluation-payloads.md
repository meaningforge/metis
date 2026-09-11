# RFC-0031: Typed Semantic Evaluation Payloads

> **Design history:** This RFC preserves its original lifecycle status and rationale.
> Implementation claims describe the recorded design, not a guarantee that every
> proposed API remains current. See [current documentation](../../README.md) for
> supported interfaces. Package paths have been updated for this repository.

- **Status:** Superseded
- **Owners:** Metis maintainers
- **Created:** 2026-08-18
- **Last updated:** 2026-08-22
- **Scope:** Phase K metric-evaluation migration into `SemanticPlan`
- **Supersedes:** None
- **Superseded by:** RFC-0039 query-scoped `MetricEvaluationPlan` and RFC-0040 typed `SemanticPlanNode` architecture

## Supersession note

This document is retained as Phase K migration history. Its claims that
`MetricEvaluationPlan` / `MetricEvaluationNode` no longer exist and that
stage-local `SemanticEvaluation` is the final representation are no longer
current: RFC-0039 introduced the permanent query-scoped metric-only plan, and
RFC-0040 replaced the stage payload/envelope model with concrete typed
`SemanticPlanNode` values.

The current pipeline is:

```text
ResolvedSemanticQuery
    -> MetricEvaluationPlan (metric-bearing queries only)
    -> SemanticPlan.Nodes
    -> SQLPlan
```

Read the remaining sections only as the record of the superseded migration.

## Migration status

Every acceptance item below is met; see *Acceptance* for what proves each.

The sections describing the migration are kept as the record of how it was carried out, and they are written in the present tense of a migration that is now over. Read them as history: `MetricEvaluationPlan`, `MetricEvaluationNode`, `SemanticStageGraph`, and `SemanticPlan.StageGraph` no longer exist. Where those sections describe a transitional structure as remaining, it does not.

*Canonical plan, staged graph* and *Acceptance* describe the state of the code and are current.

## Context

Phase J established `SemanticPlan` as Metis's canonical semantic planning IR and the stage-oriented graph inside it as the engine-neutral representation of staged semantic composition. That canonical representation is named `SemanticGraph`; its nodes are `SemanticStage` values. Staged SQL AST lowering takes stage order, dependency edges, grain, source requirements, joins, predicate ownership, and source-sharing evidence from that canonical graph.

One transitional duplication remains: metric-specific evaluation details such as resolved expressions, cumulative semantics, time offsets, conversion semantics, and semi-additive selection are still carried by `MetricEvaluationPlan` / `MetricEvaluationNode` during construction and compatibility lowering. Keeping that transitional structure authoritative downstream creates drift risk between canonical stage structure and the payload used to generate SQL.

Metric evaluation is a permanent planner capability. `MetricEvaluationPlan` is not a permanent second plan.

## Decision

Metis follows **one plan, many typed evaluations**.

`SemanticPlan` remains the only canonical semantic plan. For staged queries it owns a materialized `SemanticGraph`, and each `SemanticStage` owns a `SemanticEvaluation` payload whose stage kind is the discriminator for exactly one typed evaluation variant.

`SemanticEvaluation` names the canonical evaluation semantics, not a stage-specific sub-plan. `SemanticStage` is the current carrier and ownership boundary; the evaluation type itself is intentionally not coupled to `Stage` in its name.

The typed variants cover:

- source aggregate evaluation;
- derived / aggregate-composition evaluation;
- cumulative evaluation;
- time-offset evaluation;
- offset-to-grain evaluation and boundary proof;
- conversion evaluation, semantic conversion proof, and target-compatible event inputs;
- semi-additive first/last evaluation.

The common semantic evaluation envelope also owns the resolved metric expression and metric definition required by existing lowering while those consumers migrate.

A typed evaluation is not another plan. It does not own stage ordering, dependency topology, output grain, source sharing, predicate placement, or physical strategy. Those remain properties of `SemanticPlan` and its owned `SemanticGraph`.

## Canonical plan, staged graph

`SemanticPlan` is canonical for every query. `SemanticGraph` is the representation a query owns when it needs staged semantic composition — that is what "for staged queries" above means, and this section states the other half, which was only ever implied.

A query that needs no staged composition owns no graph and lowers from the plan's own `Root`, `Projections`, `Joins`, `Predicates`, and `GroupBy`. That is canonical flat-plan lowering: it reads canonical plan state, no transitional evaluation IR is involved, and `FingerprintSemanticPlan` covers a flat plan as fully as a staged one. It is not a legacy fallback and not a compatibility path.

The consequence worth naming, because it reads like a gap and is not one: a query with no stages has no stage fusion, no stage-owned typed payload, and no stage-level ownership clone. Those are properties of staged composition. A query that does not stage does not acquire them, and its absence from a stage-level measurement says nothing about whether it is canonical.

Where the boundary currently falls is a property of the corpus rather than of this decision, so it is recorded as a test rather than as prose here. `TestCanonicalGraphBoundaryOfTheConformanceCorpus` asserts the exact set of scenarios that lower directly, on every compiler target, and requires the three to agree — whether a query needs staged composition follows from the query and the model, never from the compile target.

Requiring every query to build a `SemanticGraph`, and reducing `BuildSQLAST` to a single path, would be a stronger decision than this RFC takes. It is not an acceptance condition for Phase K and needs its own proposal.

## Canonical graph ownership

`MetricEvaluationPlan` remains a transitional construction input while Phase K is in progress. During semantic construction and optimizer rewrites it may still be mutated by planner-owned rules. Those transient rewrites must not turn it back into a downstream source of truth.

The planner therefore separates transient projection from canonical materialization:

1. optimizer rules may project a temporary semantic graph from current construction state for correctness proofs;
2. after staged semantic construction or optimizer rewrites finish, the planner refreshes and validates the canonical graph;
3. the validated graph is materialized as `SemanticPlan.Graph`;
4. downstream stage planning, explain, validation, and lowering prefer that owned graph and receive deep copies rather than re-projecting `MetricEvaluationPlan`.

Once materialized, later mutation of transitional construction state cannot silently change downstream stage semantics. A deliberate planner rewrite must explicitly refresh canonical graph ownership.

`SemanticStageGraph` / `SemanticPlan.StageGraph` are migration-era names. During Phase K they may remain as a source-compatibility type alias and same-pointer field mirror while callers migrate, but they are not a second graph and must never become independent authority. The canonical vocabulary and long-term API are `SemanticGraph` and `SemanticPlan.Graph`; the compatibility names are removed before Phase K closes.

## Ownership and validation

Canonical stage construction copies mutable slice-backed evaluation evidence into stage-owned `SemanticEvaluation` payloads. Graph accessors also return owned deep copies, including typed evaluation payloads. Lowering must not depend on mutable caller-owned slices from transitional construction state.

The stage kind and typed evaluation variant must agree. A source-aggregate stage cannot carry a cumulative or derived payload; a conversion stage cannot carry a semi-additive payload. Canonical graph validation fails closed when a present typed payload disagrees with the stage discriminator.

`ValidateSemanticPlan` validates transitional `MetricEvaluationPlan` state only before canonical graph materialization. Once `SemanticPlan.Graph` exists, canonical graph validation is authoritative: stale construction-state drift cannot invalidate or override the materialized semantic plan, while invalid canonical stage state still fails closed.

That tightening is complete. Canonical graph validation requires exactly one typed payload on every stage, rather than checking only the stages that already carried one. A stage carrying none is rejected at validation instead of at lowering; the two used to disagree, so a graph could be valid and uncompilable at the same time. Hand-constructed graphs that omitted typed evaluation data — the only population the gap admitted, since every planner path already attached the payload — now supply it the way the planner does.

Ownership is likewise checked rather than asserted. A clone of a typed evaluation must carry every field the evaluation reaches and share no backing storage with its input, both derived by walking the type rather than from a list that a new field can be added behind. Two kinds of sharing are legitimate: an `ossie` value is a catalog declaration the plan reads and never writes through, admitted as a rule covering the whole subtree; and `ResolvedExpression.Analysis` pointer identity survives a copy, admitted by name. Evidence attached to a resolved expression deliberately does not survive — a cloned evaluation owns its evidence outright.

## Lowering migration

Staged SQL AST construction consumes the canonical `SemanticGraph` directly. Stage order, dependencies, grain, source-sharing identity, source requirements, predicates, resolved metric expressions, and typed metric-specific evaluation payloads are read from `SemanticStage` and its `SemanticEvaluation`.

`MetricEvaluationNode` remains temporarily usable as a leaf compatibility shape for metric-specific helper functions whose signatures have not yet migrated. Those temporary leaf values are reconstructed from canonical `SemanticStage` / `SemanticEvaluation` state rather than seeded from `MetricEvaluationPlan`; the transitional plan is not a staged SQL lowering input.

Calendar and offset post-processing lowerers also consume the canonical semantic graph directly. Built-in dense calendar, custom dense calendar, custom-calendar time offset, offset-to-grain, and offset-to-grain dense-calendar lowering derive their cross-stage topology and typed evaluation semantics from `SemanticGraph` / `SemanticEvaluation`; they no longer reconstruct a whole `MetricEvaluationPlan` compatibility shell for lowering.

`MetricEvaluationPlan` is therefore limited to transitional semantic construction and optimizer rewrite state. It is not a cross-stage lowering or post-materialization validation authority. Remaining Phase K work narrows or removes that construction state and migrates remaining helper signatures away from `MetricEvaluationNode`.

## Permanent architecture boundary

This migration is semantic only. It introduces no statistics, cardinality estimation, cost model, physical join-order search, access-path selection, runtime materialization policy, backend execution, or other CBO / physical planning responsibility.

Dialect renderers remain responsible for target syntax and capability translation only.

## Acceptance

Phase K is complete when the following hold. All eight were audited together; what proves each is named so a later reader can re-run it rather than take this list on trust.

1. **Canonical semantic stages own typed `SemanticEvaluation` payloads for all supported metric-evaluation kinds.** Met. All ten `SemanticStageKind` values are covered by `validateForKind`.
2. **Staged `SemanticPlan` owns a validated materialized `SemanticGraph` after semantic construction and optimizer rewrites.** Met. `materializeSemanticGraphInput` validates before taking ownership, and `TestEveryStagedScenarioBuildsAGraphValidationAccepts` re-checks every staged scenario on all three compiler targets.
3. **Kind/payload mismatch fails closed.** Met, and widened. Mismatch was already rejected; a stage carrying no payload was not, and is now — see the validation note above. Both directions are gated by `TestValidateSemanticGraphRejectsTypedEvaluationKindMismatch` and `TestValidateSemanticGraphRejectsStageWithNoTypedEvaluation`.
4. **Mutable evaluation evidence and graph access results are owned by the canonical graph representation.** Met, after one real defect: `ConversionPhysicalInputPlan` shallow-copied `BaseValue` and `ConversionValue`, leaving a planner-owned `Field` pointer aliased with its input. `TestSemanticEvaluationCloneCarriesEveryField` and `TestSemanticEvaluationCloneSharesNoStorage` derive the check from the type, so a field added later is covered when it is added.
5. **SQL AST lowering consumes canonical stage-owned metric semantics rather than stale transitional node payloads.** Met. `MetricEvaluationNode` no longer exists.
6. **Compiler, result, Doris, and ClickHouse conformance remain unchanged.** Met. 96 × 3 compiled fingerprints are byte-identical across the migration, and all three real engines pass execution conformance.
7. **`MetricEvaluationPlan` is no longer a co-equal or authoritative cross-stage semantic plan.** Met beyond the wording: the type is gone, not demoted.
8. **Migration-era `SemanticStageGraph` / `StageGraph` compatibility names are removed.** Met.

Note that none of these is "every query owns a `SemanticGraph`", and Phase K never claimed it. See *Canonical plan, staged graph* above.
