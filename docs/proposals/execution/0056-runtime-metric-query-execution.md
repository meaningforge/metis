# RFC-0056: Runtime Metric Query Execution

> **Design history:** This RFC preserves its original lifecycle status and rationale.
> Implementation claims describe the recorded design, not a guarantee that every
> proposed API remains current. See [current documentation](../../README.md) for
> supported interfaces. Package paths have been updated for this repository.

- **Status:** Implemented
- **Owners:** Metis maintainers
- **Created:** 2026-08-29
- **Last updated:** 2026-08-30
- **Scope:** `query_metrics`, application orchestration, compile/execute parity, Project/DataSource runtime resolution, public result delivery, REST/MCP projection, execution authorization, cancellation, observability, and `compile_sql` public naming
- **Supersedes:** none; refines RFC-0054's public runtime-query direction using the execution contracts accepted by [Core runtime contract](../../specs/operations/runtime-bootstrap.md) and ADR-0010
- **Related:** RFC-0034, RFC-0036, RFC-0045, RFC-0050, RFC-0051, RFC-0054, RFC-0058, ADR-0010
- **Implementation prerequisite:** PR #513, which makes `CompiledQuery` the atomic compiler/execution handoff and adds schema-governed result normalization and ownership isolation

## 1. Summary

This RFC defines the first Agent-facing Metis execution capability:

```text
query_metrics
```

`query_metrics` accepts governed semantic metric intent, compiles it through the
same canonical Semantic Core path used by compile-only SQL generation, executes
the resulting compiler artifact through the bounded Execution Runtime, and
returns one complete normalized result.

The target capability model is:

```text
compile_sql
    governed SemanticQuery + explicit SQL dialect
        -> canonical semantic compilation
        -> CompiledQuery {
             SqlRenderResult
             OutputSchema
           }

query_metrics
    governed metric SemanticQuery
        -> resolved Project
        -> Project DataSource
        -> Backend-selected Renderer
        -> canonical semantic compilation
        -> CompiledQuery
        -> ExecutionRuntime
        -> normalized QueryMetricsResult

attribute_metric                  # RFC-0057, future
    AttributionRequest
        -> deterministic analytical planning
        -> CompiledQuery bundle
        -> execution
        -> deterministic evaluation
        -> AttributionResult
```

`CompiledQuery` is not a replacement for `SqlRenderResult` as the physical query
IR. It is the atomic execution handoff that binds the physical query to the
`OutputSchema` required to interpret its result.

The key decisions are:

1. `query_metrics` is the only v1 public generic query-execution capability.
2. Metis does **not** expose `run_query`, `execute_sql`, `run_sql`, or an arbitrary raw-SQL execution API.
3. A `query_metrics` request contains semantic intent, never SQL, dialect, Renderer, DriverFactory, DataSource override, endpoint, or credentials.
4. Runtime placement is derived only from the resolved Project Registration and its configured DataSource, as defined by [Core runtime contract](../../specs/operations/runtime-bootstrap.md).
5. The DataSource type resolves one Backend; that Backend's exact Renderer instance is passed into the canonical compiler and its DriverFactory remains the execution implementation authority.
6. `compile_sql` and `query_metrics` share one semantic resolution/planning/rendering implementation. A second runtime compiler path is forbidden.
7. Runtime execution begins only from the completed atomic `CompiledQuery` compiler artifact.
8. `CompiledQuery.Query` remains the canonical `SqlRenderResult`; `CompiledQuery.OutputSchema` travels with it across the execution boundary and governs result normalization.
9. Deployment DataSource timeout, row, and byte ceilings remain mandatory execution safety policy. Public Agent requests do not bypass or relax them.
10. A limit breach, timeout, cancellation, driver failure, schema mismatch, normalization failure, or cleanup failure returns an error and never a partial successful result.
11. REST and MCP are projections over the same transport-neutral service operation.
12. Execution receives a distinct authorization scope from compile-only access.
13. The existing public `compile` capability is renamed to canonical `compile_sql`; because Metis is pre-stable, this RFC proposes a clean rename rather than permanent synonyms.

The core invariant is:

> **Runtime execution may add orchestration after compilation, but it may not create another semantic compiler, another Renderer authority, another project-routing authority, or split the compiler artifact into independently mutable query/schema arguments.**

## 2. Implemented baseline and prerequisite

The prerequisites and public capability defined by this RFC are implemented.
The current runtime and Agent contracts are normative in
[`docs/specs/operations/runtime-bootstrap.md`](../../specs/operations/runtime-bootstrap.md)
and [`docs/specs/semantic/agent-query-contract.md`](../../specs/semantic/agent-query-contract.md);
this RFC records the architectural decision and implementation rationale.

