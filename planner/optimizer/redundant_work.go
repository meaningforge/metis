package optimizer

import (
	"reflect"

	"github.com/meaningforge/metis/planner/semanticplan"
)

// ProjectionDeduplicationRule removes only projections that are structurally
// identical after optimizer canonicalization. It keeps the first occurrence so
// user-visible projection order remains stable.
type ProjectionDeduplicationRule struct{}

func (ProjectionDeduplicationRule) Name() string { return "projection_deduplication" }

func (ProjectionDeduplicationRule) Apply(plan *semanticplan.SemanticPlan) (bool, error) {
	out := make([]semanticplan.Projection, 0, len(plan.Projections))
	for _, projection := range plan.Projections {
		duplicate := false
		for _, candidate := range out {
			if reflect.DeepEqual(candidate, projection) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			out = append(out, projection)
		}
	}
	if len(out) == len(plan.Projections) {
		return false, nil
	}
	plan.Projections = out
	return true, nil
}

func (rule ProjectionDeduplicationRule) ApplySemanticPlan(state *State) (bool, error) {
	return applyRuleToState(state, rule)
}
