package builder

import (
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/resolver"
)

func planPostEvaluationPredicates(plan *semanticplan.SemanticPlan) []semanticplan.PostEvaluationPredicate {
	if plan == nil {
		return nil
	}
	definitionFilters := planPostDefinitionFilters(plan)
	out := make([]semanticplan.PostEvaluationPredicate, 0, len(plan.Output.Predicates))
	for _, predicate := range plan.Output.Predicates {
		matchedDefinition := -1
		for i, filter := range definitionFilters {
			if sameFilterIdentity(predicate.Filter, filter) {
				matchedDefinition = i
				break
			}
		}
		if matchedDefinition >= 0 {
			definitionFilters = append(definitionFilters[:matchedDefinition], definitionFilters[matchedDefinition+1:]...)
			continue
		}
		out = append(out, predicate)
	}
	return out
}

func planPostDefinitionFilters(plan *semanticplan.SemanticPlan) []query.Filter {
	if plan == nil {
		return nil
	}
	var out []query.Filter
	for _, node := range plan.Nodes {
		metric := semanticplan.NodeMetric(node)
		spec, ok, err := ossie.MetricDefinitionFilter(metric)
		if err != nil || !ok || ossie.EffectiveMetricDefinitionFilterStage(spec) != ossie.MetricDefinitionFilterStagePostAggregation {
			continue
		}
		for _, filter := range spec.Filters {
			normalized, err := resolver.NormalizeFilterValue(filter, metric.Datatype)
			if err == nil {
				out = append(out, normalized)
			}
		}
	}
	return out
}
