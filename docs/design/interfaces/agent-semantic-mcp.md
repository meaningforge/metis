# Agent Semantic MCP Design

The primary MCP adapter is a thin, progressively disclosed view over shared
application services. It does not own semantic resolution or compilation.

`app/mcp` constructs one transport-neutral production `mcp.Server`. The HTTP
process wraps it with the SDK Streamable HTTP handler behind shared Bearer
authentication; the local `metis mcp` process runs it over the SDK stdio
transport. Both paths therefore register identical tools, schemas, server
instructions, error mapping, and observations.

```text
remote/shared Agent -> Gin + Bearer -> Streamable HTTP --+
                                                       +-> one MCP Server -> app/service/semantic
local Agent --------> local Principal -> stdio --------+
```

The stdio process is owned by one local Agent client. Stdout carries only MCP
JSON-RPC frames, while structured operational logs use stderr. Its explicit
wildcard local Principal enters the same Project-action authorizer as HTTP; the
transport does not contain a second authorization policy.

```text
MCP registration and project-context adapter (`app/mcp`)
  -> compact list/detail orchestration (`app/service/semantic.AgentSemanticService`)
    -> SemanticManifest and compatibility (`manifest`, `app/service/semantic.DiscoveryService`)
    -> compile orchestration (`app/service/semantic.CompileService`)
      -> resolver -> planner -> compiler
  -> live value orchestration (`app/service/semantic.DimensionValuesService`)
    -> exact Project Backend Renderer -> compiler -> bounded Runner
```

Compilation and governed execution use the shared application data-policy
snapshot before semantic optimization. MCP does not own policy evaluation. See
[data access policy](../../specs/operations/data-access-policy.md).

The interface uses compact discovery tools (`list_projects`, `list_models`,
`list_metrics`, `get_dimensions`) and explicit detail tools (`get_model`,
`get_metric`, `get_dimension`, `get_relationships`). `get_dimensions` follows
the established dbt MCP meaning: retrieve dimensions compatible with selected
metrics. Metis additionally permits a model anchor for metric-free queries and
retains typed canonical grouping objects required by its compiler.

`search_ontology_concepts` supplies exact concept identities when unknown;
`resolve_ontology_concept` returns visible canonical dimension candidates and
direct/descendant evidence. Both reuse generation routing and DiscoveryService.
Even a unique candidate requires caller selection; neither tool calls Planner
or changes semantic query resolution. See the [ontology contract](../../specs/semantic/ontology-resolution.md).

`list_metrics` deliberately separates two concerns at the MCP boundary:

```text
search                         meta_filter
lightweight metric retrieval   deterministic manifest narrowing
                               └── models: canonical model refs (OR)
```

The Agent interprets the user's language and chooses search terms. Metis applies
bounded deterministic matching and relevance ordering: canonical identity and
name matches rank above authored descriptions, and candidates matching all OR
terms rank above partial matches. Identifier separators normalize to token
boundaries. This is retrieval ordering, not natural-language semantic
resolution. `meta_filter` is a
typed MCP object rather than a free-form metadata map so supported narrowing
remains visible in the tool schema. The service layer may use narrower internal
DTOs; the MCP adapter owns the public transport shape.

Every compact metric candidate carries sorted `semantic_kinds` derived
from its Ossie extensions. This lets an Agent distinguish an ordinary metric
from cumulative, offset, conversion, fill, definition-filter, time-binding, or
semi-additive behavior without retrieving full detail. The kinds are discovery
evidence only; planning and physical lowering remain Metis responsibilities.
Narrow focused retrieval additionally carries the typed constraint parameters
needed to choose between candidates sharing a broad kind. A constraint-field
match also retains those parameters even on a larger page, so an Agent can
compare the explicitly requested semantic scope rather than infer it from a
metric name. Broad identity listings omit those parameters to preserve their
bounded index role.

List ordering is stable retrieval presentation, not an answer. A multi-candidate
or truncated metric result carries `selection_required`; exact same-name
cross-model collisions additionally carry `ambiguous_names` and canonical refs.
`selection_differences` tells the Agent whether visible candidates differ by
name, source model, datatype, authored aliases, semantic constraints, or already
available compatible dimensions. The Agent chooses a ref, requests selected
detail/compatibility as needed, or asks the user to clarify. Metis never turns
an un-authored business word into a default aggregation, rollup, or time meaning.

