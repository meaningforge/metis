# RFC-0062: Compiler and Physical Query Package Responsibilities

> **Design history:** This RFC preserves its original lifecycle status and rationale.
> Implementation claims describe the recorded design, not a guarantee that every
> proposed API remains current. See [current documentation](../../README.md) for
> supported interfaces. Package paths have been updated for this repository.

- **Status:** Implemented
- **Owners:** Metis maintainers
- **Created:** 2026-08-30
- **Last updated:** 2026-08-31
- **Scope:** `compiler/`, `compiler/artifact/`, `renderer/sql/`, `renderer/`, `sqlplan/`, planner-to-physical conversion, execution Driver boundary, package dependency rules
- **Supersedes:** None
- **Related:** RFC-0038, RFC-0054, RFC-0058, RFC-0060, RFC-0061, ADR-0005, ADR-0010

## Summary

This RFC makes the compiler-side package layout reflect the responsibilities that
already exist in Metis's semantic-to-execution pipeline.

Before this migration, the root `compiler` package primarily owned low-level physical-query values
and compilation-result contracts, while the actual generic compilation
orchestration lives in `compiler/pipeline`. That naming is backwards:
`compiler` looks like the orchestrator but is imported as a vocabulary package
by Renderer and execution code.

The target ownership is:

```text
physical/                       rendered SQL query values
sqlplan/                        renderer-neutral structured physical SQL IR
renderer/                       SQLPlan -> rendered SQL query
compiler/artifact/              completed compilation result contracts
compiler/                       generic one-query compilation orchestration
compiler/attribution/           attribution-specific bundle orchestration
execution/driver/               execution of completed compilation artifacts
```

The target end-to-end flow is:

```text
SemanticPlan
    -> sqlplan.Plan
    -> renderer.Renderer
    -> sql.SQLQuery
    -> compiler/artifact.CompiledQuery
    -> execution/driver
```

`sqlplan.Plan` remains Metis's sole renderer-neutral structured physical SQL IR.
`sql.SQLQuery` is not another plan IR; it is the already-rendered SQL text,
SQL dialect identity, and parameters.

The root `compiler` package becomes the generic compilation orchestrator only
after its low-level value and artifact contracts have moved below it. This lets
`compiler/pipeline` disappear without creating import cycles or making Renderer
and Driver depend on orchestration.

## Motivation

The pre-migration package graph had a responsibility inversion:

```text
compiler                 sqlplan
    ^                        ^
    |                        |
    +-------- renderer ------+

compiler/pipeline -> planner + renderer + compiler
```

The root `compiler` package defined values such as `SQLDialect`,
`RendererCapabilities`, `QueryParameter`, `SQLQuery`, `OutputSchema`, and
`CompiledQuery`. `renderer.Renderer` therefore imports `compiler` even though
`compiler/pipeline` is the layer that actually orchestrates Renderer selection,
SQLPlan conversion, rendering, and final `CompiledQuery` construction.

This has four costs:

1. package names do not describe ownership;
2. Renderer and Driver public signatures transitively depend on a broad root
   `compiler` package;
3. execution depends on compiler vocabulary even though it only needs the
   immutable completed compilation artifact; and
4. moving `compiler/pipeline` directly into root `compiler` would create import
   cycles while planner conversion and Renderer still import root compiler
   contracts.

The source tree should state the architecture directly:

```text
planner owns semantic meaning and SQLPlan construction
sqlplan owns the renderer-neutral structured physical SQL IR
renderer owns SQL dialect rendering
physical owns rendered SQL query values
compiler/artifact owns the immutable completed compilation result
compiler owns compilation orchestration
execution owns running the completed result
```

## Goals and non-goals

### Goals

- Make root `compiler` the generic one-plan compilation orchestrator.
- Preserve two compiler entry paths:
  - compile-only callers resolve a Renderer from an explicit SQL dialect;
  - runtime or upstream callers pass an already-selected Renderer instance and
    never perform a second lookup.
