# RFC-0070: Production Runner Real-Engine Conformance

> **Design history:** This RFC preserves its original lifecycle status and rationale.
> Implementation claims describe the recorded design, not a guarantee that every
> proposed API remains current. See [current documentation](../../README.md) for
> supported interfaces. Package paths have been updated for this repository.

- **Status:** Implemented
- **Owners:** Metis maintainers
- **Created:** 2026-09-02
- **Last updated:** 2026-09-02
- **Scope:** `tests/engine`, `execution/runner`, `planner/conversion`, SQL rendering and testing specifications
- **Supersedes:** None

## Summary

Real-engine semantic conformance executes every generated query through the
production `DataSource -> Backend -> execution/runner -> Driver` path. Test-only
connections remain responsible only for fixture lifecycle and low-level Driver
tests. Decimal root projections receive a direct closed `DECIMAL(38,18)` typed
cast boundary so the complete compiled artifact can satisfy its production
`OutputSchema` on engines that widen Decimal arithmetic to binary floating
point. The narrower `DECIMAL(20,12)` contract remains limited to ratio
intermediates.

## Motivation

The former harness executed generated SQL through engine-specific helpers.
Doris used `database/sql`, ClickHouse used its HTTP protocol, and DuckDB reused
the production Driver but bypassed Runner. Those paths duplicated result
normalization and could pass while production routing, Renderer identity,
parameter execution, limits, cleanup, or `OutputSchema` enforcement failed.

Moving the shared corpus onto Runner immediately exposed a hidden mismatch:
canonical physical fixtures used binary-float columns for Ossie Decimal fields,
and `AVG` or division could still return a binary float even after those source
columns were corrected. The production runtime is required to reject that
lossy representation.

## Design

The shared harness now requires `Prepare` and `OpenExecution`:

1. `Prepare` uses a test-owned writable connection for fixture DDL/DML.
2. `OpenExecution` creates a fixture-scoped production Runner from one concrete
   DataSource and production Backend.
3. The Runner resolves the DataSource and Backend once.
4. Compilation uses the exact Renderer from that resolved Backend.
5. `ExecuteResolved` receives the unchanged route and complete
   `CompiledQuery`.
6. Runner validates and normalizes the physical result against OutputSchema;
   the harness only converts that normalized result into the coarser canonical
   comparison representation.
7. The Runner closes before another fixture mutates shared table names.

Unordered numeric results use exact-rational tolerance edges followed by a
complete bipartite match, avoiding greedy row-order bias. Local Docker
launchers use unique names, Docker-assigned networks, and dynamic host ports so
parallel engines and concurrent CI jobs do not contend for fixed resources.

Canonical fixture numeric columns now carry their authored Ossie Decimal
datatype. Physical adapters may deliberately use either an exact decimal or a
binary-float source to prove that the compiled output boundary, rather than
test DDL, satisfies Runner. Ordinary Decimal root projections use a typed
`DECIMAL(38,18)` cast, preserving 20 integer digits while establishing the exact
output representation required by the runtime contract. The narrower
`DECIMAL(20,12)` cast remains limited to ratio intermediates.
Metis-owned undefined divisions use the typed `NullOnZeroDivideExpr`; authored
opaque expressions keep their explicit SQL semantics. Dialects may specialize
the closed cast through `sql.CastBehavior` without adding target switches to
the compiler.

The registered metric-scale contract is part of the shared harness and uses a
canonical fixture plus the same production Runner route. Backend-local raw SQL
executors no longer duplicate that semantic assertion.

Optimizer differential tests execute their optimized and unoptimized complete
artifacts through production Runner. Focused attribution projections that do
not originate as a standalone semantic compilation use a narrow test-only raw
query helper and a schema selected from the source compiler artifact; Runner
and the production Driver still own execution and normalization. AgentBench may
retain its arbitrary-SQL executor because discovering and repairing externally
authored SQL is its explicit subject. It does not count as production Backend
semantic execution evidence.

## Alternatives

- Keep direct engine executors and add more production smoke tests: rejected
  because the full shared corpus would still bypass production behavior.
- Make Runner accept binary floats for Decimal: rejected because it violates the
  lossless result contract.
- Let each engine adapter infer or replace OutputSchema: rejected because it
  creates a second semantic authority in test code.
- Route fixture DDL/DML through Runner: rejected because public execution is
  intentionally narrow and compiled-query-only.

## Rollout and migration

DuckDB is the default embedded gate. Doris and ClickHouse use the same harness
under the explicit real-engine matrix. New production Backends must provide
fixture setup separately and open the shared production execution adapter.

## Test and acceptance criteria

- The complete DuckDB shared corpus passes through Runner and the production
  DuckDB Driver.
- Doris and ClickHouse shared packages compile against their production Backend
  composition and execute the same path when their environments are present.
- Compilation and execution share one resolved Renderer identity.
- A Decimal result decoded as binary float fails Runner; compiled Decimal
  arithmetic produces an exact physical output instead.
- Fixture DDL/DML remains outside Runner.
- Backend-local conformance SQL executors and result parsers are removed;
  optimizer and focused raw-projection evidence use the shared Runner-backed
  test utility.
- SQL conformance fingerprints are regenerated deliberately.

## Documentation updates

- `docs/specs/testing/architecture.md`
- `docs/specs/semantic/compilation-pipeline.md`
- `docs/specs/sql/dialect-rendering.md`
- `tests/benchmarks/CONFORMANCE.md`
