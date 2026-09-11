# RFC-0016: Agent Semantic Context and Explainability

> **Design history:** This RFC preserves its original lifecycle status and rationale.
> Implementation claims describe the recorded design, not a guarantee that every
> proposed API remains current. See [current documentation](../../README.md) for
> supported interfaces. Package paths have been updated for this repository.

- **Status:** Implemented
- **Owners:** Metis maintainers
- **Created:** 2026-08-15
- **Last updated:** 2026-08-16
- **Scope:** `app/service/semantic`, Catalog/Resolver/Planner read models, REST/MCP interfaces, Agent-facing conformance
- **Supersedes:** None

## Summary

Metis exposes a deterministic, bounded, Agent-oriented semantic context contract and a semantic query explanation contract.

The goal is to let an Agent obtain the minimum trustworthy context needed to reason about metrics, dimensions, relationships, and query semantics without loading an entire Ossie model or reconstructing planner behavior itself.

The same semantic truth used by compilation MUST drive context and explanation. REST and MCP MUST expose the same transport-neutral service contracts.

This RFC introduced two public use cases:

1. **Semantic Context** — focused context for selected semantic assets, including dependencies, source datasets, dimension compatibility, relationship paths, and semantics-bearing constraints.
2. **Explain Query** — a structured explanation of how a semantic query resolves and is planned, including important semantic transformations and fail-closed decisions, without exposing internal AST implementation details.

Metis remains compile-only. This proposal does not add database connectivity, query execution, SQL `EXPLAIN`, natural-language interpretation, or result analysis.

## Motivation

Phase B established strong semantic correctness and real-engine evidence. Agents can already search semantic assets, inspect individual metrics and dimensions, ask for metric/dimension compatibility, and compile a query.

Those primitives are correct but still require the Agent to reconstruct too much context across multiple calls. In a large semantic estate this has three problems:

- the Agent may repeatedly load semantic definitions that are irrelevant to the current task;
- relationship-path, dependency, time, fill, or semi-additive semantics can be understood inconsistently when reconstructed outside Metis;
- a compile failure can be deterministic while still being difficult for an Agent to explain or repair.

Metis should therefore become the semantic context runtime shared by Agents, not merely a deterministic SQL-producing endpoint.

## Design principles

The Agent-facing context contract follows these rules:

1. **Derived from semantic truth** — responses come from the current Catalog, Resolver, and Planner contracts rather than a second interpretation layer.
2. **Focused and bounded** — Metis MUST NOT dump an entire semantic model by default. Context expansion must be limited to assets relevant to the request and any explicitly requested compatibility page.
3. **Deterministic** — equivalent requests over the same Catalog snapshot and target resolution produce stable ordering and equivalent structured output.
4. **Structured before prose** — stable codes, kinds, subjects, paths, and evidence are authoritative. Human-readable summaries may be included but MUST NOT be the only machine-consumable explanation.
5. **Fail closed** — Metis MUST NOT invent a relationship path, type fact, semantic transformation, or compatibility claim that cannot be proven from the current semantic state.
6. **Transport-neutral** — REST and MCP use the same service DTOs and semantic error payloads.
7. **No internal-plan leakage** — public explanations describe semantic decisions, not Go types, SQL AST node shapes, optimizer implementation details, or unstable internal identifiers.

## Semantic Context contract

A semantic context request identifies a project and model and focuses on one or more metrics and/or dimensions. It may request a bounded page of compatible dimensions for selected metrics.

Conceptually:

```text
SemanticContextRequest
  project
  model
  metrics[]
  dimensions[]
  compatible_dimensions_page? { limit, cursor }
```

At least one metric or dimension MUST be supplied. Project and model remain explicit; there is no implicit default project.

The response contains only context needed to understand the requested focus:

```text
SemanticContext
  project
  model
  metrics[]
    name
    description?
    metric_type?
    data_type?
    dependencies[]
    source_datasets[]
    semantic_constraints[]
  dimensions[]
    name
    qualified_name
    dataset
    data_type?
    compatibility?
    relationship_paths[]
    issues[]
  relationships[]
    name
    from
    to
  compatible_dimensions_page?
    items[]
    next_cursor?
```

