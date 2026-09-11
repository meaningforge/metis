# RFC-0060: Warehouse Renderer and Execution Backend Extension Architecture

> **Design history:** This RFC preserves its original lifecycle status and rationale.
> Implementation claims describe the recorded design, not a guarantee that every
> proposed API remains current. See [current documentation](../../README.md) for
> supported interfaces. Package paths have been updated for this repository.

- **Status:** Implemented
- **Owners:** Metis maintainers
- **Created:** 2026-08-30
- **Last updated:** 2026-08-30
- **Scope:** `renderer/`, migration from `sql/` and `sql/render/`, `sqlplan/`, compiler artifacts and physical compilation, `execution/`, warehouse Renderer packages, Backend and Driver extension contracts, built-in composition, community extension authoring, conformance tests, package dependency direction
- **Supersedes:** None
- **Related:** RFC-0035, RFC-0038, RFC-0054, RFC-0056, RFC-0058, RFC-0059, ADR-0005, ADR-0010

## 1. Summary

Metis will make physical warehouse support an explicit, package-oriented Go extension architecture.

The current implementation has the correct runtime authorities but concentrates several different responsibilities in broad packages:

```text
sql/
    Renderer contract and registry

sql/render/
    shared SQLPlan traversal
    DuckDB Renderer
    Doris Renderer
    ClickHouse Renderer

execution/
    physical compilation
    compiler artifact ownership
    project-source configuration
    DataSource configuration and registry
    Backend contract and registry
    DriverFactory and execution contracts
    Runner lifecycle and admission
    result normalization
    Doris DriverFactory, pool, Executor, and ResultStream
```

This layout is manageable with one executable Backend and three Renderers, but it is not an appropriate long-term contributor surface for Snowflake, BigQuery, PostgreSQL, ClickHouse execution, Databricks, or other warehouse integrations. Adding each integration as another file in `execution/` or `sql/render/` would mix vendor dependencies with core orchestration and make the stable extension boundary difficult to identify.

RFC-0060 reorganizes those responsibilities into explicit packages:

```text
compiler/                       physical query and output contracts
compiler/              SemanticPlan -> SQLPlan -> Renderer compilation
compiler/attribution/           attribution-specific compiled bundle composition

sqlplan/                        renderer-neutral typed physical SQL IR

renderer/                       public Renderer contract and sole registry
renderer/sql/                   rendered query types and shared SQL rendering
renderer/duckdb/                concrete DuckDB Renderer
renderer/doris/                 concrete Doris Renderer
renderer/clickhouse/            concrete ClickHouse Renderer
renderer/builtin/               explicit built-in compile-only composition

execution/datasource/           DataSource instance config, policy, SecretRef, registry
execution/driver/               public execution driver SPI
execution/backend/              Backend definition and sole type-level registry
execution/backend/doris/        concrete Doris execution integration
execution/runner/               Metis-owned compiled-query running, lifecycle, admission, normalization

app/bootstrap/                  project/deployment decoding and validation composition
cmd/metis/                      executable selection of built-in Backends
```

The product architecture remains unchanged:

```text
Agent intent
    -> Semantic Core
    -> SQLPlan
    -> one selected Renderer
    -> CompiledQuery
    -> optional Runner
    -> one Backend-selected Driver
    -> warehouse
```

This RFC changes package ownership and the contributor API. It does not introduce another semantic engine, another physical IR, another Renderer registry, dynamic Go plugins, arbitrary SQL execution, or caller-selected runtime placement.

## 2. Motivation

### 2.1 Package names should expose architectural responsibility

The top-level `sql/` package currently contains only the Renderer interface, Renderer registry, and SQL dialect normalization. The name `sql` is broader and less precise than its actual responsibility. Renaming this boundary to `renderer/` makes the primary extension point visible to a contributor reading the repository for the first time.

`sqlplan/` remains separate because it owns the typed physical SQL IR. A Renderer consumes SQLPlan; it does not own or replace SQLPlan.

### 2.2 Concrete warehouse implementations need isolated packages

A Snowflake implementation will contain different dependencies, configuration, authentication, connection lifecycle, and physical SQL syntax from a BigQuery or PostgreSQL implementation. Those dependencies must not accumulate in a single core package.

The desired contributor experience is:

```text
add renderer/snowflake
add execution/backend/snowflake, when execution is supported
register the integration explicitly at the composition root
run shared contract and real-engine conformance
```

The contributor must not edit Runner internals, semantic resolution, query planning, or transport handlers merely to add a warehouse.

### 2.3 Compile-only and executable support are distinct

A physical Renderer can be useful before a production Driver exists. Metis must continue to support compile-only dialects without endpoints, credentials, connections, or execution authority.

Therefore Renderer implementations and execution Backends remain distinct packages:

```text
renderer/snowflake
    -> compile-only physical SQL support

execution/backend/snowflake
    -> optional production execution support
    -> binds the exact Snowflake Renderer instance to a DriverFactory
```

