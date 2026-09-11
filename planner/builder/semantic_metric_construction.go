package builder

import (
	"github.com/meaningforge/metis/planner/evaluation"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/resolver"
	"github.com/meaningforge/metis/serrors"
)

// lowerMetricEvaluationPlan is the explicit evaluation.MetricEvaluationPlan ->
// semanticplan.SemanticPlanNode lowering seam. Metric identity, dependency edges, stable
// evaluation kind, typed metric semantics, canonical time binding, and resolved
// metric expressions come only from evaluationPlan. The resolved query/model is
// consulted below this seam only for source-aware facts that semanticplan.SemanticPlan owns:
// datasets, relationships, fields, source roots, grains, and predicate placement.
//
// Keeping this as a named lowering boundary is deliberate: callers can no
// longer confuse building/validating metric semantics with source-aware semantic
// graph construction, and later cutover work can gate all metric-bearing paths on
// this one authority transfer.
func lowerMetricEvaluationPlan(q *resolver.SemanticQuerySpec, evaluationPlan *evaluation.MetricEvaluationPlan, metricEvaluationRequired bool, groups []semanticplan.GroupBy, predicates []semanticplan.Predicate) (ConstructionInput, error) {
	if q == nil || q.Model == nil {
		return ConstructionInput{}, &serrors.Error{Code: serrors.ErrInternalInvariant, Message: "resolved query and model are required at metric evaluation lowering"}
	}
	if evaluationPlan == nil {
		if metricEvaluationRequired {
			return ConstructionInput{}, &serrors.Error{Code: serrors.ErrInternalInvariant, Message: "metric-bearing query reached semantic lowering without a evaluation.MetricEvaluationPlan"}
		}
		return ConstructionInput{}, nil
	}
	if err := evaluation.ValidateMetricEvaluationPlan(evaluationPlan); err != nil {
		return ConstructionInput{}, &serrors.Error{Code: serrors.ErrInternalInvariant, Message: "metric evaluation plan validation failed at semantic lowering", Details: map[string]any{"cause": err.Error()}}
	}

	preAggregationPredicates, postEvaluationPredicates, err := SplitEvaluationPredicates(evaluationPlan, groups, predicates)
	if err != nil {
		return ConstructionInput{}, err
	}

	input := ConstructionInput{
		OutputGrain:              cloneGroups(groups),
		PostEvaluationPredicates: append([]semanticplan.PostEvaluationPredicate(nil), postEvaluationPredicates...),
		SourceRequirements:       map[string]SourceRequirement{},
	}

	requestedSet := map[string]struct{}{}
	appendRequested := func(name string) {
		if name == "" {
			return
		}
		if _, ok := requestedSet[name]; ok {
			return
		}
		requestedSet[name] = struct{}{}
		input.Requested = append(input.Requested, name)
	}
	for _, metric := range q.Metrics {
		appendRequested(metric.Name)
	}
	for _, filter := range q.Filters {
		if filter.Kind == resolver.FilterTargetMetric {
			appendRequested(filter.Filter.Field)
		}
	}

	conversionSideInputs := map[string]struct{}{}
	evaluationNodes := make(map[string]evaluation.MetricEvaluationNode, len(evaluationPlan.Nodes))
	for _, node := range evaluationPlan.Nodes {
		evaluationNodes[node.ID] = node
		if node.Kind == evaluation.MetricEvaluationConversion && node.Spec.Conversion != nil {
			conversionSideInputs[node.Spec.Conversion.Spec.ConversionMetric] = struct{}{}
		}
	}

	byName := make(map[string]semanticplan.SemanticPlanNode, len(evaluationPlan.Nodes))
	for _, node := range evaluationPlan.Nodes {
		dependency, ok := q.Model.MetricDependency(node.ID)
		if !ok {
			return ConstructionInput{}, &serrors.Error{Code: serrors.ErrInternalInvariant, Message: "metric dependency is not indexed", Details: map[string]any{"metric": node.ID}}
		}

		// SemanticManifest dependency analysis remains available here only for source-aware
		// dataset/reference facts. Metric-to-metric edges come exclusively from
		// evaluation.MetricEvaluationPlan and are never copied across this seam.
		var sourceRequirement SourceRequirement
		if node.Kind == evaluation.MetricEvaluationSource {
			planned, err := BuildMetricSourceRequirement(q.Model, node.ID, dependency)
			if err != nil {
				return ConstructionInput{}, err
			}
			sourceRequirement = planned
			input.SourceRequirements[node.ID] = planned
		}

		dependencies := metricEvaluationInputNames(node.Inputs)
		base := semanticplan.SemanticPlanNodeBase{
			ID:                node.ID,
			OutputGrain:       cloneGroups(groups),
			ExtensionEvidence: node.Expression.ExtensionEvidenceValues(),
		}
		source := semanticplan.SemanticSourceState{RequiredDatasets: append([]string(nil), dependency.Datasets...)}
		metricState := semanticplan.SemanticMetricState{Metrics: []string{node.ID}}
		metric := semanticplan.CloneMetric(node.Metric)
		resolvedExpression := semanticplan.CloneResolvedExpression(node.Expression)

		var offsetBoundary *semanticplan.OffsetToGrainPlan
		var conversionPlan *semanticplan.ConversionPlan
		var conversionPhysicalInputs *semanticplan.ConversionPhysicalInputPlan

		switch node.Kind {
		case evaluation.MetricEvaluationTimeOffset:
			if err := ValidateTimeOffsetQueryGrain(node.ID, node.Spec.TimeOffset.Spec, groups); err != nil {
				return ConstructionInput{}, err
			}
		case evaluation.MetricEvaluationOffsetToGrain:
			boundary, err := PlanOffsetToGrain(node.ID, node.Spec.OffsetToGrain.Spec, groups)
			if err != nil {
				return ConstructionInput{}, err
			}
			offsetBoundary = &boundary
		case evaluation.MetricEvaluationConversion:
			planned, err := planConversion(q.Model, node.ID, node.Spec.Conversion.Spec, evaluationNodes)
			if err != nil {
				return ConstructionInput{}, err
			}
			conversionPlan = &planned
			physicalInputs, err := planConversionPhysicalInputs(q, planned, evaluationNodes)
			if err != nil {
				return ConstructionInput{}, err
			}
			conversionPhysicalInputs = &physicalInputs
		}

		var semanticKind semanticplan.SemanticPlanNodeKind
		if node.Kind == evaluation.MetricEvaluationSource {
			semanticKind = semanticplan.SemanticPlanNodeSourceAggregate
			sourceGroups := groups
			sourcePredicates := preAggregationPredicates
			if _, conversionSide := conversionSideInputs[node.ID]; conversionSide {
				sourceGroups = nil
				sourcePredicates = nil
			}
			base.OutputGrain = cloneGroups(sourceGroups)
			root, joins, required, populationEvidence, err := PlanSourceEvaluation(q.Model, node.ID, node.Expression, sourceRequirement, sourceGroups, sourcePredicates)
			if err != nil {
				return ConstructionInput{}, err
			}
			source.SourceRoots = []string{root}
			dataset := q.Model.Datasets[root]
			source.Root = semanticplan.DatasetRef{Name: dataset.Name, Source: dataset.Source}
			source.Joins = append([]semanticplan.Join(nil), joins...)
			source.RequiredDatasets = append([]string(nil), required...)
			source.PopulationPreservationEvidence = semanticplan.ClonePopulationPreservationEvidence(populationEvidence)
			for _, predicate := range sourcePredicates {
				owned := predicate
				base.Predicates = append(base.Predicates, semanticplan.SemanticPlanNodePredicate{
					Scope:       semanticplan.SemanticPredicatePreAggregation,
					OwnerNodeID: base.ID,
					Proof:       semanticplan.SemanticPredicateProofSourceOwnership,
					Predicate:   &owned,
				})
			}
		} else {
			rootSet := map[string]struct{}{}
			for _, dependencyName := range dependencies {
				inputNode, ok := byName[dependencyName]
				if !ok {
					return ConstructionInput{}, &serrors.Error{Code: serrors.ErrInternalInvariant, Message: "metric evaluation dependency is not ordered before its consumer", Details: map[string]any{"metric": node.ID, "dependency": dependencyName}}
				}
				inputBase := inputNode.NodeBase()
				base.Inputs = append(base.Inputs, semanticplan.SemanticPlanNodeInput{NodeID: dependencyName, Grain: cloneGroups(inputBase.OutputGrain)})
				inputSource, _ := semanticplan.NodeSourceState(inputNode)
				for _, root := range inputSource.SourceRoots {
					rootSet[root] = struct{}{}
				}
			}
			source.SourceRoots = sortedSet(rootSet)

			switch node.Kind {
			case evaluation.MetricEvaluationDerived:
				switch {
				case len(source.SourceRoots) <= 1:
					semanticKind = semanticplan.SemanticPlanNodePostAggregate
				case len(groups) == 0:
					semanticKind = semanticplan.SemanticPlanNodeCrossJoinAggregates
				default:
					semanticKind = semanticplan.SemanticPlanNodeJoinAggregates
				}
			case evaluation.MetricEvaluationCumulative:
				semanticKind = semanticplan.SemanticPlanNodeCumulativeWindow
			case evaluation.MetricEvaluationTimeOffset:
				semanticKind = semanticplan.SemanticPlanNodeTimeOffset
			case evaluation.MetricEvaluationOffsetToGrain:
				semanticKind = semanticplan.SemanticPlanNodeOffsetToGrain
			case evaluation.MetricEvaluationConversion:
				semanticKind = semanticplan.SemanticPlanNodeConversion
			case evaluation.MetricEvaluationSemiAdditive:
				switch node.Spec.SemiAdditive.Spec.Aggregation {
				case "last":
					semanticKind = semanticplan.SemanticPlanNodeSemiAdditiveLast
				case "first":
					semanticKind = semanticplan.SemanticPlanNodeSemiAdditiveFirst
				default:
					_, kindErr := semiAdditiveEvaluationKind(node.ID, node.Spec.SemiAdditive.Spec.Aggregation)
					return ConstructionInput{}, kindErr
				}
			default:
				return ConstructionInput{}, &serrors.Error{Code: serrors.ErrInternalInvariant, Message: "unsupported metric evaluation kind during semantic lowering", Details: map[string]any{"metric": node.ID, "kind": node.Kind}}
			}
		}

		for _, group := range base.OutputGrain {
			base.Dimensions = append(base.Dimensions, group.Name)
		}
		boundary := semanticplan.NodeBoundaryForKind(semanticKind)
		base.Boundary = boundary
		base.PredicateBoundaryEvidence = semanticplan.PredicateBoundaryEvidence(boundary)

		var semanticNode semanticplan.SemanticPlanNode
		switch node.Kind {
		case evaluation.MetricEvaluationSource:
			semanticNode = semanticplan.SourceAggregateNode{Base: base, Source: source, MetricState: metricState, Metric: metric, Expression: resolvedExpression, Rollup: sourceRollupContract(q.Model, node.ID, node.Expression.SourceDialect)}
		case evaluation.MetricEvaluationDerived:
			switch semanticKind {
			case semanticplan.SemanticPlanNodePostAggregate:
				semanticNode = semanticplan.PostAggregateNode{Base: base, Source: source, MetricState: metricState, Metric: metric, Expression: resolvedExpression}
			case semanticplan.SemanticPlanNodeCrossJoinAggregates:
				semanticNode = semanticplan.CrossJoinAggregatesNode{Base: base, Source: source, MetricState: metricState, Metric: metric, Expression: resolvedExpression}
			default:
				semanticNode = semanticplan.JoinAggregatesNode{Base: base, Source: source, MetricState: metricState, Metric: metric, Expression: resolvedExpression}
			}
		case evaluation.MetricEvaluationCumulative:
			semanticNode = semanticplan.CumulativeWindowNode{Base: base, Source: source, MetricState: metricState, Metric: metric, Expression: resolvedExpression, Spec: node.Spec.Cumulative.Spec}
		case evaluation.MetricEvaluationTimeOffset:
			semanticNode = semanticplan.TimeOffsetNode{Base: base, Source: source, MetricState: metricState, Metric: metric, Expression: resolvedExpression, Spec: node.Spec.TimeOffset.Spec}
		case evaluation.MetricEvaluationOffsetToGrain:
			semanticNode = semanticplan.OffsetToGrainNode{Base: base, Source: source, MetricState: metricState, Metric: metric, Expression: resolvedExpression, Spec: node.Spec.OffsetToGrain.Spec, OffsetPlan: semanticplan.CloneOffsetToGrainPlan(offsetBoundary)}
		case evaluation.MetricEvaluationConversion:
			semanticNode = semanticplan.ConversionNode{Base: base, Source: source, MetricState: metricState, Metric: metric, Expression: resolvedExpression, Spec: semanticplan.CloneConversionMetricSpec(node.Spec.Conversion.Spec), Conversion: semanticplan.CloneConversionPlan(conversionPlan), PhysicalInputs: semanticplan.CloneConversionPhysicalInputPlan(conversionPhysicalInputs)}
		case evaluation.MetricEvaluationSemiAdditive:
			semanticNode = semanticplan.SemiAdditiveNode{Base: base, Source: source, MetricState: metricState, Metric: metric, Expression: resolvedExpression, Spec: semanticplan.CloneSemiAdditiveMetricSpec(node.Spec.SemiAdditive.Spec)}
		}
		if semanticNode == nil || semanticNode.Kind() != semanticKind {
			return ConstructionInput{}, &serrors.Error{Code: serrors.ErrInternalInvariant, Message: "semantic plan node construction produced an unexpected kind", Details: map[string]any{"metric": node.ID, "kind": semanticKind}}
		}
		input.Nodes = append(input.Nodes, semanticNode)
		byName[node.ID] = semanticNode
	}

	return input, nil
}

func metricEvaluationInputNames(inputs []evaluation.MetricEvaluationInput) []string {
	out := make([]string, len(inputs))
	for i, input := range inputs {
		out[i] = input.Metric
	}
	return out
}
