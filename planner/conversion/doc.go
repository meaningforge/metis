// Package conversion owns validated semanticplan.SemanticPlan to renderer-neutral SQLPlan
// conversion and target-neutral output-schema derivation. It consumes an
// already selected Renderer only as expression-compatibility evidence; it does
// not look up Renderers, render SQL text, resolve semantic names, or execute
// queries.
package conversion
