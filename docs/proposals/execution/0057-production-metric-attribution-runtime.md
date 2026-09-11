# RFC-0057: Production Metric Attribution Runtime

> **Design history:** This RFC preserves its original lifecycle status and rationale.
> Implementation claims describe the recorded design, not a guarantee that every
> proposed API remains current. See [current documentation](../../README.md) for
> supported interfaces. Package paths have been updated for this repository.

- **Status:** Implemented
- **Owners:** Metis maintainers
- **Created:** 2026-08-31
- **Last updated:** 2026-09-01
- **Scope:** `attribute_metric`, attribution request resolution, attribution bundle orchestration, bounded execution, deterministic evaluation, REST/MCP projection, authorization, errors, observability
- **Supersedes:** ADR-0008 through the implementing decision ADR-0012
- **Related:** RFC-0034, RFC-0036, RFC-0052, RFC-0053, RFC-0054, RFC-0056, RFC-0059, RFC-0060, RFC-0061, RFC-0062, ADR-0010

## 1. Summary

RFC-0057 productizes deterministic metric-change attribution as one complete
Agent-facing analytical capability:

```text
attribute_metric
    -> resolve one governed attribution request
    -> build one independent-query AttributionBundle
    -> compile every query with one selected Renderer identity
    -> execute the bundle through the bounded Execution Runtime
    -> evaluate normalized evidence deterministically
    -> return one typed AttributionResult
```

The public capability is `attribute_metric`. There is no public
`compile_attribution`, raw bundle, physical-query list, or arbitrary-SQL
execution surface.

This changes attribution from the compile-only boundary recorded by ADR-0008
into a complete optional-runtime workflow. It does **not** remove Metis'
compile-only deployment mode or the public `compile_sql` capability. A Metis
deployment may still register Renderers without executable Backends, and a
Project without a DataSource remains valid for discovery and compilation.

The product boundary becomes:

```text
Metis product                not every deployment/capability
--------------------------   ---------------------------------------------
not globally compile-only    execution remains optional per deployment
query_metrics executes       only governed Metis-compiled metric queries
attribute_metric executes    only its internal governed attribution bundle
compile_sql remains          no run_query or arbitrary SQL execution
```

RFC-0052 remains semantic and physical attribution authority. RFC-0057 adds
request resolution, execution orchestration, a database-independent evaluator,
and public transport contracts; it does not redefine additive or ratio
decomposition mathematics.

## 2. Motivation and evidence

RFC-0052 implemented deterministic additive and ratio attribution compilation.
It produces one independent `SemanticPlan`, `SQLPlan`, `CompiledQuery`, and
output schema for each requested decomposition dimension. The bundle preserves
exact periods, shared filters, population alignment, undefined ratio evidence,
and reconciliation.

RFC-0053 then compared two Agent workflows over the same governed semantic
world and planted warehouse evidence:

| Workflow | Correct | Tool calls |
| --- | ---: | ---: |
| Agent-managed ordinary compile loop | 0/4 | 44 |
| Deterministic attribution bundle | 4/4 | 4 |

That result supports a product decision: an Agent should request attribution as
one analytical task instead of manually compiling, executing, joining, and
reconciling every period/dimension query.

RFC-0056 has since supplied the missing production substrate: Project to
DataSource to Backend resolution, one selected Renderer, atomic
`CompiledQuery { SQLRenderResult + OutputSchema }`, bounded normalized execution,
shared authentication, and REST/MCP service parity.

The remaining gap is a production vertical that connects those two proven
boundaries without moving Agent reasoning, SQL construction, database-specific
behavior, or execution policy into the wrong layer.

## 3. Goals and non-goals

### 3.1 Goals

- expose exactly one complete public `attribute_metric` operation;
- accept canonical semantic refs and exact periods, never natural language;
- resolve every dimension independently against one metric/time/filter context;
- preserve the RFC-0052 `AttributionBundle` as an internal boundary;
- compile all bundle entries with the exact Backend-selected Renderer instance;
- execute only atomic `CompiledQuery` values through `execution/runner`;
- enforce one bundle-wide timeout, row budget, byte budget, and cancellation;
- return no partial result when any bundle entry fails;
- evaluate normalized evidence without a database, Renderer, or semantic
  manifest dependency;
- return typed additive or ratio results with explicit undefined states and
  reconciliation evidence;
- preserve compiler-only deployments and Renderer-only warehouse extensions.

### 3.2 Non-goals

RFC-0057 does not:

- expose `compile_attribution` or internal bundle/planner operations;
- accept raw SQL, dialect, Renderer, DataSource, Backend, Driver, credentials,
  runtime limits, or concurrency controls from the caller;
- choose which metric, period, time dimension, or decomposition dimensions the
  Agent should investigate;
