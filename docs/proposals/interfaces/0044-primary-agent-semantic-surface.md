# RFC-0044: Staged Agent Semantic Query Surface

> **Design history:** This RFC preserves its original lifecycle status and rationale.
> Implementation claims describe the recorded design, not a guarantee that every
> proposed API remains current. See [current documentation](../../README.md) for
> supported interfaces. Package paths have been updated for this repository.

- **Status:** Implemented
- **Owners:** Metis maintainers
- **Created:** 2026-08-25
- **Last updated:** 2026-08-26
- **Scope:** Agent-facing MCP contracts, project context, semantic discovery facade, compile adapter, execution binding, diagnostics/repair, AgentBench
- **Supersedes:** the earlier unpublished `search_semantics + compile` primary MCP design

> **Refinement:** RFC-0050 preserves these four tools but supersedes the
> metric-mandatory discovery/compile routing described here. The current
> contract also supports model-scoped discovery and metric-free compilation.
>
> **Current surface:** RFC-0051 supersedes this RFC's fixed-four-tool and
> bundled-response decisions with narrow list/detail tools. The compile-only,
> canonical-ref, project-scope, and deterministic-service decisions remain.

## 1. Decision

Metis v1 exposes exactly four primary Agent-facing semantic MCP tools:

```text
list_projects
list_metrics
get_dimensions
compile
```

The cold-start funnel is:

```text
User question
    |
    v
Agent understands natural language
    |
    v
list_projects          # only when project context is unknown or user wants to switch
    |
    v
Agent selects project / business domain
    |
    v
list_metrics
    |
    v
Agent selects metric refs
    |
    v
get_dimensions
    |
    v
Agent selects dimensions proven compatible with those metrics
    |
    v
compile
    |
    v
physical SQL
```

The normal configured-MCP path SHOULD be shorter:

```text
MCP/application has active project_id
        |
        v
list_metrics
        |
        v
get_dimensions
        |
        v
compile
```

Project selection is therefore a **resolvable context**, not a parameter the
Agent must repeatedly guess on every tool call.

Stages are skippable whenever application, MCP request context, or conversation
state already contains the required canonical identities:

```text
project unknown                    -> 4 semantic calls
active project bound               -> 3 semantic calls
project + metric refs known        -> 2 semantic calls
all semantic refs already known    -> 1 semantic call
```

This is a decision funnel, not a mandatory protocol. An Agent MUST NOT call an
earlier discovery tool only to satisfy ordering when the required identity is
already known.

## 2. Product boundary

The long-term ownership boundary is normative:

> **The Agent understands the user. Metis exposes semantic choices and guarantees their semantics.**

The Agent/application layer owns probabilistic interpretation such as:

```text
natural-language understanding
multilingual interpretation
business-language mapping
relative-time interpretation
synonym inference
intent decomposition
candidate selection
```

Metis owns deterministic semantic behavior such as:

```text
project/catalog identity
metric identity
metric-to-dimension compatibility
ambiguity handling
reachability
relationship/path resolution
semantic validation
SemanticPlan construction
optimization
SQL AST lowering
physical dialect rendering
```

Metis Core MUST NOT require embeddings, vector databases, LLM rerankers,
intent classifiers, hosted relevance services, stochastic graph ranking, or
LLM-generated synonym expansion for correctness.

A deterministic lexical filter may use catalog-authored metadata, but it is not
a natural-language retrieval contract.

## 3. Why the primary surface is staged

A generic `search_semantics` tool minimizes the number of MCP tool schemas
but makes one tool carry several different Agent decisions:

```text
which project?
which metric?
which model?
which dimension?
which compatibility constraint?
```

That can work for a small catalog but becomes ambiguous when one tenant has many
projects and each project contains hundreds or thousands of metrics.

The v1 design intentionally reduces the decision space hierarchically:

```text
business domain
    -> measure
    -> compatible dimensions
    -> compile
```

The optimization target is therefore not "fewest tool definitions". It is:

> **the smallest and most deterministic choice set at each Agent decision.**

