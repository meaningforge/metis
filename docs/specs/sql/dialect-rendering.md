# Physical SQL Dialect Rendering Specification

This document is normative for the physical SQL extension boundary implemented
by `renderer/` and `sqlplan/`.

RFC-0060 PRs 3–5 moved the Renderer contract, registry, shared traversal, and
concrete built-ins to `renderer/`. DuckDB, Doris, and ClickHouse live in their
own `renderer/<warehouse>/` packages; `renderer/builtin` exposes their official
instances for explicit composition. The migration must preserve this single
Renderer authority.

## Boundary

Renderer is the sole physical SQL compilation extension point:

```text
SQLDialect
    -> Renderer registry (one lookup)
    -> selected Renderer
    -> QueryResolver using selected Renderer evidence
    -> SemanticPlan
    -> planner/conversion.BuildSQLPlan
    -> sqlplan.Plan
    -> same Renderer.Render
    -> sql.SqlStatement
```

A SQL dialect identifies the requested physical SQL language. Its selected
Renderer supplies expression compatibility, capabilities, and final rendering.
Metis has one semantic compiler; adding a warehouse MUST NOT add a semantic
engine or alternate SemanticPlan materializer.

`planner/conversion` may inspect the selected Renderer's expression dialect and
capabilities while producing `sqlplan.Plan`, but it MUST NOT perform registry
lookup, import a concrete Renderer, or call `Render`. Callers import this owner
directly; root `planner` exposes no conversion facade.

## Registry contract

The required Renderer boundary is conceptually:

```go
type Renderer interface {
    SQLDialect() sql.SQLDialect
    ExpressionDialect() string
    Capabilities() renderer.Capabilities
    Render(*sqlplan.Plan) (sql.SqlStatement, error)
}
```

The Renderer contract and `renderer.Registry` are the only SQLDialect-to-
Renderer registry; no parallel registry may be introduced. Concrete
implementations live in `renderer/<warehouse>/`. Composition explicitly calls
`renderer.NewRegistry(builtin.Renderers()...)`; `renderer/builtin` does not
provide a second registry implementation or global registration side effect.
Compile-only resolves its explicit SQLDialect exactly once. Runtime
receives the Renderer from `BackendRegistry[DataSource.type]` and performs no
independent dialect lookup.

The same selected Renderer identity/instance MUST flow through expression
selection, capability validation, and final `Render(SQLPlan)`. A downstream
lookup is forbidden even if it returns an implementation with the same
SQLDialect name.

Dialect names are case-insensitive at input boundaries and normalized to their
canonical uppercase identity for lookup and output. Unknown or empty dialects
MUST fail explicitly; Metis MUST NOT guess a dialect from installed renderers, Ossie
expressions, sources, or database naming.

Configuration and CLI adapters MUST perform this normalization exactly once.
Resolver, planner, extension, and renderer contracts consume the selected
Renderer and MUST NOT repair, re-resolve, or rewrite its identity.

## Ownership

A renderer owns physical SQL behavior, including:

- identifier and source quoting;
- functions, operators, casts, intervals, and time-grain lowering;
- physical join, predicate, grouping, ordering, window, and limit syntax;
- parameter placeholder conventions and binding order;
- target-specific lowering of concepts already represented in `sqlplan`.

A renderer MUST treat the caller-owned plan as immutable. SQLPlan contains no
Renderer, SQLDialect, engine, or target authority; opaque physical expressions
carry only source-dialect compatibility evidence. Renderer-only
lowering MAY clone the plan into private state or insert owned typed blocks,
but rendering the same plan twice MUST produce identical SQL and parameters
without changing its fingerprint.

`QueryInputCTE` and `QueryInputDerivedTable` are physical scope decisions
already owned by SQLPlan. A renderer MUST honor them directly; it MUST NOT
reconstruct scope from aliases or route through a shared compatibility AST.

A renderer MUST preserve the null semantics of typed outer joins. If a target
uses physical type defaults for unmatched `FULL OUTER JOIN` columns unless a
target setting is enabled, the renderer MUST lower the typed join so unmatched
columns remain `NULL`; downstream `COALESCE` and derived expressions must never
observe fabricated dates, empty strings, zeros, or infinities.

`RelationRef` selects exactly one ordinary source, query input or
`FilteredTableSource`. The latter owns typed column predicates and MUST be
rendered as a filtered relation input before any join, including outer joins.
Moving these constraints into the enclosing WHERE is forbidden. Values MUST
use physical parameters; no policy lookup or reinterpretation is permitted.
Unsupported relation forms MUST fail explicitly. The built-ins lower this
form through `renderer/sql` as a parameterized derived table.

