package builder

import "github.com/meaningforge/metis/planner/semanticplan"

func semanticPlanNodePredicates(node semanticplan.SemanticPlanNode) []semanticplan.Predicate {
	if node == nil {
		return nil
	}
	predicates := make([]semanticplan.Predicate, 0, len(node.NodeBase().Predicates))
	for _, predicate := range node.NodeBase().Predicates {
		if predicate.Predicate != nil {
			predicates = append(predicates, *predicate.Predicate)
		}
	}
	return predicates
}