- infer natural-language dates or business causality;
- rank or compare effects across different decomposition dimensions;
- combine dimensions into one multi-column attribution grain;
- support arbitrary `AVG`, distinct-count, nonlinear, semi-additive, conversion,
  or otherwise unproven attribution algebra;
- introduce a generic Analytics Runtime IR before a second analytical vertical
  proves shared structure;
- add result caching, pre-aggregation, or warehouse optimization;
- require every compile-only Renderer to provide an executable Backend.

## 4. Public request contract

The transport-neutral conceptual request is:

```go
type MetricAttributionQuery struct {
    ProjectID        string             `json:"project_id,omitempty"`
    Metric           string             `json:"metric"`
    TimeDimension    string             `json:"time_dimension"`
    Baseline         AttributionPeriod  `json:"baseline"`
    Current          AttributionPeriod  `json:"current"`
    Dimensions       []string           `json:"dimensions"`
    Filters          []query.Filter     `json:"filters,omitempty"`
}

type AttributionPeriod struct {
    Start string `json:"start"`
    End   string `json:"end"`
}
```

`metric`, `time_dimension`, `dimensions`, and filter fields are canonical refs
returned by the progressive discovery tools. MCP may inherit Project context in
the same manner as `compile_sql` and `query_metrics`; an explicit invalid
Project never falls back.

Periods are exact non-empty half-open `[start, end)` ranges. Instants MUST be
RFC3339 values with an explicit offset and are normalized to UTC without losing
precision. Metis does not interpret phrases such as "last month". Baseline and
current periods may be adjacent, disjoint, or overlapping; RFC-0052 requires
exact ranges but does not manufacture an ordering restriction.

The request MUST contain:

- exactly one metric;
- exactly one compatible time dimension;
- from one through sixteen distinct non-time decomposition dimensions;
- zero or more shared typed filters applied identically to both periods, none of
  which references the selected time dimension.

The selected time dimension MUST NOT appear as a decomposition dimension or in
`filters`. Baseline and current are the sole time predicates for an attribution
request; a shared time filter could otherwise narrow the two periods
through a second, potentially conflicting time predicate. Such a request fails with
`INVALID_QUERY`.

The request has no `dialect`, `data_source`, `target`, `renderer`, `driver`,
`limit`, `order_by`, `top_k`, or raw predicate field. Attribution ordering and
complete-population reconciliation are semantic output contracts, not caller
presentation controls.

## 5. Resolution and planning

The application resolves Project and runtime placement before target-compatible
semantic binding:

```text
Project Registration
    -> DataSource
    -> DataSource.type
    -> Backend
    -> exact Renderer instance
```

The selected Renderer supplies expression-dialect and capability evidence to
the QueryResolver. No attribution layer may resolve the Renderer again by name.

Attribution resolution MUST:

1. bind the metric, time dimension, decomposition dimensions, and filters to
   one canonical semantic model;
2. validate the metric/time/filter context once;
3. resolve each decomposition dimension independently against that context;
4. preserve ambiguity, fan-out, temporal, fill, extension, and source-expression
   failures from the ordinary semantic pipeline;
5. produce only already-resolved evidence for attribution planning.

Resolution MUST NOT submit one ordinary query grouped by every requested
dimension. That would create a combined grain and change the meaning of the
independent decompositions.

The attribution planner then reuses RFC-0052 authority:

```text
resolved attribution evidence
    -> MetricEvaluationPlan
    -> MetricAttributionPlan
    -> one independent SemanticPlan per dimension
    -> MetricAttributionBundle
```

No stage below resolution may re-read `SemanticManifest`, reparse raw Ossie
expressions, rediscover metric dependencies, or choose another relationship
path. `MetricEvaluationPlan` remains metric-dependency authority;
`MetricAttributionPlan` remains decomposition authority.

RFC-0061 owns the final planner package layout. RFC-0057 implementation targets
its responsibility boundaries:

```text
planner/attribution       request, proof, independent plans, bundle
planner/semanticplan      closed additive/ratio node implementations
planner/conversion        SemanticPlan -> SQLPlan attribution shapes
compiler/attribution      compiled-bundle orchestration
```

## 6. Compilation and internal bundle contract

`compiler/attribution` compiles the canonical bundle in dimension-ref order.
Every entry is one atomic `compiler.CompiledQuery`; the compiled bundle also
retains the minimum typed attribution descriptor needed for result evaluation.
It does not expose or duplicate semantic manifests, DataSource state, secrets,
or Renderer lookup keys.

```text
MetricAttributionBundle
    + exact selected Renderer
        |
        v
MetricAttributionCompilation
    |- request/decomposition descriptor
    |- dimension A -> CompiledQuery
    |- dimension B -> CompiledQuery
    `- dimension N -> CompiledQuery
