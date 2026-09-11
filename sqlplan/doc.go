// Package sqlplan defines Metis's typed SQL physical planning IR.
//
// A Plan is the complete renderer-neutral physical boundary between a
// semanticplan.SemanticPlan and a concrete SQL Renderer. It owns one root
// query block, deterministic topologically ordered blocks, and explicit input
// edges. The package deliberately has no dependency on planner, sql, renderers,
// database implementations, or application transports.
//
// SQLPlan is the only production renderer input. It is not a wrapper around a
// second physical IR, and renderers must not recover semantic authority from
// planner state.
package sqlplan
