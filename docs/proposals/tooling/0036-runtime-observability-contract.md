# RFC-0036: Runtime Observability Contract

> **Design history:** This RFC preserves its original lifecycle status and rationale.
> Implementation claims describe the recorded design, not a guarantee that every
> proposed API remains current. See [current documentation](../../README.md) for
> supported interfaces. Package paths have been updated for this repository.

- **Status:** Implemented
- **Owners:** Metis maintainers
- **Created:** 2026-08-22
- **Last updated:** 2026-08-25
- **Scope:** server runtime, REST/MCP transport boundaries, compile service, semantic analysis, planner, optimizer, SQL renderer, model loading, Prometheus exposition, structured diagnostics
- **Supersedes:** None

## Summary

Metis exposes a small, stable, low-cardinality runtime observability contract
before adding broad telemetry instrumentation.

The first implementation target is Prometheus metrics. The contract answers four
operational questions:

1. **Is Metis receiving and successfully compiling requests?**
2. **Where is compile time spent?**
3. **Which bounded failure class is responsible when compilation fails?**
4. **Is the semantic optimizer rewriting plans in real workloads, without changing
   compilation semantics or running a second compile only for telemetry?**

The metrics surface is intentionally not a semantic-query audit log. Metric names
and labels MUST describe bounded operational dimensions. Customer-, model-, and
query-specific identity belongs in controlled structured diagnostics rather than
Prometheus labels. Trace correlation is permitted, but semantic request identity
and contents remain excluded from default span attributes.

The metrics contract uses the `metis_` namespace and a Prometheus exposition
endpoint. The same closed operation vocabulary also defines OpenTelemetry spans
so an operator can follow one failing request through transport and semantic
compilation and inspect its exception event.

The observation architecture is informed by MetricFlow's typed event/reporter/
handler separation; see the
[MetricFlow observability research note](../../research/metricflow-observability.md).
Metis deliberately observes closed semantic-runtime operations rather than
arbitrary package functions. Metric observations carry no client identity,
semantic request, SQL, plan, fingerprint, or exception text. Exception detail
is isolated to sampled OpenTelemetry traces.

## Motivation

Metis now has strong build-time and test-time correctness evidence: canonical
semantic scenarios, deterministic plan and SQL fingerprints, optimizer
optimized-vs-unoptimized differential execution, and real-engine execution on
concrete SQL targets.

Those mechanisms answer questions such as:

- did a code change alter a plan or SQL unexpectedly?
- does optimized execution preserve results?
- does generated SQL execute correctly on a supported physical engine?

They do not answer production questions such as:

- are compile errors increasing?
- is DuckDB compilation slower than Doris compilation?
- is semantic analysis or SQL rendering responsible for a latency increase?
- how often does the optimizer rewrite plans in actual workloads?
- is model loading becoming a significant part of request latency?

Without a runtime contract, instrumentation tends to grow organically around
individual incidents. That produces unstable metric names, high-cardinality
labels, duplicated timers, and accidental leakage of semantic/query identity into
monitoring systems.

RFC-0036 defines the boundary before implementing it.

## Decision

The following rules are normative.

1. **Runtime metrics MUST use bounded dimensions.** A label whose value can grow
   with tenants, users, projects, models, metrics, queries, files, or arbitrary
   error text is not a valid Prometheus label.
2. **Core compiler instrumentation MUST be transport-neutral.** REST and MCP may
   expose their own request metrics, but compile/planner/optimizer/render metrics
   belong to the shared service/compiler path so the same compilation is not
   counted differently by transport.
3. **Telemetry MUST NOT change compilation semantics or materially duplicate
   compilation work.** In particular, runtime optimizer metrics MUST NOT run an
   extra unoptimized compile in order to decide whether optimization happened.
4. **Error metrics MUST classify errors by stable bounded code/class, never by
   raw message.** Error messages remain diagnostic text, not metric identity.
5. **Prometheus exposition MUST be operator-controlled.** The metrics endpoint
   MUST be possible to disable and MUST NOT require customer API credentials or
   contribute to application request accounting. Deployment networking or an
   operator-facing listener is responsible for restricting access.
6. **Metrics are an operational API.** Renaming a metric, changing label meaning,
   or introducing an unbounded label requires deliberate review rather than being
   treated as an internal refactor.
7. **Core code emits typed observations, not Prometheus calls.** Compile,
   compile-phase and optimizer observations flow through an injected sink.
   Prometheus is one adapter over that boundary, and an in-memory sink is used
   for deterministic tests.