Strong metric aliases are explicit governed metadata. While Ossie has no native
alias field, Metis reads only the typed `METIS` `agent_discovery` metric
extension. Alias validation fails closed during Ossie model validation and the
aliases are returned on the candidate that owns them. Free-form descriptions
and AI context remain weaker search evidence; their arbitrary structure is not
treated as an alias schema.

Unscoped metric retrieval returns a progressively trimmed metric identity index
without descriptions or dimension fan-out. Focused retrieval retains authored
descriptions subject to the response-size budget. Once `meta_filter.models`
resolves to one model, a narrow result may add compact dimension routing hints.
Custom-calendar dimension discovery indexes logical grains and exposes a typed
canonical grouping contract: Agents submit the base time dimension plus logical
grain, while Metis owns lowering to the authored bucket. Compile validation
returns that same group-by object as structured remediation when a caller uses
the bucket directly or supplies a mismatched type/grain.

Canonical refs link stages; the MCP adapter only inherits or overrides project
scope, maps the typed `list_metrics` input, and maps tool errors. It does not
retry, rank with an LLM, choose a model, or call another transport. All tools
observe the same project authorization. A known canonical metric ref may skip
`list_metrics`; model discovery is a narrowing aid, not a mandatory hierarchy.

Agent guidance is layered by responsibility. Server instructions state shared
workflow and stopping rules once. Each tool description states only its purpose,
call condition, and critical semantic boundary. Input schemas own parameter
grammar, valid shapes, limits, and canonical-ref requirements; structured
results and errors own dynamic selection or remediation evidence. Exhaustive
behavior and examples remain in the normative specifications rather than being
repeated in every tool description.

Server guidance asks the Agent to preserve every requested output, grouping,
filter, ordering, and limit in the explicit semantic request and not substitute
a merely related field for a named business dimension. Point-in-time wording
directs the Agent toward authored temporal relationship evidence. These are
interaction safeguards; Metis does not parse the user's natural language or
reject an otherwise valid query because the Agent omitted unstated intent.

Selected metric detail exposes typed semantic constraints derived from Ossie,
including cumulative, offset, conversion, fill, and semi-additive behavior.
They help the Agent select and explain the governed metric while Metis remains
responsible for planning and compiling the behavior. When execution is wired,
server guidance prefers `query_metrics` for requested results and reserves
`compile_sql` for explicit external-execution SQL workflows.

`list_projects` first filters Projects by the caller's shared `discover`
decision, then exposes the closed operation capabilities of each visible
Project. `compile_sql` additionally requires `compile`; `query_metrics`
requires `execute`. This lets the Agent choose `query_metrics` only when runtime placement
is configured, without deliberately failing an execution call. The capability
check resolves configuration only; it is not a warehouse health probe and
reveals no DataSource identity.

`compile_sql` is read-only from the Agent's perspective and deterministically
returns a physical query and output schema. Structured MCP output remains the
complete compiler artifact; text content renders the complete physical-query
JSON object plus the output schema for text-only clients. It explicitly selects
only an SQL output dialect and never opens a warehouse connection. `query_metrics` is the
separate governed, bounded execution tool: it accepts the same semantic intent
without a dialect or DataSource override, derives placement from the resolved
project, and returns normalized rows without physical SQL.

`get_dimension_values` is a separate governed, bounded live-value tool. It
accepts one canonical dimension, up to eight optional compatible metric refs,
an optional logical time grain, and a limit. Metric-free calls use the existing
`distinct_values` semantic-query path; metric-scoped calls use an ordinary
grouped metric query. The tool returns only the typed dimension members plus
count and truncation evidence. It is read-only and idempotent but open-world
because it performs warehouse I/O. The closed-world `get_dimension` metadata
tool never conditionally executes it.

The server gives Agents a conditional trigger for this tool: inspect live
members when a user filter term may not match the canonical stored
representation, such as a name versus a code. The hint does not make the call
mandatory and does not move value selection or natural-language translation
into Metis.

`compare_metrics` is the closed two-period comparison tool. It accepts
canonical governed refs and exact periods, then returns complete typed values,
delta, and percent change at one shared grain. `attribute_metric` remains the
separate contribution workflow. Neither tool exposes its internal compiled
queries or asks the Agent to align warehouse rows.

The normative response fields, limits, and repair behavior live in
[`../../specs/semantic/agent-semantic-search.md`](../../specs/semantic/agent-semantic-search.md)
and [`../../specs/semantic/agent-query-contract.md`](../../specs/semantic/agent-query-contract.md).
The comparison result contract is specified in
[`../../specs/semantic/metric-comparison.md`](../../specs/semantic/metric-comparison.md).
