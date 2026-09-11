package manifest

import (
	"sort"

	"github.com/meaningforge/metis/serrors"
)

// MetricDependencyGraph is the immutable, model-scoped DAG used to plan metric
// evaluation order. Edges point from a metric to the metrics it directly uses.
// It contains semantic identities only; physical SQL planning remains outside
// SemanticManifest.
type MetricDependencyGraph struct {
	dependencies map[string][]string
}

func buildMetricDependencyGraph(dependencies map[string]MetricDependency) (*MetricDependencyGraph, map[string]MetricDependency, error) {
	graph := &MetricDependencyGraph{dependencies: make(map[string][]string, len(dependencies))}
	names := make([]string, 0, len(dependencies))
	for name, dependency := range dependencies {
		graph.dependencies[name] = append([]string(nil), dependency.Metrics...)
		sort.Strings(graph.dependencies[name])
		names = append(names, name)
	}
	sort.Strings(names)

	state := make(map[string]uint8, len(dependencies))
	stack := make([]string, 0, len(dependencies))
	expanded := make(map[string]MetricDependency, len(dependencies))
	var visit func(string) (MetricDependency, error)
	visit = func(name string) (MetricDependency, error) {
		switch state[name] {
		case 2:
			return expanded[name], nil
		case 1:
			start := 0
			for i := range stack {
				if stack[i] == name {
					start = i
					break
				}
			}
			cycle := append(append([]string(nil), stack[start:]...), name)
			return MetricDependency{}, &serrors.Error{
				Code:    serrors.ErrMetricDependencyCycle,
				Message: "metric dependency cycle detected",
				Details: map[string]any{"cycle": cycle},
			}
		}

		dependency, ok := dependencies[name]
		if !ok {
			// Reached only while expanding the model's own declared metrics,
			// so the missing name was written by the model author, never by
			// the caller.
			return MetricDependency{}, &serrors.Error{
				Code:    serrors.ErrUnresolvedSemanticReference,
				Message: "metric definition depends on a metric the model does not define",
				Details: map[string]any{"metric": name},
			}
		}
		state[name] = 1
		stack = append(stack, name)
		if dependency.DirectReferences == nil {
			dependency.DirectReferences = append([]SemanticReference(nil), dependency.References...)
		}
		if dependency.DirectDatasets == nil {
			dependency.DirectDatasets = append([]string(nil), dependency.Datasets...)
		}
		refs := make(map[SemanticReference]struct{}, len(dependency.References))
		datasets := make(map[string]struct{}, len(dependency.Datasets))
		for _, ref := range dependency.References {
			refs[ref] = struct{}{}
		}
		for _, dataset := range dependency.Datasets {
			datasets[dataset] = struct{}{}
		}
		for _, child := range graph.dependencies[name] {
			childDependency, err := visit(child)
			if err != nil {
				return MetricDependency{}, err
			}
			for _, ref := range childDependency.References {
				refs[ref] = struct{}{}
			}
			for _, dataset := range childDependency.Datasets {
				datasets[dataset] = struct{}{}
			}
		}
		stack = stack[:len(stack)-1]
		state[name] = 2

		dependency.References = sortedSemanticReferences(refs)
		dependency.Datasets = sortedKeys(datasets)
		dependency.Metrics = append([]string(nil), graph.dependencies[name]...)
		expanded[name] = dependency
		return dependency, nil
	}

	for _, name := range names {
		if _, err := visit(name); err != nil {
			return nil, nil, err
		}
	}
	return graph, expanded, nil
}

func (g *MetricDependencyGraph) DirectDependencies(metric string) ([]string, bool) {
	if g == nil {
		return nil, false
	}
	dependencies, ok := g.dependencies[metric]
	return append([]string(nil), dependencies...), ok
}

func (g *MetricDependencyGraph) EvaluationOrder(metrics ...string) ([]string, error) {
	if g == nil {
		return nil, &serrors.Error{Code: serrors.ErrInvalidModel, Message: "metric dependency graph is not available"}
	}
	seen := make(map[string]struct{}, len(g.dependencies))
	order := make([]string, 0, len(g.dependencies))
	// An unknown name means two different things here depending on who supplied
	// it. A requested metric that does not exist is the caller's to fix; a
	// dependency of an existing metric that does not exist is the model's, and
	// the caller cannot act on it at all. requestedBy carries the metric whose
	// definition named this one, and is empty for the request's own names.
	var visit func(name, requestedBy string) error
	visit = func(name, requestedBy string) error {
		if _, ok := seen[name]; ok {
			return nil
		}
		dependencies, ok := g.dependencies[name]
		if !ok {
			if requestedBy == "" {
				return &serrors.Error{Code: serrors.ErrMetricNotFound, Message: "metric not found", Details: map[string]any{"metric": name}}
			}
			return &serrors.Error{
				Code:    serrors.ErrUnresolvedSemanticReference,
				Message: "metric definition depends on a metric the model does not define",
				Details: map[string]any{"metric": name, "referenced_by": requestedBy},
			}
		}
		seen[name] = struct{}{}
		for _, dependency := range dependencies {
			if err := visit(dependency, name); err != nil {
				return err
			}
		}
		order = append(order, name)
		return nil
	}
	for _, metric := range metrics {
		if err := visit(metric, ""); err != nil {
			return nil, err
		}
	}
	return order, nil
}

func sortedSemanticReferences(values map[SemanticReference]struct{}) []SemanticReference {
	out := make([]SemanticReference, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Dataset != out[j].Dataset {
			return out[i].Dataset < out[j].Dataset
		}
		return out[i].Field < out[j].Field
	})
	return out
}

func sortedKeys(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
