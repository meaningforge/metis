package resolver

import (
	"github.com/meaningforge/metis/expression"
	"github.com/meaningforge/metis/ossie"
)

// SelectExpressionForDialect exposes the Resolver's selected-Renderer-dialect
// expression policy to non-Planner callers. Planner consumes expressions
// already registered on SemanticQuerySpec and never selects dialects.
func SelectExpressionForDialect(source ossie.Expression, expressionDialect string) (expression.ResolvedExpression, error) {
	return selectExpression(source, expressionDialect)
}
