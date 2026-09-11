package semanticplan

import "reflect"

// RequiresComposedPlan reports whether a plan's logical shape requires DAG
// composition rather than the compact direct-source form. It is semantic shape
// evidence; it neither chooses a renderer nor constructs SQLPlan.
func RequiresComposedPlan(plan *SemanticPlan) bool {
	if plan == nil || len(plan.Nodes) == 0 {
		return false
	}
	for _, node := range plan.Nodes {
		switch node.(type) {
		case AdditiveAttributionNode, RatioAttributionNode:
			return true
		}
	}
	if plan.DenseCalendar != nil || plan.CustomDenseCalendar != nil || CustomCalendarDomainCount(plan.Nodes) != 0 || len(plan.Output.Predicates) != 0 {
		return true
	}
	if err := ValidateNode(plan.Nodes[0]); err != nil {
		return true
	}
	firstBase := plan.Nodes[0].NodeBase()
	firstSource, ok := NodeSourceState(plan.Nodes[0])
	if !ok || firstSource.Root.Name == "" {
		return true
	}
	for _, node := range plan.Nodes {
		if err := ValidateNode(node); err != nil {
			return true
		}
		base := node.NodeBase()
		if len(base.Inputs) != 0 || (base.Boundary != SemanticPlanNodeBoundarySourceSelection && base.Boundary != SemanticPlanNodeBoundarySourceAggregate) {
			return true
		}
		source, ok := NodeSourceState(node)
		if !ok || source.Root != firstSource.Root || canonicalGrainKey(base.OutputGrain) != canonicalGrainKey(firstBase.OutputGrain) || !SamePredicateSet(nodePredicates(node), plan.Predicates) {
			return true
		}
	}
	return false
}

func nodePredicates(node SemanticPlanNode) []Predicate {
	base := node.NodeBase()
	out := make([]Predicate, 0, len(base.Predicates))
	for _, predicate := range base.Predicates {
		if predicate.Predicate != nil {
			out = append(out, *predicate.Predicate)
		}
	}
	return out
}

// SamePredicateSet reports whether two predicate collections carry the same
// semantic filter identities, independent of stable caller ordering.
func SamePredicateSet(left, right []Predicate) bool {
	if len(left) != len(right) {
		return false
	}
	matched := make([]bool, len(right))
	for _, candidate := range left {
		found := false
		for i, other := range right {
			if !matched[i] && candidate.Filter.Field == other.Filter.Field && candidate.Filter.Operator == other.Filter.Operator && reflect.DeepEqual(candidate.Filter.Value, other.Filter.Value) {
				matched[i], found = true, true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
