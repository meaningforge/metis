package builder

import (
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/planner/temporal"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/serrors"
)

// ValidateConversionQueryGrains proves that every visible conversion output
// grouping remains owned by the base-event population.
func ValidateConversionQueryGrains(nodes []semanticplan.SemanticPlanNode, groups []semanticplan.GroupBy) error {
	if len(groups) == 0 {
		return nil
	}
	for _, node := range nodes {
		conversion, ok := node.(semanticplan.ConversionNode)
		if !ok || conversion.Conversion == nil {
			continue
		}
		for _, group := range groups {
			if conversionGroupOwnedByBase(*conversion.Conversion, group) {
				continue
			}
			return &serrors.Error{
				Code:    serrors.ErrIncompatibleQueryGrain,
				Message: "conversion query grain must be owned by the base event in v1",
				Details: map[string]any{
					"metric":        conversion.Base.ID,
					"base_root":     conversion.Conversion.BaseRoot,
					"group":         group.Name,
					"group_dataset": group.Dataset,
				},
			}
		}
	}
	return nil
}

func conversionGroupOwnedByBase(plan semanticplan.ConversionPlan, group semanticplan.GroupBy) bool {
	if group.CustomCalendar != nil {
		return group.CustomCalendar.BaseTimeField != nil && group.CustomCalendar.BaseTimeField.Name == plan.BaseTime.Name
	}
	return group.Dataset == plan.BaseRoot
}

// ValidateGrainToDateQueryGrains verifies that built-in grain-to-date metrics
// have their time dimension at an explicit grain finer than the reset unit.
func ValidateGrainToDateQueryGrains(nodes []semanticplan.SemanticPlanNode, groups []semanticplan.GroupBy) error {
	for _, node := range nodes {
		cumulative, ok := node.(semanticplan.CumulativeWindowNode)
		if !ok || cumulative.Spec.Window.Type != "grain_to_date" || !IsBuiltInCumulativeUnit(cumulative.Spec.Window.Unit) {
			continue
		}
		var matched *semanticplan.GroupBy
		for i := range groups {
			group := &groups[i]
			if group.Field != nil && (group.Field.Name == cumulative.Spec.TimeDimension || unqualifiedName(group.Name) == cumulative.Spec.TimeDimension) {
				matched = group
				break
			}
		}
		if matched == nil || matched.Grain == nil {
			return &serrors.Error{Code: serrors.ErrIncompatibleQueryGrain, Message: "grain-to-date metric requires its time dimension at an explicit query grain", Details: map[string]any{"metric": cumulative.Base.ID, "time_dimension": cumulative.Spec.TimeDimension}}
		}
		reset := query.TimeGrain(cumulative.Spec.Window.Unit)
		if !temporal.IsFinerGrain(*matched.Grain, reset) {
			return &serrors.Error{Code: serrors.ErrIncompatibleQueryGrain, Message: "grain-to-date query grain must be finer than reset boundary", Details: map[string]any{"metric": cumulative.Base.ID, "time_dimension": cumulative.Spec.TimeDimension, "reset_unit": reset, "query_grain": *matched.Grain}}
		}
	}
	return nil
}
