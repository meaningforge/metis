# Optimizer Differential Conformance Contract

## Purpose

Metis treats semantic-plan optimization as a semantics-preserving query-shape transformation. The repository MUST therefore prove the default optimized Planner path against an equivalent Planner path with optimizer rewrites disabled.

This contract is the durable Phase D acceptance layer. It complements the canonical scenario and real-engine contracts in [`architecture.md`](architecture.md); it does not define a second semantic mode for product callers.

## In-process differential contract

For the same `SemanticQuerySpec`, canonical differential tests MUST compare optimized and unoptimized planning and require:

- identical logical `OutputSchema` identity and column order;
- identical Agent-facing `QueryExplanation` semantic evidence where applicable;
- valid semantic plans on both paths;
- deterministic optimized query shape;
- at least one structural assertion showing that representative optimization actually removes redundant work rather than merely rendering different SQL.

Rendered SQL text and optimizer trace are explicitly excluded from semantic equality.

## Real-engine differential contract

`tests/engine/harness` provides an unoptimized compilation path only for conformance. Doris and ClickHouse differential tests MUST compile the same canonical scenario through both Planner configurations, execute both physical queries against the same prepared fixture, normalize both results into `scenarios.ResultSet`, and compare the two result sets under the scenario's existing ordered/unordered comparison contract.

The differential comparison MUST preserve the same typed result obligations as ordinary real-engine conformance:

- column names and order;
- engine-neutral value kinds;
- row width;
- NULL state;
- canonical normalized values;
- row sequence when `order_by` makes ordering semantic;
- unordered multiset equality otherwise.

Backend-specific SQL strings MUST NOT become the oracle for optimizer correctness.

## Required semantic coverage

Representative differential execution MUST exercise semantics that constrain safe optimizer movement or pruning, including:

- relationship-backed filtering and final order/limit shaping;
- temporal relationship boundary selection;
- derived and multi-source composition;
- cumulative evaluation, including filtered cumulative results;
- time-offset alignment and derived period-over-period results;
- post-evaluation metric filters;
- conversion population semantics;
- semi-additive selection, including explicit tie-breaks, NULL skipping, and grouped rollups;
- dense and custom-calendar evaluation.

The canonical in-process layer additionally protects output-schema and Explain invariance even when a specific scenario does not require real-engine execution in every PR.

## CI contract

Changes to optimizer behavior, semantic-plan requirement closure, predicate placement, source-node sharing, SQL lowering affected by optimized shape, or this differential harness MUST remain subject to the repository correctness gates:

```bash
make fmt-check
make check
make test-engine-doris
make test-engine-clickhouse
```

GitHub Actions may skip a real-engine job only under the repository's explicit impact-detection policy. A relevant optimizer or execution-differential change MUST trigger the corresponding engine gate.

A Phase D optimizer change is not considered semantically proven merely because `make check` passes if its affected behavior requires real-engine differential execution.

## Failure policy

Any optimized/unoptimized mismatch is a correctness failure. Tests MUST report the scenario and normalized difference rather than accepting optimizer-specific expected results.

The correct response to an incomplete rewrite proof is to retain the unoptimized semantic behavior or fix the proof. Differential tests MUST NOT normalize away meaningful schema, ordering, NULL, or value differences to make a rewrite pass.