`search_semantics` may remain as a lower-level REST/admin/internal Catalog
capability. It is not a peer tool in the primary Agent MCP surface.

## 4. Reference-first compatibility principle

For Agent-facing semantic operations that have mature production analogues,
Metis SHOULD begin from those established contracts rather than inventing an API
from first principles.

The v1 reference set is:

```text
dbt / MetricFlow
  -> staged semantic funnel
  -> list_metrics / get_dimensions request shapes
  -> typed group_by and order_by query concepts

Snowflake Semantic Views / managed MCP
  -> metric-to-dimension compatibility as first-class semantic truth
  -> semantic resources and execution environment may be bound outside tool arguments

Databricks MCP
  -> separation between LLM-generated query intent and deterministic _meta/runtime config

Cube MCP
  -> compact discovery
  -> progressive disclosure
  -> semantic system remains the governed query/compile authority
```

Metis does not attempt wire compatibility with any of these systems. The goal is
to reuse production lessons where the semantics are equivalent.

A deviation from a mature analogous contract requires at least one of:

1. an existing Metis semantic/runtime constraint;
2. a deliberate compile-only boundary difference;
3. a materially simpler and safer MCP schema;
4. AgentBench or production evidence showing that the inherited contract is worse.

API taste alone is not sufficient justification.

## 5. Array-first parameter rule

The v1 Agent MCP schema follows an **array-first rule** for naturally repeatable
semantic values.

Normative rule:

> If an argument naturally admits multiple values, the MCP contract SHOULD expose
> one array form from v1 rather than a scalar form, a scalar-or-array union, or
> parallel singular/plural parameters.

Examples:

```text
project_ids        -> []string
search terms       -> []string
metrics            -> []string
group_by           -> []GroupByParam
filters            -> []Filter
order_by           -> []OrderByParam
```

Values that represent one active scope or one query bound remain scalar:

```text
project_id         -> string
limit              -> integer
```

Metis intentionally prefers:

```json
{"search":["revenue","arr"]}
```

over either a scalar-only field or a scalar-or-array union.

Unless explicitly stated otherwise, repeated search terms use deterministic OR
semantics and results are deduplicated.

## 6. Project identity and active project context

Project has two distinct concepts that MUST NOT be conflated:

```text
Project identity
  -> project_id
  -> name
  -> description
  -> model summaries

Active project context
  -> which project a current Agent operation should use by default
```

`project_id` is the canonical routing identity. Human-readable project `name`
exists for discovery and user interaction, but MUST NOT be the authoritative
routing key when a canonical ID is available.

### 6.1 Active project resolution

For project-scoped primary tools, Metis resolves one active project using this
precedence:

```text
1. explicit tool argument project_id
2. MCP call _meta project-id
3. HTTP X-Metis-Project-Id header
4. otherwise PROJECT_REQUIRED
```

The `_meta` key SHOULD use a Metis-owned namespaced key, for example:

```json
{
  "_meta": {
    "<metis-namespace>/project-id": "prj_finance"
  }
}
```

The exact namespace is an implementation/configuration detail until Metis owns a
stable public namespace, but unnamespaced custom metadata SHOULD be avoided.

The HTTP default is:

```text
X-Metis-Project-Id: prj_finance
```

A typical configured MCP client therefore sends the project once at connection
or request transport level and does not require the Agent to repeat it:

```text
Authorization: Bearer ...
X-Metis-Project-Id: prj_finance
```

### 6.2 Explicit override

Project-scoped tool schemas expose optional `project_id` as an **override**, not
a required value:

```go
ProjectID string `json:"project_id,omitempty"`
```

This supports a user intent such as:

```text
"Switch to the sales project and show bookings"
```

The Agent may discover `sales` through `list_projects`, obtain its
canonical `project_id`, and explicitly pass that ID on subsequent calls.

An explicit project override applies to that tool operation. V1 MUST NOT silently
mutate server-global state merely because one call specifies another project.
Applications that want a persistent conversation-level switch may update their
own MCP/session header or `_meta` context after the user explicitly changes
project.

