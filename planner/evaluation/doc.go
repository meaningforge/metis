// Package evaluation owns the source-independent MetricEvaluationPlan IR.
//
// It derives metric roots, dependency closure, metric kinds, validation,
// cloning, explanation, and fingerprinting from a resolved semantic query. It
// intentionally does not own dataset roots, joins, output grain, semantic-plan
// nodes, SQLPlan construction, renderer lookup, or SQL rendering.
package evaluation
