package resolver

import (
	"sort"

	"github.com/meaningforge/metis/ossie"
)

// applyMetricTimeBindings computes the effective canonical time binding in
// metric evaluation order. Derived metrics inherit a single dependency binding;
// mixing differently-bound dependencies is a semantic conflict.
func applyMetricTimeBindings(q *SemanticQuerySpec) error {
	if q == nil || q.Model == nil {
		return nil
	}

	effective := make(map[string]*ossie.MetricTimeBindingSpec, len(q.EvaluationMetrics))
	for i := range q.EvaluationMetrics {
		metric := &q.EvaluationMetrics[i]
		dependency, hasDependency := q.Model.MetricDependency(metric.Name)
		dependencyBindings := map[string]struct{}{}
		if hasDependency {
			for _, dependencyName := range dependency.Metrics {
				if binding := effective[dependencyName]; binding != nil {
					dependencyBindings[binding.TimeDimension] = struct{}{}
				}
			}
		}

		conversion, isConversion, err := ossie.ConversionSpec(metric.Metric)
		if err != nil {
			return invalidQuery("invalid conversion metric extension", map[string]any{"metric": metric.Name, "cause": err.Error()})
		}
		if isConversion {
			baseBinding := effective[conversion.BaseMetric]
			if baseBinding == nil {
				return invalidQuery("conversion metric requires a canonical base-event time binding", map[string]any{
					"metric":      metric.Name,
					"base_metric": conversion.BaseMetric,
				})
			}
			if metric.TimeBinding != nil && metric.TimeBinding.TimeDimension != baseBinding.TimeDimension {
				return invalidQuery("conversion metric time binding conflicts with base-event time binding", map[string]any{
					"metric":                    metric.Name,
					"metric_time_dimension":     metric.TimeBinding.TimeDimension,
					"base_event_time_dimension": baseBinding.TimeDimension,
				})
			}
			binding := *baseBinding
			metric.TimeBinding = &binding
			effective[metric.Name] = metric.TimeBinding
			continue
		}

		if len(dependencyBindings) > 1 {
			return timeBindingConflict(metric.Name, dependencyBindings)
		}
		var inherited string
		for dimension := range dependencyBindings {
			inherited = dimension
		}

		if metric.TimeBinding != nil {
			if inherited != "" && metric.TimeBinding.TimeDimension != inherited {
				return invalidQuery("metric time binding conflicts with dependency time binding", map[string]any{
					"metric":                    metric.Name,
					"metric_time_dimension":     metric.TimeBinding.TimeDimension,
					"dependency_time_dimension": inherited,
				})
			}
			effective[metric.Name] = metric.TimeBinding
			continue
		}
		if inherited != "" {
			binding := ossie.MetricTimeBindingSpec{Kind: ossie.MetricExtensionTimeBinding, TimeDimension: inherited}
			metric.TimeBinding = &binding
			effective[metric.Name] = metric.TimeBinding
		}
	}

	for i := range q.Metrics {
		if binding := effective[q.Metrics[i].Name]; binding != nil {
			copy := *binding
			q.Metrics[i].TimeBinding = &copy
		}
	}
	return nil
}

func timeBindingConflict(metric string, bindings map[string]struct{}) error {
	values := make([]string, 0, len(bindings))
	for binding := range bindings {
		values = append(values, binding)
	}
	sort.Strings(values)
	return invalidQuery("derived metric dependencies have incompatible canonical time bindings", map[string]any{
		"metric":                    metric,
		"canonical_time_dimensions": values,
	})
}
