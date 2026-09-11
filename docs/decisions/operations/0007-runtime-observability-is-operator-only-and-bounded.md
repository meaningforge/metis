# ADR-0007: Runtime Observability Is Operator-Only and Bounded

> **Decision record:** Read alongside the [current documentation](../../README.md);
> superseded decisions are retained for their rationale.

**Status:** Accepted (current)
**Date:** 2026-08-25
**Last reviewed:** 2026-08-25

## Context

Metis needs production evidence for request health, compile latency, pipeline
hotspots, optimizer activity, and escaping exceptions. Instrumenting individual
REST or MCP handlers independently would count the same semantic work
differently, and labels derived from projects, models, queries, SQL, paths, or
errors would make cardinality and data exposure grow with customer traffic.

Observability is operational state for Metis operators. It is not semantic
discovery data and does not help an Agent choose or compile a semantic query.
Putting metrics or trace identifiers into MCP tools would expand the public
Agent contract without adding semantic capability.

## Decision

Runtime observations use a closed, code-owned vocabulary in
`app/observability`. The shared compile service and normal planner/compiler path
emit typed observations; REST and MCP add only their bounded transport
observations. Prometheus and OpenTelemetry are adapters over those boundaries.

Custom Prometheus labels may scale only with code-owned dialects, stable error
codes, phases, route templates, HTTP methods/status classes, MCP tool methods,
and optimizer outcomes. They must not contain customer identity, semantic
request values, raw paths, SQL, plans, fingerprints, trace identifiers, or raw
error text. An exhaustive scrape test locks the finite label product.

Prometheus exposition and traces are operator-facing and configurable. They do
not add MCP tools, fields, resources, or error payloads. W3C context may enter
through the HTTP boundary, but trace and span identifiers remain in telemetry
systems rather than Agent responses.

Observation must follow the work already performed. It may not replay or
duplicate semantic compilation, and sink failure may not change compilation,
authentication, or transport behavior.

## Consequences

REST and MCP share comparable compile evidence while retaining distinct
transport metrics. Operators can aggregate Prometheus histograms across
instances and follow sampled exception trajectories through OpenTelemetry.
Metric growth remains reviewable from code changes rather than customer data.

The closed vocabulary must be updated deliberately when routes, tools,
dialects, phases, or error codes change. Traces can contain more diagnostic
detail than metrics, so exporter access, sampling, retention, and redaction are
deployment responsibilities.

Metis does not expose observability through the Agent-facing MCP surface and
does not become a log store, trace backend, or downstream query executor.

## Alternatives considered

**Expose observability through MCP.** This confuses operator diagnosis with
semantic Agent capabilities and leaks implementation correlation into a stable
public protocol.

**Instrument transports only.** This loses phase ownership and makes shared
compile behavior depend on which adapter invoked it.

**Use request-derived labels.** Project, model, metric, query, path, and error
values improve one-off filtering but create customer-scaled cardinality and a
larger data-exposure surface.

**Compare a second unoptimized compile at runtime.** Differential execution is
strong test evidence, but duplicating work in production would change cost and
failure behavior merely to produce telemetry.
