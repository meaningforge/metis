# Metric Comparison Runtime Contract

This specification defines the production `compare_metrics` analytical
workflow. It is a closed deterministic operation over governed semantic intent,
not a generic query API, SQL surface, or causal-analysis capability.

## Public operation

REST and MCP expose the same service operation:

```text
POST /v1/compare-metrics
MCP compare_metrics
    -> CompareMetricsService.CompareMetrics
```

The request contains:

- an optional Project ID;
- one through sixteen distinct canonical metric refs;
- one canonical governed time-dimension ref;
- exact baseline and current half-open periods;
- zero through eight distinct canonical non-time dimension refs;
- one shared typed filter set.

Periods use RFC3339Nano instants with an explicit offset and normalize to UTC.
Metrics, dimensions, time dimension, and filter fields MUST resolve to one
model. The time dimension cannot also be a comparison dimension or shared
filter. Metrics and dimensions canonicalize lexicographically.

The request contains no dialect, SQL, DataSource, Backend, Renderer, Driver,
target, timeout, row/byte limit, ordering, top-k, or physical execution option.
Unknown JSON fields fail with `INVALID_QUERY`.

## Compilation and execution

The resolved Project directly supplies its DataSource. `DataSource.type`
selects one Backend, and the exact Renderer instance from that Backend performs
semantic resolution, capability checks, and both renders. No registry relookup
or request override is allowed.

The service constructs exactly two ordinary `query.SemanticQuery` values. They
have identical model, metrics, dimensions, and shared filters; only these
predicates differ:

```text
time_dimension >= period.start
time_dimension <  period.end
```

Both queries pass through the normal Resolver, Planner, optimizer, and compiler.
There is no comparison-specific SQL builder or compiler plan. Their ordered
`OutputSchema` values MUST be identical and contain canonical dimensions
followed by canonical metrics.

Execution is baseline then current under one context timeout and cumulative
deployment `max_rows` and `max_bytes`. The second execution receives only the
remaining budget. Cancellation, limit, compilation, execution, normalization,
stream cleanup, or evaluation failure returns no partial result. One opaque
analysis ID correlates both atomic Runtime calls.

Execution Runtime receives each completed `CompiledQuery` atomically and never
receives the comparison request, SemanticManifest, Resolver, SemanticPlan,
SQLPlan, or Renderer.

## Deterministic evaluator

`analytics/comparison` receives only a comparison descriptor and two normalized
schema/row evidence sets. It has no SQL, database, execution, or semantic
resolution dependency.

Before returning a result it MUST verify:

- baseline/current schemas are identical and match the descriptor;
- dimension columns are typed dimensions with supported scalar datatypes;
- metric columns are Integer or Decimal typed metrics;
- every row width and normalized value representation matches its schema;
- every complete composite dimension tuple is unique within each period;
- a zero-dimension result contains at most one row per period.

String, Integer, Decimal, Float, Boolean, Date, Time, DateTime, and DateTimeTZ
dimension members are supported. Null is a valid tuple member. Integer and
Decimal metric evidence is exact; Float metric evidence is unsupported.
Objects, arrays, opaque values, invalid decimals, and Decimal values decoded
through binary floats fail closed.

Rows are the union of complete baseline/current tuples. Numeric dimension
members compare by mathematical value, so `1.0` and `1.00` are duplicate
identities. Rows sort lexicographically across ordered members; non-null sorts
before null, numeric values sort mathematically, and other scalar values sort by
canonical JSON token.

## Result semantics

The result contains the analysis ID, canonical time dimension and periods,
canonical metric/dimension inventories, and aligned rows. Every row states
`baseline_present` and `current_present`; every value states the canonical
metric ref and nullable decimal-string evidence.

For one aligned metric value:

```text
change_defined  = baseline row/value exists AND current row/value exists
delta           = current - baseline, when change_defined
percent_defined = change_defined AND baseline != 0
percent_change  = delta / baseline * 100, when percent_defined
```

Absent tuples, SQL NULL values, and numeric zero are distinct. Metis never fills
an absent or null metric with zero. A zero baseline has a defined delta but an
undefined percent change. Negative baselines retain their sign. Integer and
Decimal arithmetic uses arbitrary-precision rationals; non-terminating percent
changes use scale 18 with trailing zero removal.

The result is complete evidence of values and mathematical change. It does not
rank movers, attribute contribution, explain causality, or select another
investigation.

## Authorization, errors, and observability

The operation requires shared `semantic:execute` authorization. Compile-only
Projects and Projects without executable DataSources return
`QUERY_EXECUTION_UNAVAILABLE` without guessing placement.

Existing project, semantic, compilation, and execution codes remain
authoritative. Comparison additionally defines:

- `UNSUPPORTED_METRIC_COMPARISON` with `CHANGE_REQUEST` when compiled output
  cannot satisfy the closed exact evidence contract;
- `INCONSISTENT_COMPARISON_RESULT` with `REPORT_DEFECT` when complete normalized
  evidence violates schema, uniqueness, scalar, or evaluation invariants.

One logical operation emits only result, stable error code, duration, executed
query count, cumulative rows, and cumulative bytes. Prometheus uses
`metis_comparison_requests_total`, `metis_comparison_duration_seconds`, and
`metis_comparison_errors_total`. Semantic refs, periods, tuple values, result
values, SQL, Project, DataSource, and analysis ID are never labels.

## Architectural boundary

`compare_metrics` and `attribute_metric` are two concrete analytical verticals
over the same optional Execution Runtime. Their existence does not establish or
authorize a generic Analytics Runtime IR, generic bundle scheduler, evaluator
interface, or arbitrary analytical execution surface.
