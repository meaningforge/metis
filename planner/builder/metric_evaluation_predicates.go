package builder

import (
	"github.com/meaningforge/metis/planner/evaluation"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/serrors"
)

// conversionEvaluationPredicates derives conversion-specific raw predicate
// suppression from the already-built query-scoped metric authority. It runs
// before evaluation.MetricEvaluationPlan -> semanticplan.SemanticPlan lowering; SQLPlan construction
// consumes only the resulting typed semanticplan.SemanticPlan nodes.
func conversionEvaluationPredicates(evaluationPlan *evaluation.MetricEvaluationPlan, predicates []semanticplan.Predicate) ([]semanticplan.Predicate, error) {
	if evaluationPlan == nil || len(predicates) == 0 {
		return predicates, nil
	}

	nodes := make(map[string]evaluation.MetricEvaluationNode, len(evaluationPlan.Nodes))
	for _, node := range evaluationPlan.Nodes {
		nodes[node.ID] = node
	}
	outputRoots := 0
	for _, root := range evaluationPlan.Roots {
		if root.Role != evaluation.MetricEvaluationRoleOutput {
			continue
		}
		outputRoots++
		node, ok := nodes[root.Metric]
		if !ok {
			return nil, &serrors.Error{Code: serrors.ErrInternalInvariant, Message: "metric evaluation output root has no authoritative node", Details: map[string]any{"metric": root.Metric}}
		}
		if node.Kind != evaluation.MetricEvaluationConversion {
			return predicates, nil
		}
		if node.Spec.Conversion == nil {
			return nil, &serrors.Error{Code: serrors.ErrInternalInvariant, Message: "conversion metric evaluation is missing its typed spec", Details: map[string]any{"metric": node.ID}}
		}
	}
	if outputRoots == 0 {
		return predicates, nil
	}

	out := make([]semanticplan.Predicate, 0, len(predicates))
	for _, predicate := range predicates {
		if predicate.Field == nil {
			out = append(out, predicate)
		}
	}
	return out, nil
}