```

The same Renderer object MUST provide expression evidence, capabilities, SQLPlan
rendering, and every bundle query's physical dialect. A bundle containing mixed
Renderer identity, dialect, Project, model, metric, period, filter, or
decomposition evidence fails before execution.

The compiled bundle is internal. REST and MCP responses MUST NOT expose SQL,
parameters, output schemas, query count, DataSource identity, or Backend type.

## 7. Application orchestration and execution

`app/service/semantic` owns the complete use case:

```text
AttributeMetricService
    -> authorize
    -> resolve Project/DataSource/Backend once
    -> resolve and plan attribution
    -> compile bundle with Backend.Renderer
    -> execute compiled queries
    -> evaluate normalized evidence
    -> return AttributeMetricResult
```

Semantic Core, `compiler/attribution`, `execution/runner`, and the evaluator do
not call back into `app/service/semantic`.

### 7.1 Sequential v1 scheduling

The first production implementation executes bundle entries sequentially in
canonical dimension-ref order. Sequential scheduling is deliberately selected
for v1 because it:

- is bounded by the existing maximum of sixteen dimensions;
- produces deterministic first-failure identity;
- cannot self-contend for one DataSource's admission slots;
- makes cumulative budget accounting exact;
- avoids introducing bundle concurrency configuration before latency evidence
  justifies it.

Future bounded parallel execution requires a follow-up contract preserving the
same all-or-nothing result, deterministic error selection, DataSource admission,
and bundle-wide budgets. It is not an implementation-local optimization.

### 7.2 Bundle-wide limits

One `attribute_metric` call is one logical runtime operation even though it may
execute several physical queries. The resolved DataSource policy applies to the
whole bundle:

- one timeout starts before attribution execution and covers all entries;
- `MaxRows` is cumulative across completed dimension results;
- `MaxBytes` is cumulative across completed dimension results;
- the service passes the remaining row/byte allowance as tightening
  `ExecutionOptions` to each subsequent Runner call;
- if either cumulative allowance is exhausted while entries remain, the bundle
  fails before starting another query rather than treating zero as "no limit";
- caller input cannot relax or replace any deployment policy;
- cancellation stops current work and prevents later entries from starting.

Compilation and semantic resolution remain subject to the request context but
do not consume warehouse row/byte budgets.

### 7.3 All-or-nothing result

Bundle execution never returns partial success. If compilation, admission,
secret resolution, execution, normalization, cleanup, cumulative limits, or
evaluation fails for any dimension, the operation:

1. stops scheduling later dimensions;
2. cancels in-flight work, if any;
3. discards every completed dimension result;
4. returns one structured error naming only safe canonical dimension evidence;
5. never returns an `AttributionResult` alongside the error.

The Runner receives only one atomic `CompiledQuery` at a time. It MUST NOT
receive `SemanticManifest`, attribution plans, Renderer, DataSource overrides,
or independently mutable query/schema arguments.

## 8. Attribution Evaluator

A focused `analytics/attribution` package owns deterministic evaluation. It is
the first Analytics Runtime vertical, not a generic analytical framework.

The evaluator consumes an attribution-specific value containing:

```text
resolved attribution descriptor
+ canonical dimension identities
+ normalized rows paired with their OutputSchema
```

It does not consume SQL text, parameters, Driver values, database connections,
Renderer, Runner, SemanticManifest, Resolver, or unvalidated arbitrary maps.
`app/service/semantic` adapts complete Runner `ResultSet` values into this input only
after all bundle queries succeed.

The evaluator MUST:

- verify exact schema names, kinds, datatypes, and row widths;
- reject duplicate dimension values and inconsistent repeated summary fields;
- preserve canonical dimension order and deterministic segment order;
- compute additive baseline/current totals and total delta from complete rows;
- verify additive segment delta and total reconciliation exactly;
- compute/verify ratio numerator and denominator totals, global ratios,
  segment effects, decomposed delta, and residual;
- preserve entry, continuing, exit, absent, zero, and undefined states;
- return typed result values rather than raw database rows;
- be fully testable from fixtures without a database.

### 8.1 Numeric contract

The public v1 evaluator accepts only normalized `Integer` and `Decimal` numeric
evidence. Binary floating-point and opaque numeric results fail closed as
unsupported public attribution evidence until a separate precision contract is
accepted.

Numeric values are parsed from their normalized lexical representation into
arbitrary-precision decimal/rational arithmetic. Public numeric evidence is
serialized as decimal strings so JSON encoding cannot lose precision.

Additive reconciliation is exact. Ratio reconciliation uses the target-neutral
absolute tolerance already proven by RFC-0053 fixtures:

```text
abs(reconciliation_residual) <= 1e-8
abs(ratio_delta - decomposed_delta) <= 1e-8
```

The tolerance is evaluator policy, not Renderer behavior and not caller input.
Changing it is a public analytical-contract change.

## 9. Public result contract

The response is a closed typed analytical result, not an array of physical
query results. The following Go-like DTOs define the JSON contract; no omitted
field has an implementation-defined shape.

```go
// DecimalString is a base-10, finite decimal lexical value. It contains no
// exponent, NaN, infinity, locale formatting, or JSON number coercion.
type DecimalString string

