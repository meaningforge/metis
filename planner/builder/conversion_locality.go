package builder

import "github.com/meaningforge/metis/planner/semanticplan"

// validateConversionPhysicalLocality keeps the P1 raw-event contract executable:
// every entity/constant-property expression used by candidate matching must be
// owned directly by its event root. A merely reachable related-dataset field
// would require preserving and cardinality-proving an event-side relationship
// path in semanticplan.ConversionPhysicalInputPlan; that contract is not present in P1.
func validateConversionPhysicalLocality(plan semanticplan.ConversionPlan) error {
	validate := func(side, root string, ref semanticplan.ConversionFieldRef) error {
		if ref.Dataset == root {
			return nil
		}
		return conversionPlanningError(plan.BaseMetric, "conversion link property must be owned directly by its event root in v1", map[string]any{
			"side":             side,
			"root_dataset":     root,
			"property_dataset": ref.Dataset,
			"property":         ref.Name,
		})
	}
	if err := validate("base", plan.BaseRoot, plan.Entity.Base); err != nil {
		return err
	}
	if err := validate("conversion", plan.ConversionRoot, plan.Entity.Conversion); err != nil {
		return err
	}
	for _, property := range plan.ConstantProperties {
		if err := validate("base", plan.BaseRoot, property.Base); err != nil {
			return err
		}
		if err := validate("conversion", plan.ConversionRoot, property.Conversion); err != nil {
			return err
		}
	}
	return nil
}