Target-neutral searched `CASE`, null tests, and explicit parenthesized grouping
are typed SQLPlan expressions. Renderers MUST preserve their branch, null, and
grouping semantics directly. Semantic lowering MUST use these typed forms when
missing-population state must remain distinct from a present zero value; it MUST
NOT substitute dialect-specific conditional functions or opaque query templates.
`NullOnZeroDivideExpr` is the corresponding typed form for Metis-owned
workflows whose zero denominator is undefined; every Renderer lowers it to the
equivalent division by `NULLIF(denominator, 0)`. It does not rewrite authored
opaque expressions or guess a user's divide-by-zero policy.
`CastExpr` is likewise a closed typed expression. Its admitted
`DECIMAL(38,18)` form preserves evaluator-compatible exact numeric evidence at
the output boundary. Its `DECIMAL(20,12)` form bounds intermediate ratio
precision so that engines which promote `SUM(DECIMAL)` to maximum precision do
not discard required fractional scale during subsequent ratio arithmetic.
Renderers MUST render both as the equivalent target cast without changing the
represented precision or scale. Every ordinary Decimal metric root projection
uses `DECIMAL(38,18)` directly so `AVG`, division, and other engine-widening
expressions satisfy the compiled `OutputSchema` without reducing the available
integer range through `DECIMAL(20,12)` first.
The target form MUST preserve SQL NULL. A target whose ordinary cast rejects a
non-finite binary-float intermediate MAY use a null-returning exact cast; the
built-in ClickHouse Renderer uses `accurateCastOrNull` for this reason.
`renderer/sql.CastBehavior` is the optional specialization point; engines
whose standard `CAST` already satisfies the contract need no implementation.

A renderer MUST NOT own:

- metric, dimension, or relationship resolution;
- semantic dependency or root-dataset selection;
- project, DataSource, or runtime placement;
- credentials, connectivity, query execution, or result handling;
- Agent reasoning or natural-language interpretation.

When a physical feature cannot be represented, prefer adding a target-neutral
concept to `sqlplan` over inserting full-query string templates or
rediscovering semantic structure in a renderer.

## Expression selection

Ossie expression dialects provide physical implementations of one semantic
definition; they never select execution placement. The required selection order
is:

```text
selected Renderer expression dialect -> compatible ANSI_SQL expression -> explicit error
```

Selection occurs before rendering. A renderer MUST NOT reselect expressions or
change semantic dependencies.

## Compatibility

Adding or refactoring a dialect MUST preserve existing output unless the change
documents a correctness fix. A pure move, registry change, or additive dialect
extension must keep existing conformance fingerprints stable.

### DuckDB dialect

The `DUCKDB` target is the concrete SQL dialect validated against DuckDB. Its
current renderer preserves the previously proven SQL shape, including double-
quoted identifiers, `FETCH FIRST`, and `DATE_TRUNC('<GRAIN>', expression)`. The
shared SQLPlan toolkit is implementation infrastructure, not a public portable
or ANSI execution target.

Formal dialect support currently requires:

1. a `Renderer` implementation in the active concrete Renderer package
   (`renderer/<warehouse>/`);
2. explicit registration in the single Renderer registry;
3. expression dialect and capabilities supplied by that Renderer;
4. target-specific and ANSI-fallback expression-selection coverage;
5. renderer unit tests for physical syntax;
6. shared compiler conformance scenarios;
7. real-engine execution conformance before claiming production support;
8. updates to current specifications and
   [testing architecture](../testing/architecture.md).

Dialect-specific behavior belongs in the renderer and its tests. Shared
semantic scenarios must not be copied into warehouse-specific test suites.

Each new implementation belongs under `renderer/<warehouse>/`; optional
executable support belongs separately under
`execution/backend/<warehouse>/`. The rendering helpers in `renderer/sql` are optional and
experimental until separately stabilized. The package is not a default or ANSI Renderer.
The required package ownership, explicit composition, Driver boundary, and
support evidence are specified in [Renderer and Backend Extension
Authoring](extension-authoring.md).

## Experimental SQLPlan toolkit

`renderer/sql` provides optional deterministic traversal of `sqlplan.Plan`.
It receives an explicit `Behavior` from a concrete Renderer for every
warehouse-specific decision: quoting, source rendering, inequality syntax,
expression fallback, lowering, limit syntax, CTE behavior, and statement
suffixes. It owns no semantic input, registry, selected Renderer identity, or
execution capability.

The toolkit has no `Renderer` implementation and cannot be selected by dialect.
Concrete Renderers may use the helpers or render SQLPlan directly. The helper
API is experimental; a separate stabilization decision requires evidence from
an additional warehouse. `SQLDialect`, `QueryParameter`, and `SqlStatement` live in
the same package and retain their existing extension compatibility contract.