// AttributionMemberValue is one complete JSON scalar token. It is never an
// object or array. A literal JSON null represents a semantic null member.
type AttributionMemberValue = json.RawMessage

type AttributionMemberType string

const (
    AttributionMemberString     AttributionMemberType = "string"
    AttributionMemberInteger    AttributionMemberType = "integer"
    AttributionMemberDecimal    AttributionMemberType = "decimal"
    AttributionMemberFloat      AttributionMemberType = "float"
    AttributionMemberBoolean    AttributionMemberType = "boolean"
    AttributionMemberDate       AttributionMemberType = "date"
    AttributionMemberTime       AttributionMemberType = "time"
    AttributionMemberDateTime   AttributionMemberType = "datetime"
    AttributionMemberDateTimeTZ AttributionMemberType = "datetime_tz"
)

type AttributionStrategy string

const (
    AttributionStrategyAdditive AttributionStrategy = "additive_contribution"
    AttributionStrategyRatio    AttributionStrategy = "ratio_mix_rate"
)

type AttributeMetricResult struct {
    AnalysisID    string                       `json:"analysis_id"`
    Metric        string                       `json:"metric"`
    TimeDimension string                       `json:"time_dimension"`
    Baseline      AttributionPeriod            `json:"baseline"`
    Current       AttributionPeriod            `json:"current"`
    Strategy      AttributionStrategy          `json:"strategy"`
    Dimensions    []DimensionAttributionResult `json:"dimensions"`
}

type DimensionAttributionResult struct {
    Dimension  string                     `json:"dimension"`
    MemberType AttributionMemberType      `json:"member_type"`
    Additive   *AdditiveAttributionResult `json:"additive,omitempty"`
    Ratio      *RatioAttributionResult    `json:"ratio,omitempty"`
}

type AdditiveAttributionResult struct {
    Summary  AdditiveAttributionSummary   `json:"summary"`
    Segments []AdditiveAttributionSegment `json:"segments"`
}

type AdditiveAttributionSummary struct {
    BaselineTotal       DecimalString `json:"baseline_total"`
    CurrentTotal        DecimalString `json:"current_total"`
    TotalDelta          DecimalString `json:"total_delta"`
    ContributionDefined bool          `json:"contribution_defined"`
    Reconciled          bool          `json:"reconciled"`
}

type AdditiveAttributionSegment struct {
    Value           AttributionMemberValue `json:"value"`
    BaselineValue   DecimalString          `json:"baseline_value"`
    CurrentValue    DecimalString          `json:"current_value"`
    Delta           DecimalString          `json:"delta"`
    ContributionPct *DecimalString         `json:"contribution_pct"`
}

type RatioAttributionResult struct {
    Summary  RatioAttributionSummary   `json:"summary"`
    Segments []RatioAttributionSegment `json:"segments"`
}

type RatioAttributionSummary struct {
    BaselineNumerator       *DecimalString `json:"baseline_numerator"`
    BaselineDenominator     *DecimalString `json:"baseline_denominator"`
    CurrentNumerator        *DecimalString `json:"current_numerator"`
    CurrentDenominator      *DecimalString `json:"current_denominator"`
    BaselineRatio           *DecimalString `json:"baseline_ratio"`
    CurrentRatio            *DecimalString `json:"current_ratio"`
    RatioDelta              *DecimalString `json:"ratio_delta"`
    DecomposedDelta         *DecimalString `json:"decomposed_delta"`
    ReconciliationResidual  *DecimalString `json:"reconciliation_residual"`
    AttributionDefined      bool           `json:"attribution_defined"`
    Reconciled              bool           `json:"reconciled"`
}

