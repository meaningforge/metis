package builder

import (
	"github.com/meaningforge/metis/expression"
	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/resolver"
)

func resolvedTestExpression(source string) expression.ResolvedExpression {
	return expression.NewResolvedExpression("ANSI_SQL", source)
}

func resolvedTestQuery(model *manifest.ModelIndex, dialect string) *resolver.ResolvedSemanticQuery {
	q := &resolver.ResolvedSemanticQuery{
		Model:            model,
		FieldExpressions: map[string]expression.ResolvedExpression{},
	}
	for key, handle := range model.Fields {
		selected, err := resolver.SelectExpressionForDialect(handle.Field.Expression, dialect)
		if err == nil {
			q.FieldExpressions[key] = selected
		}
	}
	for name, metric := range model.Metrics {
		selected, err := resolver.SelectExpressionForDialect(metric.Expression, dialect)
		if err == nil {
			q.EvaluationMetrics = append(q.EvaluationMetrics, resolver.ResolvedMetric{Name: name, Metric: metric, Expression: selected})
		}
	}
	return q
}
