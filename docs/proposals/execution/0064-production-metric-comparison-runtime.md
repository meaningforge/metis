# RFC-0064: Production Metric Comparison Runtime

> **Design history:** This RFC preserves its original lifecycle status and rationale.
> Implementation claims describe the recorded design, not a guarantee that every
> proposed API remains current. See [current documentation](../../README.md) for
> supported interfaces. Package paths have been updated for this repository.

- **Status:** Implemented
- **Owners:** Metis maintainers
- **Created:** 2026-09-01
- **Last updated:** 2026-09-01
- **Implemented:** 2026-09-01
- **Scope:** `compare_metrics`, exact-period comparison orchestration, normalized comparison evaluation, REST/MCP projection, authorization, errors, observability
- **Supersedes:** None
- **Related:** RFC-0034, RFC-0036, RFC-0054, RFC-0056, RFC-0057, RFC-0059, RFC-0060, ADR-0010, ADR-0012, ADR-0013

## 1. Summary

RFC-0064 defines `compare_metrics` as the second complete Analytics Runtime
vertical. It compares one through sixteen governed metrics between two exact
time periods at an optional shared dimension grain:

```text
compare_metrics
    -> resolve one governed comparison request
    -> compile one baseline and one current SemanticQuery
    -> execute both atomic CompiledQuery values through Execution Runtime
    -> align complete normalized rows by dimension tuple
    -> compute exact delta and percent change
    -> return one typed CompareMetricsResult
```

The operation is intentionally narrower than a general analysis engine. It does
not perform attribution, rank movers, explain causality, accept different
filters per period, or expose arbitrary SQL. It reuses the existing Semantic
Core and Execution Runtime without introducing a generalized Analytics Runtime
IR.

Once implemented, this vertical and `attribute_metric` satisfy RFC-0054's
requirement that two real analytical workflows reuse the same Execution Runtime
before Metis evaluates shared generic analytical primitives.

## 2. Motivation

`query_metrics` can execute either period independently, but asking an Agent to
issue two calls, preserve an identical semantic grain, align composite and null
dimension members, distinguish absence from a numeric zero, and calculate
changes repeats deterministic work outside Metis.

`attribute_metric` is not the replacement. Attribution answers how one metric's
change decomposes across independently analyzed dimensions and requires closed
additive or ratio algebra. Comparison answers what changed for one or more
ordinary governed metrics at one shared output grain. It must support metrics
that ordinary semantic compilation admits even when attribution algebra does
not apply, subject to exact public numeric evidence.

The second vertical also provides the evidence RFC-0054 deliberately required
before any generic analytical IR is considered. Shared implementation should be
observed from real verticals rather than predicted into an abstraction.

## 3. Goals and non-goals

### 3.1 Goals

- expose one complete public `compare_metrics` capability;
- compare one through sixteen canonical metrics over two exact half-open
  periods;
- use zero through eight canonical non-time dimensions as one shared comparison
  grain;
- apply one identical shared filter set to both periods;
- compile through the ordinary SemanticQuery path with one Backend-selected
  Renderer identity;
- execute exactly two atomic `CompiledQuery` values sequentially under one
  timeout and cumulative row/byte limits;
- align complete normalized rows without guessing missing values;
- compute delta and percent change with arbitrary-precision decimal arithmetic;
- return deterministic typed JSON scalars and decimal strings;
- fail all-or-nothing for compile, execution, normalization, or evaluation
  errors;
- preserve compile-only deployment and warehouse-extension boundaries.

### 3.2 Non-goals

RFC-0064 does not:

- add arbitrary SQL execution;
- add top-K, ordering, limit, mover ranking, anomaly detection, attribution, or
  causal explanation;
- accept natural language or relative time expressions;
- compare different metrics, filters, models, dimensions, or populations on the
  two sides;
- coerce an absent row or null metric value to zero;
- support Float metric evidence in v1;
- define currency/unit conversion between metrics;
- expose compiled SQL, schemas, runtime budgets, DataSource, Backend, Renderer,
  Driver, or target controls;
- create `AnalysisPlan`, `AnalysisBundle`, a generic evaluator interface, or a
  shared Analytics Runtime IR;
- approve a third analytical vertical.

## 4. Layer ownership

The workflow uses existing boundaries directly:

```text
REST / MCP
    -> CompareMetricsService
        -> Project -> DataSource -> Backend
        -> Resolver / Planner / Compiler (baseline)
        -> Resolver / Planner / Compiler (current)
        -> Runner.ExecuteResolved(baseline CompiledQuery)
        -> Runner.ExecuteResolved(current CompiledQuery)
        -> analytics/comparison evaluator
```

`CompareMetricsService` owns request normalization, two-query orchestration,
bundle-wide budgets, and all-or-nothing behavior. It does not build SQL or own
comparison arithmetic.

Semantic Core sees two ordinary `query.SemanticQuery` values and has no
comparison-specific branch. Execution Runtime sees only two atomic
`CompiledQuery { PhysicalQuery + OutputSchema }` values and receives no
comparison request, manifest, Resolver, SemanticPlan, SQLPlan, or Renderer.

`analytics/comparison` owns schema/row validation, deterministic alignment,
exact arithmetic, and the public typed result. It has no database, SQL,
DataSource, transport, or semantic-resolution dependency.

The vertical may duplicate small request/result concepts where ownership is
not yet proven reusable. Implementation MUST NOT extract a generic analytical
plan, scheduler, evaluator, or result model merely because attribution has a
similar field.

## 5. Public request

The transport-neutral request is:

```go
type ComparisonPeriod struct {
    Start string `json:"start"`
    End   string `json:"end"`
}

type MetricComparisonQuery struct {
    ProjectID     string           `json:"project_id,omitempty"`
    Metrics       []string         `json:"metrics"`
    TimeDimension string           `json:"time_dimension"`
    Baseline      ComparisonPeriod `json:"baseline"`
    Current       ComparisonPeriod `json:"current"`
    Dimensions    []string         `json:"dimensions,omitempty"`
    Filters       []query.Filter   `json:"filters,omitempty"`
}
```

Validation is closed:

1. `metrics` contains one through sixteen distinct canonical `metric:` refs;
2. `time_dimension` is one canonical `dimension:` ref whose Ossie field is a
   time dimension;
3. `dimensions` contains zero through eight distinct canonical non-time
   `dimension:` refs;
4. metrics, time dimension, dimensions, and filter fields resolve to one model;
5. filter fields use canonical metric or dimension refs;
6. the selected time dimension cannot be a comparison dimension or shared
   filter;
7. period instants use RFC3339Nano with an explicit `Z` or numeric offset;
8. each period is non-empty and interpreted as `[start, end)`;
9. instants normalize to UTC before they become request evidence;
10. unknown JSON fields and duplicate semantic identities fail;
11. metrics and dimensions canonicalize lexicographically before compilation
    and result construction.

The periods may overlap and need not have equal duration. Metis reports exact
values and change; it does not infer that unequal periods are business-wise
comparable.

The request contains no dialect, DataSource, Backend, Renderer, Driver, target,
SQL, limit, order, top-K, timeout, row budget, or byte budget.

## 6. Semantic compilation

The service constructs exactly two ordinary queries with identical model,
metrics, dimensions, and shared filters. It adds only the corresponding time
predicate pair:

```text
time_dimension >= period.start
time_dimension <  period.end
```

The baseline and current queries pass independently through the same Resolver,
Planner, optimizer, compiler, and exact selected Renderer instance. Existing
multi-metric shared-grain, fan-out, advanced metric, fill, and output-schema
rules remain authoritative. Comparison MUST NOT relax a semantic failure or
reconstruct metric expressions.

Both compiled output schemas MUST have identical ordered names, kinds, and
datatypes. The expected projection is the canonical dimensions followed by the
canonical metrics. A mismatch fails before execution as an internal invariant.

This is not a public compiled bundle. The service may keep an internal pair of
completed artifacts, but no new compiler package abstraction or public
`compile_comparison` capability is introduced.

## 7. Execution contract

Project resolution follows explicit -> configured default -> sole project ->
`PROJECT_REQUIRED`. The resolved Project's DataSource type selects one Backend
and exact Renderer. Public input cannot override placement.

Execution order is baseline then current. One context timeout begins before the
first execution and covers both calls. Deployment `max_rows` and `max_bytes`
are cumulative across both results; the second call receives only the remaining
budget. Cancellation, timeout, limit, capacity, Driver, stream, normalization,
or cleanup failure prevents evaluation and returns no partial result.

The two executions share one opaque `analysis_id` for internal correlation.
Runtime continues to enforce its own atomic artifact ownership and normalized
scalar domain.

