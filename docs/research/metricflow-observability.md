# MetricFlow observability notes for RFC-0036

## Scope and source

This note reviews MetricFlow's telemetry implementation as architectural input
to RFC-0036. It is research, not a Metis contract.

The reviewed source is dbt-labs/metricflow commit
[`24c248833b27993fc23dc2ff087f4335e380356b`](https://github.com/dbt-labs/metricflow/tree/24c248833b27993fc23dc2ff087f4335e380356b),
dated 2026-08-24.

## Useful patterns

MetricFlow defines versioned typed function-start and function-end events, then
routes them through a reporter to pluggable handlers. Its decorator pairs events
with an invocation ID, records duration in a `finally` block, and captures both
success and exception completion. A bounded in-memory handler makes the event
contract directly testable.

Relevant sources:

- [event models](https://github.com/dbt-labs/metricflow/blob/24c248833b27993fc23dc2ff087f4335e380356b/metricflow/telemetry/models.py)
- [reporter and `log_call` decorator](https://github.com/dbt-labs/metricflow/blob/24c248833b27993fc23dc2ff087f4335e380356b/metricflow/telemetry/reporter.py)
- [handler boundary](https://github.com/dbt-labs/metricflow/blob/24c248833b27993fc23dc2ff087f4335e380356b/metricflow/telemetry/handlers/handlers.py)
- [in-memory contract tests](https://github.com/dbt-labs/metricflow/blob/24c248833b27993fc23dc2ff087f4335e380356b/tests_metricflow/telemetry/test_telemetry.py)

The durable lesson is separation: compiler code emits an observation contract;
destinations decide whether that becomes logs, metrics, traces, or test evidence.

## Boundaries Metis should not copy

MetricFlow's current telemetry is function-oriented: module and function names
form the event identity, and decorators are attached to many CLI and engine
entry points. The reporter may derive a client identifier from platform, OS
release, and MAC address, and exception events may contain a full traceback.
The engine also logs the complete query request at INFO at its query entrypoint.

Those choices do not match Metis's runtime contract:

- package/function names are implementation details, not a stable operational
  API;
- Metis does not need a user, host, or client identifier for local operational
  metrics;
- raw exceptions, semantic requests, SQL, plans, and fingerprints must not enter
  metric labels; exception details belong in an explicitly configured trace
  signal with its own sampling, retention, and access policy;
- arbitrary decorators make it too easy for telemetry vocabulary to expand with
  code structure rather than with reviewed compiler phases.

## Metis-specific direction

Metis should retain the typed, pluggable, testable observation pattern while
changing the observed subject:

1. Observations name closed semantic-runtime operations such as compile,
   analysis, planning, optimization, and rendering—not Go functions.
2. Result identity is bounded: success/error, stable semantic error code, and
   optimizer `rewritten`/`noop` proof derived from normal plan state.
3. Metric observations contain operational facts only. They do not contain
   semantic intent, model/metric names, SQL, plans, fingerprints, credentials,
   request IDs, or principal identity.
4. Escaping errors are recorded on OpenTelemetry spans using the stable
   `exception.type`, `exception.message`, and `exception.stacktrace` conventions.
   Unlike MetricFlow's generic function decorator, spans follow Metis's closed
   transport and semantic-pipeline operations.
5. Prometheus is one sink over the typed observation boundary, not an API called
   directly from resolver/planner/compiler packages.
6. An in-memory sink proves event count, phase closure, and no duplicate compile
   work without scraping Prometheus text in every semantic test.

This makes Metis observability reflect its product identity: deterministic
semantic compilation and proof-gated planning, rather than generic function-call
telemetry.

## OpenTelemetry exception alignment

Metis uses the stable OpenTelemetry exception attributes rather than inventing
another exception payload. The official conventions define
`exception.type`, `exception.message`, and `exception.stacktrace`, while warning
that exception messages may contain sensitive information. OpenTelemetry Go's
`RecordError` creates the exception event but does not set span status, so Metis
must also set the span to `Error` explicitly.

- [OpenTelemetry exception attributes](https://opentelemetry.io/docs/specs/semconv/registry/attributes/exception/)
- [OpenTelemetry Go error recording](https://opentelemetry.io/docs/languages/go/instrumentation/#record-errors)

Prometheus and tracing therefore have separate data budgets: exception detail is
allowed only on explicitly configured, sampled traces and never enters metric
labels.
