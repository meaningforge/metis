package manifest

import (
	"sort"

	"github.com/meaningforge/metis/ossie"
)

// ExpressionAnalyzer is the manifest-build boundary for extracting canonical
// semantic dependencies from metric expressions. Query-time Resolver code must
// never depend on a concrete SQL parser implementation.
type ExpressionAnalyzer interface {
	AnalyzeMetric(metric *ossie.Metric, model *ModelIndex) (MetricAnalysis, error)
}

// DefaultExpressionAnalyzer is the single authoritative production analyzer.
// Unsupported syntax must fail explicitly with EXPRESSION_PARSE_FAILED rather
// than falling back to heuristic reference inference.
func DefaultExpressionAnalyzer() ExpressionAnalyzer {
	return NewNativeExpressionAnalyzer()
}

func buildMetricDependencyIndexWithAnalyzer(model *ModelIndex, analyzer ExpressionAnalyzer) (map[string]MetricAnalysis, map[string]MetricDependency, error) {
	if analyzer == nil {
		analyzer = DefaultExpressionAnalyzer()
	}
	analyses := make(map[string]MetricAnalysis, len(model.Metrics))
	dependencies := make(map[string]MetricDependency, len(model.Metrics))
	names := make([]string, 0, len(model.Metrics))
	for name := range model.Metrics {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		metric := model.Metrics[name]
		analysis, err := analyzer.AnalyzeMetric(metric, model)
		if err != nil {
			return nil, nil, err
		}
		dependency := analysis.Dependency
		if spec, ok, err := ossie.ConversionSpec(metric); err != nil {
			return nil, nil, err
		} else if ok {
			// A conversion metric's physical inputs are owned by its typed
			// conversion extension, not by the placeholder/output expression.
			// Clear direct field ownership before the dependency graph expands
			// base/conversion metric dependencies transitively. Use non-nil empty
			// slices so graph normalization does not treat "known empty" as legacy
			// unset metadata and restore the analyzer's single-dataset fallback.
			dependency.DirectDatasets = []string{}
			dependency.DirectReferences = []SemanticReference{}
			dependency.Datasets = []string{}
			dependency.References = []SemanticReference{}
			dependency.Metrics = mergeMetricDependencies(dependency.Metrics, spec.BaseMetric, spec.ConversionMetric)
		}
		analysis.Dependency = dependency
		analyses[name] = analysis
		dependencies[name] = dependency
	}
	return analyses, dependencies, nil
}

func mergeMetricDependencies(existing []string, required ...string) []string {
	set := make(map[string]struct{}, len(existing)+len(required))
	for _, name := range existing {
		if name != "" {
			set[name] = struct{}{}
		}
	}
	for _, name := range required {
		if name != "" {
			set[name] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for name := range set {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
