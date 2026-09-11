# Metric Change Attribution Specification

## Contract

Metis resolves, compiles, optionally executes, and deterministically evaluates
evidence for the change of one governed metric between exact baseline and
current half-open periods. The independent compiled bundle remains internal.
The public `attribute_metric` operation returns a closed typed analytical result,
never SQL or raw physical rows. Metis does not infer business causality.

The resolved request contains one project, metric, time dimension, baseline
`[start, end)`, current `[start, end)`, one shared filter set, and from one
through sixteen compatible decomposition dimensions. Natural-language time
interpretation and investigative dimension selection remain caller work.

## Semantic authority

`MetricEvaluationPlan` remains the authority for metric identities, dependency
topology, resolved expressions, and typed evaluation. Attribution derives one
closed decomposition kind from that evidence:

- `additive`: a governed additive metric whose aligned missing values may use
  its proven identity fill;
- `ratio`: an explicit numerator/denominator topology whose two operands are
  governed additive metrics over the same population;
- `unsupported`: every other topology, which fails closed.

Names, descriptions, opaque extensions, and rendered SQL MUST NOT manufacture a
decomposition proof. Source `AVG`, `MIN`, `MAX`, distinct count, unknown
aggregation, nonlinear formulas, incompatible grains, unsafe fan-out, and
unsupported fill semantics are not silently admitted.

`MetricAttributionPlan` owns the derived strategy, ordered components, exactness,
and reconciliation contract. It lowers to exactly one concrete
`AdditiveAttributionNode` or `RatioAttributionNode` per dimension. The node owns
the resolved time coordinate, exact periods, decomposition dimension, input
grain, and metric component identities. Explain, clone, validation,
fingerprinting, and optimizer handling MUST cover all of this state.

## Independent-query bundle

A multi-dimension request is a bundle of independent decompositions, not one
multi-column attribution grain. Each dimension retains its own `SemanticPlan`,
`SQLPlan`, physical query, output schema, population reconciliation, and
deterministic ordering.

The bundle MUST:

- contain exactly one plan for every requested dimension;
- reject empty, duplicate, unknown, or more than sixteen dimensions;
- canonicalize entries by dimension reference;
- prove identical project, model, target, metric, time coordinate, periods,
  filters, decomposition kind, and ratio operands across entries;
- retain shared filters in both period scans of every entry;
- never rank or combine contributions across dimensions.

No bundle construction or physical lowering may re-read the SemanticManifest or reparse
raw model expressions.

## Runtime evaluation

`AttributeMetricService` authorizes `semantic:execute`, resolves one Project,
DataSource, Backend, and exact Renderer, compiles every canonical bundle entry,
then executes entries sequentially with one timeout and cumulative row/byte
budgets. Any failure discards all completed results. The Runner still receives
only one atomic `CompiledQuery` at a time.

`analytics/attribution` validates exact schemas and normalized rows, accepts
only Integer/Decimal numeric evidence, performs arbitrary-precision rational
reconciliation, and emits decimal strings. Additive reconciliation is exact;
ratio reconciliation uses absolute tolerance `1e-8`. Undefined ratios remain
typed null evidence. Public dimensions and members are canonicalized by semantic
identity/value rather than warehouse row order.

The public surfaces are `POST /v1/attribute-metric` and MCP
`attribute_metric`. Both call the same service. A compile-only Project returns
`QUERY_EXECUTION_UNAVAILABLE`.

## Period alignment

Each dimension query independently aggregates baseline and current over exact
`[start, end)` predicates. Shared non-time filters apply identically to both
periods. A `FULL OUTER` alignment retains the union of segment populations and
explicit presence flags distinguish absence from a present numeric zero.

Missing aggregate operands may be zero-filled only when their existing semantic
contract proves zero as the legal identity. An observed zero denominator is not
a missing value and MUST remain distinguishable from absence.

Attribution reconciliation always runs across the complete aligned population.
Top-K or limit filtering MUST NOT occur before reconciliation.

## Additive evidence

For each aligned segment:

```text
delta = current_value - baseline_value
total_delta = SUM(delta)
contribution_pct = delta / NULLIF(total_delta, 0) * 100
```

Positive and negative effects are preserved without clamping. Contribution
percentage is null when total delta is zero. `SUM(delta)` MUST reconcile to the
ordinary total change under the same periods and filters.

## Ratio evidence

For `ratio = numerator / denominator`, each period aggregates numerator and
denominator together. Global ratios, segment rates, and denominator-derived
weights guard all denominators with `NULLIF`-equivalent semantics.

Continuing segments use symmetric rate/mix decomposition. Entry and exit
segments use explicit effects without inventing a missing-period rate. The sum
of all segment effects is `decomposed_delta`, and:

```text
ratio_delta = current_ratio - baseline_ratio
reconciliation_residual = ratio_delta - decomposed_delta
```

The result includes period presence, period numerator/denominator, rates,
weights, period and segment defined state, rate/mix/entry/exit/total effects,
global ratios, delta, decomposition, residual, and `attribution_defined`.

If either global ratio or any required segment ratio is undefined,
decomposition and reconciliation values are null and `attribution_defined` is
false. Undefined ratios MUST NOT be converted to zero.

## Physical compilation and ordering

Attribution follows the canonical `SemanticPlan -> SQLPlan -> renderer` boundary.
Typed SQLPlan conditional, null-test, and parenthesized expressions carry the
formulas; renderers own only target syntax. Semantic period instants remain
canonical RFC3339 evidence, while SQLPlan parameters represent the same UTC
instants using portable SQL timestamp lexical form.

Each period source is scanned once per independent dimension query. Result order
is deterministic: defined/null rank as specified by the plan, absolute segment
effect descending, then dimension value ascending.

## Conformance

Planner tests cover decomposition admission, typed-node invariants, exact period
and filter lowering, alignment, formulas, output schema, bundle independence,
clone/explain/fingerprint stability, and failure cases. Renderer tests cover the
typed physical expressions without changing ordinary-query SQL.

DuckDB, ClickHouse, and Doris execute the same internal test-only ratio bundle
against independently calculated planted evidence. The fixture proves shared
filter preservation, deterministic multi-dimension independence,
continuing/entry/exit behavior, symmetric reconciliation, and explicit segment
and total zero-denominator results. These tests do not create a public compile
surface or move execution ownership into Metis.
