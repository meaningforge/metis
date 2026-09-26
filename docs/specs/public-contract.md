# Public Contract and Vocabulary

This specification defines which Metis names and interfaces are intended to be stable for the v0.1 line and fixes several terms before the first public release.

## Compatibility surface

The v0.1 compatibility surface is intentionally narrower than the set of exported Go identifiers in the repository.

`metis project test` is a project-author developer tool, not an additional query
or analytics operation. Its [suite and report contract](testing/project-compile-regression.md)
covers offline compilation and runtime metric-result checks. It does not expose
fixture management, arbitrary assertions, or host-policy testing. Internal engine
tests and existing public analytics services remain separate contracts.

The following are user-facing contracts and changes to them require an explicit compatibility decision:

- Metis manifest fields and their serialized names;
- Metis-owned Ossie extension fields and their serialized names;
- REST and MCP request/response fields;
- stable Metis error-code strings;
- documented CLI commands, flags, and emitted machine-readable formats;
- `SqlRenderResult`, `OutputSchema`, explicit compile-only SQLDialect values, and
  Project/DataSource references exposed through user-facing service contracts.

`CompileTarget` and semantic Engine have been removed. Transitional
`ExecutionBinding`, `target`, and `default_target` configuration values are not
forward compatibility promises. Their removal under [Core runtime contract](operations/runtime-bootstrap.md) and ADR-0010 does not
require a replacement alias that would preserve their authority.

The public attribution operation is `POST /v1/attribute-metric` and MCP
`attribute_metric`. It accepts governed semantic refs and exact periods only,
and returns `AttributeMetricResult` with decimal-string numerical evidence.
Compiled attribution SQL, bundle entries, schemas, DataSource identity, and
runtime controls are internal contracts.

The public comparison operation is `POST /v1/compare-metrics` and MCP
`compare_metrics`. It accepts governed canonical refs, one shared filter/grain,
and two exact periods. It returns complete aligned tuple evidence with explicit
absence/null/zero states and decimal-string delta/percent values. Compiled SQL,
period artifacts, schemas, DataSource identity, and runtime controls remain
internal contracts.

An exported Go identifier is not automatically a v0.1 compatibility promise. Packages that are primarily implementation boundaries, including planner internals, may expose identifiers because Go package boundaries require it. A package becomes a supported library API only when its package documentation says so explicitly. Before v1, source compatibility for other Go packages is best-effort rather than guaranteed.

This rule prevents accidental compatibility commitments from the repository's internal package graph while keeping externally observable contracts deliberate and reviewable.

Principal-aware data-access enforcement is specified in
[data policy](operations/data-access-policy.md). It does not add caller-controlled
policy to compilation or governed execution.

Ontology metadata discovery and explicit candidate resolution are defined by
[ontology resolution](semantic/ontology-resolution.md). They supply evidence
only; canonical query refs remain the sole caller-selected query identities.

## Grain

Metis uses **grain** as the canonical term and serialized field name. It is not being renamed to `granularity` for v0.1.

A grain is the semantic grouping level at which a value is defined, produced, or compared. For a time dimension, Metis `grain` corresponds to what dbt/MetricFlow and some other semantic-layer systems call time **granularity**. Grain is broader than a time-unit label: output grain and shared metric grain may also include non-time grouping dimensions.

Examples:

- `day` is a time grain;
- `country + day` is an output grain;
- two metrics may be shareable only at a compatible common grain.

`granularity` may be used in explanatory comparisons with other systems, but it is not a second Metis field name.

## Conversion entity

**Conversion entity** is the identity link used by an Ossie conversion metric to associate a base event with a conversion event. Metis preserves the Ossie vocabulary rather than introducing a second Metis-owned synonym.

The canonical conversion field is `entity`, and its value uses the same property-pair shape as `constant_properties` entries:

```json
{
  "kind": "conversion",
  "entity": {
    "base_property": "user_id",
    "conversion_property": "purchaser_id"
  }
}
```

In Go, the canonical Ossie representation is `ConversionMetricSpec.Entity` with a `ConversionPropertyPair` value.

