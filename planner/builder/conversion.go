package builder

import (
	"fmt"

	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner/evaluation"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/serrors"
)

func planConversion(model *manifest.ModelIndex, owner string, spec ossie.ConversionMetricSpec, evaluationNodes map[string]evaluation.MetricEvaluationNode) (semanticplan.ConversionPlan, error) {
	if model == nil {
		return semanticplan.ConversionPlan{}, &serrors.Error{Code: serrors.ErrInternalInvariant, Message: "conversion model is required", Details: map[string]any{"metric": owner}}
	}
	baseNode, ok := evaluationNodes[spec.BaseMetric]
	if !ok {
		return semanticplan.ConversionPlan{}, conversionPlanningError(owner, "conversion base input is missing from metric evaluation plan", map[string]any{"input_metric": spec.BaseMetric})
	}
	conversionNode, ok := evaluationNodes[spec.ConversionMetric]
	if !ok {
		return semanticplan.ConversionPlan{}, conversionPlanningError(owner, "conversion conversion input is missing from metric evaluation plan", map[string]any{"input_metric": spec.ConversionMetric})
	}
	baseRoot, baseTime, err := resolveConversionInput(model, owner, baseNode)
	if err != nil {
		return semanticplan.ConversionPlan{}, err
	}
	conversionRoot, conversionTime, err := resolveConversionInput(model, owner, conversionNode)
	if err != nil {
		return semanticplan.ConversionPlan{}, err
	}
	baseEventKey, err := conversionEventKey(model, owner, "base", baseRoot)
	if err != nil {
		return semanticplan.ConversionPlan{}, err
	}
	conversionEventKey, err := conversionEventKey(model, owner, "conversion", conversionRoot)
	if err != nil {
		return semanticplan.ConversionPlan{}, err
	}
	baseEntity, err := resolveConversionField(model, owner, "base", baseRoot, spec.Entity.BaseProperty)
	if err != nil {
		return semanticplan.ConversionPlan{}, err
	}
	conversionEntity, err := resolveConversionField(model, owner, "conversion", conversionRoot, spec.Entity.ConversionProperty)
	if err != nil {
		return semanticplan.ConversionPlan{}, err
	}
	if err := requireCompatibleConversionFields(model, owner, "entity", baseEntity, conversionEntity); err != nil {
		return semanticplan.ConversionPlan{}, err
	}
	constants := make([]semanticplan.ConversionPropertyPlan, 0, len(spec.ConstantProperties))
	for i, pair := range spec.ConstantProperties {
		baseField, err := resolveConversionField(model, owner, "base", baseRoot, pair.BaseProperty)
		if err != nil {
			return semanticplan.ConversionPlan{}, err
		}
		conversionField, err := resolveConversionField(model, owner, "conversion", conversionRoot, pair.ConversionProperty)
		if err != nil {
			return semanticplan.ConversionPlan{}, err
		}
		if err := requireCompatibleConversionFields(model, owner, fmt.Sprintf("constant_properties[%d]", i), baseField, conversionField); err != nil {
			return semanticplan.ConversionPlan{}, err
		}
		constants = append(constants, semanticplan.ConversionPropertyPlan{Base: baseField, Conversion: conversionField})
	}
	var window *ossie.ConversionWindow
	if spec.Window != nil {
		copy := *spec.Window
		window = &copy
	}
	conversionEntityLink := semanticplan.ConversionPropertyPlan{Base: baseEntity, Conversion: conversionEntity}
	candidateMatch := planConversionCandidateMatch(baseEventKey, conversionEventKey, baseTime, conversionTime, conversionEntityLink, constants, window)
	return semanticplan.ConversionPlan{
		BaseMetric: spec.BaseMetric, ConversionMetric: spec.ConversionMetric,
		BaseRoot: baseRoot, ConversionRoot: conversionRoot,
		BaseEventKey: baseEventKey, ConversionEventKey: conversionEventKey,
		BaseTime: baseTime, ConversionTime: conversionTime,
		Entity:             conversionEntityLink,
		ConstantProperties: constants, Calculation: spec.Calculation, Window: window,
		Assignment:     semanticplan.ConversionAssignmentNearestPrecedingBase,
		CandidateMatch: candidateMatch,
	}, nil
}

func planConversionCandidateMatch(
	baseEventKey []semanticplan.ConversionFieldRef,
	conversionEventKey []semanticplan.ConversionFieldRef,
	baseTime semanticplan.ConversionFieldRef,
	conversionTime semanticplan.ConversionFieldRef,
	conversionEntity semanticplan.ConversionPropertyPlan,
	constants []semanticplan.ConversionPropertyPlan,
	window *ossie.ConversionWindow,
) semanticplan.ConversionCandidateMatchPlan {
	equality := make([]semanticplan.ConversionPropertyPlan, 0, 1+len(constants))
	equality = append(equality, conversionEntity)
	equality = append(equality, constants...)
	partition := append([]semanticplan.ConversionFieldRef(nil), conversionEventKey...)
	order := make([]semanticplan.ConversionCandidateOrder, 0, 1+len(baseEventKey))
	order = append(order, semanticplan.ConversionCandidateOrder{Field: baseTime, Direction: "desc"})
	for _, field := range baseEventKey {
		order = append(order, semanticplan.ConversionCandidateOrder{Field: field, Direction: "desc"})
	}
	var copiedWindow *ossie.ConversionWindow
	if window != nil {
		copy := *window
		copiedWindow = &copy
	}
	return semanticplan.ConversionCandidateMatchPlan{
		PartitionBy:                partition,
		Equality:                   equality,
		BaseTime:                   baseTime,
		ConversionTime:             conversionTime,
		Window:                     copiedWindow,
		OrderBy:                    order,
		KeepRank:                   1,
		AggregateBaseIndependently: true,
	}
}