- Remove the dependency from `renderer` to root `compiler`.
- Give Renderer-facing SQL values a neutral low-level owner.
- Give `CompiledQuery` and `OutputSchema` an artifact package that planner and
  execution can import without depending on orchestration.
- Preserve `sqlplan.Plan` as the only structured physical SQL planning IR.
- Preserve one exact selected Renderer instance from target resolution through
  semantic planning, SQLPlan conversion, and final rendering.
- Make `OutputSchema` value ownership distinct from semantic schema derivation.
- Preserve SQL bytes, parameters, output schema, execution behavior, and current
  conformance evidence during package-only migration.
- Keep the package graph acyclic and authority-directed.
- Complete the package migration as a clean cut: internal callers import the
  responsibility owner directly and root `compiler` exposes no compatibility
  aliases or forwarding facade.

### Non-goals

This RFC does not:

- change metric semantics, planning, optimization, attribution mathematics, or
  SQL conformance behavior;
- introduce a second physical plan, execution plan, or general engine IR;
- introduce a generalized non-SQL Renderer abstraction before one exists;
- move `sqlplan` under `compiler` or `renderer`;
- make `renderer/sql` responsible for SQL rendering;
- make `compiler` responsible for execution, DataSource routing, secrets, or
  connection lifecycle;
- add dynamic plugins, shared-object loading, or process-global registration;
- stabilize `renderer/sqlkit`; or
- stabilize the final Renderer/Driver SPI before RFC-0061 and this RFC have
  established the final package ownership and signatures.

## Design

### Package layout and responsibilities

The target layout is:

```text
physical/
    sql.go                     SQLDialect, QueryParameter, SQLQuery

sqlplan/
    ...                        renderer-neutral typed physical SQL planning IR

renderer/
    renderer.go                Renderer, Capabilities
    registry.go                Registry
    <warehouse>/               concrete SQL Renderers
    sqlkit/                    optional experimental rendering helper

compiler/
    compiler.go                generic compilation orchestration
    artifact/
        compiled_query.go      CompiledQuery
        output_schema.go       OutputSchema and OutputColumn
    attribution/               attribution-specific compiled bundle orchestration

execution/
    driver/                    executes compiler/artifact.CompiledQuery
    ...
```

The role of each package is normative.

| Package | Owns | Must not own |
| --- | --- | --- |
| `renderer/sql` | rendered SQL query values: `SQLDialect`, query parameters, `SQLQuery` | semantic meaning, SQLPlan nodes, Renderer registry, compilation orchestration, execution |
| `sqlplan` | renderer-neutral typed physical SQL IR, validation, cloning, explanation, fingerprints | semantic plan authority, Renderer lookup, rendered SQL artifacts, execution |
| `renderer` | SQL Renderer contract, Renderer capabilities, registry, and dialect-specific `sqlplan.Plan -> sql.SQLQuery` rendering | semantic resolution, planner orchestration, final `CompiledQuery`, DataSource routing, execution |
| `compiler/artifact` | immutable completed compilation output: `CompiledQuery`, `OutputSchema`, output columns, structural validation and clone rules | semantic schema derivation, planner orchestration, Renderer selection, execution policy |
| `compiler` | Renderer resolution for compile-only callers, generic one-plan compilation orchestration, invocation of planner conversion and rendering, final artifact composition | semantic meaning, SQL syntax implementation, runtime execution, credentials |
| `compiler/attribution` | attribution-specific orchestration over multiple already-defined semantic plans and the generic compiler | attribution mathematics, Renderer implementation, execution |
| `execution/driver` | request-scoped execution of an immutable compiled artifact through a Driver Runtime | compiler orchestration, semantic planning, Renderer selection |

### `renderer/sql`: rendered SQL query values