Grouping Renderer and Driver in one indivisible package would force compile-only users to link vendor execution SDKs and would blur the difference between “can generate SQL” and “can execute against this database family.”

### 2.4 Community extensions need reusable infrastructure without hidden authority

The current concrete Renderers share an unexported default SQLPlan traversal in `sql/render`. Moving each Renderer into its own package would make that private implementation unavailable to external modules.

Community authors need an optional supported toolkit for the mechanically shared parts of typed SQL rendering. That toolkit must not become:

- a public ANSI execution target;
- a default Renderer selected by Metis;
- a second SQLPlan or compatibility AST;
- a source of guessed dialect behavior;
- a replacement for warehouse-specific conformance.

### 2.5 Go extension mechanisms should be explicit and portable

Go's runtime `plugin` mechanism has platform, build-version, dependency-version, and operational limitations. Package-global `init()` registration also hides composition, complicates tests, and creates process-global mutable state.

Metis extensions will use ordinary Go interfaces, constructors, and explicit registry composition. A deployment that needs an external Backend builds a binary that imports and registers it.

## 3. Goals and non-goals

RFC-0060 MUST:

- make Renderer the clearly named physical compilation extension point;
- place each concrete warehouse Renderer in its own package;
- preserve one selected Renderer identity across expression evidence, capabilities, and rendering;
- preserve SQLPlan as the sole typed physical SQL structure;
- define a minimal public Driver SPI for external warehouse implementations;
- prevent Driver implementations from owning Metis runtime policy or routing;
- isolate vendor dependencies from core runtime packages;
- keep RendererRegistry, BackendRegistry, and DataSourceRegistry as distinct authorities;
- provide explicit built-in composition without global registration side effects;
- define locally testable Renderer and Driver behavioral requirements for
  third-party implementations without committing to a shared test-helper API;
- preserve deterministic SQL and current conformance fingerprints during pure moves;
- allow compile-only Renderer support without an executable Backend;
- allow a third-party Go module to implement a warehouse without importing Metis internal packages.

RFC-0060 does NOT:

- define a dynamic binary plugin ABI;
- load arbitrary `.so` plugins at runtime;
- discover Backends from configuration strings or filesystem scanning;
- add a second dialect registry;
- infer a Renderer from `DataSource.type` outside BackendRegistry;
- merge DriverFactory and Renderer into one interchangeable interface;
- make warehouse SDK configuration part of Ossie semantic models;
- add connection details to BackendRegistry;
- expose raw SQL execution;
- change semantic resolution or planning behavior;
- generalize SQLPlan into a non-SQL physical IR;
- require every compile-only Renderer to have an execution Driver.

## 4. Architectural invariants

Implementation of this RFC is constrained by the following invariants.

### 4.1 One Renderer authority

One compilation selects exactly one Renderer instance. The same instance supplies:

```text
SQLDialect()
ExpressionDialect()
Capabilities()
Render(SQLPlan)
```

Package reorganization MUST NOT introduce a second lookup or a separate capabilities registry.

### 4.2 Backend binds rendering and execution once

Runtime placement remains:

```text
Project Registration
    -> DataSource
    -> DataSource.type
    -> BackendRegistry[type]
    -> Backend{
           Renderer,
           DriverFactory,
       }
```

The exact Renderer stored in the resolved Backend is passed to Semantic Core compilation. Runner does not look it up again.

### 4.3 DataSource and Backend remain different authorities

`execution/datasource` owns concrete deployment instances, policies, and SecretRefs. `execution/backend` owns database-family implementations. Neither package may absorb the other.

### 4.4 Drivers execute only completed compiler artifacts

A Driver receives a completed `artifact.CompiledQuery`. It cannot access SemanticManifest, SemanticGraph, QueryResolver, SemanticQuerySpec, MetricEvaluationPlan, SemanticPlan, SQLPlan, Renderer, or Project routing.

### 4.5 Vendor packages do not change core semantics

A new warehouse package may implement physical rendering and execution. It may not add semantic resolution rules, model fields, Agent reasoning, or transport behavior.

### 4.6 No hidden registration

Renderer and Backend registration is explicit. Concrete packages MUST NOT register themselves through `init()` or mutate a global registry.

### 4.7 SPI stability is evidence-gated

Accepting the architecture did not itself declare its Go extension packages
stable. The staged migration, concrete Renderer and Doris Backend/Driver
implementations, dependency guards, and conformance evidence now support the
separate stabilization decision in ADR-0011: `renderer.Renderer`, the
DataSource, Driver, and Backend contracts are stable. Shared rendering helpers
in `renderer/sql` remain experimental pending broader independent use; the
query value types retain their existing compatibility contract.

## 5. Terminology

RFC-0060 uses these terms precisely:

