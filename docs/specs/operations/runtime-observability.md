# Runtime observability

This specification defines the implemented operator-facing runtime observation
surface. It is subordinate to Metis semantic behavior: observation failure must
not change a response, plan, physical query, or stable error contract.

## Enablement and ownership

`metis serve --metrics` enables Prometheus exposition at `GET /metrics` on the
shared HTTP listener. It is disabled by default and is outside customer Bearer
authentication. Deployment networking must restrict operator access.

Metis uses a private Prometheus registry with the standard Go and process
collectors. It does not mutate Prometheus or OpenTelemetry global providers.
Core services emit typed observations through `app/observability`; they do not
invoke Prometheus collectors directly.

## Compile metrics

One call to the shared `CompileService.Compile` records exactly one logical
compile operation, regardless of whether REST or MCP invoked it:

```text
metis_compile_requests_total{dialect,result}
metis_compile_duration_seconds{dialect}
metis_compile_errors_total{dialect,code}
```

`result` is `success` or `error`. `code` is a registered stable Metis error code
and exists only for failed compiles. Unknown errors fail closed to
`INTERNAL_ERROR`.

The bounded dialect values are `DUCKDB`, `DORIS`, `CLICKHOUSE`, and
`unresolved`. `unresolved` represents a failure before target selection or an
unsupported configured dialect; raw configuration never becomes a label.

## Compile pipeline metrics

Physical compiles expose non-overlapping lifecycle timing owned by the normal
single compile path:

```text
metis_compile_phase_duration_seconds{phase,dialect}
metis_optimizer_runs_total{outcome}
```

The closed phase values are `analysis`, `planning`, `optimization`, and
`rendering`. Analysis covers target-aware semantic resolution and extension
capability validation. Planning covers typed `SemanticPlan` construction.
Optimization covers pre-validation, the configured node-native optimizer, and
post-validation. Rendering covers output-schema construction, SQLPlan lowering,
target validation, and dialect rendering.

Optimizer outcome is `rewritten` when the post-optimization plan differs from
the planner-produced input after excluding `OptimizationTrace`; otherwise it is
`noop`. The comparison uses the plan values already present in the normal path.
It does not invoke another planner, optimizer, compiler, or renderer.

Validate and Explain reuse semantic planning but are not physical compile
operations, so they do not emit compile-phase metrics or spans. Project/model
loading currently occurs at startup rather than on the request path and has no
request-time model-load metric.

## Physical execution metrics

Every Runner execution attempt, including the physical queries used by closed
analytical workflows, exposes:

```text
metis_execution_requests_total{backend,result}
metis_execution_duration_seconds{backend}
metis_execution_errors_total{backend,code}
```

`backend` is one of `duckdb`, `doris`, `clickhouse`, or `unresolved`.
`result` is `success` or `error`; `code` is one of Runner's closed internal
execution codes and exists only for failures. Typed observations also carry
aggregate row and byte counts for non-label sinks. Query ID, Project,
DataSource identity, SQL, semantic refs, result values, concrete Driver types,
and raw errors are never metric labels or default span attributes.

The same minimization applies to every resilience failure stage. Connection
endpoints and embedded credentials, resolved SecretRef values, bound SQL
parameters, partially decoded row values, vendor error strings, and cleanup
errors are not retained in `ExecutionError` or `ExecutionObservation`.
Equivalent acquisition, execution, and stream faults project to the same closed
Runner code regardless of backend. Public error code and caller action are
then derived deterministically by the semantic service; backend text cannot
change either projection.

## Transport metrics

Registered REST operations expose:

```text
metis_http_requests_total{route,method,status_class}
metis_http_request_duration_seconds{route,method}
```

`route` is the Gin route template, never the request path. Methods are the
registered `GET` and `POST` values. Status class is one of `2xx`, `4xx`, or
`5xx`. Requests rejected before the authenticated Agent route boundary and the
operator `/metrics` route do not contribute to these metrics.

Registered MCP tool calls expose:

```text
metis_mcp_requests_total{method,result}
metis_mcp_request_duration_seconds{method}
```