`renderer/sql` is deliberately below `renderer` and `compiler` in the import graph.
It contains only the values needed to describe an already-rendered SQL query.

The target shape is conceptually:

```go
package physical

type SQLDialect string

type QueryParameter struct {
    Name  string
    Value any
}

type SQLQuery struct {
    Dialect    SQLDialect
    SQL        string
    Parameters []QueryParameter
}
```

`renderer/sql` must not import `planner`, `sqlplan`, `renderer`, `compiler`, or any
execution package.

This RFC deliberately uses `SQLDialect`, not a generic `Dialect`. The value
identifies one SQL language and must not become an accidental authority for a
future non-SQL physical query kind.

This RFC also deliberately does not introduce `physical.QueryType` or a generic
`physical.Query` interface. Metis currently has one physical renderer boundary:
`sqlplan.Plan -> SQL`. Generalizing the rendered output before a second real
physical query kind exists would create an unused abstraction. A future non-SQL
physical query form requires a separate design decision.

The package name does not mean that `sql.SQLQuery` is a physical plan.
Metis has exactly one structured physical SQL planning IR: `sqlplan.Plan`.

### `renderer`: SQL rendering authority

`renderer.Renderer` is explicitly a SQL Renderer. Its input is `sqlplan.Plan`
and its output is `sql.SQLQuery`.

Renderer capabilities belong to `renderer`, because they are facts supplied by
the selected Renderer rather than facts owned by compiler orchestration.
Capabilities describe actual Renderer behavior, not the already-fixed fact that
its output is SQL.

The target contract is conceptually:

```go
package renderer

type Capabilities struct {
    // SQL-rendering capabilities only; no Outputs []QueryType pseudo-generalization.
}

type Renderer interface {
    SQLDialect() sql.SQLDialect
    ExpressionDialect() string
    Capabilities() Capabilities
    Render(*sqlplan.Plan) (sql.SQLQuery, error)
}
```

`Capabilities` must not contain `Outputs []physical.QueryType`. Once the
Renderer contract itself guarantees a `sql.SQLQuery` result, an
`Outputs: [SQL]` capability is tautological and cannot represent another output
kind. New capability fields require concrete SQL-rendering behavior to justify
them.

Returning one `sql.SQLQuery` keeps SQL text, SQL dialect, and parameters as
one owned value instead of a parallel `(string, []Parameter)` tuple. The
compiler validates that the returned query is structurally valid and that its
`SQLDialect` matches the exact selected Renderer.

The same selected Renderer instance remains the sole source of expression
compatibility, capabilities, and final rendering. No compiler or planner stage
may reconstruct Renderer evidence from copied strings or perform a second
registry lookup.

`renderer/sqlkit` remains optional and experimental. This RFC neither
stabilizes it nor makes it part of the required Renderer implementation path.

### `compiler/artifact`: completed compilation result

`CompiledQuery` is the immutable boundary between Semantic Core compilation and
Execution Runtime. `OutputSchema` is the target-neutral semantic output contract
paired with the rendered SQL query.

Those values must be importable by planner conversion and execution without
importing the compiler orchestrator. They therefore move to
`compiler/artifact`.

The target shape is conceptually:

```go
package artifact

type OutputSchema struct {
    Columns []OutputColumn
}

type CompiledQuery struct {
    PhysicalQuery sql.SQLQuery
    OutputSchema  OutputSchema
}
```

`compiler/artifact` may import `renderer/sql`. It must not import root `compiler`,
`planner`, `renderer`, or `execution`.

Artifact ownership is intentionally split from semantic schema derivation:

- `compiler/artifact` owns the `OutputSchema` and `OutputColumn` value types,
  defensive copying, structural validation, and artifact immutability;
- `planner/conversion` owns the semantic rules that derive an `OutputSchema`
  from a validated `SemanticPlan`, including column order, metric/dimension
  roles, datatypes, and grain/output-contract evidence;
