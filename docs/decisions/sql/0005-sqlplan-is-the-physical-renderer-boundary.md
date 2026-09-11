# ADR-0005: SQLPlan Is the Physical Renderer Boundary

> **Decision record:** Read alongside the [current documentation](../../README.md);
> superseded decisions are retained for their rationale.

**Status:** Accepted (current)
**Date:** 2026-08-23
**Last reviewed:** 2026-08-23

ADR-0010 supersedes this decision only where it makes compile target state or a
semantic engine authoritative. SQLPlan remains the sole physical SQL IR and the
selected Renderer remains its only consumer.

## Context

Metis previously lowered `SemanticPlan` into a shared SQL syntax tree rooted at
one select. Query-block identity and dependencies were implicit in nested CTEs,
validation was distributed, and several physical rewrites still needed both
the syntax tree and semantic plan after the nominal lowering boundary.

That arrangement allowed semantic authority to cross into renderer-adjacent
code and made plan-wide ownership, reachability, cloning, validation,
fingerprinting, and explanation difficult to state. A compatibility adapter
was useful during migration, but retaining it would preserve two physical IRs
and two extension paths.

## Decision

`sqlplan.Plan` is Metis's sole physical SQL planning IR and the only input
accepted by a selected Renderer.

`planner/conversion.BuildSQLPlan` performs every transformation that requires
`SemanticPlan`, validates the complete plan, and returns owned state. A plan
has one explicit root, unique query-block IDs, topologically ordered input
edges, and typed physical expressions and relations. It does not own an
independent Renderer or target authority.

Renderers treat caller-owned plans as immutable,
and own only dialect syntax and renderer-private physical lowering. They may
clone SQLPlan into private state but may not consume `SemanticPlan`, infer
semantic dependencies, or route through another shared physical IR.

The former shared SQL AST package, builder, renderer aliases, compatibility
adapter, and semantic-aware post-plan mutation path are deleted. Repository
architecture tests guard against imports or calls reappearing in production or
test code.

## Consequences

- Semantic authority ends at `BuildSQLPlan`; one previously selected Renderer
  consumes the plan without another registry lookup.
- Query-block identity, dependency reachability, ordering, cloning, and
  validation have one typed contract independent of renderer implementation.
- Adding a dialect extends the registry and renderer visitor without modifying
  semantic planning or creating another physical-plan builder.
- Renderer-only transformations must preserve input immutability and rendering
  determinism.
- Rollback after cutover is a normal revert, not a permanent fallback or dual
  renderer interface.
- The migration preserved the frozen 96-by-3 SQL bytes and parameter evidence;
  future changes require the normal documented correctness-review process.

## Alternatives considered

**Rename the old syntax tree.** This would leave implicit dependencies and
distributed validation unchanged under a stronger name.

**Wrap the old tree permanently.** This would make SQLPlan an envelope rather
than the physical authority and keep adapters as a second extension surface.

**Keep semantic-aware rewrites in the native engine.** This would make the
producer boundary incomplete and allow engine orchestration to alter semantic
meaning after planning.

**Build target-specific physical plans.** This would duplicate semantic
lowering per dialect and violate the registry-driven renderer architecture.

## References

- RFC-0038, *Typed SQLPlan and Physical SQL Lowering*
  (`docs/proposals/sql/0038-typed-sql-plan.md`)
- [`../../specs/semantic/compilation-pipeline.md`](../../specs/semantic/compilation-pipeline.md)
- [`../../specs/sql/dialect-rendering.md`](../../specs/sql/dialect-rendering.md)
- [`../../../sqlplan/doc.go`](https://github.com/meaningforge/metis/blob/019b5cdafb7981aa4c2844a769995b4c2fc1171e/sqlplan/doc.go)
