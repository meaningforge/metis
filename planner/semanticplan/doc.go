// Package semanticplan owns the typed, source-aware semantic plan IR.
//
// The package is the home of SemanticPlan, its closed node family, logical
// output contract, and the value vocabulary carried between semantic planning
// and physical conversion. It does not resolve semantic names, select a
// Renderer, construct SQLPlan, render SQL, or execute queries.
package semanticplan