8. **Escaping errors MUST remain traceable.** A failing transport or compiler
   span records the stable semantic error code and an OpenTelemetry exception
   event. When full exception capture is enabled, the event follows the stable
   `exception.type`, `exception.message`, and `exception.stacktrace` conventions.
   The span status is set to `Error`; recording the event alone is insufficient.

## Observation model

RFC-0036 uses a closed, code-owned observation vocabulary. Observation kinds
and their payload variants are versioned runtime contracts. The initial variants
cover compile completion, compile-phase completion, optimizer outcome, transport
completion, and model-load completion.

An observation destined for metrics may contain only bounded operational fields
required by this RFC: selected dialect, closed result/outcome enum, stable
semantic error code, closed phase/method/route identity, status class, and
duration. It MUST NOT contain the semantic query, project/model/metric names,
raw SQL, plan structures or fingerprints, extension payloads, Principal
identity, request/trace ID, raw errors, stack traces, host fingerprints,
MAC-derived identity, or credentials.

Trace spans use the same closed operation names but are a separate high-detail
signal. W3C Trace Context is propagated across the HTTP boundary. Trace
attributes remain bounded and exclude semantic request bodies, SQL, plans,
fingerprints, credentials, and Principal identity. Exception events may contain
the dynamic error type, message, and Go stack trace because diagnosis is their
purpose; operators must control tracing enablement, sampling, exporter access,
retention, and redaction at this boundary. Exception text never becomes a
Prometheus label.

Instrumentation records the operation already happening in the normal pipeline.
It must complete observations on both success and failure, but observation
failure must never change compilation behavior. Sink fan-out, export, and
in-memory capture belong to the observability package rather than semantic
packages.

## Metric model

### Compile RED metrics

The initial compile surface SHOULD expose:

```text
metis_compile_requests_total{dialect,result}
metis_compile_duration_seconds{dialect}
metis_compile_errors_total{dialect,code}
```

`dialect` is bounded by the registered physical target set, currently:

```text
duckdb
doris
clickhouse
unresolved
```

`unresolved` is the bounded fallback for failures before target selection and
for unsupported configured dialect values. Raw configuration never becomes a
label.

`result` is a bounded enum such as `success` or `error`.

`code` is the stable Metis error code presented by the shared domain/service
contract. Raw error messages MUST NOT be labels.

The request counter and duration MUST describe one logical compile operation.
Transport retries or wrapper calls must not accidentally double-count the same
service invocation.

### Compile-phase latency

Metis SHOULD expose one histogram with a bounded phase label:

```text
metis_compile_phase_duration_seconds{phase,dialect}
```

The initial phase vocabulary is:

```text
analysis
planning
optimization
rendering
```

A phase label is preferable to four separately named histograms because the
phases form one bounded pipeline and should be comparable in the same dashboard.
The vocabulary MUST remain closed and code-owned; arbitrary function or package
names MUST NOT become phase values.

Phase duration is diagnostic evidence, not a requirement that the sum of phases
exactly equal total compile duration. Service orchestration, validation, model
lookup, allocations, and other overhead may exist outside the timed phases.

### Optimizer activity

The runtime SHOULD expose:

```text
metis_optimizer_runs_total{outcome}
```

with the bounded outcomes:

```text
rewritten
noop
```

This metric answers whether the optimizer changed the `SemanticPlan` presented to
later compilation phases.

The classification is defined at the plan layer, not by emitted SQL. A plan
rewrite may legitimately render to byte-identical SQL, so SQL text is not valid
evidence that the optimizer was idle.

Runtime instrumentation MUST observe state already present in the normal compile
pipeline. It MAY compare the pre-optimization and post-optimization plan using an
existing deterministic plan fingerprint or a cheaper equivalent change signal,
but it MUST NOT invoke a second planner/compiler path solely for telemetry.

The test corpus remains the place for optimized-vs-unoptimized differential
proof. Runtime metrics are observation, not a replacement for that proof.

### Model loading

If model/project loading occurs on the request path, the initial operational
surface SHOULD expose:

```text
metis_model_load_total{result}
metis_model_load_duration_seconds
```

These metrics describe loading as an operation. Model name, path, project ID, and
workspace identity MUST NOT be labels.

If future caching makes cache behavior operationally important, cache outcome may
be added only as a small code-owned enum after its ownership boundary is clear.

### Transport metrics

REST SHOULD expose bounded HTTP RED metrics such as:

```text
metis_http_requests_total{route,method,status_class}
metis_http_request_duration_seconds{route,method}
```

