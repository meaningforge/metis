# RFC-0050: Metric-free Agent discovery and sufficient semantic evidence

> **Design history:** This RFC preserves its original lifecycle status and rationale.
> Implementation claims describe the recorded design, not a guarantee that every
> proposed API remains current. See [current documentation](../../README.md) for
> supported interfaces. Package paths have been updated for this repository.

- **Status:** Implemented
- **Owners:** Metis maintainers
- **Created:** 2026-08-27
- **Last updated:** 2026-08-27
- **Scope:** `app/service/semantic`, `app/mcp`, Agent semantic specifications, AgentBench
- **Supersedes:** the metric-mandatory routing portions of RFC-0044
- **Related:** RFC-0019, RFC-0044, RFC-0049

## Summary

Preserve the then-current four-tool MCP surface while supporting both valid query shapes:

> **Current interface refinement:** RFC-0051 preserves metric-free compilation
> but replaces the fixed-four-tool payload with `list_models`, compact
> `get_dimensions`, selected `get_dimension`, and `get_relationships`.

```text
metric query:
  list_metrics -> get_dimensions(metrics) -> compile(output_metrics)

metric-free query:
  list_projects -> get_dimensions(model) -> compile(model, output_metrics=[])
```

Discovery returns enough compact authored evidence for the Agent to choose the
next typed request without probing `compile`: canonical refs, model and dataset
ownership, data type, labels/descriptions, AI context, primary-key status,
time-grain capabilities, and focused relationship semantics.

## Evidence

AgentBench exposed two contract failures. A filtered derived metric was added to
the selected metrics and therefore incorrectly became a result column because
the request did not distinguish output and filter roles. A temporal dimension
query could not be expressed through the primary MCP because compile required a
metric, even though the core semantic query/compiler already supported zero
metrics. The Agent consequently searched unrelated metrics and retried until it
exhausted its tool budget.

The dbt MCP surface independently separates metric, dimension, entity, compiled
SQL, and query operations. MetricFlow's query semantics distinguish selected
metrics, dimensions/entities, metric filters, and query filters. Cube's official
Meta API and AI-context guidance treat semantic metadata, descriptions, and
authored AI context as inputs to Agent reasoning. Metis adopts the useful
discovery principles without copying execution tools or expanding beyond its
compile-only boundary.

References:

- <https://github.com/dbt-labs/dbt-mcp>
- <https://github.com/dbt-labs/dbt-core/blob/main/crates/dbt-metricflow/docs/metric-semantics.md>
- <https://docs.cube.dev/docs/introduction>
- <https://docs.cube.dev/docs/data-modeling/ai-context>

## Decisions

### Explicit result-column role

`AgentCompileRequest.output_metrics` contains only metrics that must appear in
the output schema. Metrics used solely in filters stay in `filters[].field`.
The ambiguous `metrics` input is removed because this interface was unpublished.

### Metric-free compile

`AgentCompileRequest.model` is accepted as a canonical model anchor and
`output_metrics` may be empty. The model may also be mechanically derived from
canonical dimension/filter/order refs. All selected refs must resolve to one
model, and execution binding resolution remains unchanged.

### Two deterministic dimension scopes

`get_dimensions` accepts exactly one anchor:

- `metrics` returns the compatibility intersection for a metric query;
- `model` returns model-scoped dimensions for a metric-free query.

Model-scoped results include only relationships touching the returned
dimensions' datasets. Temporal relationships expose point-in-time validity
fields and cardinality as semantic evidence, never physical SQL or a chosen
join plan.

### Authored reasoning metadata

Model, metric, and dimension records expose canonical refs, descriptions, and
authored Ossie AI context. Metrics and dimensions also expose data type and
model ownership; dimensions expose dataset, label, and primary-key status.
Search includes the same authored context. Metis does not invent synonyms,
sample values, relevance scores, or natural-language interpretations.
Metric search deliberately excludes model-level description/AI context from
matching so one model-level term cannot return every metric in that model.
Authored dimension descriptions may state stable value encodings, such as ISO
country-code conventions, when a user phrase cannot otherwise be translated to
the stored semantic value. Both raw-asset and MCP paths receive that same
authored fact; Metis does not inspect warehouse values to infer it.

### Repair follows query shape

Dimension failures in metric queries repair through
`get_dimensions(metrics, search)`. Dimension failures in metric-free queries
repair through `get_dimensions(model, search)`. Neither path retries compile
automatically.

## Acceptance criteria

- A filter-only metric does not appear in compiled output columns.
- A zero-metric temporal relationship query is discoverable and compilable via
  the four primary tools.
- Temporal discovery exposes the authored event-time and validity dimensions.
- Model, metric, and dimension records expose the bounded metadata described
  above through both service and MCP paths.
- MCP schemas expose `output_metrics`, omit the old compile `metrics`, and do
  not require metrics for `get_dimensions`.
- Metric-free dimension failures return a directly callable model-scoped repair.
