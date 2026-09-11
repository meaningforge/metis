# Renderer and Backend Extension Authoring

This specification defines how a Go module adds a physical SQL Renderer and,
optionally, an executable Backend. Read it with
[dialect rendering](dialect-rendering.md) and the operational
[runtime bootstrap contract](../operations/runtime-bootstrap.md).

`renderer`, `execution/datasource`, `execution/driver`, and
`execution/backend` are stable extension contracts. The rendering helpers in `renderer/sql` are
explicitly experimental; they are optional and not a compatibility commitment.
The stabilization decision is recorded in
[ADR-0011](../../decisions/sql/0011-renderer-and-driver-spi-stabilization.md),
with the Config representation refined by
[ADR-0014](../../decisions/operations/0014-flat-string-datasource-config.md).

An extension is an ordinary Go package. Metis does not load shared objects,
discover packages from configuration, or register integrations through `init()`.

```text
semantic compilation
    -> renderer/<target>        optional compile-only support
    -> compiler/artifact.CompiledQuery
    -> execution/backend/<target>  optional executable support
```

## SPI compatibility closure

Stability covers the public types reachable from the extension interfaces, not
only the package names. The following exported types are part of the extension
compatibility commitment:

| Boundary | Stable transitive types |
| --- | --- |
| `renderer.Renderer` | `renderer.Capabilities`, `sql.SQLDialect`, `sql.SqlRenderResult`, `sql.QueryParameter`, and `sqlplan.Plan` |
| `driver.Executor` | `artifact.CompiledQuery`, `artifact.OutputSchema`, `artifact.OutputColumn`, `artifact.OutputColumnKind`, `sql.SqlRenderResult`, `sql.SQLDialect`, and `sql.QueryParameter` |
| `sqlplan.Plan` consumed by a Renderer | `QueryBlockID`, `QueryBlock`, `QueryInput`, `QueryInputMode`, `RelationRef`, `TableSource`, `FilteredTableSource`, `InputRef`, `Projection`, `Join`, `JoinKind`, `Predicate`, `Order`, `Expr`, `OpaqueExpr`, `ColumnRef`, `BinaryExpr`, `LogicalExpr`, `FunctionCallExpr`, `NullTestExpr`, `CaseWhen`, `CaseExpr`, `ParenthesizedExpr`, `TimeGrainExpr`, `CalendarShiftExpr`, `LatestValueExpr`, `EarliestValueExpr`, `WindowFrame`, `WindowExpr`, and `RowNumberExpr` |
| SQLPlan field types | `query.FilterOperator`, `query.SortDirection`, `query.TimeGrain`, and `ossie.DataType` where they appear in the listed contracts |

This is a compatibility closure, not a promise that every exported helper in
`compiler/artifact`, `renderer/sql`, `sqlplan`, `query`, or `ossie` is stable extension API. Changes to
the listed types, their exported fields, or the behavior required by the
Renderer and Driver contracts require extension compatibility review.

[Data access policy contract](../operations/data-access-policy.md) adds `RelationRef.FilteredSource` / `FilteredTableSource` to this
closure. Extensions must render its typed predicates inside the relation input
before joins, or explicitly reject the form. Treating it as an unrestricted
table is forbidden. Existing unrestricted SQLPlan forms are unchanged; the
built-in Renderers share filtered-source conformance and real-engine tests.

## Renderer

A Renderer belongs in its own package and implements the public
`renderer.Renderer` contract:

```go
type Renderer struct{}

func (Renderer) SQLDialect() sql.SQLDialect
func (Renderer) ExpressionDialect() string
func (Renderer) Capabilities() renderer.Capabilities
func (Renderer) Render(*sqlplan.Plan) (sql.SqlRenderResult, error)
```

The same Renderer instance selected by `renderer.Registry` supplies expression
compatibility, capabilities, and rendering. A Renderer MUST NOT look itself up
again by name, infer a target, consult deployment configuration, or own query
execution.

`renderer/sql.Render` is an optional experimental helper for typed `sqlplan.Plan`
traversal. It has no default behavior and no selectable dialect. An extension
may implement `Renderer.Render` directly instead. Its exported API is not a
stable compatibility promise.

Renderers MUST:

- accept only compatible target or `ANSI_SQL` opaque expressions;
- treat the caller's `SQLPlan` as immutable and render deterministically under
  concurrent calls;
- implement physical syntax, quoting, parameter conventions, and
  target-specific lowering locally;
- return an explicit error for unsupported physical behavior.

