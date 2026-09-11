// Package builder constructs source-aware SemanticPlan DAG state from resolved
// query evidence and a validated metric-evaluation plan, including source,
// grain, and calendar behavior. It does not resolve semantic names, select or
// render a Renderer, construct SQLPlan, or execute queries.
package builder