| Term | Meaning |
| --- | --- |
| Warehouse | A database or analytical service family such as Doris, Snowflake, BigQuery, ClickHouse, or PostgreSQL |
| SQLDialect | Canonical physical SQL language identity returned by a Renderer |
| Renderer | Immutable/concurrency-safe implementation that supplies dialect evidence, capabilities, and SQLPlan rendering |
| DriverFactory | Type-level execution extension that validates vendor config and constructs one process-scoped DataSourceRuntime |
| DataSourceRuntime | Process-scoped live resource such as a connection pool or SDK client |
| Executor | Request-scoped lease that executes one completed compiler artifact |
| Backend | One coherent database-family binding of an exact Renderer and DriverFactory; its SQLDialect is derived from that Renderer |

Warehouse and SQLDialect often correspond, but core code MUST NOT assume they are universally one-to-one. A Backend declares the relationship through its exact Renderer and does not store a second dialect field.

## 6. Target package topology

### 6.1 Physical compiler packages

```text
physical/
    sql.go
    sql_parameters.go

compiler/
    compiler.go

compiler/artifact/
    compiled_query.go
    output.go

compiler/attribution/
    compilation.go

sqlplan/
    plan.go
    validate.go
    clone.go
    explain.go
```

As refined by RFC-0062, `artifact.CompiledQuery` is the canonical atomic
compiler/runtime handoff:

```go
type CompiledQuery struct {
    PhysicalQuery sql.SQLRenderResult
    OutputSchema OutputSchema
}
```

The former `execution/compiler.go` was split by responsibility rather than
mechanically moved as one file. RFC-0062 subsequently moved low-level rendered
SQL values to `renderer/sql`, completed artifact contracts to `compiler/artifact`,
and generic one-plan physical compilation orchestration to root `compiler`.
This final graph is acyclic because planner conversion imports only
`compiler/artifact`, never root `compiler`.

`MetricAttributionCompiledQuery`, `MetricAttributionCompilation`, and `CompileMetricAttributionBundle*` are attribution-specific bundle composition, not generic physical compilation. They move to the focused `compiler/attribution` package, which may compose `compiler`; they MUST NOT be placed in the generic pipeline merely because they currently share `execution/compiler.go`. Future feature-specific compilation workflows follow the same rule instead of turning `compiler` into another broad orchestration package.

Artifact construction and ownership helpers move with the compiler artifact where possible. Runtime keeps only the defensive snapshot required at its trust boundary.

### 6.2 Renderer packages

```text
renderer/
    doc.go
    renderer.go
    registry.go
    dialect.go

renderer/sql/
    doc.go
    behavior.go
    render.go
    expression.go
    lowering.go

renderer/duckdb/
renderer/doris/
renderer/clickhouse/

renderer/builtin/
    registry.go
```

The root `renderer` package owns only contracts and registry behavior. It MUST NOT import concrete warehouse Renderer packages.

Concrete Renderer packages may import:

- `compiler` contracts;
- `sqlplan`;
- `renderer/sql`;
- domain-neutral utilities;
- their own focused dependencies.

They MUST NOT import execution runtime, DataSource configuration, application services, or transports.

### 6.3 Execution packages

```text
execution/datasource/
    datasource.go
    policy.go
    registry.go
    secret_ref.go
    decode.go

execution/driver/
    doc.go
    factory.go
    runtime.go
    executor.go
    result_stream.go
    secrets.go

execution/backend/
    backend.go
    registry.go
    validation.go

execution/backend/doris/
    backend.go
    config.go
    factory.go
    runtime.go
    executor.go
    result_stream.go

execution/runner/
    runtime.go
    datasource_entry.go
    admission.go
    lifecycle.go
    result.go
    observation.go

execution/runner/internal/artifactcopy/
execution/runner/internal/resultnorm/
```

The `execution/` path becomes an organizational namespace rather than one broad package. New code imports the narrow package it needs.

### 6.4 Bootstrap and executable composition

Project semantic-source configuration currently in `execution/config.go` moves to `app/bootstrap`, because it is deployment/bootstrap packaging rather than physical execution behavior.

The production executable owns the list of linked executable Backends:

```go
func defaultBackends() (*backend.Registry, error) {
    return backend.NewRegistry(
        dorisbackend.New(),
    )
}
```

Compile-only Renderer composition is separate:

```go
func NewRegistry() (*renderer.Registry, error) {
    return renderer.NewRegistry(
        duckdb.New(),
        doris.New(),
        clickhouse.New(),
    )
}
```

This separation preserves the rule that installing a Renderer does not grant execution capability.

## 7. Renderer contract

The public Renderer interface retains the current authority:

```go
package renderer

type Renderer interface {
    SQLDialect() sql.SQLDialect
    ExpressionDialect() string
    Capabilities() renderer.Capabilities
    Render(*sqlplan.Plan) (sql.SQLRenderResult, error)
}
```