### 2.1 Implemented runtime foundation

The implemented foundation includes:

- deterministic Project resolution is implemented;
- Project Registration may reference one DataSource;
- `DataSourceRegistry` is implemented for concrete runtime instances;
- `BackendRegistry` is implemented for type-level `SQLDialect + Renderer + DriverFactory` authority;
- the production compiler uses one selected Renderer identity through semantic expression evidence, capability validation, planning, and rendering;
- `SemanticQuerySpec` is the canonical planner-facing resolved query type;
- the bounded `execution/runner.Runner` is implemented;
- SecretRef resolution, timeout, row/byte ceilings, cancellation, ResultStream consumption, cleanup, and fail-closed execution behavior are owned by Execution Runtime;
- REST and MCP project through the shared transport-neutral `QueryMetricsService`.

### 2.2 Implemented prerequisite: atomic `CompiledQuery`

PR #513 established the runtime substrate that this RFC composes:

```text
Semantic Core
    -> SqlRenderResult + OutputSchema
    -> CompiledQuery
    -> ExecutionRuntime
    -> Executor
    -> ResultStream
    -> schema-governed normalization
    -> ResultSet
```

The implemented prerequisite provides all of the following:

- `ExecutionRuntime` and `Executor` consume one `CompiledQuery`, not separately passed query/schema values;
- compiler artifact containers have defined ownership and mutation rules;
- Runtime snapshots the known mutable parts of the artifact at the execution boundary;
- an Executor cannot mutate caller/compiler artifact evidence or the Runtime's validation copy;
- `QueryParameter.Value` has a closed, validated runtime domain rather than arbitrary `any` values escaping into execution;
- every non-NULL driver value is normalized according to logical `OutputSchema` when a datatype is declared;
- values that cannot be represented losslessly under the declared logical datatype fail closed;
- supported mutable result containers are copied before they escape through `ResultSet`;
- unknown driver objects fail rather than leaking driver-dependent values into the public result.

RFC-0056 composes that completed compiler artifact and bounded runtime; it does
not redesign `Executor`, `DriverFactory`, `ResultSet`, SecretRef handling,
normalization rules, or execution-limit mechanics.

## 3. Architectural authority

[Core runtime contract](../../specs/operations/runtime-bootstrap.md) and ADR-0010 remain authoritative for runtime placement and physical
implementation selection:

```text
request project
    |
    v
Project resolution
    explicit project
        > default_project
        > sole registered project
        > PROJECT_REQUIRED
    |
    v
Project Registration
    |
    v
data_source
    |
    v
DataSourceRegistry
    |
    v
DataSource.type
    |
    v
BackendRegistry[type]
    +-- SQLDialect
    +-- Renderer
    `-- DriverFactory
```

RFC-0058 remains authoritative for Semantic Core:

```text
SemanticManifest
    -> SemanticGraph
    -> SemanticManifestLookup
    -> QueryResolver
    -> SemanticQuerySpec
    -> MetricEvaluationPlan
    -> SemanticPlan
    -> Optimizer
    -> SQLPlan
    -> Renderer
    -> SqlRenderResult
```

The compiler packages the resulting `SqlRenderResult` together with its
`OutputSchema` as `CompiledQuery` for execution handoff. RFC-0056 composes these
authorities without adding another one.

## 4. Problem

Metis historically stopped after deterministic semantic compilation. A caller
that wanted live results had to perform the remaining orchestration itself:

```text
semantic request
    -> Metis compile
    -> inspect returned SQL
    -> select a database connection
    -> execute SQL
    -> fetch rows
    -> normalize results
    -> enforce timeout / size / cancellation policy
