# Agent Semantic Query Contract

## Scope

Metis has five related query contracts:

1. `query.SemanticQuery` remains the transport-neutral structured semantic-intent
   contract used by REST validation, explain, `compile_sql`, and `query_metrics`;
2. the primary MCP exposes compact `AgentCompileRequest` and
   `AgentQueryMetricsRequest` DTOs that accept canonical refs from staged
   discovery and map them into the existing `SemanticQuery`;
3. `MetricAttributionQuery` accepts one canonical metric/time dimension, exact
   baseline/current periods, independent canonical dimensions, and shared
   typed filters for the closed `attribute_metric` workflow;
4. `MetricComparisonQuery` accepts canonical metrics, one canonical time
   dimension, exact baseline/current periods, one shared canonical dimension
   grain, and shared typed filters for the closed `compare_metrics` workflow.
5. `DimensionValuesQuery` accepts one canonical dimension, zero to eight
   compatible canonical metrics, an optional logical time grain, and a bounded
   limit for the closed `get_dimension_values` workflow.

The MCP adapter does not create an independent semantic language. Resolver,
Planner, optimizer, and compiler still receive the same canonical
`query.SemanticQuery`.

Natural-language interpretation remains outside Metis Core.

`attribute_metric` contains no dialect, DataSource, target, Renderer, Driver,
limit, ordering, top-k, or raw predicate field. Its periods are exact RFC3339
instants with explicit offsets. Agents select refs and periods, but must not
reconstruct the returned decomposition through query loops, rank across
dimensions, or describe deterministic contribution as causality.

`compare_metrics` has the same physical-control exclusion. It canonicalizes
metrics and dimensions, builds two ordinary semantic queries, and returns exact
values and mathematical change. Agents must not use it as attribution, infer
causality, or coerce absent/null values to zero.

`get_dimension_values` also excludes physical controls, arbitrary filters, and
raw predicates. With no metrics it normalizes to one existing metric-free
`distinct_values` SemanticQuery. With metrics it normalizes to one ordinary
grouped SemanticQuery. Both forms contain exactly one selected dimension, an
internal `is_not_null` filter, ascending order, and an internal `limit + 1` so
the service can return deterministic truncation evidence. The service projects
only the normalized dimension column; metric columns remain internal.

## Primary Agent compile shape

RFC-0044 defines the v1 Agent request:

```go
type AgentCompileRequest struct {
    ProjectID     string              `json:"project_id,omitempty"`
    Dialect       sql.SQLDialect `json:"dialect"`
    Model         string              `json:"model,omitempty"`
    OutputMetrics []string            `json:"output_metrics,omitempty"`
    GroupBy       []AgentGroupByParam `json:"group_by,omitempty"`
    Filters       []query.Filter      `json:"filters,omitempty"`
    OrderBy       []AgentOrderByParam `json:"order_by,omitempty"`
    Limit         *int                `json:"limit,omitempty"`
}
```

`output_metrics` are only metrics that must appear in the result. A metric used
solely as `filters[].field` MUST NOT also be added to `output_metrics`.
Output metric refs come from `list_metrics`; group-by dimension refs come from
`get_dimensions`.

For metric queries the adapter derives the model from canonical refs and fails
closed when refs cross models. Metric-free queries set `model` to the canonical
model ref returned by `list_models`, leave `output_metrics` empty, and may
select dimensions, filters, and ordering. Canonical dimension/filter/order refs
can also establish the model, but an explicit model keeps this path unambiguous.

## Group by

The Agent-facing group-by item follows the established Semantic Layer pattern:

```go
type AgentGroupByParam struct {
    Name  string           `json:"name"`
    Type  AgentGroupByType `json:"type"`
    Grain *query.TimeGrain `json:"grain,omitempty"`
}
```

Supported v1 types are `dimension` and `time_dimension`. Time dimensions may
carry a typed grain. Entity grouping is not exposed until Metis has an entity
semantic query type.

