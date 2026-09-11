# Semantic Plan Quality Conformance Contract

## Purpose

Metis treats plan quality as a typed semantic-plan engineering contract, not as a database execution-cost estimate. This contract freezes the Phase G regression boundary around deterministic structural observation, canonical fingerprints, optimizer non-regression, and differential semantic correctness.

## Structural observation

A validated `SemanticPlan` MUST be observable through deterministic internal structural evidence, and every supported query MUST produce one. There is no query shape whose structure is unobservable because it lowers compactly: a metric-bearing query is observed through its typed metric-bearing nodes and a metric-free dimension or `distinct_values` query through its typed `SourceSelectionNode`.

`PlanShapeSummary` records stable units of semantic-plan work such as joins, predicates, projections, grouping/sort shape, metric-bearing semantic nodes, source nodes, evaluation joins and predicates, and dense/custom-calendar domains. Summary generation MUST NOT mutate the observed plan.

`FingerprintSemanticPlan` is the sole full-plan semantic fingerprint and records canonical typed plan structure for repository regression tests. Its projection MUST be explicit and typed; every field of every plan-shaping type MUST be declared either projected or deliberately excluded with a stated reason, and every field declared projected MUST be shown to move the fingerprint when it changes, so a field that is projected in name only cannot silently report less than it claims. Fingerprinting MUST exclude optimizer trace state and derived extension capability/provenance evidence, normalize map ordering and planner-declared set ordering, and preserve sequence ordering where ordering is semantically meaningful. An extension that affects planning or physical compilation MUST first materialize that effect into a `ResolvedExpression` or another explicit typed plan field; opaque or derived evidence MUST NOT become a hidden lowering input merely to participate in the fingerprint. The fingerprint is not semantic identity, cache identity, authorization state, or an Agent-facing compatibility promise.

## Optimizer structural non-regression

For representative default-optimizer cases, repository tests MUST compare the unoptimized and optimized structural quality vectors in addition to RFC-0017 semantic-plan invariant validation.

The structural signal protects duplicated semantic work such as source nodes, metric-bearing nodes, evaluation joins, and top-level joins from accidental growth. It MUST NOT be interpreted as estimated runtime cost and MUST NOT justify a rewrite whose semantic equivalence has not already been proven.

Correctness remains authoritative. If a structural comparison is incomplete or incomparable, Metis retains the semantically proven behavior rather than inventing a physical cost preference.

## Plan-shape regression corpus

The repository MUST keep representative typed plan-shape fixtures for ordinary and advanced metric semantics, including composition, cumulative evaluation, conversion, time offset, fill/densification, grain-to-date, and semi-additive evaluation where applicable.

Fixtures SHOULD freeze `PlanShapeSummary` values and canonical fingerprints at the typed planner boundary. Rendered SQL text MUST NOT be the primary plan-quality oracle.

A fingerprint or summary change is review evidence, not automatically a semantic failure. Intentional planner improvements may update the corpus, but accidental query-shape growth MUST be visible in review.

## Differential correctness

Structural quality checks complement rather than replace optimized-versus-unoptimized semantic correctness.

Representative optimizer-sensitive scenarios MUST continue to prove:

- semantic-plan invariant validity before and after optimization;
- logical output-schema equivalence;
- Agent-facing semantic explanation equivalence where applicable;
- canonical semantic conformance;
- normalized Doris and ClickHouse optimized-versus-unoptimized result equality where repository impact detection requires real-engine execution.

A structural improvement cannot excuse a semantic or execution mismatch.

## Permanent optimizer boundary

Metis Core is a semantic compiler and semantics-aware query-shape optimizer. It MUST NOT become a statistics-driven cost-based physical optimizer.

The target database remains responsible for cardinality/selectivity estimation, statistics-driven join ordering, physical operator and access-path choice, distribution/exchange planning, runtime adaptive optimization, and engine-specific execution-cost decisions.

Metis MUST NOT introduce row-count, NDV, selectivity, runtime-cost, or similar statistics solely to drive optimizer decisions; MUST NOT make cost-based relationship-path choices; and MUST NOT perform statistics-based physical join reordering.

## CI gates

Changes that affect semantic planning, optimizer structure, plan summaries/fingerprints, regression fixtures, or optimized physical shape MUST remain subject to the relevant repository gates, including:

```bash
make fmt-check
make check
make test-engine-doris
make test-engine-clickhouse
```

Real-engine jobs may be skipped only by the repository's explicit impact-detection policy. A relevant optimizer or differential-execution change must trigger the corresponding engine gate.
