# ADR-0018: Semantic Model Selects One Applied DataSource

**Status:** Accepted
**Date:** 2026-09-16

## Context

A Project may describe analytics held in several databases. Treating the whole
Project as one physical connection prevents that common deployment shape.
Letting each request choose a source would make callers a second placement
authority, expose infrastructure details to Agents, and permit the same
semantic query to compile differently from one call to the next.

Metis queries already resolve within one semantic model. That boundary can
select a connection without introducing a route map or allowing one compiled
query to span warehouses.

## Decision

A root Project Registration may apply several named DataSources with
`data_sources`. The existing singular `data_source` form represents a one-item
set. The forms are mutually exclusive, and every referenced name must exist in
the Deployment DataSource registry.

A semantic model may select one logical DataSource name with a typed Ossie
custom extension:

```json
{"kind":"data_source","name":"orders"}
```

The extension uses vendor `METIS` and is strict: it occurs at most once, has no
unknown fields, and contains a non-empty trimmed name. It contains no endpoint,
credential, database family, Driver, Renderer, or execution policy.

Placement is resolved while building each immutable semantic generation:

1. zero applied sources means compile-only;
2. one applied source is inferred for models without an extension;
3. several applied sources require every model to select one explicitly;
4. an explicit name not present in the applied set rejects activation.

All governed execution workflows resolve the model placement before selecting
`DataSource.type -> Backend -> Renderer + DriverFactory`. REST, MCP, and Agent
requests cannot override it. A complete semantic model and every query against
it use exactly one DataSource. Cross-source joins, federation, and request-time
source switching remain unsupported.

## Consequences

- One Project and Deployment can use several database connections.
- Semantic Releases stay portable because they carry logical names while each
  Deployment supplies the concrete connection records and secrets.
- A sole-source Project remains concise and needs no model annotations.
- Multi-source activation fails early if placement is incomplete or stale.
- Independent queries can target different sources; one query cannot combine
  them.

## Alternatives considered

**One DataSource per Project.** Simple, but forces users to split one analytical
product into artificial Projects and duplicates Agents, Releases, and access
configuration.

**DataSource on every query.** Rejected because it leaks infrastructure into
the public semantic API and makes the Agent a placement authority.

**Dataset-level placement.** Rejected because a model could then require an
implicit cross-source join. Model-level placement keeps the execution boundary
deterministic.

**A separate route-map object.** Rejected because it duplicates the stable
semantic model identity and recreates `ExecutionBinding` under another name.

## References

- [Runtime bootstrap](../../specs/operations/runtime-bootstrap.md)
- [Agent query contract](../../specs/semantic/agent-query-contract.md)
- [ADR-0010: Renderer and DataSource Type Are the Sole Compile and Runtime Authorities](0010-renderer-and-datasource-type-are-the-sole-compile-runtime-authorities.md)