`get_dimensions` is authoritative for the dimension `ref` and `type`, which
map directly to `AgentGroupByParam.Type`. For a selected time dimension,
`get_dimension` returns the `valid_grains` accepted by compile. Its optional
`semantic_evidence` reports
material query behavior such as `preserves_empty_periods` for specific custom
calendar grains without exposing planner flags or physical SQL details.

For custom calendars, the canonical semantic group is always the authored base
time dimension plus the requested custom grain. `get_dimensions` and
`get_dimension` expose this as `canonical_groupings[].group_by`; the associated
`physical_bucket` explains lowering but is not a valid substitute for the
group-by name. The compile adapter validates the supplied type and grain against
the selected dimension. A mismatched custom bucket returns
`INCOMPATIBLE_QUERY_GRAIN` with `details.remediation.group_by` containing the
complete corrected item; it never guesses or silently rewrites the request.

## Filters

Metis intentionally retains the existing typed `query.Filter` contract rather
than adding a dbt-specific free-form `where` grammar solely for MCP symmetry.

The JSON representation of `Filter.value` preserves natural JSON values:

- string, number, and boolean scalars are accepted;
- `null` is representable for operators whose semantic validation permits it;
- a flat array of scalar or null values is accepted for set/range operators;
- JSON objects and nested arrays are rejected.

Operator-specific arity and nullability remain Resolver validation
responsibilities.

The MCP input schema enumerates the supported operator vocabulary directly:
`eq`, `neq`, `gt`, `gte`, `lt`, `lte`, `in`, `not_in`, `between`, `is_null`,
and `is_not_null`. `between` is inclusive and takes a two-item array; `in` and
`not_in` take arrays; null operators omit `value`. Dates and timestamps use
ISO-8601 strings. Multiple filter objects are combined with `AND`.

Canonical metric/dimension refs used as filter fields are normalized by the MCP
adapter before Resolver receives the existing `SemanticQuery`.

## Order by

The Agent-facing sort item is:

```go
type AgentOrderByParam struct {
    Name       string `json:"name"`
    Descending bool   `json:"descending,omitempty"`
}
```

The adapter maps it to the existing typed `query.OrderBy` direction enum.
Array order is sort precedence, so a descending metric followed by an ascending
dimension expresses deterministic tie-breaking without compile probing.

## Renderer and runtime placement ownership

Runtime placement is not part of Agent semantic intent. Compile-only dialect
selection is an explicit physical-output parameter, not a semantic field.

The primary MCP request does not expose:

```text
execution_binding
engine
CompileTarget
endpoint
data_source
renderer
driver
```

`compile` accepts an explicit SQLDialect at its physical compile boundary.
That dialect selects one Renderer exactly once; the same Renderer identity is
used for expression evidence, capabilities, and final rendering.

Runtime callers do not override SQLDialect, Renderer, Driver, DataSource, or
target. Project resolution selects the root Project Registration. The resolved
semantic model then selects one source from the Deployment's applied DataSource
set; a sole applied source is inferred. That DataSource selects Backend through
`DataSource.type`. Agent requests never contain a source selector.

When a decoded non-null result cannot satisfy its compiled logical datatype,
`query_metrics` returns `QUERY_RESULT_SCHEMA_MISMATCH` rather than the generic
execution-failed code. Its bounded details identify
`stage=result_normalization`, the semantic output `column`,
`expected_datatype`, a closed `observed_family`, and `retryable=false`. The
Agent must not retry the same request; no result value, concrete Driver type,
physical SQL, or raw Driver error is exposed.

A query-level `target` or `execution_binding` object remains rejected rather
than silently competing with these authorities. Project and service-level
`ExecutionBinding` fields have been removed.

## Validate Query

`CompileService.Validate` continues to accept the internal transport-neutral
`CompileRequest`. Validation runs the shared Renderer selection, Resolver,
Planner, and semantic-plan validation path and stops before physical SQL
rendering.

