# RFC-0067: Governed Dimension Value Discovery

> **Design history:** This RFC preserves its original lifecycle status and rationale.
> Implementation claims describe the recorded design, not a guarantee that every
> proposed API remains current. See [current documentation](../../README.md) for
> supported interfaces. Package paths have been updated for this repository.

- **Status:** Implemented
- **Owners:** Metis maintainers
- **Created:** 2026-09-01
- **Last updated:** 2026-09-02
- **Scope:** `get_dimension_values`, bounded dimension-value execution, Semantic Query orchestration, REST/MCP projection, authorization, errors, observability, AgentBench
- **Supersedes:** None
- **Related:** RFC-0034, RFC-0037, RFC-0044, RFC-0050, RFC-0051, RFC-0056, RFC-0059, RFC-0060, RFC-0066, ADR-0010

## 1. Summary

RFC-0067 proposes `get_dimension_values` as a separate governed,
execution-backed Agent capability for retrieving a bounded set of actual values
for one canonical dimension. Callers may optionally scope the member population
to selected metrics.

```text
get_dimension_values
    -> resolve one canonical dimension and optional metric refs
    -> prove deterministic model/source and optional metric compatibility
    -> construct one existing SemanticQuery
       - no metrics: distinct_values intent
       - metrics: ordinary grouped metric query
    -> compile with the Project DataSource Backend's exact Renderer
    -> execute one atomic CompiledQuery through Execution Runtime
    -> validate and project one normalized dimension column
    -> return bounded typed values and truncation evidence
```

The operation is intentionally distinct from `get_dimension`:

- `get_dimension` reads closed-world metadata from SemanticManifest;
- `get_dimension_values` performs live warehouse I/O and is governed by
  execution authorization, placement, timeout, row, byte, concurrency, and
  cancellation policy.

The operation is also narrower than `query_metrics`. It accepts no SQL,
dialect, DataSource, Renderer, Driver, arbitrary filters, or output-metric
selection. `query_metrics` remains the only generic public semantic execution
capability. `get_dimension_values` is a closed value-discovery workflow.

## 2. External evidence