Renderer implementations MUST be immutable or safe for concurrent calls after construction. Request-specific state must be local to `Render` or an owned clone. A Renderer MUST treat the caller's SQLPlan as immutable.

### 7.1 Dialect ownership

Each concrete package owns its canonical dialect constant:

```go
package bigquery

const Dialect sql.SQLDialect = "BIGQUERY"

type Renderer struct{}

func New() *Renderer { return &Renderer{} }
```

Dialect normalization remains centralized in the root Renderer registry. Concrete implementations return canonical identities and do not normalize caller input.

### 7.2 Sole registry

`renderer.Registry` is the only SQLDialect-to-Renderer registry:

```go
func NewRegistry(renderers ...Renderer) (*Registry, error)
func (r *Registry) Register(Renderer) error
func (r *Registry) Resolve(sql.SQLDialect) (Renderer, error)
func (r *Registry) Dialects() []sql.SQLDialect
func (r *Registry) Freeze()
```

Construction returns errors rather than panicking on duplicate or invalid community registrations. A registry is build/validate/freeze state and supports concurrent reads after freeze. Runtime Backend resolution still bypasses this registry and uses the exact Backend Renderer.

There is no independent DialectRegistry or CapabilitiesRegistry.

## 8. SQL rendering toolkit

### 8.1 Purpose

`renderer/sql` extracts the mechanically shared typed SQLPlan traversal currently private to `sql/render`. It lets a warehouse implementation answer only its physical syntax decisions while reusing deterministic block traversal, parameter accounting, and validation.

The toolkit is optional. A Renderer may implement the public Renderer interface directly. `renderer.Renderer` is the surface intended to become the stable public rendering SPI after the evidence gates in section 4.7; the rendering helpers in `renderer/sql` begin as explicitly experimental public APIs.

### 8.2 Required dialect decisions

The toolkit MUST require a concrete implementation to answer every known divergent behavior. Conceptually:

```go
type Behavior interface {
    QuoteIdentifier(string) string
    QuoteSource(string) (string, error)
    NotEqualOperator() string
    RenderExpression(sqlplan.Expr) (string, error)
    LowerPlan(*sqlplan.Plan) (*sqlplan.Plan, error)
    RenderLimit(int) string
    CTEBehavior() Behavior
    StatementSuffix(*sqlplan.Plan) string
}

func Render(
    plan *sqlplan.Plan,
    behavior Behavior,
) (string, []sql.QueryParameter, error)
```

The exact API may be refined during implementation, but it must preserve these properties:

- no selectable `sql.Renderer` exists;
- missing warehouse decisions fail at build time or explicit validation;
- known divergent behavior has no silent default;
- SQLPlan remains the only shared physical IR;
- the toolkit does not accept SemanticPlan or semantic model state;
- a toolkit render is deterministic and does not mutate SQLPlan;
- target-specific lowering returns an owned cloned plan.

### 8.3 Public versus internal helpers

Only helpers required by external Renderer authors are public. Test fixtures, formatting internals, private traversal state, and implementation-only aliases remain unexported inside `renderer/sql`.

Its first implementation SHOULD expose the smallest useful API and avoid exporting the current file structure mechanically, but its exported surface is not yet a stable compatibility commitment. Package documentation MUST label it experimental. The `Behavior` shape shown above is conceptual and may change while qualifying real warehouse needs such as BigQuery `QUALIFY`, Snowflake `VARIANT`, and Databricks lateral/explode lowering. Metis may stabilize the shared rendering helpers only after at least one additional warehouse implementation outside the current three Renderers has validated the abstraction. Stabilizing the toolkit requires an explicit review; accepting this RFC does not stabilize it.

## 9. Driver SPI

The public execution SPI is deliberately narrower than the current `execution.DriverFactory` signature.

### 9.1 Factory

```go
package driver

type Factory interface {
    DataSourceType() datasource.Type
    ValidateConfig(map[string]any) error
    OpenDataSource(context.Context, OpenRequest) (Runtime, error)
}

type OpenRequest struct {
    Config  map[string]any
    Secrets Secrets
}
```

A Factory does not receive the full DataSource value. In particular, it cannot own or reinterpret:

- `max_concurrency`;
- `max_queue`;
- `queue_timeout`;
- query timeout/row/byte policy;
- DataSource routing identity;
- Project identity.

Runner owns those values and passes only Driver-owned configuration plus resolved secret access.

`OpenRequest` has one normative representation:

```text
Config
    -> core-owned deep copy
    -> SecretRef envelopes remain unchanged
    -> resolved plaintext never appears

Secrets
    -> resolved values keyed by the exact SecretRefs present in Config
    -> lookup only; no map/iteration/string formatting surface
```

