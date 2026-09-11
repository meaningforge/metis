# Agent Semantic Discovery

This specification defines the deterministic discovery capabilities used by the
primary Agent MCP surface and the lower-level generic SemanticManifest search API.
RFC-0051 records the current progressive-disclosure design; this document is
the normative contract.

## Primary MCP surface

The Agent-facing MCP server exposes eleven narrow closed-world discovery and
compilation tools:

```text
list_projects       list_models       get_model
list_metrics        get_metric
get_dimensions     get_dimension     get_relationships
compile_sql
search_ontology_concepts       resolve_ontology_concept
```

It also exposes one optional live, governed discovery workflow:

```text
get_dimension_values
```

When an executable Project DataSource is configured, the same primary surface
also exposes governed `query_metrics`. Agents use it by default when the user
needs result rows; `compile_sql` remains the explicit path when physical SQL is
needed for external execution.

Every discovery and compilation tool is read-only, idempotent, and closed-world.
Stages may be skipped when canonical identities or optional detail are already
known.

Tool descriptions state purpose, call conditions, and critical semantic
boundaries without duplicating parameter grammar. Input schemas are the
authority for field shapes, limits, canonical-ref requirements, and valid
values. Server instructions state shared workflow once, while structured
results and errors carry request-specific selection and remediation evidence.

The MCP server instructions define the stopping rule once for every client:
discover only the missing semantic identities, retrieve compatible dimensions
once, retrieve only selected details, submit the semantic request immediately
when its fields are known, and do not inspect unrelated assets speculatively.

Typical metric query when metric identity is unknown:

```text
list_metrics(search, meta_filter?) -> get_dimensions(metrics, search)
  -> get_dimension(selected time dimension, when needed)
  -> query_metrics (results) | compile_sql (external SQL)
```

If a canonical metric ref is already known, `list_metrics` is skipped. A model
is not a mandatory discovery parent for a metric; `meta_filter.models` is an
optional narrowing mechanism when candidate models are already known.

Typical metric-free query:

```text
list_models(search) -> get_dimensions(model, search)
  -> get_relationships(model, datasets, when needed) -> compile
```

`list_projects` is only for missing or changing project context. `get_model`
and `get_metric` are optional detail calls, not mandatory funnel stages.

## Compact indexes and explicit detail

Discovery-list tools are indexes. They return identity and the smallest metadata needed
to choose a candidate; they do not repeat authored AI context or nested assets.
Detail tools return richer metadata for one selected asset.

`list_projects(project_ids?)` returns `project_id`, display `name`, optional
`description`, and a closed `capabilities` list. Unauthorized Projects are
filtered before the result is built. `compile_sql` appears only when the caller
has the Project `compile` action. `query_metrics` appears only when the caller
has the Project `execute` action and the Project is wired to a DataSource whose Backend is
available through Runner. Capability discovery
does not open a Driver, resolve a secret, probe connectivity, or expose
DataSource identity. It does not embed models.

`list_models(project_id?, search?, limit?, cursor?)` returns canonical `ref`, `name`,
optional `description`, and typed `governance`. Search also considers visible
contained dataset, metric, and
dimension names without returning them in every list item. The default limit is
10, maximum 50. `get_model(ref)` adds authored `ai_context` and sorted name-only
inventories for datasets, metrics, and dimensions.

The Agent-facing `list_metrics` JSON contract is:

```text
list_metrics(
  project_id?: string,
  search?: string | string[],
  meta_filter?: {
    models?: string[]
  },
  limit?: int,
  cursor?: string
)
```

`list_metrics` returns canonical `ref`, `name`, `model`, `model_ref`, `data_type`,
explicitly authored `aliases`, compact `semantic_kinds`, focused typed
`semantic_constraints`, and optional
`description`, plus typed `governance`. `semantic_kinds` is the
sorted set of typed governed behaviors authored on that Ossie metric, including
cumulative, offset, conversion, fill, definition-filter, time-binding, and
semi-additive behavior. It is selection evidence; the Agent does not reproduce
the behavior in SQL. A focused search or model scope retains the typed
constraint parameters on narrow pages needed to distinguish candidates with
the same broad kind—for example, global semi-additive selection versus per-entity selection
followed by a governed rollup. An unconstrained broad identity listing omits
those parameters. Constraint identities, dimensions, selectors, and rollup
operators also participate in deterministic search, so callers can narrow by a
known semantic scope without first choosing a metric.
When one or more requested search terms match a metric's typed constraint
fields, that result retains its typed `semantic_constraints` even if the page
also contains more than five candidates. `search` is lightweight resource-specific retrieval.
A single string is normalized to one search term; a string array supplies one or
more OR terms. Matching is deterministic and case-insensitive over metric
identity, authored description, authored AI context, and typed semantic-constraint
fields. Identifier separators
normalize to token boundaries. Exact canonical/name matches rank above
substring matches, primary identity matches rank above descriptive evidence,
and candidates matching every supplied term rank above partial OR matches. Ties
sort by canonical ref. Model ranking awards the all-term bonus only when terms
co-occur in one contained semantic asset, not by combining unrelated fields.
At most 20 distinct non-empty search terms are accepted. This is not an
embedding or natural-language query; the Agent remains responsible for
understanding the user's language and choosing terms.

