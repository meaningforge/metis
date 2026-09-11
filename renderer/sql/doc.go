// Package sql provides shared SQLPlan rendering and rendered query values.
// Concrete warehouse Renderers supply dialect-specific Behavior; this package
// owns deterministic traversal, physical lowering helpers, SQL text, and
// parameter collection. SQL values remain separate from parameter bindings.
//
// SQLDialect, QueryParameter, and SQLRenderResult participate in the Renderer and
// Driver contracts. Behavior and the rendering helpers are optional,
// experimental APIs and are not yet a stable extension commitment.
//
// This package does not select Renderers, resolve semantics, orchestrate
// compilation, or execute queries.
package sql
