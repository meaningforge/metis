package builder

import (
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/serrors"
)

// ValidateConversionFilterOwnership verifies that conversion raw-event
// filters stay on the governed base-event source during semantic construction.
func ValidateConversionFilterOwnership(nodes []semanticplan.SemanticPlanNode, predicates []semanticplan.Predicate) error {
	for _, node := range nodes {
		conversion, ok := node.(semanticplan.ConversionNode)
		if !ok || conversion.Conversion == nil {
			continue
		}
		plan := conversion.Conversion
		for _, predicate := range predicates {
			if predicate.Field == nil || predicate.Dataset == plan.BaseRoot {
				continue
			}
			return &serrors.Error{
				Code:    serrors.ErrInvalidConversionMetric,
				Message: "conversion raw-event filter must be owned by the base event in v1",
				Details: map[string]any{
					"metric":         conversion.Base.ID,
					"base_root":      plan.BaseRoot,
					"filter_field":   predicate.Filter.Field,
					"filter_dataset": predicate.Dataset,
				},
			}
		}
	}
	return nil
}