Metric ordering is deterministic retrieval, not semantic selection. A result
with more than one returned candidate, or with `truncated: true`, sets
`selection_required: true`; the caller must inspect candidate evidence and use
one exact canonical `metric:` ref in later metric selection calls. Unqualified
metric names are rejected for those calls. `ambiguous_names` records every exact
metric-name collision among the returned candidates and its canonical refs.
`selection_differences` is an ordered closed list drawn from `name`,
`model_ref`, `data_type`, `authored_aliases`, `semantic_constraints`, and
`compatible_dimensions`. It states which visible authored facts differ across
the returned candidates; it does not choose a candidate. A difference in
compact `semantic_kinds` is sufficient to report `semantic_constraints` even
when detailed constraint parameters were omitted for response size.
Metis does not infer an omitted aggregation, time meaning, rollup, or business
synonym from a lexical term such as `total`. Such vocabulary may participate in
matching only when it is authored on the relevant semantic asset.

Until Ossie defines a native metric-alias field, strong aliases use one
incubating Ossie custom extension:

```yaml
custom_extensions:
  - vendor_name: METIS
    data: '{"kind":"agent_discovery","aliases":["GMV","merchandise sales"]}'
```

`aliases` must contain 1–20 unique, non-empty strings of at most 128 Unicode
characters each. Duplicate extension kinds, malformed JSON, invalid types,
empty aliases, duplicate aliases, and oversized aliases fail model validation.
The extension affects deterministic Agent discovery only; it does not alter
metric evaluation, planning, or compilation. Alias exact/substring matches use
the same strong identity tier as the canonical metric name. Description and
arbitrary AI-context text remain weaker authored retrieval evidence and are
never interpreted as implicit aliases.

`meta_filter` is deterministic structured narrowing. Its initial `models` field
accepts canonical model refs returned by discovery; selected models are ORed and
deduplicated. The MCP contract MUST keep metadata filters typed rather than
accepting a free-form map. Additional filter fields require explicit semantics
and schema additions. `models` MUST NOT reappear as a top-level `list_metrics`
field.

The default metric limit is 10, maximum 50. An unconstrained call with neither
search nor model scope remains valid but behaves as a broad identity index: it
omits descriptions and dimension hints and reports `details_omitted`. Focused
search or model scope retains descriptions subject to the encoded response-size
budget. Compatible dimension refs are included as routing hints only when
`meta_filter.models` resolves to exactly one model and the result contains at
most five metrics. Unscoped search omits those hints so generic metric names
cannot fan out large dimension inventories or encourage refs from different
models to be combined. `get_dimensions` remains authoritative for types and
multi-metric intersection.
`get_metric(ref)` adds authored `ai_context` and typed semantic constraints
derived from the selected Ossie metric. Constraints identify governed behavior
such as cumulative windows, time offsets, offset-to-grain, conversion, fill,
definition filters, time binding, and semi-additive selection. They are semantic
evidence Metis applies during planning, not instructions for an Agent to
reconstruct physical SQL.

`compile_sql` retains the complete `CompiledQuery` as structured MCP output.
For text-only clients, its text content presents the complete `render_result`
JSON object followed by the output schema. This transport representation does
not alter the compiler artifact or relax client-side JSON validation.

List results include `truncated: true` and an opaque `next_cursor` when more
matching records exist. The Agent may continue with that cursor or use narrower
search terms or `meta_filter.models`. Cursors bind the resolved Project,
normalized query shape, caller and asset-visibility scope, and that Project's
independent semantic generation. Changing filters or moving a cursor to another
Project is `INVALID_QUERY`; activation or a visibility-scope change returns
`SEMANTIC_PAGINATION_RESTART_REQUIRED` with caller action `CHANGE_REQUEST`.
The caller restarts from the first page and never guesses an offset. Hidden
assets are filtered before rank, truncation, and cursor creation and therefore
cannot consume a cursor position.