- root `compiler` only receives the already-derived schema and composes it with
  the rendered `sql.SQLQuery`; it must not re-derive, reorder, reinterpret,
  or infer schema from rendered SQL.

Moving these types must not weaken the existing atomic
`CompiledQuery { PhysicalQuery + OutputSchema }` contract.

### Root `compiler`: orchestration owner

After low-level contracts have moved out, the current generic orchestration in
`compiler/pipeline` moves into root `compiler`.

The compiler must preserve two distinct entry paths because Renderer evidence is
needed before final physical compilation. In particular, `ExpressionDialect()`
and Renderer capabilities may participate in semantic resolution/planning, so a
runtime or application flow may select the Renderer before a `SemanticPlan`
exists.

The target API is conceptually:

```go
package compiler

type RendererResolver interface {
    Resolve(sql.SQLDialect) (renderer.Renderer, error)
}

type Compiler struct {
    renderers RendererResolver
}

// Compile-only entry: select from an explicit SQL dialect, then compile.
func (c *Compiler) Compile(
    ctx context.Context,
    plan *planner.SemanticPlan,
    dialect sql.SQLDialect,
) (*artifact.CompiledQuery, error)

// Upstream selection entry: expose the one registry lookup for callers that
// must carry Renderer evidence through semantic planning before compilation.
func (c *Compiler) ResolveRenderer(
    dialect sql.SQLDialect,
) (renderer.Renderer, error)

// Selected-Renderer entry: use the exact supplied Renderer and never look it up again.
func (c *Compiler) CompileWithRenderer(
    ctx context.Context,
    plan *planner.SemanticPlan,
    selected renderer.Renderer,
) (*artifact.CompiledQuery, error)
```

A package-level `CompileWithRenderer` helper is allowed if implementation structure
benefits from it, but the two logical entry paths are normative:

```text
compile-only caller
    explicit SQLDialect
        -> ResolveRenderer once
        -> semantic/physical compilation

runtime or upstream caller
    Backend/upstream selected Renderer
        -> semantic planning with that exact instance
        -> CompileWithRenderer with that same instance
```

The `Compile` method may be implemented as `ResolveRenderer` followed by
`CompileWithRenderer`. `CompileWithRenderer` must not normalize a dialect and re-resolve
the Renderer. Runtime execution through a Backend uses the Backend's exact
Renderer instance.

Conceptually `CompileWithRenderer` performs:

```text
exact selected Renderer
        |
        v
planner conversion: SemanticPlan -> sqlplan.Plan
        |
        +----> artifact.OutputSchema
        |
        v
same Renderer.Render(sqlplan.Plan)
        |
        v
sql.SQLQuery
        |
        v
artifact.NewCompiledQuery(physicalQuery, outputSchema)
```

The compiler does not reinterpret semantic meaning and does not contain SQL
dialect syntax. Its responsibility is sequencing and enforcing the contracts
between already-owned layers.

`compiler/pipeline` becomes unnecessary once this migration completes and must
be removed rather than retained as a second orchestration authority.

### Planner interaction and RFC-0061

RFC-0061 proposes `planner/conversion` as the owner of
`SemanticPlan -> sqlplan.Plan` and semantic `OutputSchema` derivation. This RFC
keeps that responsibility unchanged but refines the compiler-side dependencies.

If both RFCs are accepted, the target dependency is:

```text
planner/conversion -> sqlplan
planner/conversion -> renderer          selected Renderer evidence only
planner/conversion -> compiler/artifact OutputSchema value type only
```

`planner/conversion` must not import root `compiler`.

The responsibility split is normative:

```text
planner/conversion
    SemanticPlan semantics
        -> sqlplan.Plan
        -> artifact.OutputSchema

compiler/artifact
    owns OutputSchema representation and structural invariants

compiler
    combines the already-derived OutputSchema with Renderer output
```