Core MUST validate every SecretRef reachable from `Config` and resolve all of them before invoking `OpenDataSource`. If any reference cannot be validated or resolved, Core fails closed and does not call the Driver. `Secrets` contains only references present in that Config snapshot; it MUST NOT contain ambient or unrelated deployment secrets. `Config` and its nested maps/slices are owned by Core, and neither `ValidateConfig` nor `OpenDataSource` may mutate them. Resolved plaintext MUST NOT be written back into Config, retained in errors, logged, or exposed through formatting.

Conceptually, secret access remains reference-keyed:

```go
type Secrets interface {
    Value(datasource.SecretRef) (string, bool)
}
```

### 9.2 Process and request lifecycles

```go
type Runtime interface {
    Acquire(context.Context) (Executor, error)
    Close(context.Context) error
}

type Executor interface {
    Execute(
        context.Context,
        *artifact.CompiledQuery,
    ) (ResultStream, error)

    Close() error
}

type ResultStream interface {
    Next(context.Context) ([]any, error)
    Close() error
}
```

The RFC-0059 lifecycle remains normative:

```text
Factory
    -> one process-scoped Runtime per configured DataSource
    -> zero or more request-scoped Executors
    -> zero or more request-scoped ResultStreams
```

Closing an Executor cannot close the shared DataSource pool/client. Closing Runtime owns process-level destruction. `Runtime.Close(ctx)` MUST honor context cancellation/deadline or otherwise guarantee bounded cleanup semantics; an implementation that ignores `ctx` and may block indefinitely is non-conforming. Core Runtime also bounds how long it waits for Driver cleanup, but that protection does not relax the Driver contract.

### 9.3 Configuration decoding

Vendor packages own typed config structs and strict decoding. Secret-bearing fields decode as `datasource.SecretRef`, not strings:

```go
type SnowflakeConfig struct {
    Account   string               `json:"account"`
    Warehouse string               `json:"warehouse"`
    Database  string               `json:"database"`
    User      string               `json:"user"`
    Password  datasource.SecretRef `json:"password"`
}

password, ok := req.Secrets.Value(cfg.Password)
```

Metis may provide a small generic strict-decoding helper, but the canonical DataSource model remains vendor-neutral `config`. Vendor fields do not enter Ossie or core DataSourcePolicy.

Sensitive fields must use SecretRef envelopes and remain redacted by core validation and Driver error boundaries. Drivers MUST use the decoded SecretRef to retrieve its value from `Secrets`; individual warehouse packages MUST NOT invent field-name lookup or plaintext-substitution conventions.

## 10. Backend composition

`execution/backend.Backend` remains one coherent type-level binding:

```go
type Backend struct {
    Type          datasource.Type
    Renderer      renderer.Renderer
    DriverFactory driver.Factory
}

func (b Backend) SQLDialect() sql.SQLDialect {
    return b.Renderer.SQLDialect()
}
```

Registration validates:

- non-empty normalized DataSource type;
- non-nil Renderer and DriverFactory;
- non-empty canonical dialect returned by Renderer;
- DriverFactory DataSource type equals Backend type;
- no duplicate DataSource type;
- concurrency-safe/frozen registry lifecycle.

A concrete built-in Backend constructor composes one exact Renderer instance:

```go
package doris

func New() backend.Backend {
    physicalRenderer := dorisrenderer.New()
    return backend.Backend{
        Type:          datasource.Type("doris"),
        Renderer:      physicalRenderer,
        DriverFactory: NewFactory(),
    }
}
```

The constructor MUST NOT perform global registration or establish a live connection.

## 11. Community extension model

A community integration can live in an independent Go module:

```text
github.com/example/metis-snowflake/
    renderer/
        renderer.go
        expression.go
        renderer_test.go
    driver/
        config.go
        factory.go
        runtime.go
        executor.go
        driver_test.go
    backend.go
```

It imports only public Metis packages:

```text
compiler
sqlplan
renderer
renderer/sql rendering helpers  optional, experimental
execution/datasource
execution/driver
execution/backend
```

It MUST NOT import any Metis `internal/` package.

A custom deployment explicitly composes it:

```go
backends, err := backend.NewRegistry(
    dorisbackend.New(),
    snowflake.New(),
)
```

Official inclusion in `cmd/metis` is a separate support decision. A community package can be usable without being linked into the default Metis binary.

## 12. Extension test responsibilities

### 12.1 Renderer tests

Renderer implementations keep tests at their owning package boundary. They
should cover:

- canonical non-empty SQLDialect;
- non-empty expression dialect;
- deterministic capabilities;
- deterministic SQL and parameter order;
- input SQLPlan immutability;
- concurrent rendering safety;
- explicit unsupported-plan failure;
- target expression selection and ANSI fallback fixtures;
- typed outer-join null preservation fixtures where applicable;
- no unregistered query output type.

Metis does not publish a generic Renderer test helper. The small shared
invariants do not justify another public compatibility surface, and focused
fixtures belong with the extension that owns its physical behavior. These tests
do not replace target-specific golden tests or real-engine conformance.

### 12.2 Driver tests