A Project without an executable DataSource remains valid for discovery and
`compile_sql`; comparison returns `QUERY_EXECUTION_UNAVAILABLE`.

## 8. Evaluator input and validation

The evaluator receives:

```text
comparison descriptor
baseline OutputSchema + normalized rows
current OutputSchema + normalized rows
```

It validates before returning any result:

- schemas are identical and match the descriptor's ordered dimensions/metrics;
- dimension columns are `OutputDimension` with String, Integer, Decimal, Float,
  Boolean, Date, Time, DateTime, or DateTimeTZ datatype;
- metric columns are `OutputMetric` with Integer or Decimal datatype;
- every row width matches the schema;
- every value matches its declared normalized JSON scalar representation;
- Float dimension values are finite JSON numbers; binary float metric evidence
  remains unsupported;
- each normalized composite dimension tuple occurs at most once per period;
- zero-dimension results contain at most one row per period;
- objects, arrays, opaque values, invalid decimals, and inconsistent evidence
  fail closed.

Null dimension members are valid tuple members and distinct from an absent row.
Numeric dimension identities normalize mathematically, so Decimal members
`"1.0"` and `"1.00"` are duplicates, not separate groups.

## 9. Public result

The closed result contract is:

```go
type DecimalString string
type ComparisonMemberValue = json.RawMessage

type ComparisonMember struct {
    Dimension  string                `json:"dimension"`
    MemberType ComparisonMemberType  `json:"member_type"`
    Value      ComparisonMemberValue `json:"value"`
}

type MetricComparisonValue struct {
    Metric         string         `json:"metric"`
    BaselineValue  *DecimalString `json:"baseline_value"`
    CurrentValue   *DecimalString `json:"current_value"`
    Delta          *DecimalString `json:"delta"`
    PercentChange  *DecimalString `json:"percent_change"`
    ChangeDefined  bool           `json:"change_defined"`
    PercentDefined bool           `json:"percent_defined"`
}

type MetricComparisonRow struct {
    Members         []ComparisonMember      `json:"members"`
    BaselinePresent bool                    `json:"baseline_present"`
    CurrentPresent  bool                    `json:"current_present"`
    Values          []MetricComparisonValue `json:"values"`
}

type CompareMetricsResult struct {
    AnalysisID    string                `json:"analysis_id"`
    TimeDimension string                `json:"time_dimension"`
    Baseline      ComparisonPeriod      `json:"baseline"`
    Current       ComparisonPeriod      `json:"current"`
    Metrics       []string              `json:"metrics"`
    Dimensions    []string              `json:"dimensions"`
    Rows          []MetricComparisonRow `json:"rows"`
}
```

`ComparisonMemberType` and `ComparisonMemberValue` use the same stable JSON
scalar mapping already proven for attribution, but remain comparison-owned
until post-implementation evidence justifies a shared public type:

| Member datatype | JSON representation |
| --- | --- |
| String, Date, Time, DateTime, DateTimeTZ | string |
| Integer, Float | number |
| Decimal | decimal string |
| Boolean | boolean |
| semantic null | `null` |

For each aligned row and metric:

```text
change_defined = baseline row/value exists AND current row/value exists
delta          = current_value - baseline_value, when change_defined
percent_defined = change_defined AND baseline_value != 0
percent_change = delta / baseline_value * 100, when percent_defined
```

Absent rows and null values remain null; they are never filled with zero.
`baseline_present` and `current_present` distinguish a missing tuple from a
present tuple whose metric is zero or null. A negative baseline retains its
sign in the percent-change denominator. Percent change is undefined at a zero
baseline rather than represented as infinity or zero.

All numerical result fields are decimal strings. Integer/Decimal inputs and
delta are exact arbitrary-precision rationals. Non-terminating percent changes
use deterministic decimal formatting at scale 18 with trailing zero removal.

Rows are the full union of baseline/current tuples. Metrics, dimensions,
members, and values use canonical ref order. Rows sort lexicographically by the
ordered member comparator: non-null before null, numeric by mathematical value,
other scalar types by canonical JSON token. No effect or magnitude ranking is
applied. With zero dimensions there is at most one row whose `members` is empty.
If both periods have no rows, `rows` is an empty array.

## 10. Public surfaces and Agent guidance

The proposed surfaces are:

```text
POST /v1/compare-metrics
MCP tool: compare_metrics
```

