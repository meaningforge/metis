package conversion

import (
	"reflect"

	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/serrors"
)

func conversionInputDefinitionPredicates(conversion semanticplan.ConversionNode, nodesByID map[string]semanticplan.SemanticPlanNode, queryPredicates []semanticplan.Predicate) ([]semanticplan.Predicate, []semanticplan.Predicate, error) {
	nodeBase := conversion.Base
	if conversion.Conversion == nil || len(nodeBase.Inputs) != 2 {
		return nil, nil, metricLoweringError("conversion input definition filters require a typed conversion plan", nodeBase.ID)
	}
	base, ok := nodesByID[conversion.Spec.BaseMetric]
	if !ok {
		return nil, nil, metricLoweringError("conversion base dependency node is unavailable during raw-event lowering", conversion.Spec.BaseMetric)
	}
	converted, ok := nodesByID[conversion.Spec.ConversionMetric]
	if !ok {
		return nil, nil, metricLoweringError("conversion dependency node is unavailable during raw-event lowering", conversion.Spec.ConversionMetric)
	}
	basePredicates, err := conversionSideDefinitionPredicates(nodeBase.ID, "base", conversion.Conversion.BaseRoot, semanticPlanNodePreAggregationPredicates(base), queryPredicates)
	if err != nil {
		return nil, nil, err
	}
	conversionPredicates, err := conversionSideDefinitionPredicates(nodeBase.ID, "conversion", conversion.Conversion.ConversionRoot, semanticPlanNodePreAggregationPredicates(converted), queryPredicates)
	if err != nil {
		return nil, nil, err
	}
	return basePredicates, conversionPredicates, nil
}

func conversionSideDefinitionPredicates(metric, side, root string, inputPredicates, queryPredicates []semanticplan.Predicate) ([]semanticplan.Predicate, error) {
	out := make([]semanticplan.Predicate, 0, len(inputPredicates))
	for _, candidate := range inputPredicates {
		if conversionPredicateCovered(candidate, queryPredicates) {
			continue
		}
		if candidate.Field == nil || candidate.Dataset != root {
			return nil, &serrors.Error{Code: serrors.ErrInvalidConversionMetric, Message: "conversion input definition filter must be owned directly by its event root in v1", Details: map[string]any{"metric": metric, "side": side, "root_dataset": root, "filter_field": candidate.Filter.Field, "filter_dataset": candidate.Dataset}}
		}
		out = append(out, candidate)
	}
	return out, nil
}

func conversionPredicateCovered(candidate semanticplan.Predicate, predicates []semanticplan.Predicate) bool {
	for _, predicate := range predicates {
		if candidate.Dataset == predicate.Dataset && candidate.Expression == predicate.Expression && candidate.Filter.Field == predicate.Filter.Field && candidate.Filter.Operator == predicate.Filter.Operator && reflect.DeepEqual(candidate.Filter.Value, predicate.Filter.Value) {
			return true
		}
	}
	return false
}

func conversionPhysicalField(plan semanticplan.ConversionPhysicalInputPlan, ref semanticplan.ConversionFieldRef) (semanticplan.ConversionPhysicalFieldRef, bool) {
	fields := make([]semanticplan.ConversionPhysicalFieldRef, 0, len(plan.BaseEventKey)+len(plan.ConversionEventKey)+4+2*len(plan.ConstantProperties))
	fields = append(fields, plan.BaseEventKey...)
	fields = append(fields, plan.ConversionEventKey...)
	fields = append(fields, plan.BaseTime, plan.ConversionTime, plan.Entity.Base, plan.Entity.Conversion)
	for _, property := range plan.ConstantProperties {
		fields = append(fields, property.Base, property.Conversion)
	}
	for _, field := range fields {
		if field.Dataset == ref.Dataset && field.Name == ref.Name {
			return field, true
		}
	}
	return semanticplan.ConversionPhysicalFieldRef{}, false
}