`method` is one of the finite `metis_*` tool names registered by the server.
Protocol operations such as initialization and tool listing are not semantic
tool calls and do not contribute to these metrics.

The transport-neutral attribution service exposes one logical operation even
when its bundle executes several physical queries:

```text
metis_attribution_requests_total{result}
metis_attribution_duration_seconds
metis_attribution_errors_total{code}
```

Its typed observation also carries aggregate query, row, and byte counts for
non-label sinks. Semantic refs, periods, values, SQL, and DataSource identity
are never labels or default span attributes.

The transport-neutral comparison service likewise emits one logical operation
for its baseline/current pair:

```text
metis_comparison_requests_total{result}
metis_comparison_duration_seconds
metis_comparison_errors_total{code}
```

Its typed observation carries only aggregate query, row, and byte counts in
addition to result, duration, and stable code. Metric/dimension refs, periods,
tuple values, result values, analysis ID, SQL, Project, and DataSource identity
are never labels or default span attributes.

## Project authorization metrics and audit

Every shared project/action decision exposes:

```text
metis_project_authorization_decisions_total{action,effect,reason}
```

`action` is one of `discover`, `compile`, `execute`, `author`, `publish`,
`activate`, or `admin`. `effect` is `allow` or `deny`; `reason` is one of the
closed reasons owned by the project authorization contract. Unknown action,
effect, or reason values are rejected by typed observation validation.

The JSON audit log may additionally contain tenant, subject, API-key identifier,
and Project identity. Those fields never become Prometheus labels. The audit
contract excludes bearer/API-key values, Principal scopes, semantic request
payloads, SQL, credentials, and free-form policy-engine messages.

This operator signal does not expand the Agent-facing MCP protocol. Metis adds
no observability, metrics, or tracing tool; `tools/list` is unchanged, and tool
results and shared error payloads contain no trace IDs, span IDs, exception
events, stack traces, or internal metric fields.

## Trace trajectory and exceptions

The runtime extracts W3C Trace Context at the authenticated HTTP boundary.
REST requests create a `metis.http` span; MCP tool calls create a `metis.mcp`
span. A physical compile creates one `metis.compile` child span in the shared
service, so transport adapters cannot double-count it.

`metis.analysis`, `metis.planning`, `metis.optimization`, and `metis.rendering`
are sibling children of `metis.compile`, matching the phase metrics above.

An escaping error records a stable `metis.error.code`, sets span status to
`Error`, and emits an OpenTelemetry exception event containing type, message,
and Go stack trace. Exception detail is trace data only. It must never become a
metric label.

Tracing is disabled by default. `metis serve --otlp-traces` enables the
OTLP/HTTP exporter; standard `OTEL_EXPORTER_OTLP_*` environment variables select
the endpoint, headers, TLS, and compression. `--trace-sample-ratio` selects the
root sampling ratio from `0` to `1`, while parent-based sampling preserves an
incoming W3C sampled decision. The process flushes the batch exporter within the
configured graceful-shutdown timeout.

Because exception events can contain dynamic messages and stack traces,
operators must control collector access, sampling, transport security,
retention, and redaction. Metis does not set the OpenTelemetry global provider.

## Data minimization

Custom metric labels and default span attributes must not contain project,
tenant, principal, model, metric, dimension, semantic request, SQL, plan,
fingerprint, raw URL path, request/trace ID, credential, raw error message, or
extension payload values. Closed observation validation rejects values outside
the registered dialect, route, method, result, status, and error-code sets.

## Cardinality budget

The custom metrics have a finite label product. An exhaustive private-registry
scrape test emits every code-owned combination and locks the following label-set
budget before process/replica multiplicity:

| Metric | Label-set budget |
| --- | ---: |
| `metis_compile_requests_total` | 4 dialects × 2 results = 8 |
| `metis_compile_errors_total` | 4 dialects × 68 registered stable error codes = 272 |
| `metis_compile_duration_seconds` | 4 dialects = 4 |
| `metis_compile_phase_duration_seconds` | 4 phases × 4 dialects = 16 |
| `metis_optimizer_runs_total` | 2 outcomes = 2 |
| `metis_http_requests_total` | 17 registered route/method pairs × 3 status classes = 51 |
| `metis_http_request_duration_seconds` | 17 registered route/method pairs = 17 |
| `metis_mcp_requests_total` | 15 registered methods × 2 results = 30 |
| `metis_mcp_request_duration_seconds` | 15 registered methods = 15 |
| `metis_attribution_requests_total` | 2 results = 2 |
| `metis_attribution_duration_seconds` | 1 unlabeled operation = 1 |
| `metis_attribution_errors_total` | 68 registered stable error codes = 68 |
| `metis_comparison_requests_total` | 2 results = 2 |
| `metis_comparison_duration_seconds` | 1 unlabeled operation = 1 |
| `metis_comparison_errors_total` | 68 registered stable error codes = 68 |
| `metis_execution_requests_total` | 4 backends × 2 results = 8 |
| `metis_execution_duration_seconds` | 4 backends = 4 |
| `metis_execution_errors_total` | 4 backends × 12 execution codes = 48 |
| `metis_project_authorization_decisions_total` | 7 actions × 2 effects × 9 reasons = 126 |
| `metis_semantic_activation_requests_total` | 1 success + 7 redacted failure values = 8 |
| `metis_semantic_activation_duration_seconds` | 1 unlabeled operation = 1 |

The table counts label sets. Prometheus histogram exposition adds the configured
bucket, sum, and count series for each histogram label set. Route, MCP method,
dialect, phase, or stable error-code additions therefore require an intentional
budget update and test change. Custom metric cardinality scales with the Metis
code surface, never tenants, projects, models, queries, or request volume. The
current twenty-one custom metric families produce 708 label sets in an exhaustive
scrape. Histogram label sets expand through the Prometheus default buckets; the
test locks that bucket choice as well as the label products.

## PromQL reference

These expressions are starting points for dashboards and recording rules. The
window and aggregation scope must match the deployment's traffic and replica
labels.

Successful compile throughput:

```promql
sum(rate(metis_compile_requests_total{result="success"}[5m]))
```

Compile error ratio and stable error-code breakdown:

```promql
sum(rate(metis_compile_requests_total{result="error"}[5m]))
/
clamp_min(sum(rate(metis_compile_requests_total[5m])), 1e-9)

sum by (dialect, code) (rate(metis_compile_errors_total[5m]))
```

Compile and phase p95 latency:

```promql
histogram_quantile(
  0.95,
  sum by (le, dialect) (rate(metis_compile_duration_seconds_bucket[5m]))
)

histogram_quantile(
  0.95,
  sum by (le, dialect, phase) (rate(metis_compile_phase_duration_seconds_bucket[5m]))
)
```

Compile duration intentionally has no `result` label, so its percentile
includes successful and failed attempts. Operators must not add a result label
ad hoc; success-only latency would be a future contract and cardinality
decision.

REST 5xx rate, MCP error ratio, and optimizer activity:

```promql
sum by (route, method) (rate(metis_http_requests_total{status_class="5xx"}[5m]))

sum by (method) (rate(metis_mcp_requests_total{result="error"}[5m]))
/
clamp_min(sum by (method) (rate(metis_mcp_requests_total[5m])), 1e-9)

sum by (outcome) (rate(metis_optimizer_runs_total[15m]))
```

## Alert suitability

The following signals are suitable inputs to availability and latency alerts,
after the deployment defines traffic floors, evaluation windows, and SLOs:

- scrape `up` for exporter/process reachability;
- REST `5xx` rate for the registered Agent HTTP boundary;
- stable internal compile error codes such as `INTERNAL_ERROR` and
  `INTERNAL_INVARIANT_VIOLATION` for semantic-runtime faults;
- total compile latency percentiles for service responsiveness.

Total compile/MCP error ratios also contain caller-repairable semantic failures,
so they are useful product-health signals but are not availability paging
signals by themselves. Phase latency and optimizer outcomes diagnose a slow or
unexpected pipeline; they are not standalone SLOs. This contract deliberately
does not prescribe thresholds or paging policy.