func conversionEventKey(model *manifest.ModelIndex, owner, side, root string) ([]semanticplan.ConversionFieldRef, error) {
	dataset := model.Datasets[root]
	if dataset == nil {
		return nil, conversionPlanningError(owner, "conversion event dataset is not present in semantic model", map[string]any{"side": side, "root_dataset": root})
	}
	if len(dataset.PrimaryKey) == 0 {
		return nil, conversionPlanningError(owner, "conversion event root requires primary_key for stable event identity in v1", map[string]any{"side": side, "root_dataset": root})
	}
	key := make([]semanticplan.ConversionFieldRef, 0, len(dataset.PrimaryKey))
	for _, column := range dataset.PrimaryKey {
		handle := model.Fields[root+"."+column]
		if handle == nil || handle.Field == nil {
			return nil, conversionPlanningError(owner, "conversion event primary key field is not available", map[string]any{"side": side, "root_dataset": root, "field": column})
		}
		key = append(key, semanticplan.ConversionFieldRef{Dataset: root, Name: column})
	}
	return key, nil
}

func resolveConversionInput(model *manifest.ModelIndex, owner string, node evaluation.MetricEvaluationNode) (string, semanticplan.ConversionFieldRef, error) {
	metricName := node.ID
	if metricName == "" {
		return "", semanticplan.ConversionFieldRef{}, conversionPlanningError(owner, "conversion input metric evaluation node has no id", nil)
	}
	if node.Kind != evaluation.MetricEvaluationSource || len(node.Inputs) != 0 {
		return "", semanticplan.ConversionFieldRef{}, conversionPlanningError(owner, "conversion input must be a source event metric in v1", map[string]any{
			"input_metric": metricName,
			"kind":         node.Kind,
			"dependencies": metricEvaluationInputNames(node.Inputs),
		})
	}

	dependency, ok := model.MetricDependency(metricName)
	if !ok {
		return "", semanticplan.ConversionFieldRef{}, conversionPlanningError(owner, "conversion input source dependency is not indexed", map[string]any{"input_metric": metricName})
	}
	if len(dependency.DirectDatasets) != 1 {
		return "", semanticplan.ConversionFieldRef{}, conversionPlanningError(owner, "conversion input must read directly from exactly one event dataset in v1", map[string]any{"input_metric": metricName, "direct_datasets": append([]string(nil), dependency.DirectDatasets...)})
	}
	root := dependency.DirectDatasets[0]

	if node.TimeBinding == nil {
		return "", semanticplan.ConversionFieldRef{}, conversionPlanningError(owner, "conversion input requires a valid time binding", map[string]any{"input_metric": metricName})
	}
	if err := ossie.ValidateMetricTimeBinding(*node.TimeBinding); err != nil {
		return "", semanticplan.ConversionFieldRef{}, conversionPlanningError(owner, "conversion input requires a valid time binding", map[string]any{"input_metric": metricName, "cause": err.Error()})
	}
	timeField, err := resolveConversionField(model, owner, metricName+" time", root, node.TimeBinding.TimeDimension)
	if err != nil {
		return "", semanticplan.ConversionFieldRef{}, err
	}
	return root, timeField, nil
}

func resolveConversionField(model *manifest.ModelIndex, owner, side, root, name string) (semanticplan.ConversionFieldRef, error) {
	if handle, ok := model.Fields[root+"."+name]; ok && handle != nil && handle.Field != nil {
		return semanticplan.ConversionFieldRef{Dataset: root, Name: name}, nil
	}
	matches := make([]semanticplan.ConversionFieldRef, 0, 1)
	for _, handle := range model.Fields {
		if handle == nil || handle.Field == nil || handle.Field.Name != name || handle.Dataset == root {
			continue
		}
		if model.Graph != nil && model.Graph.CanReachDirected(root, handle.Dataset) {
			matches = append(matches, semanticplan.ConversionFieldRef{Dataset: handle.Dataset, Name: name})
		}
	}
	if len(matches) != 1 {
		return semanticplan.ConversionFieldRef{}, conversionPlanningError(owner, "conversion property is not uniquely reachable from its event root", map[string]any{"side": side, "root_dataset": root, "property": name, "matches": len(matches)})
	}
	return matches[0], nil
}

func requireCompatibleConversionFields(model *manifest.ModelIndex, owner, label string, base, conversion semanticplan.ConversionFieldRef) error {
	baseHandle := model.Fields[base.Dataset+"."+base.Name]
	conversionHandle := model.Fields[conversion.Dataset+"."+conversion.Name]
	if baseHandle == nil || baseHandle.Field == nil || conversionHandle == nil || conversionHandle.Field == nil {
		return conversionPlanningError(owner, "conversion link field metadata is unavailable", map[string]any{"link": label})
	}
	if baseHandle.Field.Datatype != "" && conversionHandle.Field.Datatype != "" && baseHandle.Field.Datatype != conversionHandle.Field.Datatype {
		return conversionPlanningError(owner, "conversion link fields must have compatible datatypes", map[string]any{"link": label, "base_datatype": baseHandle.Field.Datatype, "conversion_datatype": conversionHandle.Field.Datatype})
	}
	return nil
}

func conversionPlanningError(metric, message string, details map[string]any) error {
	if details == nil {
		details = map[string]any{}
	}
	details["metric"] = metric
	return &serrors.Error{Code: serrors.ErrInvalidConversionMetric, Message: message, Details: details}
}
