# Agent Semantic Context and Explainability Specification

## Scope

This specification defines the lower-level semantic-context read model and
query explanation. These transport-neutral service behaviors remain available
to REST/admin clients and do not execute database queries.

## Discovery contract

Semantic search is a bounded discovery aid. `SearchSemanticsRequest` accepts an explicit project plus an optional model filter, asset-kind filters, query text, and limit.

Results MUST be deterministic for the same SemanticManifest. The default limit is 20 and explicit limits MUST NOT exceed 100. Invalid model names, unsupported asset kinds, and invalid limits fail with typed semantic errors.

Search ranking MUST NOT perform implicit semantic resolution. Agents remain responsible for explicitly selecting the metric or dimension identity used by later context or query requests.

The primary Agent discovery surface follows the same boundedness rule.
`get_dimensions` returns at most 20 items by default and accepts an explicit
limit no greater than 100. A truncated response returns an opaque cursor that
is valid only with the same model-or-metrics anchor and normalized search.

`list_metrics` MAY expose `definition_equivalent_refs` when metrics have the
same authored datatype, expression, extensions, and normalized semantic
constraints. This is discovery evidence only: Metis MUST NOT infer that
different business identities are interchangeable or select one by ranking.
The authoring quality report uses this same exact-definition evidence for
`MODEL_QUALITY_EQUIVALENT_METRIC_DEFINITION`; discovery MUST NOT run a separate
prose-based equivalence rule. Primary Agent surfaces do not provide a project
quality-scanning tool. A future selected-asset detail MAY include a bounded
already-computed warning without changing discovery ranking or selection.

A field with `dimension.is_time: true` but a non-temporal datatype remains a
time-labelled grouping identity, but it MUST NOT advertise built-in calendar
grains. A grain-bearing query for such a field fails with
`INCOMPATIBLE_QUERY_GRAIN`; Metis MUST NOT lower a numeric value through
`DATE_TRUNC` or another temporal operation.

## Semantic Context contract

`SemanticContextService.Get` accepts a project, model, and at least one focused metric or dimension. It derives context from the same SemanticManifest and discovery compatibility logic used elsewhere in Metis; transports MUST NOT reimplement semantic interpretation.

A successful context response contains only evidence relevant to the requested focus, including:

- metric identity, dependencies, source datasets, and typed semantic constraints;
- focused dimensions and their compatibility state;
- relationship-path evidence needed to justify compatible, unreachable, or ambiguous states;
- relevant relationships;
- a deterministically ordered, bounded compatible-dimension page when requested.

Compatibility states are `compatible`, `unreachable`, and `ambiguous`. A compatible traversal MUST carry the path evidence that proves it. Unreachable or ambiguous states MUST carry stable issue information rather than guessing a path.

Missing projects, models, metrics, or dimensions are request errors. An incompatible metric/dimension combination is informational context and may be represented as an issue without failing the entire context request.

The REST endpoint is:

```text
POST /v1/projects/:project/models/:model/semantic-context
```

This lower-level endpoint remains available to REST/admin clients. It is not a
separate primary MCP tool.

## Explain Query contract

`CompileService.Explain` consumes the same `CompileRequest` shape as compilation. Explain and Compile MUST share execution-binding resolution, semantic resolution/validation, and Planner orchestration. Explain MUST NOT introduce a second query language or a second semantic interpretation path.

A successful explanation contains stable semantic evidence for the applicable query decisions, including execution resolution, metrics, dimensions, relationship paths, output schema, and ordered explanation steps.

The `semantic_plan` projection uses node-native vocabulary: its ordered computation array is `nodes`, lineage references are `node_ids`, predicate ownership is `owner_node_id`, and a node-boundary placement proof is `node_semantics`. Explain MUST NOT emit the retired `stages`, `stage_ids`, `owner_stage_id`, or `stage_semantics` vocabulary.

Supported semantic step kinds are:

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

Steps describe semantic decisions. They MUST NOT expose raw `SemanticPlan`, SQLPlan node shapes, renderer-private state, or database cost/execution operators.

Explain fails closed at the same semantic boundary and with the same stable error class that Compile would encounter. It MUST NOT fabricate a relationship path or semantic transformation merely to return an explanation.

The REST endpoint is:

```text
POST /v1/explain
```

Explain remains available to REST/admin clients. It is not a separate primary
MCP tool.

## Transport parity

Where a service is exposed through both REST and MCP, both transports are
adapters over the same `app/service/semantic` use case and MUST preserve equivalent
request meaning, structured fields, enum/code meanings, ordering, pagination
behavior, and error semantics.

Transport-specific code MAY perform protocol decoding and encoding only. It MUST NOT add REST-only or MCP-only semantic behavior.

## Determinism and boundedness

Collections whose order is not user-authored MUST be deterministically ordered. User-authored metric, dimension, grouping, and sort order MUST be preserved where observable.

Context and search endpoints MUST remain bounded. Metis MUST NOT dump an entire
model by default as an Agent search response.

## Conformance

Canonical Agent-context conformance lives under `tests/conformance/agentcontext`. The corpus verifies deterministic structured evidence for representative relationship ambiguity, time offset, fill, semi-additive, composition, filter, grouping, ordering, and limit behavior.

Transport parity tests execute the same canonical fixtures through service, REST, and MCP and compare the complete Agent-facing structured payload rather than implementation internals.