The exact Go representation may use nested typed structures, but the following semantics are normative.

### Metric context

For each focused metric, Metis MUST expose stable metric identity plus planner-relevant semantic facts that can be proven before physical execution. These include direct/required dependencies and source datasets.

When present in the semantic contract, context SHOULD expose semantics-bearing properties such as time offset/alignment, fill policy, semi-additive selector/non-additive dimension behavior, and metric-definition filters through typed semantic constraint records rather than by copying arbitrary internal planner nodes.

Unknown or unsupported semantic-critical extensions MUST retain existing fail-closed behavior.

### Dimension context and compatibility

For a requested metric/dimension combination, Metis MUST distinguish at least:

- `compatible`
- `unreachable`
- `ambiguous`

A compatible claim MUST include the resolved dataset/relationship path needed to justify it when a relationship traversal is required.

An unreachable or ambiguous claim MUST include a stable issue code and enough structured path evidence for an Agent to understand the conflict. Existing semantic error codes SHOULD be reused where they already express the condition.

Context lookup is informational: an incompatible requested combination may be represented as an explicit compatibility issue rather than causing the entire context request to fail. Missing projects, models, metrics, or dimensions remain request errors.

### Bounded compatible-dimension discovery

When compatible dimensions are requested for a metric, the result MUST be deterministically ordered and bounded. The implementation supports a limit and opaque cursor rather than returning an unbounded model-wide list.

Pagination MUST NOT change compatibility semantics. A cursor is an interface token, not semantic state an Agent should interpret.

## Explain Query contract

`ExplainQuery` reuses the same `CompileRequest` input shape used by compilation:

```text
CompileRequest
  query
  execution_binding?
```

This prevents a second query language from emerging for explanation.

Explain MUST run the same execution-target resolution, semantic resolution/validation, and semantic planning contracts used by `Compile`. Implementations share orchestration rather than independently reproducing those stages.

The current response is `SQLExplainResult`, embedding the structured semantic
explanation below and adding `sql_render_result` and optional `warnings`:

```text
QueryExplanation
  project
  model
  execution_resolution
  metrics[]
  dimensions[]
  relationship_paths[]
  steps[]
  decisions[]
  output_schema?
```

The explanation MUST use stable semantic step kinds. Current support includes:

- `source`
- `relationship`
- `definition_filter`
- `aggregation`
- `time_alignment`
- `time_offset`
- `fill`
- `semi_additive_selection`
- `composition`
- `query_filter`
- `grouping`
- `ordering`
- `limit`

A step does not need to appear when the query does not use that semantic behavior.

Each step MUST identify its semantic subject and the relevant structured inputs/outputs or evidence. Human-readable text may summarize the decision, but clients MUST be able to reason from stable fields without parsing prose.

### Explanation versus physical query

Explain Query is not SQL `EXPLAIN` and does not describe database cost, scan statistics, indexes, partitions, or execution operators.

Explain reuses the compile pipeline to include the same rendered SQL, dialect,
and bound parameters in `sql_render_result`, plus output schema and compilation
warnings. The semantic evidence and SQL come from one prepared plan and one
data-policy evaluation. Semantic evidence remains redacted; the SQL result
contains the same policy filters and parameter values as Compile under the same
authorization. Explain MUST NOT expose raw SQL AST nodes or renderer-private
structures, and MUST fail if rendering fails.

### Failure behavior

Explain MUST fail with the same semantic error class that compilation would encounter at the corresponding resolution/planning boundary.

For example, if a relationship path is ambiguous, Explain MUST NOT fabricate a preferred path merely to produce an explanation. The shared serialized error payload remains authoritative, including stable code, details, and suggestions where available.

## Agent-efficient search

Semantic search supports stable filters for asset kind and model plus deterministic result limits. Search ranking remains a discovery aid and MUST NOT become semantic resolution: selecting the correct metric or dimension remains an explicit Agent/user decision unless the identity is unambiguous by the existing API contract.

This RFC does not introduce embeddings, vector databases, or LLM-based ranking into Metis Core.

## Transport contract

