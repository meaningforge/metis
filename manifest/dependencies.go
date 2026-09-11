package manifest

import (
	"sort"

	"github.com/meaningforge/metis/expression"
	"github.com/meaningforge/metis/serrors"
)

// SemanticReference is the manifest-level, target-independent identity of a
// field referenced by a semantic expression. Parser-specific AST nodes must not
// leak into the manifest/resolver contract.
type SemanticReference struct {
	Dataset string
	Field   string
}

// MetricDependency is the canonical manifest projection consumed by query-time
// semantic resolution. It is produced authoritatively by ExpressionAnalyzer
// during manifest construction and then reused by Resolver/Discover consumers.
type MetricDependency struct {
	// DirectDatasets and DirectReferences describe fields used by this metric's
	// own expression. Datasets and References additionally include the
	// transitive field dependencies inherited through Metrics.
	DirectDatasets   []string
	DirectReferences []SemanticReference
	Datasets         []string
	References       []SemanticReference
	// Metrics contains the direct metric-to-metric dependencies declared by the
	// expression. Datasets and References are expanded transitively when the
	// model's MetricDependencyGraph is built.
	Metrics   []string
	Inference string
}

// AnalyzedExpression preserves one Ossie source expression after binding and
// semantic type analysis. It remains target-independent until Resolver selects
// the source dialect compatible with the selected Renderer.
type AnalyzedExpression struct {
	Dialect string
	Source  string
	Bound   expression.BoundExpression
	Typed   expression.TypedExpression
	// AggregationProperties is aggregation-algebra derivation evidence for every
	// aggregation call site in the expression, in source order. It is recorded
	// for inspection and later phases; no planner decision consumes it yet.
	AggregationProperties []expression.AggregationProperties
}

// MetricAnalysis contains the canonical direct dependency projection plus the
// analyzed representation of every declared source dialect.
type MetricAnalysis struct {
	Dependency  MetricDependency
	Expressions []AnalyzedExpression
}

type MetricSource struct {
	Metric  string
	Dataset string
}

func dependencyFromReferences(refs map[SemanticReference]struct{}, metrics map[string]struct{}, inference string) MetricDependency {
	references := make([]SemanticReference, 0, len(refs))
	datasetSet := map[string]struct{}{}
	for ref := range refs {
		references = append(references, ref)
		datasetSet[ref.Dataset] = struct{}{}
	}
	sort.Slice(references, func(i, j int) bool {
		if references[i].Dataset != references[j].Dataset {
			return references[i].Dataset < references[j].Dataset
		}
		return references[i].Field < references[j].Field
	})
	datasets := make([]string, 0, len(datasetSet))
	for dataset := range datasetSet {
		datasets = append(datasets, dataset)
	}
	sort.Strings(datasets)
	metricNames := make([]string, 0, len(metrics))
	for metric := range metrics {
		metricNames = append(metricNames, metric)
	}
	sort.Strings(metricNames)
	return MetricDependency{
		DirectDatasets:   append([]string(nil), datasets...),
		DirectReferences: append([]SemanticReference(nil), references...),
		Datasets:         datasets,
		References:       references,
		Metrics:          metricNames,
		Inference:        inference,
	}
}

func (m *ModelIndex) MetricDependency(name string) (MetricDependency, bool) {
	if m == nil || m.MetricDependencies == nil {
		return MetricDependency{}, false
	}
	dependency, ok := m.MetricDependencies[name]
	return dependency, ok
}

func (m *ModelIndex) MetricAnalysis(name string) (MetricAnalysis, bool) {
	if m == nil || m.MetricAnalyses == nil {
		return MetricAnalysis{}, false
	}
	analysis, ok := m.MetricAnalyses[name]
	return analysis, ok
}

func (m *ModelIndex) MetricSources(names ...string) ([]MetricSource, error) {
	if m == nil || m.MetricDependencyGraph == nil {
		return nil, &serrors.Error{Code: serrors.ErrInvalidModel, Message: "metric dependency graph is not available", Details: map[string]any{"metrics": names}}
	}
	order, err := m.MetricDependencyGraph.EvaluationOrder(names...)
	if err != nil {
		return nil, err
	}
	out := make([]MetricSource, 0, len(order))
	for _, metric := range order {
		dependency, ok := m.MetricDependency(metric)
		if !ok || len(dependency.Metrics) != 0 {
			continue
		}
		datasets := append([]string(nil), dependency.DirectDatasets...)
		if len(datasets) == 0 {
			datasets = append(datasets, dependency.Datasets...)
		}
		sort.Strings(datasets)
		switch len(datasets) {
		case 0:
			return nil, &serrors.Error{Code: serrors.ErrInvalidModel, Message: "source metric has no semantic dataset", Details: map[string]any{"metric": metric}}
		case 1:
			out = append(out, MetricSource{Metric: metric, Dataset: datasets[0]})
		default:
			root, err := m.resolveMetricSourceRoot(metric, datasets)
			if err != nil {
				return nil, err
			}
			out = append(out, MetricSource{Metric: metric, Dataset: root})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Dataset != out[j].Dataset {
			return out[i].Dataset < out[j].Dataset
		}
		return out[i].Metric < out[j].Metric
	})
	return out, nil
}

func (m *ModelIndex) resolveMetricSourceRoot(metric string, datasets []string) (string, error) {
	viable := make([]string, 0, len(datasets))
	for _, candidate := range datasets {
		reachesAll := true
		for _, required := range datasets {
			if !m.Graph.CanReachDirected(candidate, required) {
				reachesAll = false
				break
			}
		}
		if reachesAll {
			viable = append(viable, candidate)
		}
	}
	if len(viable) == 1 {
		return viable[0], nil
	}
	return "", &serrors.Error{
		Code:    serrors.ErrAmbiguousMetricSource,
		Message: "source metric references multiple datasets and has no unique directed aggregate root",
		Details: map[string]any{"metric": metric, "candidate_datasets": datasets, "viable_roots": viable},
	}
}
