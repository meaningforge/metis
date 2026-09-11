package conversion

import "github.com/meaningforge/metis/planner/semanticplan"

// SemanticLoweringStrategy names a renderer-neutral SQLPlan shape.
type SemanticLoweringStrategy string

const (
	SemanticLoweringCompact             SemanticLoweringStrategy = "compact"
	SemanticLoweringComposed            SemanticLoweringStrategy = "composed"
	SemanticLoweringAdditiveAttribution SemanticLoweringStrategy = "additive_attribution"
	SemanticLoweringRatioAttribution    SemanticLoweringStrategy = "ratio_attribution"
)

// LoweringStrategyForPlan derives physical shape only from the validated
// semanticplan.SemanticPlan. It neither selects nor invokes a Renderer.
func LoweringStrategyForPlan(plan *semanticplan.SemanticPlan) SemanticLoweringStrategy {
	if plan == nil || len(plan.Nodes) == 0 {
		return SemanticLoweringComposed
	}
	for _, node := range plan.Nodes {
		switch node.(type) {
		case semanticplan.AdditiveAttributionNode:
			return SemanticLoweringAdditiveAttribution
		case semanticplan.RatioAttributionNode:
			return SemanticLoweringRatioAttribution
		}
	}
	if semanticplan.RequiresComposedPlan(plan) {
		return SemanticLoweringComposed
	}
	return SemanticLoweringCompact
}