`route` MUST be the registered route template, never the raw URL path.
`status_class` SHOULD be bounded (`2xx`, `4xx`, `5xx`) unless a small explicit set
of status codes is proven operationally necessary.

MCP SHOULD expose the analogous bounded operation metrics:

```text
metis_mcp_requests_total{method,result}
metis_mcp_request_duration_seconds{method}
```

MCP `method` values MUST come from the finite server method registry. Tool input,
resource URI, model name, metric name, and progress token MUST NOT appear as
labels.

These are operator-side observations of existing MCP tools, not new Agent-facing
MCP capabilities. The tool registry and tool response/error schemas MUST NOT
expose metrics, traces, span IDs, or exception stack traces.

## Label policy

### Allowed label characteristics

A Prometheus label is acceptable when all of the following are true:

- values come from a small code-owned set;
- the set does not grow with customer data or request volume;
- the value is useful for aggregation, alerting, or SLO diagnosis;
- the value can be documented without referring to a particular tenant/query.

Typical allowed labels are:

```text
dialect
result
code
phase
route template
HTTP method
HTTP status class
MCP method
optimizer outcome
```

### Forbidden labels

The following MUST NOT be Prometheus labels:

```text
project_id
workspace_id
tenant_id
user_id
api_key_id
model name or model path
metric name
dimension name
semantic entity name
raw SQL or SQL fingerprint
SemanticPlan fingerprint
request_id / trace_id
scenario name
file path
endpoint/DSN
raw error message
arbitrary extension name or payload
```

This rule exists for both cardinality and data-minimization reasons.

A value being useful during debugging does not make it a metric dimension.
High-specificity diagnostic context belongs in structured logs or traces, where
retention, sampling, and access policy can be controlled independently.

## Prometheus exposition

Metis SHOULD provide Prometheus text exposition at an operator endpoint conventionally
named `/metrics`.

The endpoint MUST be configurable so an embedding/deployment can disable it. It
MUST NOT require the customer-facing Metis API key contract: Prometheus scraping
is an operator concern, not an Agent/user API call. Conversely, this exemption
MUST NOT be interpreted as permission to expose the endpoint publicly; deployment
networking or a future separate administrative listener is responsible for
restricting access.

The default Go process/runtime collectors MAY be enabled with the endpoint. Their
metric namespace remains the upstream collector namespace rather than being
renamed under `metis_`.

The initial single-node implementation shares the existing HTTP listener and
registers `/metrics` only when `metis serve --metrics` is explicitly supplied.
Metrics are disabled by default. The route remains outside customer Bearer
authentication; operators that enable it are responsible for restricting the
listener through deployment networking. A separate administrative listener may
be introduced later without changing metric names or collector ownership.

## Histograms

Latency metrics SHOULD use Prometheus histograms, not summaries, so instances can
be aggregated.

Bucket boundaries are implementation-owned operational tuning rather than a
semantic compatibility contract. They SHOULD be shared between comparable Metis
latency histograms where practical and MUST be covered by tests that prevent an
accidental pathological bucket set.

## Structured diagnostics and tracing

RFC-0036 uses OpenTelemetry tracing for request and compile-pipeline trajectory.
The initial span vocabulary is closed:

```text
metis.http
metis.mcp
metis.compile
metis.analysis
metis.planning
metis.optimization
metis.rendering
```

The transport span is the parent of the compile span; compile-phase spans are
children of compile. A failure is recorded on the span from which it escapes and
on the owning compile/transport span as appropriate, without inventing a second
compile or replaying failed work. A stable error code is a span attribute; raw
messages and stack traces are exception-event fields, not span identity.

Structured logs MAY carry request correlation and bounded contextual fields that
are forbidden as metric labels, subject to existing authentication/privacy rules.
Raw SQL, complete semantic plans, credentials, and model contents SHOULD NOT be
logged by default.

Tracing configuration must support a no-op/default-off deployment and an
explicit OTLP exporter path. Enabling tracing must not be required for metrics.

## Relationship to fingerprints and conformance evidence

Runtime metrics, plan fingerprints, SQL fingerprints, and real-engine tests serve
different purposes:

| Mechanism | Question answered |
| --- | --- |
| Runtime metrics | What is happening to live requests over time? |
| SemanticPlan fingerprint | Did plan structure change? |
| SQL fingerprint | Did emitted wire SQL/parameters change? |
| Real-engine differential | Did optimization preserve observable results? |
| Real-engine conformance | Does generated SQL actually execute correctly? |

Runtime observability MUST NOT weaken or replace deterministic test evidence.
Likewise, test-only scenario names and fingerprints MUST NOT leak into production
metric labels merely because they are useful in conformance tooling.

