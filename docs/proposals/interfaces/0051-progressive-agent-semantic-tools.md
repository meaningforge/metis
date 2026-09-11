# RFC-0051: Progressive Agent Semantic Tools

> **Design history:** This RFC preserves its original lifecycle status and rationale.
> Implementation claims describe the recorded design, not a guarantee that every
> proposed API remains current. See [current documentation](../../README.md) for
> supported interfaces. Package paths have been updated for this repository.

- **Status:** Implemented
- **Owners:** Metis maintainers
- **Created:** 2026-08-27
- **Last updated:** 2026-08-27
- **Scope:** `app/service/semantic`, `app/mcp`, AgentBench, Agent semantic specifications
- **Supersedes:** the fixed-four-tool and bundled-payload portions of RFC-0044,
  RFC-0049, and RFC-0050

## 1. Decision

Replace the four bundled primary MCP tools with nine narrow, progressively
disclosed tools:

```text
list_projects       list_models       get_model
list_metrics        get_metric
get_dimensions      get_dimension     get_relationships
compile
```

List tools return compact indexes. Detail tools return richer authored context
for one selected asset. Relationship discovery is independent from dimension
listing. `compile` remains the only physical-query operation and never executes
the query.

`list_metrics` separates lightweight retrieval from deterministic narrowing:

```text
list_metrics(
  search?: string | string[],
  meta_filter?: {
    models?: string[]
  },
  limit?: int
)
```

`search` is resource-specific substring retrieval chosen by the Agent; one term
is accepted as a scalar convenience form and multiple terms are ORed. It is not
natural-language semantic resolution. `meta_filter` is a typed filter object.
Its initial `models` field accepts canonical model refs and OR-combines them.
Future metadata filters are added explicitly rather than through a free-form map.

## 2. Problem

The four-tool design reduced registration overhead but made each response do
too many jobs. `list_projects` repeated every model and its AI context;
`list_metrics` repeated metric context; `get_dimensions` mixed compatibility,
field detail, grains, and relationship evidence. Agents received large payloads
they often did not need, while missing one fact encouraged broad retries.

A single generic search tool would recreate the same mixed-result problem and
make the Agent infer which record shape is authoritative. The desired property
is a small bounded vocabulary, not the smallest possible tool count.

## 3. Industry evidence

dbt MCP separates semantic operations such as `list_metrics`, `get_dimensions`,
`get_entities`, compiled SQL, value lookup, and query execution into explicit
tools and toolsets. Its contribution contract also requires a dedicated tool
prompt and standard read-only/idempotent annotations. This supports clear tool
responsibility rather than one universal semantic dump:

- <https://github.com/dbt-labs/dbt-mcp#tools>
- <https://github.com/dbt-labs/dbt-mcp/blob/main/src/dbt_mcp/tools/toolsets.py>
- <https://github.com/dbt-labs/dbt-mcp/blob/main/CONTRIBUTING.md#steps-to-add-a-new-tool>

Its current `list_metrics` accepts name-substring `search` (one or many OR
terms) and a metadata filter rather than a semantic-model selector. Narrow
results include dimension/entity names; broad results use a cheaper listing and
CSV columns are progressively trimmed (`description`, then `metadata`) against
a character budget. Metis follows the useful separation between retrieval and
filtering while retaining stronger contracts: model narrowing lives under typed
`meta_filter.models`, uses canonical model refs, and remains deterministic.
Narrow results include canonical dimension-ref hints.

Cube documents curated views as the Agent-facing facade over measures,
dimensions, and join paths, and distinguishes user-facing descriptions from
member-specific `meta.ai_context`. This supports exposing authored context at
the selected model/member boundary rather than repeating it in every broad
list response:

- <https://docs.cube.dev/docs/data-modeling/views>
- <https://docs.cube.dev/docs/data-modeling/ai-context>

Metis adopts the progressive-disclosure principle, not either vendor's full
surface. In particular, dbt's value and query tools execute against a warehouse.
Metis cannot copy them without violating its compile-only boundary.

## 4. Contract

### 4.1 Index tools

- `list_projects`: project identity and description only.
- `list_models`: model ref/name/description; default 10, maximum 50.
- `list_metrics`: metric ref/name/model/type/description; `search` performs
  lightweight metric retrieval and optional `meta_filter.models` OR-scopes
  canonical model refs; narrow results include compatible dimension-ref hints;
  default 10, maximum 50.
- `get_dimensions`: compatible ref/name/model/dataset and exact group-by type.

Bounded model and metric lists report `truncated`. Agents narrow a truncated
result through more specific search terms or `meta_filter.models`. Wide or
truncated pages omit descriptions and report `details_omitted`.
Search remains deterministic substring matching, including authored context for
retrieval without returning that context in every item. The Agent owns natural-
language interpretation and term selection.

### 4.2 Detail tools

- `get_model`: selected model plus authored AI context and name-only dataset,
  metric, and dimension inventories for model selection.
- `get_metric`: selected metric plus authored AI context.
- `get_dimension`: selected field type, label, primary-key status, description,
  AI context, valid time grains, and semantic consequences.
- `get_relationships`: selected model relationships, optional dataset/search
  focus, join columns, AI context, and temporal evidence.

No detail tool returns expressions, internal plans, or raw extension payloads.

### 4.3 MCP behavior

Every tool declares read-only, idempotent, closed-world annotations. Tool
descriptions state when the tool is optional and name the next authoritative
tool. Server-level instructions define the single-model workflow and explicit
stop-after-compile rule once, avoiding repeated text in every tool description.
Registration remains dynamic through MCP discovery; AgentBench does not
hard-code the number of server tools.

When a canonical metric ref is already known, the Agent skips `list_metrics`.
When metric identity is unknown, the Agent may search across the active project
or narrow candidate models through `meta_filter.models`; discovery is not a
mandatory model-first hierarchy.

## 5. Rejected alternatives

### Keep exactly four tools

Rejected because tool-count minimalism increased response bytes and mixed
unrelated responsibilities. Registration schema cost is paid once; oversized
responses recur on every question and retry.

### One universal `search` tool

Rejected for the primary surface. Mixed asset shapes and relevance ranking make
the Agent reconstruct the semantic workflow. Generic search remains available
through lower-level REST/admin services.

### Free-form `meta_filter`

Rejected for the primary Metis surface. An unconstrained metadata map hides the
supported filtering contract from MCP clients and weakens validation. Metis
uses a typed object and adds filter fields only when their semantics are defined.

### Add `get_dimension_values` now

Rejected because Metis owns compilation, not credentials, warehouse access, or
result fetching. A future compile-only value-query contract is possible but is
not smuggled into discovery.

## 6. Consequences

The MCP registration payload grows from four to nine concise schemas. In
exchange, routine calls return substantially less repeated semantic context,
Agents can request only the missing fact, and tool traces identify which kind
of discovery caused retries. The `list_metrics` request remains extensible
without accumulating top-level filter parameters, while the typed MCP schema
keeps supported narrowing explicit. AgentBench prompt version advances because
the Metis arm's available interaction protocol changed; raw and Metis isolation,
native capabilities, question order, and per-attempt budget remain unchanged.

## 7. Acceptance evidence

- service tests prove list bounds, truncation, and list/detail separation;
- MCP tests prove the exact vocabulary, annotations, and `list_metrics`
  scalar-or-list `search + meta_filter.models` schema;
- Agent-context conformance proves detail grains and relationship join/temporal
  evidence;
- E2E exercises staged list, detail, relationship, and compile calls;
- AgentBench smoke confirms an Agent can discover the new surface at runtime.