Validation remains a lower-level REST/admin capability. The primary MCP does
not require `validate_query`; `compile` already crosses the
authoritative validation/resolution/planning boundary.

## Structured repair evidence

Validation may enrich supported failures with deterministic lower-level
candidate evidence. Primary compile repair actions, however, must point to
registered primary MCP tools.

Metric repair:

```json
{
  "repair": {
    "tool": "list_metrics",
    "arguments": {
      "project_id": "finance",
      "search": ["revenu"]
    }
  }
}
```

Dimension/field repair for metric queries:

```json
{
  "repair": {
    "tool": "get_dimensions",
    "arguments": {
      "project_id": "finance",
      "metrics": ["metric:sales.revenue"],
      "search": ["region"]
    }
  }
}
```

Dimension/field repair for metric-free queries uses the model anchor instead:

```json
{
  "repair": {
    "tool": "get_dimensions",
    "arguments": {
      "project_id": "finance",
      "model": "model:sales",
      "search": ["customer_tier"]
    }
  }
}
```

Repair reuses the failed semantic subject. Metis does not perform synonym
expansion, NL interpretation, hidden search, semantic replacement, or automatic
retry before returning the action.

## Validate / Explain / Compile consistency

Validate, Explain, and Compile continue to share the same internal semantic
preparation boundary:

```text
Compile success => Validate success
Explain success => Validate success
```

For failures at the same boundary, stable semantic codes and authoritative
details must agree. Explain exposes semantic evidence only; Compile alone
performs physical SQL compilation.

## Transport mapping

REST validation/explain/`compile-sql` bind `service.CompileRequest`.
`query-metrics` binds `service.QueryMetricsRequest`, which intentionally has no
dialect or physical placement field. Primary MCP `compile_sql` binds
`service.AgentCompileRequest`; `query_metrics` binds
`service.AgentQueryMetricsRequest`. Both immediately normalize to the same
internal semantic query before processing.

The MCP adapter is therefore limited to:

```text
canonical ref normalization
model derivation from metric refs
group_by/order_by representation mapping
removal of Agent control over physical target selection
```

It MUST NOT implement independent semantic resolution, relationship selection,
filter semantics, planning, or SQL generation.

REST `attribute-metric` and MCP `attribute_metric` bind the same
`MetricAttributionQuery` and call `AttributeMetricService` directly. The
compiled attribution bundle is not a public transport contract.

REST `compare-metrics` and MCP `compare_metrics` bind the same
`MetricComparisonQuery` and call `CompareMetricsService` directly. The two
compiled period queries and normalized evidence inputs are not public transport
contracts.

REST `dimension-values` and MCP `get_dimension_values` bind the same
`DimensionValuesQuery` and call `DimensionValuesService` directly. The service
proves canonical identity, datatype, model/source determinism, and optional
metric compatibility before constructing an existing `SemanticQuery`. It
selects the resolved model's DataSource Backend and its exact Renderer once, executes
one atomic `CompiledQuery`, strictly validates schema and rows, and returns
typed values without SQL or internal metric columns.

## Determinism

Equivalent staged identities against the same SemanticManifest must normalize to
equivalent internal `SemanticQuery` values. Input ordering of metrics, group-by
items, filters, and order-by keys is preserved unless an existing semantic
contract explicitly normalizes it.

Diagnostic identity and repair actions must also be deterministic.

## Compatibility and extension rules

New Agent query capabilities should map to typed semantic fields. They must not
introduce arbitrary `where_sql`, `raw_predicate`, free-form ordering SQL,
embedded physical targets, or other rendered-SQL escape hatches.

If Metis later adopts a stable semantic predicate language analogous to a
Semantic Layer `where` expression, it requires an explicit contract/RFC and a
single canonical parser path; it must not coexist as an accidental second
filter semantics beside `query.Filter`.