## Cardinality budget

The first implementation SHOULD be reviewable by inspection: every custom metric
must have a finite, documented label product.

For example:

```text
compile requests: 4 dialect values × 2 results = 8 series before process multiplicity
optimizer runs:   2 outcomes = 2 series
compile phases:   4 phases × 4 dialect values = 16 histogram label sets
```

HTTP routes and MCP methods are larger but still code-owned finite registries.
Tests SHOULD reject raw-path labeling and SHOULD make it difficult to introduce a
label whose values originate directly from request payloads.

RFC-0036 deliberately does not set a global numeric series cap because route and
method registries will evolve. The durable rule is that custom metric cardinality
must scale with **code surface**, not with **customer/request cardinality**.

## Implementation plan

Implementation should land in separately reviewable steps.

### Step 1: observability plumbing

- add the metrics registry/dependencies;
- add configurable Prometheus exposition;
- add the injected OpenTelemetry tracer-provider and W3C propagation boundary;
- prove escaping errors record status plus exception type/message/stacktrace;
- expose Go process/runtime collectors;
- add tests for endpoint enable/disable behavior;
- no compiler behavior changes.

### Step 2: transport and compile RED metrics

- instrument shared compile service once;
- instrument REST route templates and MCP registered methods;
- add compile request/error/duration metrics;
- prove transport wrappers do not double-count shared compilation.

### Step 3: pipeline and optimizer instrumentation

- add bounded compile-phase timers;
- add `rewritten` / `noop` optimizer activity observation;
- prove instrumentation does not run a second compile or change generated plans/SQL;
- add model-load metrics if loading is on the request path.

Model/project loading is currently startup-only, so the request-path condition
does not apply. Startup loading remains visible through startup success/failure
and readiness rather than being mislabeled as compile latency.

### Step 4: operational evidence

- add a minimal dashboard/query reference or documented PromQL examples;
- verify cardinality from a representative scrape;
- document which metrics are suitable for availability/latency alerting;
- update this RFC to `Implemented` only after the required runtime surface and
  tests exist.

The operator-enabled OTLP/HTTP exporter and live pipeline instrumentation build
on the Step 1 tracer-provider seam. Trace correctness is verified alongside the
metrics contract.

Steps 1 through 4 are complete. The current
[runtime observability specification](../../specs/operations/runtime-observability.md)
defines the metric, trace, PromQL, alert-suitability, and cardinality contracts.
[ADR-0007](../../decisions/operations/0007-runtime-observability-is-operator-only-and-bounded.md)
records the durable operator-only and bounded-observation decision.
An exhaustive private-registry scrape test proves the finite custom metric
label products against the current code-owned vocabularies.

## Acceptance criteria

RFC-0036 is implemented when all of the following hold:

1. Prometheus exposition is configurable and covered by tests.
2. Compile count, latency, and stable error classification are observable.
3. Compile phase latency covers analysis, planning, optimization, and rendering.
4. Optimizer activity exposes bounded `rewritten`/`noop` outcomes without a
   telemetry-only duplicate compile.
5. REST/MCP transport metrics use registered/bounded operation identity rather
   than raw request values.
6. No custom metric uses a forbidden high-cardinality/customer-specific label.
7. Instrumentation tests demonstrate that enabling metrics does not alter
   generated `SemanticPlan`/SQL behavior.
8. Standard correctness, E2E, fingerprint, and real-engine gates remain green.
9. W3C trace context crosses REST/MCP into compile-phase child spans.
10. Escaping failures record stable error code, `Error` span status, and an
    exception event with type/message/stacktrace when full capture is enabled.
11. OTLP trace export is explicitly configurable, disabled by default, and
    flushed during graceful shutdown.
12. Documented PromQL and alert guidance are backed by an exhaustive scrape
    test that locks the current finite label product and histogram buckets.

## Non-goals

This RFC does not:

- define user-facing analytics or per-tenant billing metrics;
- expose semantic/query identity through Prometheus;
- add SQL execution metrics for downstream databases, because Metis compiles but
  does not own caller-side SQL execution;
- replace structured error responses with metrics;
- define SLO thresholds or paging policy;
- require runtime differential compilation to observe optimizer activity.

## Resolved implementation questions

The initial latency histograms use the Prometheus client defaults. Bucket tuning
remains an operator-contract change requiring deliberate review and tests; it is
not a semantic compatibility contract.

Project/model loading is startup-only, so Metis does not publish a misleading
request-path model-load metric. Startup failure and readiness remain its current
operational evidence.