The dbt MCP Semantic Layer exposes a separate
[`get_dimension_values`](https://github.com/dbt-labs/dbt-mcp/blob/d16db88e4e2c60d44ab0aaa5fcd942c6e5248aeb/src/dbt_mcp/semantic_layer/tools.py#L254-L272)
tool with `dimension`, optional `metrics`, and bounded `limit` inputs. Its
implementation executes the Semantic Layer's dimension-values operation,
removes nulls, applies client-side truncation, and returns `values` plus
`truncated` rather than folding execution into dimension metadata.

MetricFlow itself supports dimension-only queries through a distinct-values
planning path: its
[`CHANGELOG`](https://github.com/dbt-labs/metricflow/blob/main/CHANGELOG.md)
records validation that every query has at least one metric or group-by item,
support for querying time dimensions without metrics, and later explicit
support for queries without metrics. This is a semantic source-selection
capability, not arbitrary table SQL.

Metis adopts the separate-tool boundary, familiar input vocabulary, and optional
metric scoping. It does not copy dbt's string-only result conversion. Metis
already has canonical refs, a typed `distinct_values` query intent, typed
OutputSchema normalization, direct
Project-to-DataSource routing, one-Renderer authority, and a bounded Execution
Runtime; the public contract should preserve those stronger guarantees.

## 3. Motivation

Agents frequently need actual dimension members after selecting governed
semantics:

- determine whether country values are names, ISO-2 codes, or ISO-3 codes;
- discover valid campaign, region, status, channel, or category members;
- avoid guessing capitalization and authored value encodings;
- construct typed `eq`, `in`, and `not_in` filters without compile/execute
  probing;
- distinguish a missing semantic dimension from a valid dimension whose
  current population has no rows.

Today `get_dimension` can describe value conventions through authored context,
but it cannot prove current warehouse values. For a metric-scoped population,
an Agent can approximate the operation with `query_metrics` grouped by the
dimension and then discard metric columns. For the owning dimension population,
Metis can compile a metric-free `distinct_values` query but exposes no governed
Agent execution workflow for it. Both paths otherwise repeat orchestration or
encourage inconsistent limit, ordering, null, and result normalization behavior.

Before Execution Runtime existed, a value tool would have violated Semantic
Core ownership by requiring database connectivity inside discovery. That
constraint has changed: Metis now has a governed execution boundary that
accepts only completed `CompiledQuery { SqlRenderResult + OutputSchema }`
artifacts and already enforces placement, credentials, limits, cancellation,
normalization, and redaction.

The remaining design question is therefore not whether MCP metadata handlers
should query a database. They should not. The question is whether Metis should
add a separate closed application workflow over the existing compiler/runtime
boundary. RFC-0067 answers yes.

## 4. Goals and non-goals

### 4.1 Goals

- expose one public capability named `get_dimension_values`;
- require one canonical dimension ref returned by discovery;
- accept zero through eight canonical metric refs;
- derive the metric-free model and source from the canonical dimension without
  guessing, or prove compatibility with every selected metric;
- support categorical and time dimensions, including valid built-in and custom
  time grains;
- execute exactly one existing semantic query through the existing compiler and
  Execution Runtime;
- return only the selected dimension's normalized typed values;
- use deterministic ascending dimension ordering for the selected Backend and
  data snapshot;
- exclude null through an internal typed semantic predicate;
- default to 100 returned values and reject limits above 500;
- execute with `limit + 1` to report exact truncation evidence;
- use existing `semantic:execute`, Project authorization, runtime error, and
  redaction contracts;
- preserve compile-only deployments and fail explicitly when execution is
  unavailable;
- keep REST and MCP behavior equivalent.

### 4.2 Non-goals

RFC-0067 does not:

- merge live values into `get_dimension`;
- expose arbitrary SQL, `SELECT DISTINCT`, expressions, predicates, dialect,
  DataSource, Backend, Renderer, Driver, endpoint, credentials, or target;
- accept arbitrary filters, a free-form `where`, value substring search,
  pagination cursors, ordering controls, or caller runtime budgets;
- return metric values, generated SQL, physical schema, DataSource identity, or
  execution plans;
- infer synonyms, normalize business codes, translate values, or choose a
  dimension from natural language;
- guarantee that different database collations select an identical bounded
  subset across different Backends;
- cache dimension values or add result caching to Metis;
- add field-level authorization or a new semantic policy schema;
- create a generic discovery execution framework, Analytics Runtime IR, or
  arbitrary semantic-query executor;
- count as a third analytical vertical under ADR-0013.

Filters, value search, and pagination may be proposed independently after
production evidence. They are not accepted implicitly by this RFC.

## 5. Architectural decision

`get_dimension_values` is a closed application workflow owned by
`app/service/semantic.DimensionValuesService`.

```text
REST / MCP
    -> DimensionValuesService
        -> Discovery / SemanticManifest compatibility
        -> Project -> DataSource -> Backend
        -> existing SemanticQuery construction
           - distinct_values when metric-free
           - ordinary grouped query when metric-scoped
        -> CompileService with exact Backend.Renderer
        -> Runner.ExecuteResolved(CompiledQuery)
        -> bounded typed result projection
```

The service owns request normalization, deterministic metric-free source
selection validation, optional metric compatibility validation, one-query
orchestration, result-shape validation, and public projection. It does not own
SQL syntax, source expressions, relationship planning, metric evaluation,
physical placement, credentials, or query execution.

Semantic Core sees one existing `query.SemanticQuery`: metric-free requests use
`QueryIntentDistinctValues`, while metric-scoped requests use the ordinary
grouped-query path. This RFC adds no planner node, compiler branch, or query
intent. Execution Runtime sees only one atomic `CompiledQuery`; it receives no
SemanticManifest, metric refs, dimension refs, or value-discovery request.

REST and MCP call `DimensionValuesService` directly. Neither transport calls
the other, and neither transport constructs SQL or independently interprets
compatibility.

This RFC expands the governed Runner reachability list to:

```text
query_metrics          generic semantic execution
get_dimension_values  closed semantic value discovery
attribute_metric      closed attribution workflow
compare_metrics       closed comparison workflow
```

`query_metrics` remains the only generic entry.

## 6. Public request

The transport-neutral request is:

```go
type DimensionValuesQuery struct {
    ProjectID string           `json:"project_id,omitempty"`
    Dimension string           `json:"dimension"`
    Metrics   []string         `json:"metrics,omitempty"`
    Grain     *query.TimeGrain `json:"grain,omitempty"`
    Limit     *int             `json:"limit,omitempty"`
}
```

### 6.1 Field semantics

`project_id` follows the shared project resolution contract and is omitted when
active MCP project context is configured.

`dimension` is one canonical `dimension:` ref copied from `get_dimensions`.
Bare names, dataset-qualified guesses, physical bucket refs substituted for a
custom-calendar base dimension, and cross-project refs fail closed.

`metrics` is optional and contains at most eight distinct canonical `metric:`
refs copied from `list_metrics`. When present, metrics are semantic population
anchors; their values are evaluated internally but never returned. All metrics
and the dimension must resolve to one model, and the dimension must be
compatible with every metric under the same compatibility authority used by
`get_dimensions`.

When `metrics` is omitted or empty, the canonical dimension ref determines the
model, owning dataset, and source-selection root. The service MUST NOT choose a
model from session recency, catalog order, a similarly named dimension, or an
unqualified physical field. Resolution must produce the same deterministic
metric-free source selection already used by `QueryIntentDistinctValues`; an
ambiguous or unreachable source/path fails before execution.

`grain` is optional. It is rejected for a non-time dimension. For a time
dimension it must be accepted by the selected dimension's ordinary compile
contract. Custom-calendar grains use the canonical base-time dimension plus the
logical grain exposed in `canonical_groupings[].group_by`; a physical bucket is
not accepted as a substitute.

`limit` defaults to 100, accepts values from 1 through 500, and controls the
number of public values. The internal semantic query uses `limit + 1`, so the
largest internal caller row ceiling requested by this workflow is 501.

The dimension must declare one of the stable scalar datatypes supported by
Execution Runtime: String, Integer, Decimal, Float, Boolean, Date, Time,
DateTime, or DateTimeTZ. An absent or Opaque datatype is rejected before
execution because it cannot provide the typed filter evidence promised by this
contract.

Unknown JSON fields, duplicate canonical refs, empty strings, mixed models, and
physical-control fields fail before execution.

### 6.2 Metric-free execution boundary

Omitting metrics deliberately enables one narrow capability that
`query_metrics` does not expose: bounded distinct values for exactly one
canonical dimension. This is not a generic metric-free query endpoint. The
caller cannot select additional columns, filters, joins, source datasets,
expressions, SQL, ordering, or physical controls.

The canonical dimension is both the requested output and the source-selection
authority. Metis uses the existing `distinct_values` intent and its resolver,
planner, Renderer, and OutputSchema contracts. If that existing path cannot
prove one deterministic query, the operation fails closed rather than asking
the Agent to choose a physical source.

## 7. Semantic meaning

For one dimension `D`, the service chooses exactly one of two existing semantic
query shapes.

Metric-free mode:

```text
intent     = distinct_values
metrics    = []
group_by   = [D]
filters    = [D IS NOT NULL]
order_by   = [D ASC]
limit      = requested_limit + 1
```

Metric-scoped mode for selected metrics `M`:

```text
metrics    = M
group_by   = [D]
filters    = [D IS NOT NULL]
order_by   = [D ASC]
limit      = requested_limit + 1
```

Metric-free mode means values present in the dimension's deterministic
Semantic Core source selection. It does not inherit any metric definition,
metric filter, measure population, or fill behavior. The owning dataset and any
required semantic join path come only from resolution of the canonical
dimension inside its model.

Metric definitions, intrinsic metric filters, population ownership,
relationships, temporal validity, fan-out safety, shared-grain planning,
advanced metric semantics, and custom-calendar lowering remain exactly those
of ordinary semantic compilation in metric-scoped mode.
`get_dimension_values` does not reconstruct or simplify a metric expression.

For multiple metrics, compatibility means `D` is valid for every metric. The
member set follows the ordinary multi-metric query's aligned output rows; it is
the union of dimension tuples emitted by any selected metric after normal
Semantic Core alignment and fill behavior. It is not described as the
intersection of physical source rows and must not be implemented by manually
joining raw datasets.

The tool returns current data evidence, not semantic truth. An empty values
array means the governed query produced no non-null members under the current
warehouse snapshot; it does not mean the dimension is undefined.

## 8. Compilation contract

`DimensionValuesService` constructs exactly one existing
`query.SemanticQuery`. In both modes it supplies the resolved project and
model, exactly one canonical dimension with optional grain, one internally
authored typed `is_not_null` dimension filter, one ascending order key for that
dimension, and `limit + 1`.

When metrics are absent, it additionally sets
`Intent: query.QueryIntentDistinctValues` and supplies no metrics. When metrics
are present, it leaves the intent ordinary and supplies the canonical selected
metrics.

The request passes through the existing Resolver, MetricEvaluationPlan,
SemanticPlan builder, optimizer, SQLPlan conversion, and selected Renderer.
There is no public or internal `compile_dimension_values` compiler API.

The Project's DataSource type resolves one Backend. The exact
`Backend.Renderer` instance supplies expression dialect, capabilities, and
physical rendering. The service MUST NOT look up another Renderer by dialect
name and MUST NOT mutate rendered SQL to inject distinctness, ordering, or a
limit after compilation.

Metric-free compilation produces exactly one dimension output column.
Metric-scoped compilation produces the selected dimension followed by internal
metric columns. The service discards metric columns only after execution and
complete schema/row validation. Hidden output roles are not added to the public
SemanticQuery contract by this RFC.

## 9. Execution contract

One opaque `query_id` is created before runtime resolution and attached to the
Runner context using the same mechanism as `query_metrics`.

Project resolution follows:

```text
explicit project
    > configured default_project
    > sole registered project
    > PROJECT_REQUIRED
```

An invalid explicit project never falls back. The resolved Project's direct
DataSource reference and DataSource type select the runtime Backend. Public
input cannot override placement.

The completed `CompiledQuery` is passed atomically to
`Runner.ExecuteResolved`. Runner remains responsible for:

- DataSource admission and queue policy;
- secret resolution and redaction;
- timeout and cancellation;
- row and byte limits;
- Driver lifecycle and stream cleanup;
- OutputSchema-governed value normalization;
- immutable artifact snapshots;
- public execution error mapping.

The workflow may pass a caller row ceiling of `limit + 1`; deployment limits
remain authoritative and the strictest non-zero limit wins. A deployment limit
that cannot admit the requested bounded operation returns the existing
execution-limit error rather than silently reducing the caller's requested
limit.

A Project without an executable DataSource remains valid for discovery and
`compile_sql`. `get_dimension_values` returns
`QUERY_EXECUTION_UNAVAILABLE` and never guesses a dialect or target.

## 10. Result validation and projection

Before returning any values, the service validates:

- the executed OutputSchema is the compiler-produced schema;
- exactly one expected group-by dimension column exists at the expected
  position;
- its public name and grain correspond to the normalized request;
- metric-free output contains no remaining columns;
- metric-scoped remaining columns are exactly the selected internal metrics;
- every row width matches the schema;
- every scalar has already been normalized under its declared logical datatype;
- the dimension column is non-null because the service supplied
  `is_not_null`;
- the ordered dimension member occurs at most once;
- the result contains at most `limit + 1` rows under the caller ceiling.

An impossible null, duplicate member, schema mismatch, unknown value kind, or
row-shape mismatch fails all-or-nothing as a redacted internal/execution error.
Metis does not return a partial values array.

The service keeps the first `limit` normalized dimension values. Presence of a
`limit + 1` row sets `truncated=true`. In metric-scoped mode, internal metric
values are validated by Runner normalization but omitted from the public
result.

## 11. Public result

The result contract is:

```go
type DimensionValuesResult struct {
    QueryID   string           `json:"query_id"`
    Dimension string           `json:"dimension"`
    Grain     *query.TimeGrain `json:"grain,omitempty"`
    DataType  ossie.DataType   `json:"data_type"`
    Values    []any            `json:"values"`
    Count     int              `json:"count"`
    Truncated bool             `json:"truncated"`
}
```

`dimension` is the canonical ref from the normalized request. `grain` echoes
the accepted logical grain. `data_type` comes from the compiled OutputSchema,
not from Driver metadata or inference. `count` equals `len(values)` and is not a
warehouse-wide cardinality count.

`values` use the same normalized JSON domain as Execution Runtime:

- String -> JSON string;
- Integer -> JSON number;
- Decimal -> exact decimal string;
- Float -> finite JSON number;
- Boolean -> JSON boolean;
- Date, Time, DateTime, DateTimeTZ -> normalized string;
- null is absent because the internal semantic filter excludes it.

Metis MUST NOT stringify every value merely to simplify MCP serialization.
Consumers interpret a decimal string or temporal string using `data_type`.

The ascending order is deterministic for the same Project, Backend, semantic
snapshot, data snapshot, and database collation. Different Backends may apply
different string collations and therefore choose a different bounded prefix.
RFC-0067 does not claim cross-Backend bounded-subset identity.

Example:

```json
{
  "query_id": "2e0b6a25c50e4f9392afe1eb2a584fa0",
  "dimension": "dimension:commerce.customers.country_code",
  "data_type": "String",
  "values": ["CN", "DE", "JP", "US"],
  "count": 4,
  "truncated": false
}
```

Custom-calendar example:

```json
{
  "query_id": "31590b336d574063994a50ea0bc43c90",
  "dimension": "dimension:fiscal_dense.calendar.calendar_day",
  "grain": "fiscal_week",
  "data_type": "Date",
  "values": ["2026-01-05", "2026-01-12", "2026-01-19"],
  "count": 3,
  "truncated": false
}
```

## 12. Authorization and data governance

`get_dimension_values` requires the shared `semantic:execute` scope. Compile
permission alone is insufficient. Wildcard scope and Principal propagation
follow the existing authentication contract.

Project authorization applies before DataSource or semantic detail is exposed.
An unauthorized project and an unauthorized semantic asset must fail under the
same non-leaking conventions as existing Agent operations.

Metric-free mode intentionally adds a narrow public execution capability beyond
the metric-required `query_metrics` endpoint. Its authority is bounded to one
canonical dimension, one compiler-proven semantic source selection, fixed
non-null and ascending semantics, and the public value limit. It does not create
a generic metric-free query or arbitrary source-scan capability.

Both modes use `semantic:execute`. A separate scope would not make the returned
warehouse data intrinsically less sensitive, and Metis does not yet have a
field-level policy contract to apply consistently across execution workflows.

Dimension values may still contain sensitive business or personal data. RFC-0067
does not claim that bounded output makes a field safe. Any future field-level or
row-level semantic authorization must be applied by the same canonical query
path and cannot be bypassed by this workflow. Until such policy exists,
deployments grant `semantic:execute` with the same care required for
`query_metrics`.

Actual values MUST NOT appear in:

- logs;
- error details;
- Prometheus labels;
- OpenTelemetry attributes;
- query IDs;
- cache keys or persisted Metis state;
- AgentBench compact trace arguments.

Only the authenticated response body contains values.

## 13. Errors

The workflow reuses stable semantic and execution codes rather than returning a
successful payload containing an `error` string.

Semantic/request failures include:

- `PROJECT_REQUIRED` and existing project lookup/access errors;
- `INVALID_QUERY` for duplicate or excessive metrics, invalid limit, unknown fields,
  grain on a non-time dimension, absent/Opaque dimension datatype, mixed
  models, or forbidden physical controls;
- `METRIC_NOT_FOUND`, `DIMENSION_NOT_FOUND`, and existing ambiguity errors;
- the existing ambiguity/unreachable-path error when metric-free resolution
  cannot prove one deterministic dimension source selection;
- the existing compatibility/unreachable-field error when the dimension is not
  compatible with every selected metric;
- `INCOMPATIBLE_QUERY_GRAIN` for an unsupported or incorrectly addressed time
  grain, including custom-calendar physical-bucket misuse.

Execution failures reuse:

- `QUERY_EXECUTION_UNAVAILABLE`;
- `QUERY_EXECUTION_BUSY`;
- `QUERY_EXECUTION_LIMIT`;
- `QUERY_EXECUTION_TIMEOUT`;
- `QUERY_EXECUTION_CANCELLED`;
- `QUERY_EXECUTION_FAILED`.

Compiler, Runner, Driver, and secret details remain redacted. Schema/result
invariants fail without returning partial values. RFC-0067 introduces no new
generic error family unless implementation proves that an existing stable code
cannot describe an observable caller action.

## 14. REST contract

The authenticated REST route is:

```text
POST /v1/dimension-values
```

It binds `DimensionValuesQuery`, calls `DimensionValuesService`, and returns
`DimensionValuesResult`. It accepts no dialect, SQL, runtime routing, endpoint,
credential, or arbitrary predicate field.

HTTP status mapping reuses the centralized stable semantic/execution error
mapping. REST does not call MCP and does not reinterpret result values.

## 15. MCP contract

The Agent-facing MCP tool is:

```text
get_dimension_values
```

Its input schema mirrors the transport-neutral request:

```json
{
  "dimension": "dimension:commerce.customers.country_code",
  "metrics": ["metric:commerce.revenue"],
  "limit": 100
}
```

Metric-free form:

```json
{
  "dimension": "dimension:commerce.customers.country_code",
  "limit": 100
}
```

The description states that the tool performs bounded live execution and that
the dimension ref must come from `get_dimensions` and optional metric refs must
come from `list_metrics`. It distinguishes owning-dimension population from
metric-scoped population and must not imply that values are static metadata.

MCP annotations are:

```text
readOnlyHint: true
destructiveHint: false
idempotentHint: true
openWorldHint: true
```

Read-only and idempotent describe side effects, not snapshot stability. The
operation performs external warehouse I/O and values may change between calls.

The production MCP surface registers the operation consistently with other
governed execution capabilities. In compile-only deployments a call fails as
execution unavailable; the server does not silently remove the capability based
on a guessed target.

`get_dimension_values` does not replace:

- `get_dimensions`, which proves compatibility and returns canonical refs;
- `get_dimension`, which returns one selected dimension's metadata;
- `query_metrics`, which returns governed metric results.

## 16. Bootstrap and service composition

Bootstrap composes one `DimensionValuesService` from the same authorities used
by `QueryMetricsService`:

```text
DiscoveryService
CompileService
ExecutionProjectResolver
Runner
shared execution authorizer
```

The service receives no independent Renderer registry, DataSource map, secret
resolver, endpoint configuration, or transport client. BackendRegistry and
DataSourceRegistry remain process-owned sole authorities.

Existing constructor compatibility may be migrated in stages, but the normal
production REST and MCP composition must expose identical service behavior.
No transport adapter may construct a fallback service or infer execution
availability from dialect registration.

## 17. Observability and auditing

The operation adds one bounded low-cardinality application/MCP operation name:

```text
get_dimension_values
```

Execution observation uses the opaque query ID, DataSource type, stable result
code, duration, and aggregate row/byte counts already allowed by Runtime. It
does not add project, model, metric, dimension, grain, value, SQL, endpoint, or
credential labels.

Tracing may record stable phase names and error codes. Actual values and
unbounded canonical ref arrays are prohibited as span attributes. Observation
failure never changes semantic compilation or execution results.

No result cache is added. Repeated calls are separately authorized, compiled,
executed, bounded, and observed.

## 18. Agent interaction

The expected flows are:

```text
get_dimensions(model)
    -> get_dimension_values(selected dimension)
    -> construct typed filters
    -> compile_sql or query_metrics

list_metrics
    -> get_dimensions(metrics)
    -> get_dimension_values(selected dimension, optional metrics)
    -> construct typed filters
    -> compile_sql or query_metrics
```

Every step is optional when the Agent already knows the required canonical refs
or authored values. MCP initialization instructions should describe the
capability but MUST NOT prescribe a mandatory call count or require value
lookup before every filter.

The Agent interprets the user's language and chooses whether current members
are needed. Metis resolves refs, proves compatibility, executes the bounded
query, and returns typed evidence. Metis does not select a business value on the
user's behalf.

## 19. Compatibility and versioning

The feature is additive to REST and MCP. Existing semantic assets, compile
requests, execution requests, and DataSource configuration do not change.

Adding the MCP tool changes `tools/list` and therefore requires an AgentBench
PromptVersion bump for new collections. Earlier collections remain historical
evidence and are not comparable across the changed tool surface.

No alias such as `list_dimension_values`, `query_dimension_values`, or a mode on
`get_dimension` is introduced. The public name follows the established dbt MCP
vocabulary and clearly distinguishes live values from dimension metadata.

Rollback removes service/transport registration without changing semantic
assets, compiler contracts, Driver SPI, or stored data because the workflow is
stateless and additive. Once released as a stable public tool, removal requires
normal compatibility review.

## 20. Alternatives

### 20.1 Fold values into `get_dimension`

Rejected. Metadata inspection is closed-world and available in compile-only
deployments; values require authorization, connectivity, admission, execution,
and changing-data semantics. Conditional I/O would make one tool's latency,
errors, annotations, and security depend on deployment state.

### 20.2 Require Agents to use `query_metrics`

Rejected as the sole experience. It is possible but forces every Agent to
construct a grouped metric query, discard metric columns, handle normalization
and truncation, and repeat deterministic work. The closed workflow is common
enough to justify a narrow tool.

### 20.3 Require at least one metric in v1

Rejected. Metis already has a typed `distinct_values` intent, deterministic
metric-free source selection, cross-Renderer conformance, and bounded Execution
Runtime. Requiring a synthetic metric would change the member population,
exclude dimensions without a suitable metric, and diverge from the mature dbt
MCP contract without adding a meaningful physical-execution safeguard.

### 20.4 Generate `SELECT DISTINCT` in the service or MCP adapter

Rejected. It bypasses SemanticQuery resolution, relationship/population
semantics, Renderer authority, OutputSchema normalization, and SQL dialect
extension boundaries. Metis is not a SQL proxy or string-template service.

### 20.5 Add a compiler-specific dimension-values plan

Rejected. Metric-free mode already uses `QueryIntentDistinctValues` and its
typed source-selection node; metric-scoped mode uses one ordinary metric query
grouped by one dimension. No new semantic or physical IR is required.

### 20.6 Return only strings

Rejected. Stringifying Integer, Boolean, Decimal, and temporal values discards
the typed evidence Execution Runtime already guarantees and increases filter
construction errors. `data_type` plus normalized JSON scalars is the stable
contract.

### 20.7 Add filters or value search immediately

Deferred. Typed filters would expand the request and value substring search
would require explicit cross-dialect predicate, escaping, collation, and
parameter semantics. Neither is necessary to establish the bounded core
workflow.

### 20.8 Cache values

Rejected. Values change with warehouse data, and result caching remains outside
Metis's intended responsibilities. A cache would add invalidation, staleness,
authorization, and data-retention concerns unrelated to this RFC.

### 20.9 Add a new `semantic:values` scope

Rejected for v1. Both modes return live warehouse data through the same bounded
Execution Runtime. A separate operation scope would create policy complexity
without reducing the underlying data authority. Future field-level governance
should be designed consistently across all execution workflows.

## 21. Rollout and migration

Implementation proceeds in bounded phases:

1. add transport-neutral request/result types and `DimensionValuesService`;
2. normalize metric-free requests to the existing `distinct_values` intent and
   metric-scoped requests to an ordinary grouped SemanticQuery, both with the
   canonical dimension, internal non-null predicate, ascending order, and
   `limit + 1`;
3. execute through the existing Project/DataSource/Backend/Runner path and add
   strict result projection;
4. compose the service in bootstrap and add shared authorization;
5. expose REST and MCP adapters with transport parity;
6. add bounded observability and error mapping;
7. update current specifications, design, AGENTS.md, and AgentBench protocol;
8. run real Backend conformance before changing RFC status to Implemented.

No semantic manifest migration or DataSource configuration migration is
required. Compile-only deployment remains supported throughout.

The RFC may move from Draft to Accepted only after review confirms both
metric-free source-selection and metric-scoped population semantics, the public
typed-value shape, and the authorization boundary. It may move to Implemented
only after every acceptance criterion below and current-document update is
complete.

## 22. Test and acceptance criteria

### 22.1 Request and semantic validation

- zero through eight canonical metrics are accepted;
- duplicate or excessive metrics, mixed models, bare names, and unknown refs
  fail closed;
- when metrics are present, the dimension must be compatible with every metric;
- when metrics are absent, the canonical dimension must resolve to one
  deterministic model, owning dataset, and source-selection path;
- limit defaults to 100 and accepts only 1 through 500;
- grain is rejected for non-time dimensions;
- absent and Opaque dimension datatypes are rejected before execution;
- valid built-in and custom-calendar grains compile;
- physical custom-calendar bucket misuse returns structured grain remediation;
- unknown fields and physical routing controls are absent/rejected;
- explicit/default/sole Project resolution matches every other Agent entrypoint;
- invalid explicit Project never falls back.

### 22.2 Compilation and Renderer authority

- metric-free mode constructs one existing `distinct_values` SemanticQuery with
  no metrics, exactly one dimension, internal non-null filter, ascending order,
  and `limit + 1`;
- metric-scoped mode constructs one ordinary SemanticQuery with selected
  metrics and the same dimension, filter, order, and limit;
- Semantic Core receives no new dimension-values-specific intent, planner node,
  or compiler mode;
- the Project DataSource Backend's exact Renderer instance is used for
  expression evidence, capabilities, and rendering;
- no same-name Renderer re-lookup occurs;
- generated SQL/parameters and OutputSchema equal direct compilation of the
  normalized internal query in each mode;
- the service does not mutate rendered SQL or reconstruct expressions.

### 22.3 Execution

- one atomic CompiledQuery reaches Runner;
- no DataSource, dialect, Renderer, Driver, target, endpoint, or credentials are
  accepted from public input;
- Project without executable DataSource returns execution unavailable;
- timeout, cancellation, capacity, row limit, byte limit, Driver, normalization,
  and cleanup errors map to stable redacted codes;
- caller row ceiling is `limit + 1` and deployment ceilings remain authoritative;
- no partial values return on any failure;
- query ID correlation matches `query_metrics` behavior.

### 22.4 Result projection

- String, Integer, exact Decimal, finite Float, Boolean, Date, Time, DateTime,
  and DateTimeTZ values preserve Execution Runtime normalization;
- values are not universally stringified;
- null is excluded by the internal semantic predicate;
- empty population returns `values: []`, `count: 0`, `truncated: false`;
- exactly `limit` rows return `truncated: false`;
- `limit + 1` rows return the first `limit` and `truncated: true`;
- result order follows ascending Backend ordering for the selected dimension;
- duplicate members, schema mismatch, wrong row width, unexpected null, and
  unknown normalized objects fail all-or-nothing;
- metric-free OutputSchema contains exactly the selected dimension;
- metric-scoped OutputSchema contains the selected dimension and expected
  internal metrics before projection;
- internal metric columns never appear in the public result.

### 22.5 Authorization, privacy, and observability

- `semantic:compile` without `semantic:execute` is denied;
- metric-free and metric-scoped modes require the same `semantic:execute`
  authorization;
- shared Principal and Project authorization propagate through REST and MCP;
- values do not appear in logs, errors, metrics, traces, compact tool arguments,
  or persisted Metis state;
- MCP annotations are read-only, non-destructive, idempotent, and open-world;
- observations remain low-cardinality and observer failure is non-semantic;
- no cache or background execution is introduced.

### 22.6 Transport parity

- REST `POST /v1/dimension-values` and MCP `get_dimension_values` call the same
  service;
- equivalent requests return equivalent typed results and stable errors;
- MCP project-context inheritance matches other primary tools;
- compile-only production composition exposes explicit execution-unavailable
  behavior;
- the MCP registry exposes `get_dimension_values` exactly once and does not add
  aliases;
- `get_dimension` remains closed-world and never performs execution.

### 22.7 Realistic conformance

- a categorical-code scenario proves that returned values can be copied into a
  typed filter and executed successfully;
- a metric-free fixture proves the existing `distinct_values` path returns the
  owning dimension population without fabricating a metric;
- a high-cardinality fixture proves deterministic bounded truncation;
- a multi-metric fixture proves compatibility and ordinary aligned-row union
  semantics;
- a custom-calendar fixture proves logical-grain values rather than physical
  bucket refs;
- a no-row fixture distinguishes empty population from semantic lookup failure;
- an ambiguous/unreachable metric-free path fixture proves fail-closed source
  selection;
- DuckDB black-box execution proves the complete path;
- every production executable Backend receives equivalent integration coverage
  or a documented CI/environment-specific real-engine gate;
- AgentBench adds a focused smoke scenario and bumps PromptVersion without
  benchmark-specific coaching.

## 23. Documentation updates

Implementation must update current truth, not leave this RFC as the only
description:

- `AGENTS.md`: add `get_dimension_values` to the narrow public execution and
  Runner reachability boundaries;
- `docs/design/interfaces/agent-semantic-mcp.md`: explain metadata versus live
  value discovery and MCP annotations;
- `docs/specs/semantic/agent-semantic-search.md`: add the tool, canonical input
  source, and removal of the current no-value-tool statement;
- `docs/specs/semantic/agent-query-contract.md`: define dual normalization into
  the existing metric-free `distinct_values` and metric-scoped SemanticQuery
  paths plus REST/MCP mapping;
- `docs/specs/operations/runtime-bootstrap.md`: add the closed workflow to
  governed Runner reachability and compile-only behavior;
- authentication/observability specifications if their enumerated operation
  lists change;
- `tests/benchmarks/agent-semantic/README.md` and PromptVersion when the MCP
  surface changes;
- `docs/proposals/README.md`: track Draft, Accepted, and Implemented lifecycle.

If implementation reveals a conflict between this RFC, current specifications,
code, or existing ADRs, the conflict must be reconciled explicitly before the
RFC becomes Implemented.