Both bind `MetricComparisonQuery`, call the same `CompareMetricsService`, and
project the same result, errors, authorization, cancellation, and Project
context. REST does not call MCP and MCP does not call REST.

The MCP tool is read-only, idempotent with respect to Metis-managed state,
non-destructive, and open-world because it reads a warehouse. Its description
and Agent instructions MUST say:

- discover canonical metric/time/dimension refs first;
- translate user time language to exact periods;
- use comparison for values and mathematical change, not attribution or causal
  explanation;
- do not reconstruct the same comparison with repeated `query_metrics` calls;
- do not infer missing rows are zero;
- start a new request only when metric, period, dimension, or filters change.

A runtime-disabled MCP server may omit the tool. The versioned REST route may
remain registered and return `QUERY_EXECUTION_UNAVAILABLE`.

There is no public `compile_comparison`, raw artifact pair, evaluator input,
`run_query`, or arbitrary-SQL surface.

## 11. Authentication and security

`compare_metrics` uses the shared Bearer authentication boundary and requires
`semantic:execute`. Project authorization occurs before DataSource resolution
or execution.

Requests and responses expose no SQL, parameters, schema internals, DataSource,
Backend, endpoint, credentials, or secret references. Error details remain
redacted. Dimension members and metric values are response data but never
metric labels or default trace attributes.

## 12. Errors

Existing project, semantic, compilation, and execution codes remain
authoritative. RFC-0064 adds only:

```text
UNSUPPORTED_METRIC_COMPARISON
    a governed compiled output cannot satisfy the closed exact comparison
    evidence contract; caller_action = CHANGE_REQUEST

INCONSISTENT_COMPARISON_RESULT
    complete normalized evidence violates schema, uniqueness, scalar, or
    arithmetic invariants; caller_action = REPORT_DEFECT
```

Malformed periods, duplicate refs, forbidden fields, and invalid counts use
`INVALID_QUERY`. Existing incompatible-grain, fan-out, metric-planning, and
execution codes are not wrapped in a generic comparison error.

## 13. Observability

One logical comparison emits a typed low-cardinality observation containing
only result, stable error code, duration, executed-query count, cumulative rows,
and cumulative bytes. Prometheus families are:

```text
metis_comparison_requests_total{result}
metis_comparison_duration_seconds
metis_comparison_errors_total{code}
```

The REST route and MCP method join the existing closed transport vocabularies.
Metrics, dimensions, periods, filters, tuple values, result values, SQL,
parameters, Project, DataSource, and analysis ID are not labels. Observation
failure cannot affect comparison behavior.

## 14. Compatibility and RFC-0054 closure

This is an additive v0.1 public capability. It does not change `compile_sql`,
`query_metrics`, or `attribute_metric` request/result behavior. Ordinary compile
fingerprints must remain unchanged.

When RFC-0064 is implemented with current specs and a durable ADR:

- RFC-0054 may become `Implemented` because comparison and attribution are two
  real Analytics Runtime verticals over one Execution Runtime;
- the implementation must record that two verticals still do not by themselves
  authorize a generalized analytical IR;
- shared primitives may be proposed later only from concrete duplication and
  compatibility evidence;
  RFC-0064 does not implement its broader external-catalog or policy model.

## 15. Alternatives

### 15.1 Let the Agent call `query_metrics` twice

Rejected as the product boundary. It externalizes deterministic period
normalization, exact request symmetry, row alignment, null/absence handling,
arithmetic, and cumulative failure behavior.

### 15.2 Reuse `attribute_metric`

Rejected. Attribution has one target metric, independent decomposition
dimensions, additive/ratio proof, contribution algebra, and reconciliation.
Comparison has multiple ordinary metrics at one shared grain and no attribution
claim.

### 15.3 Compile one comparison-specific SQL query

Rejected for v1. It would add comparison SQL construction, couple alignment to
warehouse syntax, and obscure reuse of the ordinary compiler. Two bounded
atomic queries keep Semantic Core and Runtime ownership explicit.

### 15.4 Allow separate baseline/current filters

Rejected for v1. Different populations need an explicit cohort-comparison
contract with typed population identity, not two arbitrary filter bags hidden
inside period comparison.

### 15.5 Add ranking or top movers

Rejected. Complete aligned evidence is required before ranking, and a mover
workflow needs separate ordering, limit, tie, remainder, and population
contracts.