type RatioAttributionSegment struct {
    Value               AttributionMemberValue `json:"value"`
    BaselinePresent     bool                   `json:"baseline_present"`
    CurrentPresent      bool                   `json:"current_present"`
    BaselineNumerator   DecimalString          `json:"baseline_numerator"`
    BaselineDenominator DecimalString          `json:"baseline_denominator"`
    CurrentNumerator    DecimalString          `json:"current_numerator"`
    CurrentDenominator  DecimalString          `json:"current_denominator"`
    BaselineRate        *DecimalString         `json:"baseline_rate"`
    CurrentRate         *DecimalString         `json:"current_rate"`
    BaselineWeight      *DecimalString         `json:"baseline_weight"`
    CurrentWeight       *DecimalString         `json:"current_weight"`
    BaselineDefined     bool                   `json:"baseline_defined"`
    CurrentDefined      bool                   `json:"current_defined"`
    SegmentDefined      bool                   `json:"segment_defined"`
    RateEffect          *DecimalString         `json:"rate_effect"`
    MixEffect           *DecimalString         `json:"mix_effect"`
    EntryEffect         *DecimalString         `json:"entry_effect"`
    ExitEffect          *DecimalString         `json:"exit_effect"`
    TotalEffect         *DecimalString         `json:"total_effect"`
}
```

Exactly one of `additive` or `ratio` is present for every dimension, and every
dimension uses the same strategy. `reconciled` is always `true` in a successful
response; inconsistent evidence returns `INCONSISTENT_ATTRIBUTION_RESULT` and
does not return a result.

`member_type` freezes the JSON representation of every segment's `value`:

| Member type | JSON value when non-null |
| --- | --- |
| `string`, `date`, `time`, `datetime`, `datetime_tz` | JSON string |
| `integer`, `float` | JSON number |
| `decimal` | JSON string containing `DecimalString` |
| `boolean` | JSON boolean |

`opaque`, object, and array dimension members are unsupported for public
attribution results. A semantic null member is represented as exactly
`"value": null`; it is a real grouped segment and is not omitted, an absent
period, or an empty result. All `*DecimalString` fields are present with JSON
`null` when undefined or absent; they are never omitted. A non-null
`DecimalString` is always serialized as a JSON string.

In every ratio segment, the four numerator/denominator fields are non-null
zero-filled additive identity values. `baseline_present` and `current_present`
distinguish an absent period member from an observed numeric zero. Rate, weight,
and effect fields use null exactly when the applicable guarded division or
defined-state contract does not produce a number; the corresponding
`*_defined` field carries the governing boolean evidence. The numeric-evidence
restriction in section 8.1 applies to attribution totals/effects, not a scalar
dimension member whose declared `member_type` is `float`.

### 9.1 Additive result

`contribution_pct` is null for every segment when `total_delta` is zero.
Positive and negative contributions are never clamped. `reconciled` can only be
true in a successful response; an inconsistency is an error, not a partial
result with `reconciled=false`.

### 9.2 Ratio result

Undefined ratios are successful governed evidence. When a required global or
segment denominator is present and zero, the corresponding ratio/effect fields
remain null and the explicit `*_defined` flags are false. Metis never converts
undefined evidence to zero.

### 9.3 Empty population

- An empty additive population whose plan proves zero identity fill returns zero
  baseline/current totals, zero total delta, `contribution_defined=false`, and
  an empty segment list.
- An empty ratio population returns null global ratios/effects,
  `attribution_defined=false`, and an empty segment list.

Neither case is a query-execution error. The Agent may report that no governed
population was present; it must not invent causal conclusions.

### 9.4 Canonical output ordering

Public result ordering is evaluator-owned and never inherits a warehouse's row
order. `Dimensions` sort by ascending canonical dimension-ref UTF-8 byte order.
Each dimension's `segments` sort by its normalized member value, not by effect,
contribution, or any other analytical magnitude:

1. non-null members sort before the null member;
2. `integer`, `decimal`, and `float` members sort by mathematical numeric value
   ascending; equal numeric values tie-break by canonical JSON byte sequence;
3. `boolean` members sort `false` before `true`;
4. string-like members (`string`, `date`, `time`, `datetime`, `datetime_tz`)
   sort by ascending valid UTF-8 byte sequence;
5. the one null member sorts last.

The evaluator rejects duplicate normalized member values before sorting. The
tie-breaker is retained for defensive determinism and does not permit duplicate
segments. RFC-0052's physical evidence query may retain its existing internal
defined/effect/member order; RFC-0057 canonicalizes the public typed result
after normalized evaluation. This does not create cross-dimension ranking or a
public magnitude-ranking default.

## 10. Agent responsibility and stopping guidance

The `attribute_metric` tool description and current Agent guidance MUST state:

- use discovery to select canonical metric/time/dimension refs;
- convert user time language into exact periods before calling Metis;
- treat a successful result as the complete deterministic evidence for the
  requested dimensions;
- do not reconstruct the same decomposition with repeated `query_metrics`
  calls;
- do not compare or rank contribution magnitude across different dimensions;
- do not describe contribution as business causality;
- use a new semantic request only when the user asks for another dimension,
  period, filter, or metric.

This is interaction guidance, not an Agent reasoning engine. Metis does not
select follow-up investigations or generate a narrative explanation.

## 11. Authentication, authorization, and security

`attribute_metric` uses the shared Bearer authentication boundary and Principal
contract. REST and MCP adapters do not implement private API-key checks.

Because the operation executes governed physical queries, v1 requires the
existing `semantic:execute` scope. It does not introduce an attribution-specific
scope before deployment evidence demonstrates a distinct policy need. Project
authorization is checked before DataSource resolution and before any compiled
query executes.

The operation never accepts or returns credentials, secret refs, endpoints,
DataSource IDs, Backend types, SQL, or parameters. Errors remain redacted.
Dimension values and metric results are user data and remain in the response;
observability must not record them.

## 12. Error contract

Existing semantic and execution codes remain authoritative wherever they name
the condition precisely:

- `PROJECT_REQUIRED`, `PROJECT_NOT_FOUND`, `PROJECT_ACCESS_DENIED`;
- metric/dimension/filter lookup and ambiguity codes;
- `INCOMPATIBLE_QUERY_GRAIN`, `UNSUPPORTED_RELATIONSHIP_FANOUT`, and other
  semantic fail-closed codes;
- `QUERY_EXECUTION_UNAVAILABLE`, `QUERY_EXECUTION_BUSY`,
  `QUERY_EXECUTION_LIMIT_EXCEEDED`, `QUERY_EXECUTION_TIMEOUT`,
  `QUERY_EXECUTION_CANCELLED`, and `QUERY_EXECUTION_FAILED`.

RFC-0057 adds only two attribution-specific conditions:

```text
UNSUPPORTED_METRIC_ATTRIBUTION
    the governed metric/evidence cannot satisfy the closed additive or ratio
    attribution contract; details include the canonical metric and stable reason