### 6.3 Routing is not authorization

Project context selects among projects the caller is already authorized to use.
It never grants access.

Normative rule:

> **Project context is routing state, not authorization state.**

Resolution therefore behaves as:

```text
authenticated caller
      |
      v
authorized project set
      |
      v
resolved project_id
      |
      +-- authorized -> continue
      |
      +-- unauthorized -> PROJECT_ACCESS_DENIED
```

A caller MUST NOT gain access by setting a different `_meta` value or
`X-Metis-Project-Id` header.

## 7. `list_projects`

`list_projects` is the project discovery/introspection tool and is the one
primary tool that does not require an active project context.

### 7.1 Request

V1 supports optional canonical ID filtering:

```go
type ListProjectsRequest struct {
    ProjectIDs []string `json:"project_ids,omitempty"`
}
```

Examples:

```json
{}
```

lists all projects visible to the caller.

```json
{
  "project_ids": ["prj_finance"]
}
```

returns the matching visible project if authorized.

The array shape avoids introducing a separate `get_project` tool and supports
multi-project inspection without changing the contract.

### 7.2 Response

Response records expose canonical identity plus compact model metadata:

```json
[
  {
    "project_id": "prj_finance",
    "name": "finance",
    "description": "Revenue, ARR, billing, and financial performance",
    "models": [
      {
        "name": "subscription",
        "description": "Subscription and recurring revenue semantics"
      },
      {
        "name": "billing",
        "description": "Billing and invoice semantics"
      }
    ]
  }
]
```

The response MUST NOT contain:

```text
project relevance scores
AI ranking scores
embedding similarity
full model definitions
relationship graphs
metric dumps
```

The official Go MCP SDK requires structured tool output schemas to have an
object root. Wire responses therefore use compact envelopes (`projects`,
`metrics`, and `dimensions`) around the item arrays shown in this RFC; this is
a transport-shape constraint, not an additional semantic layer.

### 7.3 Models are visible but not a mandatory routing step

There is deliberately no primary `list_models` tool.

Model discovery is supported for introspection through compact model summaries,
but model selection is not a required step in the primary semantic query
workflow.

Model is a semantic namespace and ownership fact. The intended identity flow is:

```text
active project
  -> metric ref carries model ownership
  -> dimension ref carries model ownership
  -> compile infers model from selected canonical refs
```

Metric and dimension discovery results MAY expose `model` as compact metadata
for interpretation and disambiguation.

A standalone model-inspection tool requires independent evidence and MUST NOT be
added merely because Metis internally has models.

## 8. `list_metrics`

### 8.1 Request

The v1 request follows the production MetricFlow/dbt pattern while moving
project routing into resolvable context:

```go
type ListMetricsRequest struct {
    ProjectID string   `json:"project_id,omitempty"`
    Search    []string `json:"search,omitempty"`
}
```

Normal configured-project call:

```json
{
  "search": ["revenue", "arr"]
}
```

Explicit project override:

```json
{
  "project_id": "prj_sales",
  "search": ["bookings"]
}
```

### 8.2 Semantics

- `project_id` is optional and follows active-project precedence;
- omitted or empty `search` enumerates metrics in the resolved project;
- each search term is normalized deterministically;
- search terms are ORed;
- matching is deterministic case-insensitive substring matching against metric
  name, model, and authored description in v1;
- duplicate matches are returned once;
- `search` is not a free-form natural-language question contract;
- no embedding, semantic similarity, hidden synonym inference, LLM ranking, or stochastic ranking is performed.

A future deterministic extension MAY also match explicitly declared catalog
aliases/labels if the underlying semantic schema has stable first-class fields
for them. Such fields remain authored catalog metadata, not inferred AI state.

### 8.3 Response

```json
[
  {
    "ref": "metric:subscription.arr",
    "name": "arr",
    "model": "subscription",
    "description": "Annual recurring revenue"
  }
]
```