REST and MCP call the same `app/service/semantic` use cases.

The transports may choose protocol-appropriate operation names, but they MUST preserve the same request semantics, response fields, stable enums/codes, pagination behavior, and errors.

No REST-only or MCP-only semantic interpretation is allowed.

## Determinism and versioning

Collections whose order is not user-authored MUST use deterministic ordering. User-authored metric, dimension, grouping, and sort order MUST be preserved where order is semantically observable.

Context and explanation contracts are additive Agent-facing interfaces. Future fields may be added compatibly, but existing stable enum meanings, issue codes, and path semantics MUST NOT be silently redefined.

The contract SHOULD expose Catalog snapshot/version identity once Metis has a stable public snapshot identifier. RFC-0016 does not invent one solely for this feature.

## Non-goals

RFC-0016 explicitly does not add:

- natural-language-to-query translation;
- database credentials or connectivity;
- query execution or result fetching;
- database SQL `EXPLAIN` or cost estimation;
- cache or pre-aggregation planning;
- a public dump of `SemanticPlan` or SQL AST internals;
- embeddings/vector search as a core dependency;
- a second semantic-model schema beside Ossie;
- user-authored planner properties such as semi-additive composability.

## Alternatives

### Let the Agent fetch raw Ossie and reconstruct context

Rejected as the primary contract. It duplicates semantic infrastructure in every Agent, increases context usage, and makes relationship/planning interpretation probabilistic again.

### Expose the raw `SemanticPlan`

Rejected. Internal plan representation must remain free to evolve. Agents need stable semantic evidence, not implementation-specific compiler structures.

### Make `Explain` a prose-only description

Rejected. Prose is useful for people but unstable for tool consumers. Structured kinds, codes, paths, and evidence are required.

### Add a database-backed semantic search service first

Rejected for Phase C. Deterministic Catalog-based discovery is sufficient to establish the public context contract without adding infrastructure or changing Metis's product boundary.

## Rollout and migration

Implementation completed the following sequence:

1. accept RFC-0016 and establish the Phase C plan;
2. add transport-neutral Semantic Context DTOs and service behavior;
3. expose the same context contract through REST and MCP;
4. add Explain Query orchestration over the existing target-resolution, Resolver, and Planner pipeline;
5. expose Explain through REST and MCP;
6. add bounded/filterable Agent discovery improvements;
7. create canonical Agent-context/explanation conformance fixtures and gates;
8. update current specs/design and mark this RFC `Implemented`.

All additions are backward compatible with existing compile and discovery APIs. Existing APIs remain available; Phase C is an additive capability, not a forced migration.

## Test and acceptance criteria

RFC-0016 is implemented with the following evidence:

- Semantic Context is available through the shared `app/service/semantic` layer.
- REST and MCP expose semantically identical context requests/responses.
- Context is bounded and does not return the whole model by default.
- Metric dependencies and source datasets are derived from Catalog semantic analysis, not reparsed separately.
- Requested metric/dimension compatibility reports compatible, unreachable, and ambiguous cases with deterministic path evidence.
- Explain Query reuses the compile request shape and the same target-resolution/Resolver/Planner semantics as Compile.
- Current time-offset, fill, semi-additive, conversion/derived composition, filters, grouping, ordering, and limit behaviors produce applicable structured explanation steps.
- Explain fails closed with the same stable semantic error contract as compilation.
- REST and MCP contract tests prove transport parity.
- A canonical context/explanation corpus proves deterministic structured output across representative semantic scenarios.
- Current design/specification documents define the implemented public behavior normatively.

Normal `make fmt-check`, `make check`, E2E smoke, and relevant conformance gates remain required repository gates for changes to these contracts.

## Current normative documentation

Current implemented behavior is defined by:

- [`../../specs/semantic/agent-semantic-context.md`](../../specs/semantic/agent-semantic-context.md)
- [`../../specs/semantic/compilation-pipeline.md`](../../specs/semantic/compilation-pipeline.md)
- [`../../specs/testing/agent-context-conformance.md`](../../specs/testing/agent-context-conformance.md)

This RFC remains the historical design record; current specifications are authoritative for implemented behavior.