The RFC-0061 diagrams and package tables that currently name
`compiler/pipeline` or root `compiler.OutputSchema` must be updated during the
implementation of whichever RFC lands second.

### Execution interaction

Drivers execute an immutable compilation result; they do not depend on compiler
orchestration.

The target Driver boundary is:

```go
Execute(
    context.Context,
    *artifact.CompiledQuery,
) (ResultStream, error)
```

Execution packages may import `compiler/artifact` and `renderer/sql`. They must not
import root `compiler`, planner packages, or Renderer packages to execute an
already-compiled query.

This keeps the long-term layering explicit:

```text
Semantic Core compilation
        |
        v
compiler/artifact.CompiledQuery
        |
        v
Execution Runtime
```

### Dependency direction

The required authority and import direction is:

```text
                         +----------------+
                         |    sqlplan     |
                         +-------^--------+
                                 |
                                 |
+----------+             +-------+--------+
| physical |<------------|    renderer    |
+----^-----+             +-------^--------+
     |                           |
     |                           |
     |                   +-------+--------+
     +-------------------|    compiler    |
     |                   +-------^--------+
     |                           |
+----+----------------+          |
| compiler/artifact  |<----------+
+----^----------------+
     ^
     |
     +------ planner/conversion
     |
     +------ execution/driver
```

The diagram is simplified; root `compiler` also invokes planner conversion.
The following rules are normative:

- `renderer/sql` imports none of `sqlplan`, `renderer`, `compiler`, planner, or
  execution.
- `compiler/artifact` may import `renderer/sql` but must not import root `compiler`,
  planner, renderer, or execution.
- `sqlplan` must not import renderer, compiler, or planner packages.
- `renderer` may import `renderer/sql` and `sqlplan`; it must not import root
  `compiler`, planner, or execution.
- root `compiler` may import planner, `renderer`, `renderer/sql`, and
  `compiler/artifact`.
- planner conversion may import `compiler/artifact`, `sqlplan`, and the
  Renderer contract where selected Renderer evidence is required; it must not
  import root `compiler`.
- execution Drivers may import `compiler/artifact` and `renderer/sql`; they must not
  import root `compiler`, planner, or renderer.
- `compiler/attribution` may import root `compiler`, `compiler/artifact`, and
  planner attribution contracts, but must not implement semantic attribution
  mathematics or execution.

No compatibility alias or forwarding facade is permitted in the final package
graph.

### SPI compatibility follows package stabilization

This RFC defines package ownership and dependency direction before freezing the
final warehouse extension SPI. RFC-0061 and RFC-0062 implementation may change
field placement and Renderer/Driver method signatures as required to reach the
accepted package boundaries.

Only after those migrations are complete should Metis make a separate SPI
stabilization decision based on the final signatures and their full transitive
type surface.

The expected eventual Renderer compatibility surface is approximately:

```text
Renderer SPI
    renderer.Renderer
    renderer.Capabilities
    renderer.Registry
    sql.SQLDialect
    sql.QueryParameter
    sql.SQLQuery
    sqlplan.Plan and public SQLPlan node/value types reachable from Plan
```

Root `compiler` is not part of the Renderer SPI.

The expected eventual Driver compatibility surface includes:

```text
Driver SPI
    execution/driver contracts
    execution/datasource contracts referenced by Driver
    compiler/artifact.CompiledQuery
    compiler/artifact.OutputSchema and reachable output types
    sql.SQLQuery and reachable parameter/dialect types
```

Root `compiler` is not part of the Driver SPI.

These lists are guidance for the later stabilization decision, not a stability
promise made before the implementation and corresponding ADR update.

### Clean-cut package migration

Metis has not published these Go package contracts. The migration therefore
does not retain root aliases, forwarding functions, or a second orchestration
facade. Repository callers import `renderer/sql`, `compiler/artifact`, `renderer`,
or root `compiler` according to the owner named by this RFC. Commits may stage
the move, but the merged state contains no compatibility layer.