Canonical `ref` is the preferred identity for later Agent calls.

A plain metric name MAY be accepted by later tools only when it resolves
unambiguously inside the resolved project. Ambiguity MUST fail closed and tell
the caller to use the canonical ref returned by `list_metrics`.

### 8.4 Deliberately absent parameters

V1 does not add the following without evidence:

```text
model
kinds
cursor
page_size
limit
include_context
include_graph
for_metrics
```

Metric-to-dimension compatibility belongs to `get_dimensions`, not a
`for_metrics` option on generic search.

## 9. `get_dimensions`

### 9.1 Request

```go
type GetDimensionsRequest struct {
    ProjectID string   `json:"project_id,omitempty"`
    Metrics   []string `json:"metrics"`
    Search    []string `json:"search,omitempty"`
}
```

Normal configured-project call:

```json
{
  "metrics": [
    "metric:subscription.arr",
    "metric:subscription.customer_count"
  ],
  "search": ["region", "segment"]
}
```

### 9.2 Semantics

`metrics` SHOULD be canonical refs returned by `list_metrics`.

The operation is not generic dimension search. The semantic order is:

```text
CompatibleDimensions(metric_1)
    INTERSECT
CompatibleDimensions(metric_2)
    INTERSECT
...
    THEN
optional deterministic search-term filter
```

Only dimensions proven compatible with **all selected metrics** are returned.

Search terms are ORed after compatibility has been established:

```text
compatible_with_all_metrics
AND
(name contains search_1 OR name contains search_2 OR ...)
```

This is deterministic graph/compatibility resolution, not graph relevance
ranking.

Ambiguous, unreachable, or incompatible dimensions are omitted from the compact
primary response. Rich path/evidence diagnostics may remain available through
lower-level REST/admin/debug interfaces.

### 9.3 Response

```json
[
  {
    "ref": "dimension:subscription.customer.region",
    "name": "customer.region",
    "model": "subscription",
    "description": "Customer geographic region",
    "type": "dimension"
  }
]
```

As refined by RFC-0049, time dimensions additionally return
`valid_grains`. Custom calendar grains may carry compact
`semantic_evidence`, such as `preserves_empty_periods`, when that consequence
materially affects Agent routing. The response does not expose the planner's
internal representation of that behavior.

Current v1 `SemanticQuery` scope is one semantic model. Therefore selected metric
refs MUST belong to one semantic model. Cross-model metric queries fail closed
until Metis defines explicit cross-model semantic-query semantics.

## 10. `compile`

`compile` is the sole primary Agent authority for:

```text
semantic validation
canonical resolution
relationship/path resolution
SemanticPlan construction
optimization
SQL planning/lowering
physical SQL generation
```

There is no primary `validate`, `explain`, or `query_metrics` peer tool.

### 10.1 Request

```go
type AgentCompileRequest struct {
    ProjectID string              `json:"project_id,omitempty"`
    Metrics   []string            `json:"metrics"`
    GroupBy   []AgentGroupByParam `json:"group_by,omitempty"`
    Filters   []query.Filter      `json:"filters,omitempty"`
    OrderBy   []AgentOrderByParam `json:"order_by,omitempty"`
    Limit     *int                `json:"limit,omitempty"`
}
```

`project_id` is an optional scope override; it is not an Agent-required field
when project context is already bound.

### 10.2 Metrics

`metrics` are an array even for a one-metric query.

Agents SHOULD pass canonical refs returned by `list_metrics`:

```json
{
  "metrics": ["metric:subscription.arr"]
}
```

### 10.3 Group by

The group-by structure follows the mature MetricFlow/dbt MCP pattern rather than
a bare string list:

```go
type AgentGroupByParam struct {
    Name  string           `json:"name"`
    Type  AgentGroupByType `json:"type"`
    Grain *query.TimeGrain `json:"grain,omitempty"`
}
```

Supported v1 types are:

```text
dimension
time_dimension
```

Example:

```json
{
  "group_by": [
    {
      "name": "dimension:subscription.customer.region",
      "type": "dimension"
    },
    {
      "name": "dimension:subscription.metric_time",
      "type": "time_dimension",
      "grain": "month"
    }
  ]
}
```

`grain` applies only to time dimensions.

MetricFlow's entity grouping is not copied into v1 because Metis does not yet
expose entity as an equivalent first-class SemanticQuery grouping type. Adding
it requires semantic-model support, not schema imitation.

### 10.4 Filters: deliberate Metis deviation

MetricFlow/dbt MCP exposes a Semantic Layer `where` string. Metis v1 does not
introduce that grammar because Metis already has a typed filter contract and a
native expression/semantic analysis path.

V1 therefore keeps an array of existing typed filter objects:

```json
{
  "filters": [
    {
      "field": "dimension:subscription.customer.segment",
      "operator": "eq",
      "value": "enterprise"
    }
  ]
}
```

This is a deliberate compatibility deviation. Metis MUST NOT create a second
Agent-only predicate grammar solely to look more similar to dbt.

### 10.5 Order by

Order-by remains an array:

```go
type AgentOrderByParam struct {
    Name       string `json:"name"`
    Descending bool   `json:"descending,omitempty"`
}
```

### 10.6 Limit

`limit` remains scalar because it is one query-result bound, not a repeatable
semantic value.

## 11. Execution binding and CompileTarget

Execution target selection is deliberately outside the Agent semantic contract.

The primary MCP schema MUST NOT ask the LLM to select:

```text
engine
dialect
CompileTarget
execution_binding
warehouse
endpoint
```

These are deployment/project configuration.

### 11.1 Existing Project manifest is authoritative

Metis already defines execution binding in the deployable Project manifest. The
v1 RFC reuses that configuration model rather than introducing a second source
of truth.

The current manifest shape is conceptually:

```yaml
project: customer-360

default_execution_binding: doris-prod

models:
  - path: ./models/sales.ossie.yaml
  - name: product_usage
    path: ./models/product_usage.ossie.yaml
    execution_binding: snowflake-prod

execution_bindings:
  doris-prod:
    engine: metis-native
    dialect: DORIS
  snowflake-prod:
    engine: metis-native
    dialect: snowflake
```

Normative rule:

> **The existing Metis Project manifest is the authoritative configuration source
> for project-default and model-specific execution bindings.**

RFC-0044 MUST NOT introduce a second MCP metadata/header mechanism or a second
Agent-facing configuration object for execution binding selection.

The current `ExecutionBinding` schema owns `engine` and optional `dialect`.
Endpoint, warehouse, or other physical destination fields are not implied by
this RFC; if they become part of execution binding later, they remain in the
operator-controlled configuration layer rather than the primary Agent schema.

### 11.2 Primary MCP resolution chain

The intended primary MCP chain is:

```text
resolved project_id
      |
      v
canonical metric refs
      |
      v
inferred semantic model
      |
      v
Project manifest
      |
      +-- model.execution_binding, when configured
      |
      +-- otherwise default_execution_binding
      |
      v
ExecutionBinding
      |
      v
CompileTarget
      |
      +-- engine / compiler path
      +-- dialect
      |
      v
physical SQL
```

For primary MCP, the effective binding precedence is therefore:

```text
1. model.execution_binding
2. project.default_execution_binding
3. otherwise EXECUTION_BINDING_REQUIRED
```

The project manifest MAY define one project default plus model-specific
overrides. For every compile, exactly one effective binding must resolve.

For example:

```text
sales metric
  -> model=sales
  -> no model override
  -> default_execution_binding=doris-prod
  -> Doris SQL

product_usage metric
  -> model=product_usage
  -> execution_binding=snowflake-prod
  -> Snowflake SQL
```

The same Agent-facing request shape remains unchanged:

```json
{
  "metrics": ["metric:sales.revenue"],
  "group_by": [
    {"name":"dimension:sales.customer.region","type":"dimension"}
  ]
}
```

The Agent does not need to know which binding, engine, or dialect produced the
physical SQL.