Driver implementations and `execution/runner` keep tests at their owning
package boundaries. They should cover:

- strict config rejection;
- Config deep-copy ownership and input immutability;
- SecretRef envelope preservation and exact-reference-only Secrets lookup;
- redacted secret/config failures;
- one process Runtime creation;
- concurrent `Acquire` safety;
- request Executor close does not close Runtime;
- Runtime close exactly once;
- `Runtime.Close(ctx)` deadline/cancellation or otherwise bounded cleanup;
- execution and ResultStream context cancellation;
- ResultStream cleanup;
- ResultStream errors propagate without being swallowed or converted into rows;
- no mutation of CompiledQuery;
- reusable resource behavior across sequential requests.

Tests that need a live warehouse remain in the real-engine harness. Metis does
not publish a Driver test helper; Driver test setup is intentionally local to
each implementation and is not part of the Driver SPI.

The Driver contract does not own complete-result atomicity. A ResultStream may successfully yield rows and then return an error. `execution/runner` tests MUST prove that Runtime discards every row accumulated before any stream, limit, normalization, cancellation, or cleanup failure and never exposes a partial `ResultSet` to the application layer.

## 13. Warehouse support levels

Repository structure must not imply production support merely because a package exists. Support is reported independently:

| Level | Evidence |
| --- | --- |
| Experimental Renderer | package compiles and has focused unit tests |
| Compile-only supported | registry, expression fallback, compiler conformance, documented fingerprints |
| Execution experimental | Driver boundary tests and bounded runtime integration exist |
| Production execution | real-engine compilation/execution, lifecycle, cancellation, load, and secret evidence pass |

`tests/benchmarks/CONFORMANCE.md` remains the support matrix authority.

## 14. Dependency rules

The intended dependency graph is:

```text
compiler contracts       sqlplan
        ^                    ^
        |                    |
        +------ renderer ----+
                   ^
                   |
       renderer/<warehouse>

execution/datasource
        ^
        |
execution/driver ------> compiler
        ^
        |
execution/backend -----> renderer
        ^
        |
execution/backend/<warehouse>
        ^
        |
execution/runner
        ^
        |
app/bootstrap + cmd/metis
```

Additional rules:

1. `renderer` does not import concrete Renderer packages.
2. `execution/backend` does not import concrete Backend packages.
3. `execution/datasource` does not import Backend or Driver packages.
4. `execution/driver` does not import runtime orchestration.
5. Concrete Driver packages do not import `execution/runner`.
6. `execution/runner` does not import concrete warehouse packages.
7. Application services depend on runtime contracts, not concrete Drivers.
8. Concrete warehouse SDK dependencies remain below their concrete Driver package.
9. No package introduces a second Renderer or Backend registry.

These rules SHOULD be enforced with dependency tests or an architecture import audit.

## 15. Go API and repository conventions

Public extension packages follow standard Go conventions:

- package names are short, lowercase, and singular where practical;
- constructors return errors for invalid external composition rather than panic;
- public interfaces are consumer-oriented and minimal;
- implementation details use concrete types until an interface is required;
- no package-global mutable registries;
- no `init()` registration;
- no dependency on filesystem layout at runtime;
- contexts are the first parameter of blocking operations;
- returned errors remain redacted and support stable core projection;
- live resources have explicit `Close` ownership;
- interfaces used concurrently document concurrency guarantees;
- package documentation includes a minimal external implementation example.

The core module does not promise source compatibility for unaccepted Draft interfaces. Once accepted and released as the community SPI, incompatible interface changes require an explicit migration RFC.

## 16. Alternatives

### 16.1 Keep `sql/` and add more files to `sql/render`

Rejected. It preserves a package that combines all concrete dialects and hides the repository's primary physical extension point.

### 16.2 Put Renderer and Driver together under `warehouse/<name>`

Rejected as the sole package boundary. It couples compile-only support to vendor execution dependencies and obscures the fact that Renderer availability does not authorize execution.

A community repository may organize its source this way internally, but the public Metis contracts remain separate.

### 16.3 One generic `warehouse.Adapter` interface

Rejected. Combining Renderer, DriverFactory, lifecycle, and registry behavior in one interface destroys the compile/runtime boundary and encourages runtime drivers to access semantic or SQLPlan state.

### 16.4 Independent Dialect and Capability registries

Rejected. They would create multiple authorities that can disagree with the selected Renderer.

### 16.5 Go runtime plugins

Rejected. Go plugins are not a portable or stable community extension ABI and complicate dependency/version compatibility and deployment security.

### 16.6 Self-registration through `init()`

Rejected. It introduces hidden global mutation, import-order coupling, and poor test isolation.

### 16.7 Keep the shared rendering baseline private

Rejected for community usability. External Renderer authors would need to copy statement traversal and parameter accounting, causing predictable forks and correctness drift.

### 16.8 Export the current default renderer directly

