package builder

import (
	"fmt"

	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/planner/evaluation"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/resolver"
	"github.com/meaningforge/metis/serrors"
)

func planConversionPhysicalInputs(q *resolver.SemanticQuerySpec, semantic semanticplan.ConversionPlan, evaluationNodes map[string]evaluation.MetricEvaluationNode) (semanticplan.ConversionPhysicalInputPlan, error) {
	var model *manifest.ModelIndex
	if q != nil {
		model = q.Model
	}
	if model == nil {
		return semanticplan.ConversionPhysicalInputPlan{}, conversionPlanningError(semantic.BaseMetric, "conversion physical input model is required", nil)
	}
	if err := validateConversionPhysicalLocality(semantic); err != nil {
		return semanticplan.ConversionPhysicalInputPlan{}, err
	}
	baseDataset := model.Datasets[semantic.BaseRoot]
	conversionDataset := model.Datasets[semantic.ConversionRoot]
	if baseDataset == nil || conversionDataset == nil {
		return semanticplan.ConversionPhysicalInputPlan{}, conversionPlanningError(semantic.BaseMetric, "conversion physical event dataset is missing", map[string]any{"base_root": semantic.BaseRoot, "conversion_root": semantic.ConversionRoot})
	}
	hydrate := func(ref semanticplan.ConversionFieldRef) (semanticplan.ConversionPhysicalFieldRef, error) {
		handle := model.Fields[ref.Dataset+"."+ref.Name]
		if handle == nil || handle.Field == nil {
			return semanticplan.ConversionPhysicalFieldRef{}, fmt.Errorf("conversion physical field %s.%s is unavailable", ref.Dataset, ref.Name)
		}
		resolvedExpression, ok := q.FieldExpression(ref.Dataset, ref.Name)
		if !ok {
			return semanticplan.ConversionPhysicalFieldRef{}, &serrors.Error{
				Code:    serrors.ErrUnsupportedExpression,
				Message: "conversion event field has no compatible target expression",
				Details: map[string]any{"dataset": ref.Dataset, "field": ref.Name},
			}
		}
		return semanticplan.ConversionPhysicalFieldRef{Dataset: ref.Dataset, Name: ref.Name, Expression: resolvedExpression.Source}, nil
	}
	hydrateMany := func(refs []semanticplan.ConversionFieldRef) ([]semanticplan.ConversionPhysicalFieldRef, error) {
		result := make([]semanticplan.ConversionPhysicalFieldRef, 0, len(refs))
		for _, ref := range refs {
			physical, err := hydrate(ref)
			if err != nil {
				return nil, err
			}
			result = append(result, physical)
		}
		return result, nil
	}
	baseEventKey, err := hydrateMany(semantic.BaseEventKey)
	if err != nil {
		return semanticplan.ConversionPhysicalInputPlan{}, err
	}
	conversionEventKey, err := hydrateMany(semantic.ConversionEventKey)
	if err != nil {
		return semanticplan.ConversionPhysicalInputPlan{}, err
	}
	baseTime, err := hydrate(semantic.BaseTime)
	if err != nil {
		return semanticplan.ConversionPhysicalInputPlan{}, err
	}
	conversionTime, err := hydrate(semantic.ConversionTime)
	if err != nil {
		return semanticplan.ConversionPhysicalInputPlan{}, err
	}
	baseEntity, err := hydrate(semantic.Entity.Base)
	if err != nil {
		return semanticplan.ConversionPhysicalInputPlan{}, err
	}
	conversionEntity, err := hydrate(semantic.Entity.Conversion)
	if err != nil {
		return semanticplan.ConversionPhysicalInputPlan{}, err
	}
	constants := make([]semanticplan.ConversionPhysicalPropertyPlan, 0, len(semantic.ConstantProperties))
	for _, property := range semantic.ConstantProperties {
		base, err := hydrate(property.Base)
		if err != nil {
			return semanticplan.ConversionPhysicalInputPlan{}, err
		}
		conversion, err := hydrate(property.Conversion)
		if err != nil {
			return semanticplan.ConversionPhysicalInputPlan{}, err
		}
		constants = append(constants, semanticplan.ConversionPhysicalPropertyPlan{Base: base, Conversion: conversion})
	}
	baseNode, ok := evaluationNodes[semantic.BaseMetric]
	if !ok {
		return semanticplan.ConversionPhysicalInputPlan{}, conversionValuePlanningError(semantic.BaseMetric, "conversion event metric is missing from metric evaluation plan", nil)
	}
	conversionNode, ok := evaluationNodes[semantic.ConversionMetric]
	if !ok {
		return semanticplan.ConversionPhysicalInputPlan{}, conversionValuePlanningError(semantic.ConversionMetric, "conversion event metric is missing from metric evaluation plan", nil)
	}
	baseValue, err := planConversionPhysicalEventValue(q, baseNode, semantic.BaseRoot)
	if err != nil {
		return semanticplan.ConversionPhysicalInputPlan{}, err
	}
	conversionValue, err := planConversionPhysicalEventValue(q, conversionNode, semantic.ConversionRoot)
	if err != nil {
		return semanticplan.ConversionPhysicalInputPlan{}, err
	}
	return semanticplan.ConversionPhysicalInputPlan{
		BaseDataset:        semanticplan.DatasetRef{Name: baseDataset.Name, Source: baseDataset.Source},
		ConversionDataset:  semanticplan.DatasetRef{Name: conversionDataset.Name, Source: conversionDataset.Source},
		BaseEventKey:       baseEventKey,
		ConversionEventKey: conversionEventKey,
		BaseTime:           baseTime,
		ConversionTime:     conversionTime,
		Entity:             semanticplan.ConversionPhysicalPropertyPlan{Base: baseEntity, Conversion: conversionEntity},
		ConstantProperties: constants,
		BaseValue:          baseValue,
		ConversionValue:    conversionValue,
	}, nil
}

func planConversionPhysicalEventValue(q *resolver.SemanticQuerySpec, node evaluation.MetricEvaluationNode, eventRoot string) (semanticplan.ConversionEventValuePlan, error) {
	if q == nil || q.Model == nil {
		return semanticplan.ConversionEventValuePlan{}, conversionValuePlanningError(node.ID, "resolved semantic query is required", nil)
	}
	if node.ID == "" || node.Metric == nil {
		return semanticplan.ConversionEventValuePlan{}, conversionValuePlanningError(node.ID, "conversion event metric evaluation node is incomplete", nil)
	}
	if !node.Expression.IsResolved() {
		return semanticplan.ConversionEventValuePlan{}, &serrors.Error{
			Code:    serrors.ErrUnsupportedExpression,
			Message: "conversion event metric has no compatible target expression",
			Details: map[string]any{"metric": node.ID},
		}
	}
	return planConversionEventValue(q, resolver.ResolvedMetric{
		Name:       node.ID,
		Metric:     semanticplan.CloneMetric(node.Metric),
		Expression: semanticplan.CloneResolvedExpression(node.Expression),
	}, eventRoot)
}
