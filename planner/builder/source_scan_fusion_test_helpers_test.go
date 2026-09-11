package builder

import (
	"github.com/meaningforge/metis/planner/optimizer"
	"github.com/meaningforge/metis/planner/semanticplan"
)

func fusePlanSourceScans(plan *semanticplan.SemanticPlan) (bool, error) {
	if !semanticplan.RequiresComposedPlan(plan) {
		return false, nil
	}
	return (optimizer.SourceScanFusionRule{}).Apply(plan)
}