Rejected. The existing default implementation is not ANSI authority and cannot safely answer known divergent warehouse behavior. The public toolkit must require those decisions explicitly.

## 17. Rollout and migration

Implementation should use behavior-preserving, independently reviewable PRs.

### PR 1: Contract activation

- accept RFC-0060;
- update `AGENTS.md` package responsibilities and extension workflow;
- add dependency-direction tests before moving packages;
- document transition inventory and forbid new files in old broad packages.

No production behavior changes.

### PR 2: Compiler ownership cleanup

- move `CompiledQuery` out of execution (subsequently refined by RFC-0062 to
  `compiler/artifact`);
- split `execution/compiler.go` by responsibility rather than moving it whole;
- move only generic one-plan physical compilation orchestration to `compiler`;
- move metric-attribution bundle contracts and composition to focused `compiler/attribution`;
- move project semantic-source configuration from `execution/config.go` to `app/bootstrap`;
- migrate callers directly without compatibility wrappers;
- preserve compiled artifacts and fingerprints.

### PR 3: Renderer root migration

- create `renderer` contract and registry packages;
- move `sql.Renderer` and `sql.RendererRegistry` directly;
- update all imports and current rendering specification;
- preserve exactly one lookup and one Renderer identity;
- do not move concrete Renderer implementations yet if doing so would mix structural and behavioral changes.

### PR 4: SQL toolkit extraction

- extract the shared typed SQLPlan traversal into `renderer/sql`;
- define the minimal experimental public Behavior helper contract;
- migrate current Renderers without SQL output changes;
- add toolkit and Renderer contract tests;
- prove no selectable default/ANSI Renderer exists;
- label the package experimental and make no stable-API promise yet.

### PR 5: Concrete Renderer packages

- move DuckDB, Doris, and ClickHouse into separate packages;
- add explicit constructors and dialect constants;
- create `renderer/builtin` composition;
- keep conformance fingerprints stable.

### PR 6: Execution contracts

- create `execution/datasource`, `execution/driver`, and `execution/backend`;
- remove stored `Backend.Dialect`; derive SQLDialect only from the exact Renderer;
- narrow Factory OpenRequest so Drivers cannot see runtime policy and specify the SecretRef-preserving Config/Secrets contract;
- migrate registries directly;
- add Driver close-deadline, stream propagation, cleanup, and dependency tests at their owning Driver/Runtime boundaries;
- do not publish a shared Driver test package; keep Driver fixtures local to
  their implementation;
- preserve RFC-0059 lifecycle behavior.

### PR 7: Query Runner extraction

- move admission, lifecycle, resource entries, execution, artifact snapshots, and result normalization into `execution/runner`;
- keep normalization and artifact-copy implementation internal;
- keep complete-result atomicity in Runtime and test that every failure discards accumulated rows;
- preserve public errors, REST/MCP behavior, cancellation, and load limits.

### PR 8: Doris Backend extraction and composition

- move Doris DriverFactory/Runtime/Executor to `execution/backend/doris`;
- make the executable composition root register it explicitly;
- prove compile-only startup does not link or initialize live resources unnecessarily;
- run real Doris execution and lifecycle conformance.

### PR 9: Extension API stabilization

- publish a complete Renderer and Backend authoring guide;
- audit the complete public type closure exposed by Renderer and Driver
  signatures before declaring the SPI stable;
- retain extension-specific test fixtures at their owning implementation
  boundaries instead of publishing shared test-helper packages;
- use built-in Renderer and Doris Backend/Driver evidence, package dependency
  guards, runtime tests, and real-engine conformance for the stabilization
  decision;
- retain the rendering helpers in `renderer/sql` as experimental pending broader independent use.

No phase may introduce temporary duplicate Renderer or Backend authorities. Moves are direct migrations, not long-lived aliases or compatibility registries.

## 18. Test and acceptance criteria

RFC-0060 cannot become `Implemented` until all of the following are true.

### 18.1 Package ownership

- no production Go files remain in the broad `sql/` or `sql/render/` packages;
- `sqlplan/` remains the sole typed physical SQL IR;
- the broad root `execution` package no longer mixes compiler, bootstrap, runtime, and vendor code;
- project semantic-source config is outside execution;
- concrete vendor dependencies are isolated in vendor packages.

### 18.2 Renderer authority

- exactly one Renderer registry exists;
- one compile resolves Renderer exactly once;
- runtime uses the exact Backend Renderer identity;
- Backend stores no independent SQLDialect field and derives it only from that Renderer;
- no DialectRegistry or CapabilitiesRegistry exists;
- renderer packages are safe for concurrent use;
- SQLPlan input remains immutable.

### 18.3 Behavioral non-regression

- DuckDB, Doris, and ClickHouse SQL conformance fingerprints remain unchanged unless a separately documented correctness fix is approved;
- target-specific and ANSI fallback selection remains unchanged;
- `make ossie-conformance` passes;
- real-engine suites for existing supported Renderers pass.

