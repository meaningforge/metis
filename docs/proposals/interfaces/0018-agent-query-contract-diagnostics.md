# RFC-0018: Agent Query Contract and Structured Diagnostics

> **Design history:** This RFC preserves its original lifecycle status and rationale.
> Implementation claims describe the recorded design, not a guarantee that every
> proposed API remains current. See [current documentation](../../README.md) for
> supported interfaces. Package paths have been updated for this repository.

- **Status:** Implemented
- **Owners:** Metis maintainers
- **Created:** 2026-08-16
- **Last updated:** 2026-08-17
- **Scope:** `query`, `resolver`, `planner`, `app/service/semantic`, REST/MCP interfaces, Agent-facing conformance
- **Supersedes:** None

## Summary

Metis uses its typed `query.SemanticQuery` as the stable Agent-facing semantic-intent contract and exposes deterministic, machine-readable validation diagnostics over the same semantic resolution and planning truth used by Explain and Compile.

Phase E did not introduce a second query IR or natural-language interpretation layer. An Agent or other client may translate user intent into `SemanticQuery`, but Metis Core begins at structured semantic intent and validates, explains, and compiles that intent deterministically.

The implemented Agent workflow is:

```text
Discovery / Semantic Context
          -> SemanticQuery
          -> Validate
          -> structured diagnostics / suggestions
          -> Explain
          -> Compile
          -> PhysicalQuery + OutputSchema
```

## Implemented boundaries

### One semantic query contract

`query.SemanticQuery` remains the public structured semantic-intent type. Public fields represent semantic identity and query intent; physical execution selection is not embedded in the query.

Execution selection remains on the enclosing transport-neutral `service.CompileRequest` through `execution_binding`. The earlier query-local physical `target` shape is rejected rather than silently competing with execution resolution.

Filters retain natural JSON scalar/flat-array operands for compatibility, but arbitrary objects and nested arrays are rejected at the query boundary. Operator-specific arity and semantic validity remain Resolver-owned.

Raw SQL fields such as `where_sql`, `raw_predicate`, and free-form ordering expressions are not part of the semantic query contract.

### Shared Validate / Explain / Compile truth

`CompileService.Validate`, `Explain`, and `Compile` share the same semantic preparation path:

```text
CompileRequest
  -> execution target resolution
  -> Resolver
  -> Planner
  -> semantic-plan validation
  +-> Validate
  +-> Explain
  +-> Compile -> physical SQL compilation
```

Validate stops before physical compilation. It does not create a second resolver, planner, compatibility engine, or execution mode.

The consistency contract is executable:

```text
Compile success => Validate success
Explain success => Validate success
```

When operations fail at the same semantic boundary, they preserve the same stable semantic error code and authoritative structured evidence.

### Structured diagnostics

Semantic validation failures return `QueryValidationResult` with `valid=false` and structured diagnostics derived from the existing stable `serrors.Error` contract.

Diagnostics expose:

```text
SemanticDiagnostic
  code
  severity
  subject?
  message?
  details?
  suggestions[]
```

Current subject projection claims only evidence already proven by the semantic error, including metric, dimension, or field identity. Unknown subjects remain unclaimed rather than guessed.

Non-semantic/internal failures remain errors and are not converted into repairable user diagnostics.

### Bounded repair evidence

When the shared discovery read model is available, supported unknown metric/dimension failures may be enriched with deterministic bounded candidate suggestions.

Current suggestion records are structured by kind/value. Discovery evidence is capped and deterministic. It is advisory only: Metis never mutates the submitted query, silently replaces an asset, or selects an ambiguous relationship on the client's behalf.

The client-controlled loop is therefore:

```text
Agent constructs SemanticQuery
        -> Validate
        -> diagnostics / bounded evidence
        -> Agent revises SemanticQuery
        -> Validate
        -> Explain
        -> Compile
```

### Transport parity

REST and MCP expose Validate, Explain, and Compile through the same transport-neutral `service.CompileRequest` and service DTOs. Transport adapters do not reinterpret semantic fields or diagnostic meaning.

Natural-language interpretation remains outside Metis Core.

## Conformance

Phase E correctness is covered at several layers:

- query JSON contract tests for deterministic round-trip and filter operand shape;
- execution-target boundary tests proving physical target selection stays outside `SemanticQuery`;
- Validate service tests for structured diagnostic identity and bounded repair evidence;
- REST/MCP parity over the shared validation DTOs;
- bootstrap-level Validate/Explain/Compile consistency tests;
- canonical Agent query conformance under `tests/conformance/agentquery/`;
- existing semantic, optimizer-differential, Doris, and ClickHouse correctness gates.

The Agent query corpus covers representative valid metric/dimension/filter/order requests and invalid asset/filter/limit requests, including bounded repair evidence and cross-operation stable error identity.

## Non-goals

RFC-0018 does not add:

- natural-language interpretation or NL2SQL inside Metis Core;
- database connectivity, query execution, or result analysis;
- a second public query IR;
- silent typo correction or automatic semantic selection;
- arbitrary raw SQL escape hatches in `SemanticQuery`;
- embeddings/vector search as a core dependency;
- a second relationship or compatibility resolver;
- database SQL `EXPLAIN` or cost-based optimization;
- new SQL dialects merely to satisfy Phase E.

## Acceptance evidence

RFC-0018 is implemented with the following guarantees:

- `query.SemanticQuery` is the stable public structured semantic query contract;
- execution binding is separate from semantic query identity;
- supported public semantics do not depend on arbitrary SQL fragments;
- Validate reuses the same target-resolution, Resolver, Planner, and invariant truth as Explain and Compile;
- diagnostics expose stable machine-readable error identity and structured suggestions where available;
- suggestions never silently repair or reinterpret invalid semantic intent;
- REST/MCP entrypoints preserve the same service DTO semantics;
- successful Validate/Explain/Compile behavior and corresponding failure reasons are consistency-tested;
- Agent query conformance covers representative valid and invalid requests;
- existing canonical and relevant real-engine correctness gates remain required.

## Current normative documentation

Current behavior is defined by:

- [`../../specs/semantic/agent-query-contract.md`](../../specs/semantic/agent-query-contract.md)
- [`../../specs/semantic/compilation-pipeline.md`](../../specs/semantic/compilation-pipeline.md)
- [`../../specs/testing/agent-query-conformance.md`](../../specs/testing/agent-query-conformance.md)

This RFC remains the historical design record; current specifications and executable tests are authoritative for implemented behavior.
