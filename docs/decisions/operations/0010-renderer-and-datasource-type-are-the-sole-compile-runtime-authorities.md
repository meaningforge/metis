# ADR-0010: Renderer and DataSource Type Are the Sole Compile and Runtime Authorities

> **Decision record:** Read alongside the [current documentation](../../README.md);
> superseded decisions are retained for their rationale.

**Status:** Accepted
**Date:** 2026-08-29
**Last reviewed:** 2026-08-29
**Supersedes:** ADR-0009 where it requires `ExecutionBinding` routes, a user-selected Driver, or independent compile/runtime target authority

## Context

The accepted Semantic Core direction removes `CompileTarget{Engine, Dialect}`
and the semantic-engine registry. The previous Execution Runtime decision still
routed `project + ExecutionBinding` to a DataSource and let a DataSource select
a Driver. Keeping both models would leave dialect and runtime placement with
multiple authorities.

A dialect name alone is also insufficient proof of single authority. If the
Resolver and physical compiler independently look up two Doris Renderers, they
can diverge in capabilities or expression compatibility while appearing to
agree by name.

## Decision

Compile-only requests explicitly select an `SQLDialect`. Metis resolves that
dialect to one Renderer exactly once per compilation. The same selected Renderer
identity/instance supplies:

```text
ExpressionDialect()
Capabilities()
Render(SQLPlan)
```

No downstream stage may perform a second Renderer lookup. `CompileTarget`,
semantic-engine selection, and target-bearing semantic/physical state are
removed after their consumers have migrated to the selected Renderer.

Runtime requests do not accept dialect, Renderer, Driver, or target overrides.
Deterministic project resolution is:

```text
explicit project
    > configured default_project
    > sole registered project
    > PROJECT_REQUIRED
```

The resolved root Project Registration may reference one DataSource instance.
`DataSource.type` is the sole runtime database-family authority. Two registries
have deliberately different cardinality and ownership:

```text
BackendRegistry[type]
    -> SQLDialect + Renderer + DriverFactory

DataSourceRegistry[name]
    -> DataSource{type, instance configuration, SecretRefs, policy}
```

BackendRegistry is established before DataSourceRegistry so type-level
implementation authority cannot be confused with instance-level configuration.
Execution begins strictly after `SqlStatement + OutputSchema`; Executor cannot
consult semantic or rendering state.

Secrets, limits, and cancellation fail closed. Public execution is not exposed
until RFC-0056 and is named `query_metrics`, never `run_query`.

## Migration rule

Migration is replacement-first:

```text
CompileTarget authority
        |
        v
establish SQLDialect + selected Renderer
        |
        v
migrate Resolver / expression evidence / SQL lowering
        |
        v
prove one-Renderer invariant
        |
        v
delete CompileTarget / Engine / target state
```

Existing `CompileTarget`, `ExecutionBinding`, and semantic-engine code is a
bounded transition inventory. It may only be migrated or removed; new features
must not depend on it. Acceptance of this ADR does not claim runtime execution
is already implemented and does not expose an Agent-facing execution tool.

## Consequences

- Expression compatibility and final SQL syntax cannot drift behind equal
  dialect labels.
- Compile-only remains independent of DataSources and credentials.
- Runtime database-family selection has one user-configurable source:
  `DataSource.type`.
- Backend extensions and concrete instances evolve independently.
- Project omission is convenient only when resolution is deterministic.
- Execution Runtime remains downstream of the canonical compiler artifact.
- The migration temporarily carries old code, but it cannot acquire new
  authority or compatibility guarantees.

## Alternatives considered

**Delete CompileTarget first.** Rejected because it creates an interval with no
authoritative dialect evidence for Resolver and SQL lowering.

**Look up Renderer independently in Resolver and compiler.** Rejected because
same-name implementations can still diverge and recreate dual authority.

**Combine BackendRegistry and DataSourceRegistry.** Rejected because database
family implementations and concrete database instances have different
lifecycles, cardinalities, and security concerns.

**Retain ExecutionBinding as a route alias.** Rejected because it adds a second
placement vocabulary between Project Registration and DataSource without a v1
federation requirement.

**Expose `run_query`.** Rejected because it suggests arbitrary SQL execution;
the intended public request is a governed semantic metric query.

## References

- [Core runtime contract](../../specs/operations/runtime-bootstrap.md)
- [RFC-0058: Semantic Core Layering](../../proposals/semantic/0058-semantic-core-layering-and-semantic-graph.md)
- [ADR-0009: Execution Runtime Is Optional](0009-execution-runtime-is-optional-driver-factory-based-and-orchestrated.md)
- [RFC-0056: Runtime Metric Query Execution](../../proposals/execution/0056-runtime-metric-query-execution.md)