### 18.4 Driver/runtime authority

- Drivers cannot access DataSource runtime policy through their public OpenRequest;
- Config is deep-copied and retains SecretRef envelopes without plaintext substitution;
- Secrets exposes only resolved references present in Config;
- Drivers cannot mutate Config and `Runtime.Close(ctx)` is deadline-aware or otherwise bounded;
- BackendRegistry still binds one coherent Renderer and DriverFactory;
- Runner imports no concrete warehouse package;
- stream errors propagate through the Driver while Runtime never returns a partial ResultSet;
- existing RFC-0059 concurrency, queue, lifecycle, cancellation, secret, and load tests pass;
- Doris repeated execution reuses one process-scoped resource.

### 18.5 Community usability

- an independent Go module can implement a Renderer, Driver, and Backend using
  only the documented stable extension contracts and their transitive type
  closure;
- no repository-maintained fake community extension or shared test-helper
  package is required to establish that boundary;
- concrete Renderer and Doris Backend/Driver tests cover deterministic
  rendering, immutable SQLPlan input, SecretRef lookup, and bounded Runner
  integration at their owning boundaries;
- no `init()` or process-global registry is required;
- package documentation is sufficient to construct and register an extension;
- adding an extension requires no semantic resolver/planner or transport changes.

### 18.6 Architecture audit

Automated tests or CI checks prove the dependency rules in section 14. At minimum they reject:

- core registry importing concrete warehouse packages;
- concrete Driver importing runtime internals;
- Renderer importing execution packages;
- DataSource package importing Backend/Driver implementations;
- application transports importing concrete warehouse packages.

### 18.7 Core checks

Every migration PR passes:

```bash
go mod tidy
git diff --exit-code -- go.mod go.sum
go test ./...
go vet ./...
make ossie-conformance
```

Affected packages also pass race tests, and renderer migration PRs run the real-engine impact gate.

## 19. Documentation updates

Implementation must update:

- `AGENTS.md` package responsibilities and SQL dialect extension workflow;
- `docs/README.md` code-to-document map;
- `docs/specs/sql/dialect-rendering.md` to name `renderer/` and warehouse packages;
- `docs/design/operations/runtime-bootstrap.md` for new Backend/Driver packages;
- `docs/specs/operations/runtime-bootstrap.md` for the narrowed Driver SPI;
- [Core runtime contract](../../specs/operations/runtime-bootstrap.md) and RFC-0059 cross-references where package names changed;
- `tests/benchmarks/CONFORMANCE.md` support and evidence rules;
- package-level `doc.go` documentation;
- a new warehouse Renderer/Backend authoring guide.

The authoring guide must include:

- compile-only Renderer example;
- executable Backend example;
- strict vendor config and SecretRef handling;
- explicit registry composition;
- contract test invocation;
- real-engine evidence expectations;
- support-level terminology;
- rules forbidding semantic, routing, credential, and transport ownership in extensions.

## 20. Acceptance decision

RFC-0060 may move from Draft to Accepted when maintainers agree on:

1. renaming the physical extension root from `sql` to `renderer`;
2. retaining `sqlplan` as the typed physical SQL IR;
3. separate Renderer and execution Backend packages per warehouse;
4. the experimental public versus internal boundary of `renderer/sql`, without declaring its helper API stable;
5. the narrowed Driver `OpenRequest` that excludes runtime policy and preserves SecretRefs while exposing resolved values only through reference lookup;
6. explicit compile-time composition rather than dynamic plugins or `init()`;
7. the staged direct-migration sequence without duplicate authorities;
8. the local extension-test obligations for community extensions;
9. SQLDialect derives only from the exact Backend Renderer;
10. Runtime, rather than Driver, owns complete-result atomicity and every Driver Runtime provides bounded close behavior;
11. acceptance does not itself stabilize the Go SPI; stabilization remains gated on contract and external-extension evidence.

The RFC may move to Implemented only after the package migration, current documentation updates, conformance preservation, external extension proof, and dependency audits are complete.

## 21. Durable architecture

The resulting extension model is:

```text
                         Semantic Core
                              |
                              v
                           SQLPlan
                              |
                              v
                   renderer/<warehouse>
                              |
                              v
                      artifact.CompiledQuery
                              |
                 compile-only+optional execution
                              |
                              v
                     Runner core
                              |
                              v
                execution/backend/<warehouse>
                     /                  \
                    v                    v
          exact Renderer          DriverFactory
                                      |
                                      v
                              DataSourceRuntime
                                      |
                                      v
                                  Executor
                                      |
                                      v
                                  warehouse
```

The durable rule is:

> **Warehouse packages supply physical rendering and optional execution implementations through narrow public contracts; Metis core retains semantic truth, placement, lifecycle, admission, safety, and the single Renderer/Backend authorities.**