### 11.3 Lower-level request override remains internal/admin capability

The existing execution resolver also supports an explicit request binding
override. Its full internal precedence is:

```text
1. explicit request binding override
2. model.execution_binding
3. project.default_execution_binding
4. otherwise EXECUTION_BINDING_REQUIRED
```

That capability MAY remain available to lower-level REST/admin/testing/operator
workflows. It is not part of the v1 primary Agent MCP contract.

This preserves the useful existing override without making physical compilation
routing an LLM decision.

### 11.4 No MCP binding side channel

Unlike `project_id`, v1 does **not** expose `execution_binding` through any
primary Agent selection surface.

In particular, primary MCP MUST NOT add:

```text
execution_binding tool argument
execution-binding _meta key
X-Metis-Execution-Binding header
engine or dialect override
```

The distinction is intentional:

```text
project_id
  -> user-facing semantic/business scope
  -> may be inherited from MCP/application context
  -> user may explicitly switch projects

execution_binding
  -> operator-controlled physical compilation config
  -> resolved from the Project manifest
  -> Agent should not choose it
```

`_meta` and `X-Metis-Project-Id` are project-routing mechanisms only. They MUST
NOT become a second execution-binding configuration channel.

### 11.5 Three ownership layers

The final v1 ownership model is:

```text
LLM / Agent controlled
----------------------
metrics
group_by
filters
order_by
search terms
explicit user-requested project_id override

Application / MCP context controlled
------------------------------------
default active project_id
  via _meta or X-Metis-Project-Id

Project manifest / operator controlled
--------------------------------------
default_execution_binding
model.execution_binding
execution_bindings
CompileTarget
engine
dialect
future physical destination fields, if added
```

This follows the strongest common lesson from Databricks and Snowflake: context
and runtime resources that can be known deterministically SHOULD be bound outside
LLM-generated semantic arguments. In Metis, the existing Project manifest is the
specific source of truth for execution binding.

## 12. Progressive disclosure

Primary discovery responses remain compact:

```text
list_projects
  -> project id/name/description + compact model summaries

list_metrics
  -> metric ref/name/model/description

get_dimensions
  -> compatible dimension ref/name/model/description

compile
  -> physical query + existing compact output/execution metadata
```

Primary MCP discovery responses MUST NOT preload:

```text
relationship graph dumps
chosen relationship paths
metric dependency graphs
metric expressions
source dataset internals
compatibility evidence for rejected dimensions
numeric relevance scores
match reasons
sampled warehouse values
full semantic model documents
```

Detailed REST/admin/explain interfaces may retain richer evidence for humans,
diagnostics, and development tooling.

## 13. Repair contract

Compile errors may include deterministic, directly callable repair actions that
point only to registered primary discovery tools.

Repair actions SHOULD omit `project_id` when the failed request inherited active
project context. They SHOULD include it only when the failed request explicitly
overrode the active project and the repair must preserve that scope.

Metric repair using inherited project context:

```json
{
  "repair": {
    "tool": "list_metrics",
    "arguments": {
      "search": ["revenu"]
    }
  }
}
```

Dimension/field repair:

```json
{
  "repair": {
    "tool": "get_dimensions",
    "arguments": {
      "metrics": ["metric:subscription.revenue"],
      "search": ["region"]
    }
  }
}
```

Repair is advisory and deterministic.

Metis MUST NOT perform hidden:

```text
LLM synonym expansion
natural-language rewriting
automatic semantic selection
automatic retry
embedding lookup
AI reranking
```

A failed compile remains failed until the caller explicitly performs another
operation.

## 14. Lower-level capabilities are not primary MCP tools

The following capabilities may remain in service/REST/admin layers without
becoming peer Agent MCP choices:

```text
search_semantics
get_model
get_metric
get_relationships
semantic_context
validate
explain
explicit execution_binding override
```

The primary MCP surface does not mirror Metis's internal service taxonomy.

The following names are not part of the v1 primary MCP surface:

```text
search_semantics
list_models
get_model
get_metric
get_metric_dimensions
get_dimension
get_relationships
get_semantic_context
validate_query
explain_query
query_metrics
```

## 15. Dimension values

MetricFlow/dbt exposes `get_dimension_values`, and dimension-value lookup can be
useful for Agent filter construction. However it crosses Metis's current
compile-only boundary because it requires reading distinct values from the data
plane.

Therefore v1 does not add `get_dimension_values`.

This capability requires a separate RFC covering:

```text
execution ownership
result cardinality
privacy/data exposure
sampling vs exact values
authorization
latency/cost
caching
```

Its absence MUST NOT be worked around by adding sampled values to
`get_dimensions`.

## 16. AgentBench requirements

AgentBench MUST remain neutral. The system prompt MUST NOT prescribe the exact
funnel order; the Agent chooses tools based on available context.

The benchmark MUST explicitly test at least two project-context modes:

```text
cold start
  -> no active project context

configured project
  -> project_id supplied by MCP/application context
```

It MUST measure at least:

```text
compile correctness
answer correctness where execution is available
semantic calls to first valid compile
MCP request bytes
MCP response bytes
input/context tokens
latency P50/P90
compile repair count
wrong-project selection rate
wrong-metric selection rate
wrong/incompatible-dimension selection rate
```

Expected efficiency envelope:

```text
configured project:
  successful path typically <= 3 semantic calls

cold start:
  successful path typically <= 4 semantic calls

repair:
  additional targeted discovery + compile calls are acceptable
```

Benchmark comparisons SHOULD include at least:

```text
A: generic search_semantics + compile
B: staged tools with cold-start project discovery
C: staged tools with application-bound project context
```

The staged surface is accepted as the v1 design, but AgentBench remains the
mechanism for detecting regressions or evidence for future contract changes.

## 17. Observability requirements

The primary MCP vocabulary MUST remain bounded and low-cardinality:

```text
list_projects
list_metrics
get_dimensions
compile
```

Metrics/traces SHOULD capture:

```text
tool name
success/error
latency
request bytes
response bytes
compile error code
repair presence
project source = argument | meta | header | missing
execution binding resolution success/error
```

They MUST NOT use project IDs/names, execution-binding names, metric names,
dimension names, natural-language search terms, endpoint names, or SQL text as
metric labels.

## 18. Non-goals

RFC-0044 v1 does not add:

- natural-language understanding inside Metis Core;
- embeddings/vector search/LLM reranking;
- AI relevance scores;
- stochastic graph ranking;
- a mandatory model-routing step;
- a `list_models` primary tool;
- cross-model metric compilation;
- sampled dimension-value execution;
- warehouse query execution;
- a second free-form filter language;
- automatic semantic selection;
- automatic compile retry;
- one dynamically generated MCP tool per project/model;
- full semantic-context preloading;
- Agent-selected engine or dialect;
- primary-MCP `execution_binding` override;
- MCP `_meta` or HTTP-header execution-binding override.

## 19. Acceptance criteria

RFC-0044 is implemented only when all of the following are true:

- MCP exposes exactly `list_projects`, `list_metrics`,
  `get_dimensions`, and `compile` when compile is configured;
- `search_semantics` is not registered as a primary Agent MCP tool;
- `list_projects` accepts optional `project_ids []string` and does not
  require active project context;
- project responses expose canonical `project_id`, human-readable name, and
  compact model metadata without dumping full semantic models;
- there is no mandatory `list_models` stage;
- project-scoped primary tools accept optional singular `project_id` override;
- active project resolution follows argument > namespaced `_meta` >
  `X-Metis-Project-Id` > `PROJECT_REQUIRED`;
- project context is authorization-checked and never grants access;
- metric/dimension results carry model-qualified canonical refs;
- `list_metrics` exposes optional `project_id` plus optional `search []string`;
- metric search terms use deterministic OR substring matching and deduplication;
- `get_dimensions` exposes optional `project_id`, required `metrics []string`,
  plus optional `search []string`;