`conversion_entity` is not a canonical serialized field and must not be introduced as a compatibility alias in documentation or fixtures. A conversion entity is not a general semantic-model join key, relationship declaration, or globally addressable semantic entity. General dataset connectivity remains represented by relationships.

## Evidence and proof

Metis intentionally keeps **evidence** and **proof** as separate terms.

**Proof** is a machine-checkable justification required to permit a semantic transformation, placement, or other correctness-sensitive decision. If a required proof cannot be established, the operation must fail closed or remain unapplied.

**Evidence** is a structured semantic fact retained so validation, explanation, lineage, auditing, or regression checks can describe what is true about a plan or graph. Evidence may record the result of a proof, but evidence is not itself permission to perform a transformation unless the relevant contract explicitly says so.

In short:

> Proof enables a decision. Evidence records semantic facts.

A type should use `Proof` only when its value participates in authorizing a correctness-sensitive operation. Explanatory or lineage records should use `Evidence`.

## Semantic planning errors

Stable error-code strings must describe durable semantic behavior rather than retired implementation structures.

The canonical metric-planning error codes are:

- `METRIC_PLANNING_FAILED`
- `METRIC_PLANNING_UNSUPPORTED`

Names containing `METRIC_EVALUATION_*` are not part of the v0.1 public error
contract. `MetricEvaluationPlan` is a canonical internal compiler IR, but
internal pass and type names are not stable Agent-facing failure categories.

## Caller action

Every non-success REST or MCP semantic payload MUST contain a precise `code` and
one `caller_action`. `caller_action` is the next machine-actionable remediation
category for the failure:

- `CHANGE_REQUEST`: the submitted query or request parameters must change;
- `CHANGE_MODEL`: the semantic model must change, and the caller cannot act;
- `CHANGE_TARGET`: the requested SQL dialect or deployment DataSource/Backend
  configuration must change;
- `AUTHENTICATE`: credentials must be supplied or refreshed;
- `REPORT_DEFECT`: nothing outside Metis can fix the failure.

Each `code` MUST map to exactly one `caller_action`, derived centrally from the
code alone. A transport MUST NOT derive the action from runtime context, and MUST
NOT parse a code name. Unregistered codes and unknown Go errors MUST fail closed
to `REPORT_DEFECT`.

`INCONSISTENT_ATTRIBUTION_RESULT` also maps to `REPORT_DEFECT`: it explicitly
names a complete warehouse result violating a Metis-proven reconciliation
invariant. `UNSUPPORTED_METRIC_ATTRIBUTION` maps to `CHANGE_REQUEST`.

`INCONSISTENT_COMPARISON_RESULT` likewise maps to `REPORT_DEFECT`;
`UNSUPPORTED_METRIC_COMPARISON` maps to `CHANGE_REQUEST`.

`caller_action` is a domain value, not a transport one. A transport's own
behavior is a separate projection of `code`.

Offline model-quality diagnostics are successful validation evidence rather
than REST or MCP failures. Every registered `MODEL_QUALITY_*` diagnostic uses
`CHANGE_MODEL`: only an author can change the typed model evidence that caused
the finding. A project severity policy may mark a candidate ineligible without
changing its caller action. Core returns this evidence to the caller.

## Transport projection

A transport's own behavior is a projection of `code`, owned by that transport
and never by `serrors`.

REST owns an explicit, exhaustive `code`-to-HTTP-status table. It MUST NOT infer
a status from a code's name or from its `caller_action`, and an unregistered code
MUST fail closed to 500. Only codes whose action is `REPORT_DEFECT` may map to a
5xx status.

MCP returns the same JSON payload as tool-error content with `isError: true`,
and does not define another error taxonomy. Metis MUST NOT report a domain
failure as a JSON-RPC protocol error: a failed tool call is a successful
protocol call.

The `class` field and the `ErrorClass` vocabulary were removed by RFC-0034.

## MCP transports

Metis exposes one Agent-facing MCP server over two transports:

- `metis serve` provides Bearer-authenticated Streamable HTTP at `/mcp`;
- `metis mcp --config <metis.yaml>` provides a trusted local stdio process.

