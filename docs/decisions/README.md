# Architecture Decisions

Architectural decision records retained for their rationale. Superseded decisions are historical and do not define current behavior.

## Foundation

- [ADR-0001: Apache Ossie Schema Is the Semantic Model Source of Truth](foundation/0001-ossie-schema-source-of-truth.md) — Accepted (current)

## Interfaces

- [ADR-0002: Error Codes and Classes Are Transport-neutral Contracts](interfaces/0002-error-codes-and-classes.md) — Superseded by [ADR-0003](interfaces/0003-caller-action-and-transport-projection.md)
- [ADR-0003: Remediation Is Domain-owned, Transport Behavior Is a Projection](interfaces/0003-caller-action-and-transport-projection.md) — Accepted (current)
- [ADR-0016: HTTP and Stdio Share One MCP Server](interfaces/0016-http-and-stdio-share-one-mcp-server.md) — Accepted

## Operations

- [ADR-0007: Runtime Observability Is Operator-Only and Bounded](operations/0007-runtime-observability-is-operator-only-and-bounded.md) — Accepted (current)
- [ADR-0009: Execution Runtime Is Optional, Driver-Factory-Based, and Orchestrated](operations/0009-execution-runtime-is-optional-driver-factory-based-and-orchestrated.md) — Superseded by ADR-0010
- [ADR-0010: Renderer and DataSource Type Are the Sole Compile and Runtime Authorities](operations/0010-renderer-and-datasource-type-are-the-sole-compile-runtime-authorities.md) — Accepted
- [ADR-0014: DataSource Config Is a Flat String Map with External References](operations/0014-flat-string-datasource-config.md) — Accepted
- [ADR-0017: Data policy binds before semantic optimization](operations/0017-data-policy-binds-before-semantic-optimization.md) — Proposed

## Semantic

- [ADR-0004: SemanticPlan Is the Semantic DAG](semantic/0004-semantic-plan-is-the-dag.md) — Accepted (current)
- [ADR-0006: MetricEvaluationPlan Is the Query-Scoped Metric Authority](semantic/0006-metric-evaluation-plan-is-query-scoped-metric-authority.md) — Accepted (current)
- [ADR-0008: Metric Attribution Is Compile-Only Independent Evidence](semantic/0008-metric-attribution-is-compile-only-independent-evidence.md) — Superseded by ADR-0012
- [ADR-0012: Metric Attribution Is a Typed Runtime Workflow](semantic/0012-metric-attribution-is-a-typed-runtime-workflow.md) — Accepted (current)
- [ADR-0013: Analytical Workflows Remain Vertical-First](semantic/0013-analytical-workflows-remain-vertical-first.md) — Accepted (current)

## SQL

- [ADR-0005: SQLPlan Is the Physical Renderer Boundary](sql/0005-sqlplan-is-the-physical-renderer-boundary.md) — Accepted (current)
- [ADR-0011: Renderer and Driver SPI Stabilization](sql/0011-renderer-and-driver-spi-stabilization.md) — Accepted (current)
