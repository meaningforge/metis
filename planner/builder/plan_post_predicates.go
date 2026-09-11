package builder

import (
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/query"
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
		spec, ok, err := ossie.MetricDefinitionFilter(semanticplan.NodeMetric(node))
		if err != nil || !ok || ossie.EffectiveMetricDefinitionFilterStage(spec) != ossie.MetricDefinitionFilterStagePostAggregation {
			continue
		}
		out = append(out, spec.Filters...)
	}
	return out
}