The transports share one tool registration and service layer. Tool names,
schemas, structured results, errors, authorization behavior, and observations
MUST NOT vary by transport. Stdio reserves stdout for protocol frames and sends
all logs to stderr. Its complete lifecycle and local Principal contract are
specified by [`interfaces/mcp-transports.md`](interfaces/mcp-transports.md).

## Project action authorization

Every project-scoped service operation is authorized after canonical Project
resolution and before semantic or execution state is accessed. The stable
closed actions are `discover`, `compile`, `execute`, `author`, `publish`,
`activate`, and `admin`; they imply no other action. The complete typed service
contract, built-in scope mapping, decision observation, and denial behavior are
specified by
[`operations/project-action-authorization.md`](operations/project-action-authorization.md).

An authorization denial uses `PROJECT_ACCESS_DENIED` with only `project_id` and
`action` details. REST returns HTTP 403 and MCP returns the same payload as tool
error content. Routing metadata, canonical asset refs, and capability discovery
never grant access.

## Semantic asset governance

After project authorization, the shared `AssetVisibilityPolicy` evaluates the
resolved canonical asset and validated policy tags. Discovery filtering occurs
before ranking, pagination, counts, cursors, compatibility, and relationship
traversal; compile and execution recheck direct references. Hidden assets use
ordinary kind-specific not-found errors so their existence is not disclosed.
Agent summaries expose typed lifecycle, certification, ownership, and
replacement evidence, while deprecated compile/execution inputs return
structured warnings without automatic rewriting. See
[`semantic/asset-governance.md`](semantic/asset-governance.md).

Discovery indexes and cursors are scoped to one immutable Project generation.
List and enumeration cursors additionally bind normalized request shape,
principal, and visibility-policy scope. A generation or policy-scope change
returns `SEMANTIC_PAGINATION_RESTART_REQUIRED` (HTTP 409,
`CHANGE_REQUEST`); malformed or cross-query cursor use remains
`INVALID_QUERY`. See
[`semantic/agent-semantic-search.md`](semantic/agent-semantic-search.md) and
[`operations/semantic-discovery-capacity.md`](operations/semantic-discovery-capacity.md).

## Runtime replacement and offline tools

Embedders may replace one Project's semantic generation through the
[Manager API](operations/semantic-runtime-activation.md). Core provides no
runtime administration HTTP endpoint.

`metis model`, `metis project`, and `metis query` provide offline model/project
validation, inspection, formatting, comparison, and compilation.
`metis query compile` emits a query object with `dialect`, `sql`,
and optional `parameters`. Parameter values remain separate from SQL text.
The standard database drivers bind them at execution time. See the
[CLI overview](../../README.md) and
[source contract](semantic/asset-authoring-lifecycle.md).

`artifact.CompiledQuery.SqlRenderResult` holds the rendering output. Its JSON
key is `sql_render_result` in REST/MCP responses and persisted benchmark artifacts.
`output_schema` and `warnings` are unchanged.

## Naming discipline

Phase, milestone, and migration labels belong in issues, pull requests, and commits. They must not be embedded in durable file names, public types, test names, error codes, or semantic concepts.

Core cross-package terms such as `SemanticPlan`, `SemanticPlanNode`,
`SQLDialect`, `Renderer`, `Backend`, `DataSource`, `SqlRenderResult`, and
`OutputSchema` remain explicit even when a shorter package-local spelling is
possible. Metis does not perform broad renames solely to shorten identifiers;
clarity of semantic layer and ownership takes precedence over character count.

## Explain result

`CompileService.Explain` returns `SQLExplainResult`. Its embedded
`QueryExplanation` preserves the existing JSON evidence fields (`project`,
`model`, `steps`, `semantic_plan`, `output_schema`, and optional metric,
dimension, relationship, and data-constraint evidence). `sql_render_result`
contains the same dialect, SQL, and ordered parameters as Compile; `warnings`
contains the same optional compilation warnings.

Explain plans once, evaluates data policy once, and renders using the selected
Renderer. It does not connect to a database or run SQL `EXPLAIN`. Semantic
evidence stays redacted; the SQL result includes policy predicates and bound
parameter values just as Compile does, under the same compile authorization.
Clients must treat it with the same access and storage rules as compiled SQL.