```

Compiler-only mode remains valuable and first-class, but requiring every Agent
or embedding application to reimplement the execution half has three production
costs:

1. credential handling, connection lifecycle, limits, cancellation, and normalization are repeatedly reimplemented;
2. a caller can route a correctly compiled query to the wrong database instance or a mismatched execution tool;
3. higher-level deterministic analytical workflows such as metric attribution need a reusable execution substrate instead of Agent-authored compile/execute loops.

The solution must not turn Metis into a generic SQL gateway. The product boundary
is instead:

```text
Agent chooses semantic intent.
Metis resolves semantic meaning.
Metis compiles one governed artifact.
Metis may orchestrate bounded execution of that artifact.
Warehouse executes the physical query.
Metis normalizes according to the compiler schema.
Agent reasons over the normalized result.
```

## 5. Goals

RFC-0056 must:

1. expose one bounded Agent-facing execution operation named `query_metrics`;
2. reuse the existing semantic query contract instead of creating an execution-specific semantic DSL;
3. require at least one governed metric in a v1 `query_metrics` request;
4. use [Core runtime contract](../../specs/operations/runtime-bootstrap.md) Project -> DataSource -> Backend resolution with no caller runtime override;
5. use the Backend-selected Renderer instance as the same Renderer authority throughout compilation;
6. share the existing resolver, `SemanticQuerySpec`, metric evaluation, `SemanticPlan`, optimizer, `SQLPlan`, Renderer, and compiler path with compile-only compilation;
7. execute only an atomic `CompiledQuery` produced by canonical compilation;
8. return normalized rows governed by the `CompiledQuery.OutputSchema`;
9. preserve mandatory deployment timeout, row, and byte ceilings;
10. preserve compiler-only deployments and explicit-dialect `compile_sql`;
11. provide identical application semantics through REST and MCP;
12. propagate cancellation from transport to compilation and execution;
13. make complete cleanup and no-partial-success behavior mandatory;
14. provide bounded, low-cardinality observability without logging SQL, query values, secrets, endpoints, or result rows;
15. define the public naming migration from `compile` to `compile_sql`.

## 6. Non-goals

RFC-0056 does not introduce:

- arbitrary SQL execution;
- a `run_query` alias;
- a caller-selected DataSource;
- a caller-selected endpoint, credential, DriverFactory, Renderer, or runtime dialect;
- semantic federation across multiple DataSources in one query;
- result caching;
- pre-aggregation;
- warehouse scheduling or workload management;
- asynchronous query jobs;
- background execution after caller cancellation;
- partial or truncated successful results;
- streaming MCP/REST result delivery in v1;
- Agent-authored analytical workflows inside Execution Runtime;
- attribution evaluation; that belongs to RFC-0057;
- a generic Analytics Runtime IR;
- a second public representation of `CompiledQuery` distinct from the existing compiler result contract.

## 7. Public capability model

### 7.1 `compile_sql`

`compile_sql` is the compiler-only capability:

```text
SemanticQuery + explicit dialect
    -> resolve Project
    -> resolve Renderer by requested SQL dialect
    -> canonical semantic compilation
    -> CompiledQuery {
         SqlRenderResult
         OutputSchema
       }
```

The transport may expose the existing physical-query/schema fields rather than
a Go type named `CompiledQuery`; the normative requirement is that they originate
from one canonical compiler artifact.

`compile_sql` does not require a DataSource, credentials, connection, or
DriverFactory. The explicit `dialect` is valid here because the caller is
selecting an output SQL language, not an execution destination.

### 7.2 `query_metrics`

`query_metrics` is execution-oriented and therefore accepts no explicit dialect:

```text
SemanticQuery
    -> resolve Project exactly once
    -> require Project Registration.data_source
    -> resolve DataSource exactly once
    -> resolve Backend from DataSource.type exactly once
    -> obtain Backend.Renderer
    -> canonical semantic compilation with that exact Renderer
    -> CompiledQuery
    -> ExecutionRuntime.Execute(Project DataSource, CompiledQuery)
    -> QueryMetricsResult
```

The caller cannot override any runtime selection after Project choice.

### 7.3 Why not `run_query`

`run_query` is rejected because it naturally suggests API shapes such as:

```text
run_query(sql="...")
run_query(query="SELECT ...")
run_query(database="prod", sql="...")
```

Those are database-proxy contracts. `query_metrics` instead communicates that
the input is governed semantic metric intent and deterministic compilation is a
mandatory stage before execution.

## 8. The one-Renderer rule across compile and execute

The most important orchestration constraint in RFC-0056 is avoiding an
accidental second Renderer selection.

The compile-only path resolves a Renderer from an explicitly requested SQL
dialect. Runtime has a different authority: [Core runtime contract](../../specs/operations/runtime-bootstrap.md) says
`DataSource.type -> Backend -> Renderer`.

Therefore this runtime path is forbidden:

```text
Project -> DataSource -> Backend
                     |
                     v
              Backend.Dialect
                     |
                     v
        CompileService.Compile(dialect)
                     |
                     v
          RendererRegistry.Resolve()   # second authority: forbidden
```

Even if both Renderers report the same dialect name, this creates two selection
authorities and violates ADR-0010.

The required shape is:

```text
compile_sql
    dialect
      -> RendererRegistry.Resolve once
      -> compileWithRenderer(query, renderer)
      -> CompiledQuery

query_metrics
    Project -> DataSource -> Backend.Resolve once
                            |
                            v
                     Backend.Renderer
                            |
                            v
               compileWithRenderer(query, renderer)
                            |
                            v
                       CompiledQuery
                            |
                            v
                  ExecutionRuntime.Execute
