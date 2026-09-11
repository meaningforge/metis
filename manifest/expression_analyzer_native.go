package manifest

import (
	"fmt"
	"reflect"
	"sort"

	"github.com/meaningforge/metis/expression"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/serrors"
)

type NativeExpressionAnalyzer struct{}

func NewNativeExpressionAnalyzer() ExpressionAnalyzer { return &NativeExpressionAnalyzer{} }

func (a *NativeExpressionAnalyzer) AnalyzeMetric(metric *ossie.Metric, model *ModelIndex) (MetricAnalysis, error) {
	if metric == nil || model == nil {
		return MetricAnalysis{}, nil
	}
	var canonical *MetricDependency
	var canonicalDialect string
	analysis := MetricAnalysis{}
	for _, dialect := range metric.Expression.Dialects {
		profile := expression.ProfileForDialect(string(dialect.Dialect))
		ast, err := expression.Parse(dialect.Expression, profile)
		if err != nil {
			return MetricAnalysis{}, &serrors.Error{
				Code:    serrors.ErrExpressionParseFailed,
				Message: fmt.Sprintf("failed to parse expression for metric %q", metric.Name),
				Details: map[string]any{"metric": metric.Name, "dialect": string(dialect.Dialect), "cause": err.Error()},
			}
		}

		bound, err := expression.Bind(ast, newSemanticManifestExpressionResolver(model, metric.Name))
		if err != nil {
			return MetricAnalysis{}, err
		}
		dependency := dependencyFromBoundExpression(bound, model)

		semanticAnalyzer := expression.NewSemanticAnalyzerWithOptions(
			bound,
			nil,
			expression.SemanticAnalyzerOptions{AllowUnknownFunctions: true},
		)
		typed, err := semanticAnalyzer.Analyze(ast)
		if err != nil {
			details := map[string]any{
				"metric":  metric.Name,
				"dialect": string(dialect.Dialect),
				"cause":   err.Error(),
			}
			if semanticErr, ok := err.(*expression.SemanticError); ok {
				details["semantic_code"] = string(semanticErr.Code)
				details["span_start"] = semanticErr.Span.Start
				details["span_end"] = semanticErr.Span.End
			}
			return MetricAnalysis{}, &serrors.Error{
				Code:    serrors.ErrSemanticAnalysisFailed,
				Message: fmt.Sprintf("semantic analysis failed for metric %q", metric.Name),
				Details: details,
			}
		}
		analysis.Expressions = append(analysis.Expressions, AnalyzedExpression{
			Dialect:               string(dialect.Dialect),
			Source:                dialect.Expression,
			Bound:                 bound,
			Typed:                 typed,
			AggregationProperties: expression.CollectAggregationProperties(ast, nil),
		})

		if canonical == nil {
			copy := dependency
			canonical = &copy
			canonicalDialect = string(dialect.Dialect)
			continue
		}
		if !reflect.DeepEqual(canonical.References, dependency.References) || !reflect.DeepEqual(canonical.Metrics, dependency.Metrics) {
			return MetricAnalysis{}, &serrors.Error{
				Code:    serrors.ErrInconsistentExpressionReferences,
				Message: fmt.Sprintf("metric %q has inconsistent semantic references across dialect expressions", metric.Name),
				Details: map[string]any{
					"metric":                 metric.Name,
					"canonical_dialect":      canonicalDialect,
					"canonical_references":   canonical.References,
					"canonical_metrics":      canonical.Metrics,
					"conflicting_dialect":    string(dialect.Dialect),
					"conflicting_references": dependency.References,
					"conflicting_metrics":    dependency.Metrics,
				},
			}
		}
	}
	if canonical != nil {
		analysis.Dependency = *canonical
		return analysis, nil
	}
	if len(model.Datasets) == 1 {
		for dataset := range model.Datasets {
			analysis.Dependency = MetricDependency{Datasets: []string{dataset}, Inference: "native-parser:single-dataset-scope"}
			return analysis, nil
		}
	}
	analysis.Dependency = MetricDependency{Inference: "native-parser:unresolved"}
	return analysis, nil
}

func dependencyFromBoundExpression(bound expression.BoundExpression, model *ModelIndex) MetricDependency {
	fieldRefs := map[SemanticReference]struct{}{}
	metricRefs := map[string]struct{}{}
	for _, symbol := range bound.Symbols() {
		switch symbol.Kind {
		case expression.BoundColumn:
			fieldRefs[SemanticReference{Dataset: symbol.Qualifier, Field: symbol.Name}] = struct{}{}
		case expression.BoundMetric:
			metricRefs[symbol.Name] = struct{}{}
		}
	}
	if len(fieldRefs) == 0 && len(metricRefs) == 0 {
		if len(model.Datasets) == 1 {
			for dataset := range model.Datasets {
				return MetricDependency{Datasets: []string{dataset}, Inference: "native-parser:single-dataset-scope"}
			}
		}
		return MetricDependency{Inference: "native-parser:no-field-reference"}
	}
	return dependencyFromReferences(fieldRefs, metricRefs, "native-parser")
}

func sortedStringSet(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for v := range values {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}