### Naming rules

The following terms have distinct meanings and must not be collapsed:

```text
SemanticPlan               source-aware logical semantic DAG
sqlplan.Plan / SQLPlan     renderer-neutral structured physical SQL planning IR
sql.SQLQuery          rendered SQL text + SQLDialect + parameters
artifact.CompiledQuery     rendered query + semantic OutputSchema
compiler                   orchestration that constructs CompiledQuery
renderer                   dialect-specific SQLPlan -> SQLQuery conversion
execution                  running CompiledQuery against a DataSource
```

Do not introduce another catch-all package named `lowering`, `query`, `ir`, or
`runtime` to combine these responsibilities.

## Alternatives

### Keep `compiler` as a contract package and retain `compiler/pipeline`

This is mechanically smallest but preserves the responsibility inversion:
`compiler` would continue to look like an orchestrator while the actual
orchestrator remains one level below it.

Rejected.

### Move `compiler/pipeline` directly into root `compiler`

Without first extracting `renderer/sql` and `compiler/artifact`, root `compiler`
would need to import planner and renderer while renderer and planner conversion
still import root `compiler`. That either creates cycles or forces awkward
indirection.

Rejected as a migration order, though root `compiler` is the final
orchestration destination after extraction.

### Keep a generic `physical.Query` and `RendererCapabilities.Outputs`

There is currently no second physical Renderer output kind. If Renderer always
returns `sql.SQLQuery`, `Outputs: [SQL]` is a tautology rather than a real
capability boundary.

Rejected. Generalize only when a concrete non-SQL physical query form requires
it.

### Use generic `physical.Dialect`

A generic `Dialect` suggests one identity can govern future non-SQL physical
query kinds. The current value identifies SQL syntax only.

Rejected in favor of `sql.SQLDialect`.

### Put every shared type in `renderer`

`sql.SQLQuery` and `CompiledQuery` are consumed outside rendering.
Execution should not import Renderer authority merely to execute an already
compiled artifact.

Rejected.

### Put `CompiledQuery` in `renderer/sql`

`CompiledQuery` combines rendered physical SQL with the semantic output schema.
It is therefore the completed compilation artifact, not merely a rendered
physical query value.

Rejected in favor of `compiler/artifact`.

### Create a general `ir/` hierarchy

Metis already has named IR authorities (`SemanticPlan`, `sqlplan.Plan`). A broad
`ir/` namespace would make ownership less obvious and imply a generalized IR
framework that this change does not need.

Rejected.

## Rollout and migration

Implementation should be staged so every commit keeps the dependency graph
acyclic and testable.

### Phase 1: introduce low-level owners

1. Add `renderer/sql` with `SQLDialect`, `QueryParameter`, and `SQLQuery`.
2. Add `compiler/artifact` with `OutputSchema`, `OutputColumn`, and
   `CompiledQuery` contracts.
3. Migrate repository callers directly to the new owners; add no root aliases.

No behavior or SQL output changes are allowed in this phase.

### Phase 2: remove Renderer dependency on root compiler

1. Move Renderer capability ownership to `renderer.Capabilities`.
2. Remove output-kind pseudo-generalization from Renderer capabilities.
3. Change Renderer signatures and implementations to use `sql.SQLDialect`,
   `sql.QueryParameter`, and `sql.SQLQuery`.
4. Return one owned `sql.SQLQuery` from `Renderer.Render`.
5. Update Renderer registry, built-ins, SQL toolkit integration, tests, and
   architecture guards.

### Phase 3: move planner and execution artifact dependencies

1. Planner output-schema derivation returns `compiler/artifact.OutputSchema`
   while keeping all semantic derivation rules in planner conversion.
2. Execution Driver/Runner consume `compiler/artifact.CompiledQuery`.
3. New code stops importing root `compiler` solely for value contracts.

### Phase 4: make root compiler the orchestrator