A page whose encoded detail exceeds 2 KiB or a
truncated page omits item descriptions and reports `details_omitted`. If the
remaining identities still exceed 2 KiB, the lowest-ranked tail is removed and
the page reports `truncated`; use a narrower list call or the selected detail
tool. An unsearched multi-model page interleaves models before budget trimming
so one lexical model prefix cannot starve every other selected model.
Model-level context does not fan out a match to every metric.

The same cursor binding applies to generic `semantic_search` enumeration and
`get_dimensions`. An omitted cursor always begins at the first visible item.
Cursor bytes are opaque and are not a stable serialization format.

## Generation-scoped discovery index

Every immutable Project runtime generation owns a disposable discovery read
model derived from exactly its `SemanticManifest`. It contains canonical
identities, deterministic normalized substring postings, typed metric
constraints, metric dependency and source governance keys, and relationship
compatibility edges. It narrows candidates only: manifest lookup, asset
visibility, ranking, compatibility resolution, and compilation remain
authoritative. Index construction completes before generation publication;
direct embedders may build lazily and rebuild when their manifest pointer
changes. No index or cursor combines Projects.

Search preserves the existing exact, substring, context, token, and canonical
tie-break ordering. The indexed and full-scan reference order is covered by an
executable equivalence fixture. Synthetic 10k, 50k, and 100k estate benchmarks,
budgets, and reference-host evidence are recorded in RFC-0080 and the
operational capacity specification.

## Dimension compatibility and detail

`get_dimensions` accepts exactly one semantic anchor:

```go
type GetDimensionsRequest struct {
    ProjectID string   `json:"project_id,omitempty"`
    Model     string   `json:"model,omitempty"`
    Metrics   []string `json:"metrics,omitempty"`
    Search    []string `json:"search,omitempty"`
    Limit     *int     `json:"limit,omitempty"`
    Cursor    string   `json:"cursor,omitempty"`
}
```

For multiple metrics it returns only dimensions compatible with every selected
metric. Compatibility follows authored semantic population ownership; for
example, a conversion metric exposes dimensions reachable from its base-event
population. Search applies only after compatibility is proven.

Each compact dimension contains canonical `ref`, `name`, `model`, `model_ref`,
source `dataset`, and the exact `type` (`dimension` or `time_dimension`) needed
by compile. A custom-calendar base time dimension additionally contains
`canonical_groupings`; each entry carries a complete canonical `group_by`
object plus its explanatory `physical_bucket`. The Agent copies `group_by`
exactly and MUST NOT substitute `physical_bucket` as the semantic grouping ref.
Compact dimensions otherwise do not repeat descriptions, AI context, grains,
or relationships. Search includes canonical grouping grain names, complete
group-by refs/types, and explanatory physical-bucket refs. Canonical grouping
matches rank ahead of partial physical-field-name matches.

`get_dimension(ref)` returns the selected dimension's `data_type`, `label`,
primary-key status, description, authored AI context, and time `valid_grains`.
Custom calendars may also return compact semantic consequences:

```json
{
  "ref": "dimension:fiscal_dense.calendar.calendar_day",
  "type": "time_dimension",
  "valid_grains": ["year", "quarter", "month", "week", "day", "hour", "fiscal_week"],
  "canonical_groupings": [{
    "grain": "fiscal_week",
    "group_by": {
      "name": "dimension:fiscal_dense.calendar.calendar_day",
      "type": "time_dimension",
      "grain": "fiscal_week"
    },
    "physical_bucket": "dimension:fiscal_dense.calendar.fiscal_week_start"
  }],
  "semantic_evidence": [{
    "effect": "preserves_empty_periods",
    "grains": ["fiscal_week"]
  }]
}
```

`preserves_empty_periods` describes an authored semantic consequence; it does
not leak an internal planner flag.

## Relationships

`get_relationships(model, datasets?, search?)` is independent from dimension
listing. It returns relationships in deterministic name order. `datasets`
optionally restricts the result to relationships touching any named dataset.
Each item includes `name`, `from`, `to`, `from_columns`, `to_columns`, authored
AI context, and applicable semantic evidence.

Temporal relationships expose point-in-time evidence:
`from_time_dimension`, `to_valid_from`, `to_valid_to`, and `cardinality`.
This describes the authored contract, not a physical join plan. Invalid
semantic-critical extensions fail explicitly.

## Project scope and canonical refs

`project_id` is an explicit override and MUST be omitted when active project
context is configured; a model name is never a project ID. Otherwise MCP resolves
active project context from namespaced call metadata and then
`X-Metis-Project-Id`. Model, metric, and dimension refs returned by discovery
are authoritative inputs to later tools. Metis never guesses an ambiguous asset.
Every metadata operation requires the resolved Project `discover` action;
canonical refs and active project context do not carry authorization.
After that project decision, governed models, datasets, metrics, dimensions,
and relationships pass through the shared asset-visibility policy before any
ranking, limit, cursor, compatibility, or ambiguity result is constructed.
Summaries expose typed `governance` evidence; policy tags remain policy input
and are not returned. See [`asset-governance.md`](asset-governance.md).