- returned dimensions are compatible with every selected metric before search filtering;
- model is inferred from canonical metric refs rather than required as an Agent routing parameter;
- cross-model metric selection fails closed in v1;
- repeatable Agent inputs use arrays instead of scalar-or-array unions unless a
  later RFC documents a concrete exception;
- Agent-facing compile uses `metrics`, `group_by`, `filters`, and `order_by` as arrays;
- Agent-facing compile does not expose execution binding, engine, dialect,
  endpoint, warehouse, or `CompileTarget`;
- the existing Metis Project manifest is the authoritative source for
  `default_execution_binding`, model-specific `execution_binding`, and named
  `execution_bindings`;
- primary MCP effective execution-binding resolution follows
  `model.execution_binding` > `project.default_execution_binding` > fail closed;
- the lower-level explicit request binding override may remain for
  REST/admin/testing/operator workflows and, when used, precedes model/default
  configuration without becoming a primary Agent input;
- primary MCP does not introduce an execution-binding selector through tool
  arguments, `_meta`, HTTP headers, engine, or dialect overrides;
- the resolved effective execution binding deterministically produces
  `CompileTarget`;
- missing execution configuration fails closed instead of asking the Agent to
  choose an engine/dialect;
- MCP compile maps to the existing typed internal SemanticQuery/compiler path;
- compile is the only primary validation/resolution/planning/SQL-generation authority;
- supported compile failures return directly callable array-shaped repair arguments
  using `list_metrics` or `get_dimensions`;
- lower-level search/context/validate/explain/admin execution-binding capabilities
  may remain without becoming primary peer MCP choices;
- AgentBench exercises both cold-start and pre-bound-project contexts without
  hard-coding tool order;
- tool-call observability remains bounded and low-cardinality;
- correctness, transport, observability, E2E, packaging, and applicable
  real-engine gates pass before the RFC status becomes `Implemented`.

## 20. Alternatives rejected

### 20.1 Permanent `search_semantics + compile`

Rejected as the v1 production-scale primary surface. It minimizes schema count
but collapses project routing, metric discovery, dimension discovery, and metric
compatibility into one overloaded decision surface.

### 20.2 `list_projects -> list_models -> list_metrics -> get_dimensions -> compile`

Rejected. Model is useful namespace metadata but should not become a mandatory
Agent decision before metric discovery.

### 20.3 One MCP tool per project or model

Rejected. Tool count would grow with user configuration and degrade stable Agent
tool selection. Projects/models are data/configuration, not tool names.

### 20.4 Generic search with `for_metrics`

Rejected for the primary surface. `for_metrics` recreates an important semantic
operation inside a generic search parameter. Metric-constrained dimension
compatibility deserves the explicit `get_dimensions(metrics=...)` tool.

### 20.5 Full semantic context before compile

Rejected. It increases token cost and exposes graph/planner internals before the
Agent demonstrates a need for them.

### 20.6 Agent-selected execution binding / dialect

Rejected for primary MCP. Execution target selection is deterministic
Project-manifest configuration. Exposing it to the LLM through a tool argument,
`_meta`, or HTTP header would create a second source of truth, add decision
surface without adding semantic intent, and allow otherwise identical semantic
requests to produce inconsistent physical SQL.

The existing lower-level request binding override is retained for explicit
admin/testing/operator workflows; it is not promoted into the primary Agent
surface.

### 20.7 Copy every dbt parameter literally

Rejected. Mature contracts are the default reference, not an obligation to copy
semantics Metis does not have. Examples of deliberate v1 differences are:

```text
dbt search scalar-or-array union
  -> Metis array-only search for predictable MCP schemas
dbt explicit project parameter
  -> Metis project_id may be inherited from MCP/application context
dbt where string
  -> Metis existing typed filters
dbt entity group-by
  -> omitted until Metis has equivalent semantic support
dbt query_metrics execution
  -> Metis compile-only boundary
```

Any future deviation should be documented with the same discipline.
