# RFC-0012: Compiled Output Schema

> **Design history:** This RFC preserves its original lifecycle status and rationale.
> Implementation claims describe the recorded design, not a guarantee that every
> proposed API remains current. See [current documentation](../../README.md) for
> supported interfaces. Package paths have been updated for this repository.

- **Status:** Implemented
- **Owners:** Metis maintainers
- **Created:** 2026-08-14
- **Last updated:** 2026-08-14
- **Scope:** `compiler/`, `planner/`, `execution/`, `app/service/semantic/`, REST, MCP, `cmd/s2s/`
- **Supersedes:** None

## Summary

Every successful Agent-facing compilation returns a target-neutral output
schema alongside the execution resolution and physical query. The schema lists
the logical output columns in physical result order and records each column's
semantic name, role, declared Ossie datatype, and requested time grain when
present.

## Motivation

Before this change, an Agent received a physical SQL string but no stable
contract for interpreting the returned columns. It had to reconstruct column
order and types from its original request or parse target-specific SQL. That is
fragile for qualified dimensions, multiple metrics, custom calendars, and
future non-SQL physical query types.

The semantic plan already owns the resolved projection order and Ossie objects.
Returning that information makes the compiler result self-describing without
moving query execution or physical result handling into Metis.

## Design

`compiler.OutputSchema` is a public, target-neutral contract:

```go
type OutputColumn struct {
    Name     string
    Kind     OutputColumnKind // dimension or metric
    Datatype ossie.DataType   // omitted when Ossie does not declare one
    Grain    *query.TimeGrain // only for a grained dimension
}

type OutputSchema struct {
    Columns []OutputColumn
}
```

`planner/conversion.BuildOutputSchema` derives columns from `SemanticPlan.Projections`.
It must not parse rendered SQL or consult a dialect renderer. Projection order
is output order: requested dimensions first, followed by requested metrics, as
established by Resolver and Planner.

For a custom-calendar dimension, the datatype comes from the resolved bucket
field and the grain comes from the resolved custom-calendar grouping. For an
ordinary dimension or metric, the datatype is the declared Ossie datatype.
When Ossie omits a datatype, Metis preserves that absence rather than guessing
from SQL text or target behavior.

The datatype is logical semantic metadata. It is not a promise that every
database driver will expose an identically named physical wire type. Physical
result normalization remains the execution tool's responsibility.

`execution.CompiledQuery` exposes the schema as top-level `output_schema`
beside `execution` and `sql_render_result`. REST and MCP serialize the same shared
service value. The offline `s2s` compiler retains the same schema in its Go
result even though its default CLI presentation remains SQL text.

Nullability is deliberately excluded. Outer joins, offsets, and physical
lowering may introduce NULL independently of the declaration, and Metis does
not yet have a complete target-neutral nullability analysis.

## Alternatives

### Attach schema only to `compiler.SqlRenderResult`

Rejected because output semantics are not SQL-specific and should survive
future SemanticEngine implementations that produce another physical query
type.

### Infer columns by parsing rendered SQL

Rejected because renderers own physical syntax, not semantic identity. SQL
parsing would duplicate Planner state and make aliases or dialect formatting a
source of semantic truth.

### Require every model field and metric to declare a datatype

Rejected for this change because the current Ossie loader permits omitted
datatypes. The output contract reports the declared truth and does not invent a
type. Tightening model validation requires a separate specification change.

### Include endpoint, execution, or result metadata

Rejected because Metis compiles but does not execute. Output Schema describes
the shape of the compiled artifact, not database connectivity or fetched data.

## Rollout and migration

The JSON change is additive: existing `execution` and `sql_render_result` fields
are unchanged, while successful responses gain `output_schema`. Consumers may
adopt the field without changing query execution. A rollback removes only the
additive field and schema construction; SQL output remains unchanged.

## Test and acceptance criteria

- Every successful semantic compilation produces at least one output column.
- Output order matches `SemanticPlan.Projections` and rendered aliases.
- Dimensions and metrics have explicit, stable roles.
- Declared Ossie datatypes are preserved without target-specific rewriting.
- Built-in and custom time grains are retained.
- The canonical 74-scenario corpus produces the same logical schema for DuckDB,
  Doris, and ClickHouse compilation.
- REST and MCP expose the same schema through `app/service/semantic`.
- Existing SQL fingerprints and real-engine semantic results remain unchanged.

## Documentation updates

- Update `docs/design/semantic/pipeline.md` with the output-schema flow.
- Update `docs/specs/semantic/compilation-pipeline.md` with the normative
  schema contract.
- Update README and `AGENTS.md` Agent-facing compile descriptions.
