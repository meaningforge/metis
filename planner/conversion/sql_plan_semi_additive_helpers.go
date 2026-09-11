package conversion

import (
	"strings"

	"github.com/meaningforge/metis/planner/semanticplan"
)

const (
	semiAdditiveStateOrderPrefix         = "__metis_state_order_"
	semiAdditiveStateTieBreakPrefix      = "__metis_state_tiebreak_"
	semiAdditiveStateOrderInputPrefix    = "__metis_state_order_input_"
	semiAdditiveStateTieBreakInputPrefix = "__metis_state_tiebreak_input_"
	semiAdditiveStateInputPrefix         = "__metis_state_input_"
)

func validateSemiAdditiveComposableNodes(current, prior semanticplan.SemiAdditiveNode) error {
	if prior.Kind() != semanticplan.SemanticPlanNodeSemiAdditiveLast && prior.Kind() != semanticplan.SemanticPlanNodeSemiAdditiveFirst {
		return semiAdditivePlanningError(current.Base.ID, "semi-additive base metric is a terminal scalar and does not retain selector state")
	}
	if len(prior.Spec.WindowGroupings) > 0 {
		return semiAdditivePlanningError(current.Base.ID, "semi-additive base metric crossed an additive rollup boundary and no longer retains selector state")
	}
	if prior.Spec.NonAdditiveDimension != current.Spec.NonAdditiveDimension {
		return semiAdditivePlanningError(current.Base.ID, "semi-additive base metric retained selector state for a different non-additive dimension")
	}
	if prior.Spec.TieBreakDimension != current.Spec.TieBreakDimension {
		return semiAdditivePlanningError(current.Base.ID, "semi-additive base metric retained a different tie-break contract")
	}
	if prior.Spec.NullPolicy != current.Spec.NullPolicy {
		return semiAdditivePlanningError(current.Base.ID, "semi-additive base metric retained a different null-selection contract")
	}
	for _, grouping := range current.Spec.WindowGroupings {
		if !containsGroup(prior.Base.OutputGrain, grouping) {
			return semiAdditivePlanningError(current.Base.ID, "semi-additive base metric does not retain the additive grouping required for rollup")
		}
	}
	return nil
}

func semiAdditiveNodeOrderKey(node semanticplan.SemiAdditiveNode) string {
	if queriedSemiAdditiveTimeWindow(node.Base.OutputGrain, node.Spec.NonAdditiveDimension) {
		return semiAdditiveRawOrderName(node.Spec.NonAdditiveDimension)
	}
	return node.Spec.NonAdditiveDimension
}

func semiAdditiveStateOrderInput(metric string) string {
	return semiAdditiveStateColumn(semiAdditiveStateOrderInputPrefix, metric)
}
func semiAdditiveStateTieBreakInput(metric string) string {
	return semiAdditiveStateColumn(semiAdditiveStateTieBreakInputPrefix, metric)
}
func semiAdditiveStateOrderColumn(metric string) string {
	return semiAdditiveStateColumn(semiAdditiveStateOrderPrefix, metric)
}
func semiAdditiveStateTieBreakColumn(metric string) string {
	return semiAdditiveStateColumn(semiAdditiveStateTieBreakPrefix, metric)
}
func semiAdditiveStateInput(metric string) string {
	return semiAdditiveStateColumn(semiAdditiveStateInputPrefix, metric)
}

func semiAdditiveStateColumn(prefix, metric string) string {
	return prefix + strings.ReplaceAll(strings.TrimSpace(metric), ".", "_")
}
