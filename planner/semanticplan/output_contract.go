package semanticplan

import "fmt"

// SemanticOutputContract describes the final result: projections, grouping grain,
// post-evaluation predicates, ordering, and limit. It owns the output semantics;
// any equivalent fields on SemanticPlan must remain consistent with it.
type SemanticOutputContract struct {
	Projections []Projection
	Grain       []GroupBy
	Predicates  []PostEvaluationPredicate
	OrderBy     []Sort
	Limit       *int
}

// BuildOutputContract copies plan-owned projections, ordering, and limit,
// together with the supplied grain and post-evaluation predicates.
func BuildOutputContract(plan *SemanticPlan, grain []GroupBy, predicates []PostEvaluationPredicate) SemanticOutputContract {
	contract := SemanticOutputContract{
		Projections: append([]Projection(nil), plan.Projections...),
		Grain:       cloneGroups(grain),
		Predicates:  append([]PostEvaluationPredicate(nil), predicates...),
		OrderBy:     append([]Sort(nil), plan.Sorts...),
	}
	for i := range contract.Projections {
		contract.Projections[i].Datasets = append([]string(nil), plan.Projections[i].Datasets...)
	}
	if plan.Limit != nil {
		limit := *plan.Limit
		contract.Limit = &limit
	}
	return contract
}

// RequireOutputContractGrainMatchesPlan rejects disagreement between the output
// grain and resolved plan groups, so a plan cannot report a different grouping
// from the one it computes.
func RequireOutputContractGrainMatchesPlan(plan *SemanticPlan) error {
	if plan == nil {
		return nil
	}
	if got, want := canonicalGrainKey(plan.Output.Grain), canonicalGrainKey(plan.Groups); got != want {
		return fmt.Errorf("plan output grain %q does not match plan groups %q", got, want)
	}
	return nil
}

// CloneOutputContract returns an independently owned output contract.
func CloneOutputContract(in SemanticOutputContract) SemanticOutputContract {
	out := SemanticOutputContract{
		Projections: append([]Projection(nil), in.Projections...),
		Grain:       cloneGroups(in.Grain),
		Predicates:  append([]PostEvaluationPredicate(nil), in.Predicates...),
		OrderBy:     append([]Sort(nil), in.OrderBy...),
	}
	for i := range out.Projections {
		out.Projections[i].Datasets = append([]string(nil), in.Projections[i].Datasets...)
	}
	if in.Limit != nil {
		limit := *in.Limit
		out.Limit = &limit
	}
	return out
}

func cloneGroups(groups []GroupBy) []GroupBy { return append([]GroupBy(nil), groups...) }