```

The exact private Go method/type name is not normative, but there MUST be one
shared internal compilation operation that accepts an already-selected Renderer
and owns the canonical semantic pipeline. `compile_sql` and `query_metrics`
select their Renderer differently, then converge before semantic resolution.

## 9. Request contract

### 9.1 Service-level request

The transport-neutral application request is conceptually:

```go
type QueryMetricsRequest struct {
    Query query.SemanticQuery `json:"query"`
}
```

The exact Go wrapper may evolve with the existing service DTOs, but the public
semantic meaning is normative.

A v1 `query_metrics` request:

- MUST contain at least one metric;
- MAY contain dimensions/grouping, filters, ordering, time semantics, and semantic `limit` using the existing query contract;
- MAY identify a Project using the same project-selection semantics as other Agent-facing operations;
- MUST NOT contain `dialect`;
- MUST NOT contain DataSource ID/type;
- MUST NOT contain SQL;
- MUST NOT contain Renderer/driver/target identity;
- MUST NOT contain endpoint or credentials.

Metric-free compilation remains valid through `compile_sql` where already
supported. Expanding live execution to dimension-only semantic requests should
be justified separately rather than weakening the initial `query_metrics`
contract.

### 9.2 Agent-facing compact request

MCP SHOULD mirror the existing compact Agent compile shape so Agents do not need
a second semantic request language. Conceptually:

```json
{
  "project_id": "finance",
  "model_id": "model:sales",
  "output_metrics": ["metric:sales.revenue"],
  "group_by": [
    {"dimension": "dimension:sales.region"}
  ],
  "filters": [],
  "order_by": [],
  "limit": 100
}
```

The important difference from `compile_sql` is the intentional absence of
`dialect`.

### 9.3 Semantic limit vs execution safety limits

The semantic query `limit` and Execution Runtime safety ceilings are different
concepts:

```text
SemanticQuery.limit
    = requested query meaning / output shape
    = may alter generated SQL

DataSource execution policy
    = safety ceiling for timeout / returned rows / returned bytes
    = must not alter semantic meaning
    = breach fails closed
```

RFC-0056 MUST NOT silently convert deployment `MaxRows` into an injected SQL
`LIMIT`, because doing so would change query semantics and could make an
incomplete result look successful.

The first Agent-facing `query_metrics` surface does not need to expose separate
`max_rows`, `max_bytes`, or timeout knobs. It relies on mandatory DataSource
policy already enforced by Execution Runtime. A future trusted API may expose
caller options that only tighten those limits; it may never relax deployment
policy.

## 10. Runtime orchestration

The transport-neutral application operation owns this exact lifecycle:

```text
QueryMetrics(ctx, request)
    |
    +-- authenticate/authorize request context
    |
    +-- validate governed metric request
    |
    +-- resolve Project exactly once
    |
    +-- resolve Project Registration.data_source
    |       `-- missing -> execution unavailable
    |
    +-- resolve DataSource exactly once
    |
    +-- resolve Backend exactly once from DataSource.type
    |
    +-- take Backend.Renderer
    |
    +-- canonical compileWithRenderer(...)
    |       -> CompiledQuery
    |            +-- SqlRenderResult
    |            `-- OutputSchema
    |
    +-- ExecutionRuntime.Execute(
    |       dataSource,
    |       CompiledQuery,
    |       deployment-bounded options)
    |
    `-- project ResultSet as public QueryMetricsResult
```

Project resolution MUST NOT occur independently once for compilation and again
for execution. The service resolves one Project context and uses the matching
Project Registration for both semantic snapshot selection and DataSource
routing.

Likewise, DataSource and Backend resolution belong to orchestration; Executor
continues to receive no semantic state.

The application layer MUST NOT unpack `CompiledQuery` into separately mutable
query and schema arguments before calling Execution Runtime.

## 11. Result and normalization contract

The public result is one complete normalized query result:

```go
type QueryMetricsResult struct {
    QueryID string                 `json:"query_id"`
    Schema  compiler.OutputSchema `json:"schema"`
    Rows    [][]any                `json:"rows"`
    Count   int64                  `json:"count"`
}
```

The exact field names may reuse the implemented execution `ResultSet`, but these
public properties are required:

- one opaque correlation/query ID;
- the target-neutral `OutputSchema` from the executed `CompiledQuery`;
- normalized rows in schema order;
- complete row count.

Normalization is an Execution Runtime responsibility established by the
`CompiledQuery` prerequisite, not an application-layer reinterpretation step.
The selected Executor reads database result metadata and decodes physical wire
values; Runtime then normalizes values according to the logical
`CompiledQuery.OutputSchema`.

Public JSON representation MUST follow the runtime normalization contract. In
particular:

- `String` is returned as a string;
- `Decimal` is represented without binary-float rounding loss;
- `Integer` and `Float` are JSON numbers when representable under their declared logical types;
- `Boolean` is a JSON boolean;
- temporal values use the runtime-defined string representation;
- NULL remains NULL;
- an omitted datatype remains unspecified rather than guessed from a wire value;
- an unrepresentable or lossy value fails rather than silently changing meaning.

The public result SHOULD NOT include by default:

- generated SQL;
- DataSource name;
- endpoint/address;
- credentials or SecretRefs;
- DriverFactory identity;
- Renderer identity;
- internal byte-accounting values;
- raw database-driver metadata or driver-native objects.

A caller that needs the SQL artifact uses `compile_sql`. Keeping SQL out of the
normal execution result prevents the public query API from becoming a disguised
SQL execution/debugging surface.

A result is atomic from the caller's perspective. Execution Runtime may consume
a stream internally, but a timeout, cancellation, limit breach, schema mismatch,
normalization failure, stream error, or cleanup failure MUST return an error
rather than a partial successful `QueryMetricsResult`.

## 12. Artifact ownership boundary

RFC-0056 relies on the ownership contract established by the `CompiledQuery`
prerequisite.

After compilation publishes a `CompiledQuery`, callers MUST treat the artifact
and its nested values as immutable. Runtime owns the execution-boundary snapshot
needed to prevent Executor mutation from altering compiler/caller evidence.

The ownership contract is deliberately closed and explicit rather than a generic
reflection-based deep-copy facility:

- known compiler artifact containers are copied at defined boundaries;
- scalar parameter values are immutable values;
- the supported mutable parameter/container cases are copied explicitly;
- values outside the closed artifact/result domain fail closed;
- Runtime does not infer copy semantics for arbitrary pointers, structs, slices, maps, or driver objects.

This boundary matters to public execution because schema validation,
normalization, observability evidence, and the returned schema MUST all refer to
the same logical compiler artifact even when an Executor implementation is
mutable internally.

## 13. Authorization

Live warehouse execution is materially different from semantic discovery or
compile-only SQL generation. RFC-0056 therefore adds a distinct authorization
scope:

```text
semantic:execute
```

The intended capability split is:

```text
semantic:read
    discovery / inspection

semantic:compile
    compile_sql / validate / explain as applicable

semantic:execute
    query_metrics
```

`semantic:compile` alone MUST NOT authorize live execution.

A caller authorized for `semantic:execute` does not need to separately hold
`semantic:compile` merely because compilation is an internal stage of
`query_metrics`; authorization applies to the complete service operation rather
than each private implementation step.

Project-level authorization remains independent from authentication. An
execution scope never grants access to a Project the principal is otherwise not
allowed to access.

## 14. Error boundary

Execution Runtime has detailed internal execution error codes. Public
Agent-facing errors MUST preserve actionable categories without leaking
infrastructure or secret-provider details.

The application layer SHOULD normalize runtime failures into a bounded public
vocabulary such as:

```text
QUERY_EXECUTION_UNAVAILABLE
QUERY_EXECUTION_LIMIT_EXCEEDED
QUERY_EXECUTION_TIMEOUT
QUERY_EXECUTION_CANCELLED
QUERY_EXECUTION_FAILED
```

Examples:

- Project exists but has no DataSource -> `QUERY_EXECUTION_UNAVAILABLE`;
- requested Project is unknown -> existing Project error contract;
- semantic metric/dimension request is invalid -> existing semantic error contract;
- deployment/runtime wiring is absent -> `QUERY_EXECUTION_UNAVAILABLE`;
- timeout -> `QUERY_EXECUTION_TIMEOUT`;
- caller cancellation -> `QUERY_EXECUTION_CANCELLED`;
- row/byte ceiling breach -> `QUERY_EXECUTION_LIMIT_EXCEEDED`;
- secret resolution, open, driver, stream, schema, normalization, ownership, or cleanup failure -> redacted `QUERY_EXECUTION_FAILED` unless a more actionable safe category exists.

The public error MUST NOT expose:

- secret values;
- SecretRef values when sensitive;
- endpoint/address;
- database usernames;
- raw driver errors;
- raw secret-provider errors;
- SQL text;
- connection strings.

REST maps these transport-neutral errors to HTTP status according to RFC-0034;
MCP maps the same service errors to MCP tool errors. Transport adapters do not
invent independent execution semantics.

## 15. Cancellation and lifecycle

The inbound request context is the sole lifecycle root:

```text
REST/MCP request context
    -> QueryMetrics service
    -> Semantic Core compile
    -> ExecutionRuntime.Execute
    -> DriverFactory / Executor / ResultStream
```