### 15.6 Introduce a generic Analytics Runtime IR now

Rejected. RFC-0054 requires two real verticals before evaluating generalization,
not automatic generalization as part of the second vertical.

## 16. Rollout

Implementation order:

1. accept the request/result/evaluator contract;
2. implement database-independent fixtures and exact arithmetic;
3. implement two-query service orchestration with one selected Renderer;
4. add REST/MCP, auth, stable errors, and observability;
5. add DuckDB production-path additive and ratio/derived metric cases;
6. prove DuckDB, Doris, and ClickHouse compiled schemas satisfy one evaluator;
7. update current specs/design, add the durable ADR, and close RFC-0054;
8. run full CI, race, lifecycle, conformance, and deployable E2E gates.

Rollback removes the new public adapters and service while leaving Semantic
Core, Execution Runtime, and existing capabilities unchanged. No manifest or
deployment-config migration is introduced.

## 17. Test and acceptance criteria

RFC-0064 may become `Implemented` only when all of the following pass.

### Request and resolution

- one through sixteen unique metrics pass; zero, duplicates, and seventeen fail;
- zero through eight unique dimensions pass; duplicates and nine fail;
- exact offset periods normalize to UTC; invalid and empty periods fail;
- all refs and filters resolve to one model;
- time dimension reuse as a dimension/filter fails;
- forbidden physical controls and unknown JSON fields fail;
- canonical input permutations produce identical query/result ordering.

### Authority and compilation

- Backend selection occurs once and the exact Renderer instance supplies
  resolution, capabilities, and both renders;
- baseline/current use ordinary SemanticQuery compilation and identical output
  schemas;
- no comparison SQL builder, target branch, or second Renderer lookup exists;
- incompatible metrics/grains/fan-out fail through existing semantic errors;
- ordinary compile/query behavior and conformance fingerprints do not change.

### Execution

- exactly two queries execute baseline then current;
- one timeout and cumulative row/byte budgets cover the pair;
- remaining budget tightens the second call;
- cancellation or first-query failure prevents the second query;
- second-query failure discards the first result;
- Runtime receives only atomic CompiledQuery values;
- runtime-disabled Projects fail explicitly and remain compile-capable.

### Evaluation

- evaluator tests require no database;
- string, numeric, boolean, temporal, and null dimension members serialize and
  sort deterministically;
- composite tuples align correctly independent of warehouse row order;
- absent rows remain distinct from present zero/null values;
- delta is exact and percent change follows the signed-baseline formula;
- zero baseline produces explicit undefined percent change;
- empty and scalar populations follow section 9;
- malformed schemas/rows, duplicate normalized tuples, binary floats, objects,
  arrays, and inconsistent evidence fail closed;
- public numeric values are decimal strings and nullable fields serialize as
  explicit null;
- no result is ranked by magnitude.

### Interface, security, and operations

- REST and MCP call one service and have equivalent results/errors;
- `semantic:execute` and project authorization are enforced before execution;
- no response/error leaks physical or credential state;
- no public compile-comparison or arbitrary-SQL capability exists;
- observation stays within the documented cardinality budget;
- DuckDB production E2E executes comparison through real bootstrap/runtime;
- all executable built-in Backends pass applicable production-path tests;
- DuckDB, Doris, and ClickHouse schemas are evaluator-compatible without target
  evaluator branches;
- `go test ./...`, `go vet ./...`, race/lifecycle, conformance, and E2E gates
  pass.

### Architecture closure

- attribution and comparison demonstrably reuse one Runner/Driver SPI;
- no generic analytical IR is introduced;
- RFC-0054, AGENTS.md, current designs/specs, and a durable ADR describe the
  implemented two-vertical Analytics Runtime boundary.

## 18. Documentation updates

Implementation must update at least:

- `AGENTS.md` and root `README.md`;
- `docs/proposals/README.md` and RFC-0054 lifecycle;
- `docs/design/operations/runtime-bootstrap.md`;
- `docs/design/semantic/pipeline.md`;
- `docs/specs/glossary.md`;
- `docs/specs/public-contract.md`;
- `docs/specs/semantic/agent-query-contract.md`;
- `docs/specs/operations/runtime-bootstrap.md`;
- `docs/specs/operations/runtime-observability.md`;
- a comparison-specific current semantic/runtime specification;
- a new ADR recording vertical-first Analytics Runtime generalization.