## Lower-level generic SemanticManifest search

Ontology concept discovery and resolution use the bounded metadata-only
[ontology contract](ontology-resolution.md). Raw mapping expressions no longer
participate in generic or dedicated Agent search or returned evidence.

`SemanticSearchService` and `DiscoveryService.SearchSemantics` remain valid
lower-level REST/admin/internal capabilities. They may search or enumerate
mixed models, metrics, dimensions, relationships, and ontology concepts.
They are deliberately not registered as `search_semantics` on the primary MCP
surface. REST may continue to expose:

```text
POST /v1/projects/:project/semantic/search
```

## Ownership boundary

```text
Agent                         Metis
natural-language intent       deterministic SemanticManifest exposure
candidate selection           compatibility and name resolution
investigation strategy        validation, planning, and compilation
```

Discovery MUST NOT infer relative time, user intent, multilingual synonyms, or
business meaning with an LLM. Closed-world metadata tools MUST NOT expose
expressions, dependency graphs, full models, sample values, graph scores,
rejected paths, or unbounded context dumps. Actual values remain exclusive to
the separately authorized and bounded `get_dimension_values` workflow.

MCP instructions require the Agent to make every requested output, grouping,
filter, ordering, and limit explicit before compilation and prohibit replacing
a named business dimension with a merely related field. Questions about which
version or attribute applied at an event time direct the Agent to inspect
temporal relationship evidence. These instructions guide Agent behavior; they
do not authorize Metis to infer missing natural-language intent.

`get_dimension_values(dimension, metrics?, grain?, limit?)` is the separate
governed execution contract for actual members. `dimension` and optional
`metrics` are exact canonical refs returned by discovery. It accepts at most
eight compatible metrics, defaults `limit` to 100, caps it at 500, excludes
null, sorts ascending, and returns typed `values`, `count`, and `truncated`.
Metric-free calls use the owning dimension population; metric-scoped calls use
ordinary aligned-row union semantics for the selected metrics. It accepts no
filter, SQL, dialect, DataSource, Renderer, Driver, target, or endpoint field.

MCP guidance directs an Agent to consider this operation when a user-supplied
filter term may differ from the warehouse's canonical member representation,
such as a name versus a stored code. The guidance remains conditional: value
lookup is not a mandatory parent for every filter, and Metis returns evidence
without selecting or translating a business value on the Agent's behalf.
The tool contract limits this workflow to filter-member resolution. Grouping,
ordering, top-k, and limit intent use canonical discovery output and semantic
query fields directly; they MUST NOT trigger dimension-value discovery.

The operation is read-only, non-destructive, idempotent, and open-world. It
requires execution authorization and an executable Project DataSource. It MUST
NOT be conditionally embedded in the closed-world `get_dimension` metadata
tool, and returned values MUST NOT be persisted or added to logs, metrics,
traces, or errors.

## Compile repair

Metric lookup failures direct the caller to `list_metrics(search)`. Model
failures direct it to `list_models(search)`. Dimension failures direct it to
`get_dimensions(metrics, search)` or `get_dimensions(model, search)` according
to query shape. Grain failures return `INCOMPATIBLE_QUERY_GRAIN`. When a custom
grain is applied to its bucket or another dimension, details contain
machine-readable `remediation.group_by`, explanatory `physical_bucket`, and
`do_not_group_by`; suggestions contain the canonical base-time-dimension ref.
Metis does not silently replace the request or retry compilation.

Structured MCP results use object envelopes because the Go MCP SDK requires an
object-root output schema.

## Conformance

Tests must cover:

- deterministic project, model, metric, dimension, and relationship results;
- default/max list bounds and `truncated` behavior;
- compact-list versus selected-detail response boundaries;
- model-scoped metric dimension hints and unscoped hint omission;
- custom-calendar canonical grouping discovery and compile remediation;
- `list_metrics` MCP schema accepts scalar or list `search`, exposes typed
  `meta_filter.models`, and does not expose top-level `models`;
- multi-metric dimension compatibility;
- bounded typed dimension-value discovery, truncation, and metric-free versus
  metric-scoped population semantics;
- relationship join columns and temporal evidence;
- read-only, idempotent, closed-world annotations;
- MCP/service parity and staged discovery followed by compile;
- absence of generic search and arbitrary execution tools from the primary
  registry.