INCONSISTENT_ATTRIBUTION_RESULT
    complete normalized warehouse evidence violates the proven reconciliation
    contract; no partial result is returned and caller action is report_defect
```

Malformed periods, duplicates, forbidden fields, and invalid dimension counts
use `INVALID_QUERY`. Undefined zero-denominator evidence is not an error.
Implementations MUST NOT add a generic `ATTRIBUTION_FAILED` bucket.

## 13. REST and MCP surfaces

The public surfaces are:

```text
POST /v1/attribute-metric
MCP tool: attribute_metric
```

Both call the same `AttributeMetricService` and project the same request,
result, errors, authorization, cancellation, and Project-context semantics.
REST does not call MCP and MCP does not call REST.

The MCP tool is read-only and idempotent with respect to Metis-managed state,
non-destructive, and open-world because it reads an external warehouse. A
runtime-disabled MCP server need not register the tool; REST may retain its
versioned route and return `QUERY_EXECUTION_UNAVAILABLE`. When configured and
invoked, both transports MUST have semantic parity.

There is no public `compile_attribution`, `execute_attribution_bundle`, or
benchmark scenario input. The RFC-0053 benchmark-private tool remains test code
and is not promoted into production.

Post-implementation note (2026-09-01): the benchmark-private executable path
was retired after this production contract landed. Only its frozen historical
frame, oracle, artifact validation, and accepted evidence remain.

## 14. Observability

RFC-0057 extends the bounded observability vocabulary with one REST route, one
MCP method, and one service operation. Observation may include:

- success/error result;
- total operation duration;
- number of compiled/executed dimension queries;
- cumulative rows and bytes;
- stable error code;
- cancellation and limit outcomes.

Metric names, dimension names/values, filters, period values, SQL, parameters,
DataSource IDs, credentials, and result values MUST NOT become metric labels or
trace attributes. Per-query Runner observations remain valid and share one
opaque `analysis_id` correlation value.

Observability failure must not change attribution semantics or execution.

## 15. Compatibility and architectural consequences

### 15.1 Compile-only remains a supported deployment mode

RFC-0057 does not remove `compile_sql`, require a DataSource for compilation, or
turn compile-only Renderers into executable Backends. A Project without a
DataSource continues to support discovery and compile-only SQL generation.
Calling `attribute_metric` for that Project fails explicitly with
`QUERY_EXECUTION_UNAVAILABLE`.

### 15.2 Attribution is no longer public compile-only

When RFC-0057 is implemented, a new ADR MUST supersede ADR-0008's statement that
attribution does not execute or have an Agent-facing operation. The durable
parts of ADR-0008 remain:

- independent dimension queries;
- no cross-dimension ranking;
- deterministic additive/ratio algebra;
- explicit undefined evidence;
- no causal inference.

The current attribution specification must then change from an internal
compile-only contract to a layered compile-plus-runtime contract. RFC acceptance
alone does not change current behavior.

### 15.3 No generic Analytics Runtime abstraction yet

The focused `analytics/attribution` evaluator is allowed. A generic workflow IR,
generic evaluator registry, or generic analytical result envelope remains
deferred until at least one additional analytical vertical demonstrates stable
shared requirements.

### 15.4 RFC-0061 and RFC-0062 sequencing

RFC-0057 may be reviewed and accepted while RFC-0061 and RFC-0062 are in
progress, but production implementation SHOULD begin after their package
migrations land. New attribution logic must target the final planner and
physical-query ownership boundaries rather than expand files scheduled for
immediate relocation.

## 16. Alternatives

### 16.1 Remove compile-only mode now that query execution exists

Rejected. Execution is optional per deployment and per warehouse extension.
`compile_sql` remains necessary for external execution, credential isolation,
Renderer-only extensions, offline conformance, and Projects without DataSources.

### 16.2 Keep attribution compile-only permanently

Rejected. RFC-0053 demonstrated that Agent-managed execution and reconciliation
is materially less reliable and more expensive than one deterministic bundle
operation. RFC-0056 now supplies the bounded execution boundary that ADR-0008
intentionally waited for.

### 16.3 Expose `compile_attribution`

Rejected as a primary public capability. It would make Agents orchestrate an
internal implementation stage, enlarge the tool vocabulary, and recreate the
failure mode RFC-0053 measured. Internal compiled bundles remain testable and
available to Go-level composition without becoming REST/MCP operations.

### 16.4 Implement attribution by repeatedly calling `query_metrics`

Rejected. It would lose the typed bundle's shared-proof validation, duplicate
request resolution, expose partial results, and move independent-query
correlation and reconciliation into application glue.

### 16.5 Execute all dimensions concurrently in v1

Rejected pending latency evidence. It complicates deterministic errors,
cumulative limits, cancellation, and per-DataSource admission while the bounded
maximum of sixteen permits a simpler correct sequential implementation.

### 16.6 Return raw normalized rows

Rejected. Raw rows expose SQL projection details as the public analytical API
and force every Agent to repeat undefined-state and reconciliation logic. The
evaluator returns typed domain evidence instead.

### 16.7 Let Metis select and rank dimensions automatically

Rejected. Dimension selection and investigation strategy are Agent reasoning.
Overlapping dimensions do not provide a valid common contribution ranking.

## 17. Rollout and migration

### Phase A — package-boundary prerequisites

- complete or rebase onto RFC-0061 planner ownership and RFC-0062 compiler /
  physical-query ownership;
- retain compatible attribution facade entrypoints while avoiding duplicate
  implementations;
- add architecture guards for `analytics/attribution` dependencies.

### Phase B — public and resolved request construction

- add the typed attribution query DTO and exact period parser;
- add canonical-ref normalization for MCP;
- implement Renderer-aware independent-dimension resolution;
- construct RFC-0052 plans and bundle from production semantic evidence;
- add stable unsupported-attribution diagnostics.

No public transport is exposed in this phase.

### Phase C — fixture-only evaluator

- implement typed additive and ratio results;
- implement arbitrary-precision numeric parsing and reconciliation;
- cover empty, entry, continuing, exit, zero-total, zero-denominator, malformed,
  duplicate, and inconsistent evidence;
- reuse RFC-0053 planted fixtures without importing benchmark packages into
  production.

No live database is required in this phase.

### Phase D — bundle orchestration

- add `AttributeMetricService`;
- resolve one Project/DataSource/Backend/Renderer identity;
- execute sequentially through Runner with bundle-wide deadline and cumulative
  row/byte budgets;
- implement all-or-nothing failure, cancellation, and redacted errors;
- pass only complete normalized evidence to the evaluator.

### Phase E — public surfaces and observation

- add `POST /v1/attribute-metric`;
- add MCP `attribute_metric` and Agent stopping guidance;
- add shared `semantic:execute` authorization;
- add bounded REST/MCP/service observations;
- prove transport parity and compile-only runtime-unavailable behavior.

### Phase F — closure

- run package, conformance, race, cancellation, and real-engine tests;
- run a deterministic production-tool AgentBench smoke using the frozen
  RFC-0053 scenarios; no new paid statistical collection is required;
- add the ADR superseding ADR-0008;
- update current design/specification/working-contract documents;
- mark RFC-0057 `Implemented` only after the complete public path is merged.

Rollback removes the public service composition and transports while leaving
RFC-0052's internal compile bundle intact. It must not restore obsolete routing
or Renderer authority.

## 18. Test and acceptance criteria

RFC-0057 may become `Implemented` only when all of the following are true.

### Request and resolution

- exact RFC3339 periods normalize deterministically and invalid/empty periods
  fail;
- one through sixteen unique dimensions are accepted; zero, duplicates, and
  seventeen fail;
- the time dimension cannot also be a decomposition dimension or shared filter;
- metric, time dimension, dimensions, and filters resolve to one compatible
  semantic model;
- every dimension is resolved independently, with no combined grain;
- unsupported attribution algebra returns
  `UNSUPPORTED_METRIC_ATTRIBUTION` with stable reason evidence;
- no public request can select dialect, Renderer, DataSource, Driver, target,
  SQL, runtime limits, or ordering.

### Authority and compilation

- one exact Renderer instance supplies semantic evidence, capabilities, and
  rendering for every bundle entry;
- every entry is an atomic `CompiledQuery` with matching `OutputSchema`;
- mixed Project/model/metric/period/filter/decomposition/Renderer evidence fails
  before execution;
- ordinary `compile_sql` and `query_metrics` behavior/fingerprints remain
  unchanged;
- no second attribution SQL builder exists.

### Execution

- entries execute sequentially in canonical dimension order;
- one timeout covers the complete bundle;
- row and byte limits are cumulative and remaining budgets tighten each call;
- cancellation prevents later queries from starting;
- any query failure discards all earlier results;
- Runtime receives only atomic `CompiledQuery` values and no semantic/planner
  state;
- Projects without executable DataSources fail with
  `QUERY_EXECUTION_UNAVAILABLE` and continue to support `compile_sql`.

### Evaluation

- evaluator tests run without a database;
- additive totals, deltas, contribution percentages, zero-total behavior, and
  exact reconciliation are correct;
- ratio mix/rate/entry/exit effects, zero-denominator behavior, repeated summary
  consistency, and `1e-8` reconciliation are correct;
- empty additive and ratio populations follow section 9.3;
- malformed schema/rows, duplicate dimension values, binary floats, and
  inconsistent evidence fail closed;
- public numeric values preserve precision as decimal strings;
- every public result field conforms to the closed section 9 DTO, enum, JSON
  scalar, and nullable-field contract;
- a null decomposition member is returned as `"value": null` in exactly one
  retained segment rather than omitted or conflated with period absence;
- dimensions sort by canonical ref and segments sort by the section 9.4 member
  comparator, independent of warehouse row order and without effect ranking;
- no undefined ratio is converted to zero.

### Interface, security, and operations

- REST and MCP call the same service and return equivalent results/errors;
- `semantic:execute` is required and project authorization occurs before
  execution;
- responses and errors expose no SQL, parameters, DataSource, Backend, endpoint,
  credential, or secret reference;
- there is no public `compile_attribution`, raw bundle, or arbitrary-SQL tool;
- observability is low-cardinality and contains no semantic refs, periods,
  filters, dimension values, or result values;
- DuckDB and every configured executable built-in Backend pass production-path
  end-to-end tests;
- DuckDB, Doris, and ClickHouse conformance fixtures produce evaluator-compatible
  normalized evidence without target-specific evaluator branches;
- `go test ./...`, `go vet ./...`, race/lifecycle tests, and the relevant
  conformance gates pass.

### Lifecycle closure

- a new ADR supersedes ADR-0008's compile-only public attribution decision;
- current attribution, runtime, Agent query, error, observability, glossary, and
  architecture documents describe the shipped behavior;
- AGENTS.md no longer describes attribution as compile-only;
- RFC-0054 may be reassessed for closure after this vertical lands, while its
  two-vertical generic-IR criterion remains deferred.

## 19. Documentation updates

Implementation must update at least:

- `AGENTS.md`;
- `docs/proposals/README.md`;
- `docs/design/semantic/pipeline.md`;
- `docs/design/operations/runtime-bootstrap.md`;
- `docs/specs/semantic/metric-change-attribution.md`;
- `docs/specs/semantic/agent-query-contract.md`;
- `docs/specs/operations/runtime-bootstrap.md`;
- `docs/specs/operations/runtime-observability.md`;
- `docs/specs/public-contract.md`;
- `docs/specs/glossary.md`;
- `docs/decisions/semantic/` with the ADR superseding ADR-0008;
- REST/MCP examples and generated error inventories where applicable.

## 20. Final proposed decision

Metis remains capable of compile-only operation, but it is no longer globally a
compile-only product. `attribute_metric` should be the first complete Analytics
Runtime vertical:

```text
Agent chooses governed analytical intent
    -> Metis resolves and compiles deterministic attribution evidence
    -> Metis optionally executes through the Project DataSource
    -> Metis deterministically evaluates the complete normalized bundle
    -> Agent explains meaning and chooses any follow-up investigation
```

The internal compiler boundary remains valuable, but the public attribution
product boundary is the complete typed result.
