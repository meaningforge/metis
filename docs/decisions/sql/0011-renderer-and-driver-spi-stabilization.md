# ADR-0011: Renderer and Driver SPI Stabilization

> **Decision record:** Read alongside the [current documentation](../../README.md);
> superseded decisions are retained for their rationale.

**Status:** Accepted (current)
**Date:** 2026-08-30
**Updated:** 2026-09-01 by ADR-0014 and RFC-0065

## Context

RFC-0060 separated physical rendering, DataSource configuration, Drivers,
Backends, and Runner lifecycle into explicit Go packages. Existing concrete
DuckDB, Doris, and ClickHouse Renderers, the Doris Backend/Driver, package
dependency guards, runtime tests, and real-engine conformance provide the
implementation evidence for that separation. A fake in-repository community
extension would duplicate those contracts without adding production evidence.

## Decision

The following are stable public extension contracts:

- `renderer.Renderer` and `renderer.Registry`;
- `execution/datasource` DataSource, policy, SecretRef, and registry contracts;
- `execution/driver` Factory, OpenRequest, Secrets, Runtime, Executor, and
  ResultStream contracts;
- `execution/backend` Backend and BackendRegistry contracts.

The stable SPI also includes its transitive public type closure:

- `renderer.Capabilities`, `sql.SQLDialect`, `sql.SQLRenderResult`, and
  `sql.QueryParameter` exposed by `renderer.Renderer`;
- `compiler/artifact.CompiledQuery`, `OutputSchema`, `OutputColumn`, and
  `OutputColumnKind`, plus the reachable `renderer/sql` values exposed to Drivers;
- `sqlplan.Plan`, `QueryBlockID`, `QueryBlock`, `QueryInput`, `QueryInputMode`,
  `RelationRef`, `TableSource`, `InputRef`, `Projection`, `Join`, `JoinKind`,
  `Predicate`, `Order`, `Expr`, `OpaqueExpr`, `ColumnRef`, `BinaryExpr`,
  `LogicalExpr`, `FunctionCallExpr`, `NullTestExpr`, `CaseWhen`, `CaseExpr`,
  `ParenthesizedExpr`, `TimeGrainExpr`, `CalendarShiftExpr`, `LatestValueExpr`,
  `EarliestValueExpr`, `WindowFrame`, `WindowExpr`, and `RowNumberExpr`; and
- `query.FilterOperator`, `SortDirection`, `TimeGrain`, and `ossie.DataType`
  where those types occur in the preceding public structures.

This does not freeze every exported helper in `compiler/artifact`, `renderer/sql`,
`sqlplan`, `query`, or `ossie`; it freezes the types and exported fields that
form these interface signatures and their structural values. RFC-0062 made the
pre-release package migration as a clean cut: root `compiler` exposes no aliases
for these lower-level contracts.

ADR-0014 makes one reviewed pre-release refinement to this closure:
DataSource and Driver Config use `map[string]string`; environment references
use exact `${ENV_NAME}` strings while vault references use
`secret://<provider>/<key>`. It supersedes the earlier arbitrary nested
Config representation only; every other authority, lifecycle, and transitive
type decision in this ADR remains current.

Stability means compatible changes preserve the documented responsibilities:
one selected Renderer instance, DataSource type-to-Backend binding, exact
SecretRef lookup, Driver isolation from policy/routing, bounded Runtime close,
and Runner-owned complete-result atomicity.

The rendering helpers in `renderer/sql` remain experimental. They are used by built-in Renderers, but
that evidence is insufficient to freeze the `Behavior` shape for independent
warehouse implementations. Helper use remains optional and carries no compatibility
promise. The query value types retain their existing SPI compatibility contract.

Metis publishes no shared Renderer or Driver test-fixture package. Generic
assertions are kept as local tests at each implementation boundary; this avoids
turning test scaffolding into a second extension API.

## Consequences

- A community extension can depend on the stable contracts without changing
  semantic, Runner, or transport implementation packages.
- New stable-API changes require normal compatibility review.
- A future SQL toolkit stabilization requires evidence from more independent
  physical targets and a separate decision.
- Extension-specific tests and real-engine evidence remain the owner's
  responsibility; package presence alone never claims execution support.

## References

- [RFC-0060](../../proposals/execution/0060-warehouse-renderer-and-execution-backend-extension-architecture.md)
- [RFC-0062](../../proposals/sql/0062-compiler-and-physical-query-package-responsibilities.md)
- [RFC-0065](../../proposals/execution/0065-flat-string-datasource-config.md)
- [ADR-0014](../operations/0014-flat-string-datasource-config.md)
- [Renderer and Backend Extension Authoring](../../specs/sql/extension-authoring.md)
- [Dialect Rendering](../../specs/sql/dialect-rendering.md)