Client disconnect, MCP cancellation, server shutdown, or deadline expiry MUST
propagate through this chain.

`query_metrics` MUST NOT detach execution into a background context after the
caller has cancelled. Asynchronous jobs are a separate future product contract.

## 16. Observability and correlation

Every `query_metrics` operation SHOULD create one opaque `query_id` at the
application boundary and propagate it through compile and execution
observability.

Safe runtime observations include bounded fields such as:

- operation = `query_metrics`;
- success/error class;
- duration;
- compile phase durations;
- DataSource type / backend family when operator policy permits it;
- returned row/byte aggregate counts;
- stable public/internal error code;
- cancellation/timeout/limit outcome.

Do not use the following as metric labels or unrestricted trace attributes:

- SQL text;
- metric names;
- dimension names;
- filter values;
- Project name/ID unless an explicitly bounded operator-only policy permits it;
- DataSource instance name;
- endpoint;
- result values;
- credentials or SecretRefs.

`query_id` is correlation metadata, not a cache key or semantic identity.

## 17. REST projection

RFC-0056 introduces the authenticated v1 route:

```text
POST /v1/query-metrics
```

The body contains governed semantic request fields only. It does not accept SQL
or runtime-placement overrides.

REST MUST register this route inside the existing `/v1` API namespace so it uses
the same authentication, authorization, error, and observability middleware
boundary as the current public API.

REST MUST call the same transport-neutral `QueryMetrics` service operation used
by MCP. REST MUST NOT call MCP and MCP MUST NOT call REST.

Compiler-only public naming becomes:

```text
POST /v1/compile-sql
```

rather than `/v1/compile`.

Because Metis is still pre-stable and the repository explicitly does not want
two permanent synonyms, RFC-0056 proposes a clean breaking rename at the same
public-surface migration point:

```text
/v1/compile       -> removed
/v1/compile-sql   -> canonical
```

`/v1/validate` and `/v1/explain` keep their existing meanings and are not
renamed by this RFC.

No execution endpoint defined by this RFC may be registered outside the `/v1`
namespace.

## 18. MCP projection

RFC-0056 introduces the Agent-facing MCP tool:

```text
query_metrics
```

The tool accepts the compact governed semantic request shape described above.
It MUST NOT contain `sql`, `dialect`, `data_source`, `driver`, `renderer`, or
endpoint/credential parameters.

The existing MCP compile capability becomes:

```text
compile_sql
```

The old `compile` tool name is removed rather than retained as a permanent alias.
This keeps the public capability vocabulary aligned with RFC-0054:

```text
compile_sql
query_metrics
attribute_metric    # future RFC-0057
```

The `query_metrics` tool is logically read-only and non-destructive with respect
to semantic assets and warehouse data. Its implementation nevertheless performs
external warehouse I/O, so MCP annotations MUST NOT imply that it is a pure
local computation.

## 19. Bootstrap and service composition

The current bootstrap already loads Project configuration, semantic state,
BackendRegistry, and DataSourceRegistry. RFC-0056 adds application composition,
not a second configuration system.

Conceptually:

```text
bootstrap
    |
    +-- Project config / resolver
    +-- SemanticManifestLookup(s)
    +-- BackendRegistry
    +-- DataSourceRegistry
    +-- SecretResolver (when configured/required)
    |
    +-- CompileService
    |
    +-- ExecutionRuntime
    |
    `-- QueryMetricsService
```

`QueryMetricsService` MUST receive the same runtime BackendRegistry and
DataSourceRegistry authority used by ExecutionRuntime. It MUST NOT construct a
parallel registry from transport configuration.

Compiler-only startup remains valid when no executable DataSource is configured.
In that deployment:

- discovery works;
- `compile_sql` works for registered SQL Renderers;
- `query_metrics` fails explicitly as unavailable rather than guessing a target.

A missing SecretResolver is allowed only when the selected DataSource requires
no secret resolution. If SecretRefs are required, Execution Runtime fails closed
as defined by [Core runtime contract](../../specs/operations/runtime-bootstrap.md).

## 20. Compile/execute parity invariant

For the same semantic request, resolved Project semantic snapshot, and physical
SQL dialect, `query_metrics` must compile to the same canonical compiler artifact
that compile-only compilation would produce with the equivalent Renderer.

Conceptually:

```text
compile_sql(query, dialect=Backend.Dialect)
    -> CompiledQuery A
         Query        = SqlRenderResult A
         OutputSchema = OutputSchema A

query_metrics(query)
    -> Project -> DataSource -> Backend.Renderer
    -> CompiledQuery B
         Query        = SqlRenderResult B
         OutputSchema = OutputSchema B