1. Move generic one-plan orchestration from `compiler/pipeline` into root
   `compiler`.
2. Preserve and test both compiler entry paths:
   - explicit `SQLDialect -> ResolveRenderer -> CompileWithRenderer`;
   - already-selected `Renderer -> CompileWithRenderer` with no second lookup.
3. Update attribution orchestration and application composition to use root
   `compiler`.
4. Delete `compiler/pipeline` after all imports move.
5. Add CI guards preventing Renderer, planner conversion, and execution Driver
   packages from importing root `compiler` for low-level contracts.

### Phase 5: SPI hardening

1. Verify root `compiler` contains no low-level aliases or forwarding facade.
2. Update RFC-0061 references to the final compiler/artifact ownership.
3. After RFC-0061 and RFC-0062 implementation are complete, update the
   explicit SPI stabilization decision against the final Renderer/Driver
   signatures and transitive public types.

Rollback at every phase is a normal commit revert. The migration must not leave
a permanent second Renderer interface, second compiler orchestrator, or second
physical-query authority.

## Test and acceptance criteria

This RFC cannot become `Implemented` until all of the following are true:

- root `compiler` owns generic one-plan compilation orchestration and
  `compiler/pipeline` no longer exists;
- root `compiler` exposes no compatibility aliases for `renderer/sql`,
  `compiler/artifact`, or `renderer` contracts;
- root `compiler` exposes both the explicit SQL-dialect compile path and an
  already-selected Renderer compile path;
- compile-only resolution performs at most one Renderer lookup;
- `CompileWithRenderer` uses the exact supplied Renderer instance and performs no
  registry lookup;
- application/runtime flows can select a Renderer before semantic planning and
  carry that same instance through final rendering;
- `renderer` has no production import of root `compiler`;
- Renderer public signatures use only `renderer`, `renderer/sql`, and `sqlplan`
  extension-facing types;
- Renderer is explicitly SQL-specific and has no tautological output-kind
  capability field;
- the rendered dialect type is named `sql.SQLDialect`;
- `compiler/artifact` owns `CompiledQuery`, `OutputSchema`, and their structural
  validation/copy contracts without importing root `compiler`, planner,
  renderer, or execution;
- planner conversion owns semantic `OutputSchema` derivation, including column
  ordering, roles, datatype, and output grain evidence;
- root `compiler` does not re-derive or reinterpret `OutputSchema`;
- planner conversion does not import root `compiler`;
- execution Driver does not import root `compiler` or renderer packages;
- `sqlplan` remains independent from planner, renderer, and compiler authority;
- architecture CI enforces the dependency rules in this RFC;
- all existing Renderer deterministic/concurrency/immutability tests pass;
- all compile conformance fingerprints preserve SQL bytes and parameters unless
  a separately reviewed correctness change is explicitly documented;
- generic compile, attribution compile, Runner, E2E, and applicable real-engine
  execution tests pass;
- no process-global registry, compatibility Renderer path, or second physical
  IR is introduced; and
- current docs explicitly distinguish `SQLPlan`, `sql.SQLQuery`,
  `CompiledQuery`, compiler orchestration, Renderer authority, and execution.

## Documentation updates

Implementation must update at least:

- `docs/design/semantic/pipeline.md`;
- `docs/specs/semantic/compilation-pipeline.md`;
- `docs/specs/sql/dialect-rendering.md`;
- RFC-0061 diagrams/tables where they name `compiler/pipeline` or root
  `compiler.OutputSchema`;
- RFC-0060 and the extension-authoring specification where Renderer or Driver
  signatures and future stability are described;
- `docs/specs/operations/runtime-bootstrap.md` for the Driver artifact import
  boundary;
- package `doc.go` files for `renderer/sql`, `compiler`, `compiler/artifact`,
  `renderer`, and `sqlplan`; and
- architecture/testing documentation for the enforced import graph.