Renderers MUST NOT implement semantic resolution, metric planning, DataSource
routing, credentials, connections, transport handlers, or Agent reasoning.

## Executable Backend

Execution support is optional and is separate from compile-only rendering. A
Backend binds exactly one Renderer and one `driver.Factory` to a DataSource
type:

```go
func New() backend.Backend {
    return backend.Backend{
        Type:          datasource.Type("example"),
        Renderer:      renderer.New(),
        DriverFactory: NewDriverFactory(),
    }
}
```

`Backend.SQLDialect()` derives from this exact Renderer. A Backend MUST NOT
store a second dialect field. `BackendRegistry` owns type-level bindings;
`DataSourceRegistry` owns concrete endpoint, secret-reference, and policy
configuration. Neither registry replaces the other.

A DriverFactory receives only:

```go
type OpenRequest struct {
    Config  map[string]string
    Secrets driver.Secrets
}
```

`Config` is an owned flat string map. `${ENV_NAME}` identifies the `env`
provider and `secret://<provider>/<key>` identifies an injected vault provider.
These are locators, not general template syntax. Every reference string remains
intact in the per-open Driver snapshot, and `Secrets.Value(SecretRef)` is the
only way to obtain its resolved value. A Driver may use `driver.ConfigValue` to
resolve one field into a private local connection value without mutating Config.
Sensitive plaintext never appears in DataSource Config or Driver Config.
Drivers MUST validate their closed vendor configuration, reject unknown fields
and plaintext secret values, and MUST NOT mutate Config. DriverFactory MUST NOT
retain `OpenRequest` or its `Secrets` capability after open returns; it may copy
only the values required by its private SDK/client authentication state. Adding
a database's authentication modes belongs in that Backend's closed config
validation; it does not widen Core Config into an untyped profile object.

Driver Runtime owns one process-scoped client or pool. `Acquire` returns a
request-scoped Executor, and closing that Executor MUST NOT close the shared
resource. `Runtime.Close(ctx)` MUST honor cancellation/deadline or otherwise
have bounded cleanup semantics. A ResultStream may report an error after rows;
Runner, not the Driver, owns complete-result atomicity and discards partial
results on every failure.

`Acquire`, `Execute`, and `ResultStream.Next` MUST honor an already-cancelled
context and the deadline used for their blocking operation. A Runtime MUST
reject acquisition after `Close` begins. Driver-local tests own transport fault
injection; extensions MUST NOT add fault hooks, retry hints, raw vendor errors,
or test interfaces to the stable Driver SPI. Core performs no transparent
retry. A production Backend must run the shared resilience behavior through a
real engine and may supply only the engine-local long-running statement or
fault setup needed to trigger that behavior.

## Composition

A custom executable imports and composes its chosen extensions explicitly:

```go
renderers, err := renderer.NewRegistry(
    builtin.Renderers()...,
    example.NewRenderer(),
)

backends, err := backend.NewBackendRegistry(
    dorisbackend.New(),
    example.New(),
)
```

`cmd/metis` is the official binary's composition root for its linked
Backends. An external extension does not become part of the official binary
merely by compiling; it can be linked by a custom executable without changing
Metis semantic, Runner, or transport packages.

## Evidence and support level

Keep extension tests in the extension's own package. Metis intentionally does
not publish a generic Renderer or Driver test-fixture package: implementations
should test their own physical syntax, configuration, lifecycle, cancellation,
and cleanup behavior at their owning boundary.

Before claiming support, an extension MUST provide the evidence appropriate to
its level:

| Level | Required evidence |
| --- | --- |
| Experimental Renderer | focused Renderer unit tests and Registry composition |
| Compile-only supported | expression target/ANSI fallback, compiler conformance, and documented fingerprints |
| Execution experimental | Driver configuration, SecretRef, lifecycle, cancellation, and Runner integration tests |
| Production execution | real-engine compilation/execution, normalized results, FIFO admission, acquisition/execution/row-read cancellation, atomic limits, post-fault reuse, close rejection, and redaction evidence |

Update [testing architecture](../testing/architecture.md)
when claiming repository support. Do not represent a package's existence as
production execution support.

The official CGO-free composition provides production execution evidence for
ClickHouse and Doris, including governed `query_metrics` through each production
Backend. The explicit DuckDB build flavor provides the same execution level.
DuckDB's official Go client
requires CGO, so it is selected only with `CGO_ENABLED=1 -tags duckdb`.
Default CGO-free builds intentionally omit that Backend and must fail bootstrap
for `type: duckdb` rather than substituting the compile-only Renderer.