required:
    canonical(A) == canonical(B)
```

This does not permit `query_metrics` to perform a second Renderer lookup by
Backend dialect. Conformance compares artifacts while runtime still uses the
Backend-selected Renderer instance directly.

The parity gate must include:

- physical SQL / parameter fingerprint parity;
- `OutputSchema` parity;
- complete `CompiledQuery` canonical parity;
- semantic-plan parity where the same semantic snapshot and Renderer evidence apply;
- optimized/unoptimized correctness gates already required by Semantic Core;
- no hidden execution-only rewrite of SQL after compilation.

## 21. Required tests

RFC-0056 implementation is not complete without tests proving these boundaries.

### 21.1 Prerequisite gate

Before any public `query_metrics` route/tool is enabled, tests MUST prove the
`CompiledQuery` execution prerequisite:

- `ExecutionRuntime.Execute` accepts one `CompiledQuery` artifact;
- Executor receives an execution snapshot that cannot mutate the caller/runtime validation artifact;
- query parameter values outside the closed compiler artifact domain fail before opening a DriverFactory;
- `OutputSchema`-governed normalization is exercised for string, decimal, integer, float, boolean, temporal, and NULL cases supported by the runtime;
- lossy decimal-via-float and other unrepresentable values fail closed;
- unsupported mutable driver objects cannot escape through `ResultSet`;
- schema mismatch and normalization failure never return partial success.

### 21.2 Orchestration

- explicit/default/sole Project resolution matches existing operations;
- unknown explicit Project never falls back;
- Project without DataSource fails as execution unavailable;
- DataSource type resolves exactly one Backend;
- Backend.Renderer is passed directly into canonical compilation;
- no second Renderer registry lookup occurs in the runtime path;
- the exact canonical `CompiledQuery` is passed atomically to `ExecutionRuntime`;
- application orchestration does not reconstruct or independently mutate `SqlRenderResult` or `OutputSchema`.

### 21.3 Request surface

- at least one metric is required;
- `dialect` is rejected/absent for `query_metrics`;
- SQL input is impossible in the schema;
- DataSource/runtime overrides are impossible in the schema;
- compact MCP request and REST/service request normalize to equivalent semantic intent.

### 21.4 Execution behavior

Reuse the execution substrate tests and add application-level proof that:

- timeout maps to the public timeout error;
- row/byte limit breach maps to the public limit error;
- cancellation propagates from REST/MCP request context;
- runtime failures return no partial public result;
- public error redaction never leaks underlying secret/driver strings;
- returned schema is the `OutputSchema` from the executed canonical artifact.

### 21.5 Authorization

- `semantic:compile` without `semantic:execute` cannot call `query_metrics`;
- `semantic:execute` authorizes the complete operation without requiring callers to invoke compile separately;
- Project authorization remains enforced;
- wildcard scope behavior remains consistent with existing auth contracts.

### 21.6 Transport parity

For equivalent requests, REST and MCP must produce equivalent:

- semantic errors;
- execution errors;
- schema and rows;
- result count;
- cancellation behavior;
- correlation semantics.

The REST test MUST assert the route is `/v1/query-metrics`, not
`/query-metrics`. The compile rename test MUST assert `/v1/compile-sql` and the
removal of `/v1/compile`.

### 21.7 Compiler parity

For every executable Backend in the conformance matrix:

```text
compile_sql(request, backend dialect)
vs
query_metrics internal compile using backend renderer
```

must produce equivalent canonical `CompiledQuery` artifacts.

A dedicated test SHOULD use distinct Renderer objects with the same dialect name
so any accidental second lookup is observable and fails.

## 22. Implemented rollout

The rollout completed in this order:

1. PR #513 established the atomic `CompiledQuery`/schema-normalization boundary while public transports remained compile-only;
2. `compileWithRenderer` preserved one Renderer authority for the shared compiler path;
3. `QueryMetricsService` composed Project Registration, BackendRegistry, and Runner;
4. `semantic:execute` authorization and redacted public error mapping were added;
5. REST exposed `POST /v1/query-metrics` and cleanly renamed `/v1/compile` to `/v1/compile-sql`;
6. MCP exposed `query_metrics` and cleanly renamed `compile` to `compile_sql`;
7. runtime, one-Renderer, ownership/normalization, cancellation, executable-backend, and REST/MCP parity gates were added;
8. current specifications and agent guidance were updated to reflect the shipped capability.

The implementation never exposed arbitrary SQL, bypassed the `/v1` REST
namespace, or split the atomic compiler artifact at the execution boundary.

## 23. Rejected alternatives

### 23.1 Expose `run_query(sql)`

Rejected. It turns Metis into an arbitrary SQL gateway, bypasses semantic
governance, and makes the public execution boundary broader than required.

### 23.2 Let `query_metrics` accept `dialect`

Rejected. Runtime dialect comes from the Project DataSource type through one
Backend. A caller-supplied dialect creates a second physical authority and can
diverge from the execution destination.

### 23.3 Let callers choose `data_source`

Rejected for v1. Project Registration is the deterministic runtime placement
authority. Caller routing overrides recreate the removed ExecutionBinding/route
model and complicate authorization.

### 23.4 Resolve Backend only to a dialect, then call compile-only API

Rejected. A dialect-name handoff followed by RendererRegistry lookup violates
the one-Renderer invariant even when both objects report the same dialect.

### 23.5 Pass `SqlRenderResult` and `OutputSchema` separately to Runtime

Rejected for the RFC-0056 target architecture. The query and the schema that
defines its logical result form one compiler artifact. Splitting them creates
avoidable aliasing/mismatch risk and weakens the evidence used for validation
and normalization.

### 23.6 Normalize result values in the public service layer

Rejected. Result normalization belongs to the bounded Execution Runtime because
it is coupled to the executed `CompiledQuery.OutputSchema`, physical driver
values, losslessness checks, row/byte accounting, and fail-closed stream
lifecycle.

### 23.7 Return generated SQL from every `query_metrics` response

Rejected as the default contract. SQL is available through `compile_sql`; the
execution response should remain focused on governed result data and avoid
encouraging callers to treat `query_metrics` as a SQL gateway/debug protocol.

### 23.8 Inject deployment MaxRows as SQL LIMIT

Rejected. Safety policy must not silently change semantic query meaning or
convert a truncated result into apparent success.

### 23.9 Keep both `compile` and `compile_sql` indefinitely

Rejected. Two permanent names for the same operation create unnecessary Agent
surface and contradict RFC-0054's canonical capability vocabulary. Metis is
pre-stable, so this RFC prefers one clean migration.

### 23.10 Register unversioned REST execution routes

Rejected. Public REST execution belongs inside the existing `/v1` namespace and
must inherit the established authentication, authorization, error, and
observability middleware boundary.

## 24. Relationship to RFC-0057

RFC-0057 may build `attribute_metric` on the proven compiler/execution substrate:

```text
attribute_metric
    -> Attribution Planner
    -> internal CompiledQuery bundle
    -> ExecutionRuntime
    -> normalized ResultBundle
    -> Attribution Evaluator
    -> AttributionResult
```

`attribute_metric` should reuse the same execution and authorization principles
without forcing an Agent to manually call `query_metrics` for every internal
attribution query.

RFC-0056 intentionally does not introduce a generic Analytics Runtime IR. That
abstraction still requires evidence from multiple implemented analytical
verticals.

## 25. Implementation closure

The following acceptance criteria are implemented and covered by the runtime,
application, transport, and conformance test suites:

1. `query_metrics` as the only v1 generic public execution capability;
2. no arbitrary SQL / `run_query` surface;
3. Project -> DataSource -> Backend as the sole runtime placement chain;
4. Backend.Renderer passed directly into the shared canonical compiler path;
5. `CompiledQuery { SqlRenderResult + OutputSchema }` as the atomic compiler/execution handoff;
6. the #513 ownership and schema-governed normalization boundary as a prerequisite rather than an unmerged baseline claim;
7. mandatory bounded ExecutionRuntime behavior and no partial success;
8. `semantic:execute` as a distinct authorization scope;
9. REST/MCP sharing one transport-neutral service operation;
10. REST paths `POST /v1/query-metrics` and `POST /v1/compile-sql`, with `/v1/compile` removed;
11. the public MCP `compile` -> `compile_sql` clean rename;
12. the test gates in section 21.

The prerequisite, public REST/MCP behavior, authorization, bootstrap wiring,
error mapping, cancellation, one-Renderer proof, ownership/normalization proof,
and executable-backend conformance tests are merged. Current specifications
reflect the shipped behavior.

## References

- [RFC-0054: Execution and Analytics Runtime Architecture](0054-execution-analytics-runtime-architecture.md)
- [Core runtime contract](../../specs/operations/runtime-bootstrap.md)
- [RFC-0058: Semantic Core Layering and Semantic Graph](../semantic/0058-semantic-core-layering-and-semantic-graph.md)
- [ADR-0010: Renderer and DataSource Type Are the Sole Compile/Runtime Authorities](../../decisions/operations/0010-renderer-and-datasource-type-are-the-sole-compile-runtime-authorities.md)
- PR #513: atomic `CompiledQuery` runtime handoff, schema-governed normalization, and artifact ownership
