package builder

import (
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/resolver"
	"github.com/meaningforge/metis/serrors"
)

func applySemanticMetricDefinitionFilters(input *ConstructionInput, q *resolver.SemanticQuerySpec) error {
	if input == nil || q == nil || q.Model == nil {
		return nil
	}
	for i, node := range input.Nodes {
		base := node.NodeBase()
		spec, ok, err := ossie.MetricDefinitionFilter(semanticplan.NodeMetric(node))
		if err != nil {
			return DefinitionFilterPlanningError(base.ID, "invalid metric definition-filter extension", err)
		}
		if !ok {
			continue
		}
		switch ossie.EffectiveMetricDefinitionFilterStage(spec) {
		case ossie.MetricDefinitionFilterStagePostAggregation:
			for _, filter := range spec.Filters {
				input.PostEvaluationPredicates = append(input.PostEvaluationPredicates, semanticplan.PostEvaluationPredicate{Name: base.ID, Filter: filter})
			}
			continue
		case ossie.MetricDefinitionFilterStagePreAggregation:
			if _, ok := node.(semanticplan.SourceAggregateNode); !ok {
				return DefinitionFilterPlanningError(base.ID, "pre-aggregation metric definition filters require a source metric", nil)
			}
		default:
			return DefinitionFilterPlanningError(base.ID, "unsupported metric definition-filter stage", nil)
		}
		for _, filter := range spec.Filters {
			handle, err := ResolveDefinitionFilterField(q.Model, filter.Field)
			if err != nil {
				return DefinitionFilterPlanningError(base.ID, "metric definition-filter field cannot be resolved", err)
			}
			resolvedExpression, ok := q.FieldExpression(handle.Dataset, handle.Field.Name)
			if !ok {
				return DefinitionFilterPlanningError(base.ID, "metric definition-filter field has no compatible target expression", nil)
			}
			predicate := semanticplan.Predicate{Filter: filter, Dataset: handle.Dataset, Field: handle.Field, Expression: resolvedExpression}
			base.Predicates = append(base.Predicates, semanticplan.SemanticPlanNodePredicate{
				Scope:       semanticplan.SemanticPredicatePreAggregation,
				OwnerNodeID: base.ID,
				Proof:       semanticplan.SemanticPredicateProofSourceOwnership,
				Predicate:   &predicate,
			})
		}
		updated, err := semanticplan.WithNodeBase(node, base)
		if err != nil {
			return err
		}
		updated, err = refreshSemanticDefinitionFilterSource(q, input, updated)
		if err != nil {
			return err
		}
		input.Nodes[i] = updated
	}
	return nil
}

func refreshSemanticDefinitionFilterSource(q *resolver.SemanticQuerySpec, input *ConstructionInput, node semanticplan.SemanticPlanNode) (semanticplan.SemanticPlanNode, error) {
	base := node.NodeBase()
	requirement, err := constructedMetricSourceRequirement(input, base.ID)
	if err != nil {
		return nil, DefinitionFilterPlanningError(base.ID, "metric source requirement is not indexed", err)
	}
	predicates := semanticPlanNodePreAggregationPredicates(node)
	_, resolved, ok := semanticplan.NodeMetricExpression(node)
	if !ok {
		return nil, DefinitionFilterPlanningError(base.ID, "metric expression is not available", nil)
	}
	root, joins, required, populationEvidence, err := PlanSourceEvaluation(q.Model, base.ID, resolved, requirement, base.OutputGrain, predicates)
	if err != nil {
		return nil, err
	}
	dataset := q.Model.Datasets[root]
	if dataset == nil {
		return nil, DefinitionFilterPlanningError(base.ID, "metric definition-filter source root dataset is missing", nil)
	}
	source, ok := semanticplan.NodeSourceState(node)
	if !ok {
		return nil, DefinitionFilterPlanningError(base.ID, "metric definition-filter node has no source state", nil)
	}
	source.SourceRoots = []string{root}
	source.Root = semanticplan.DatasetRef{Name: dataset.Name, Source: dataset.Source}
	source.Joins = append([]semanticplan.Join(nil), joins...)
	source.RequiredDatasets = append([]string(nil), required...)
	source.PopulationPreservationEvidence = semanticplan.ClonePopulationPreservationEvidence(populationEvidence)
	return semanticplan.WithNodeSourceState(node, source)
}

func semanticPlanNodePreAggregationPredicates(node semanticplan.SemanticPlanNode) []semanticplan.Predicate {
	var out []semanticplan.Predicate
	for _, predicate := range node.NodeBase().Predicates {
		if predicate.Scope == semanticplan.SemanticPredicatePreAggregation && predicate.Predicate != nil {
			out = append(out, *predicate.Predicate)
		}
	}
	return out
}

func applySemanticSemiAdditiveInputGrains(input *ConstructionInput, q *resolver.SemanticQuerySpec) error {
	if input == nil || q == nil || q.Model == nil {
		return nil
	}
	privateBaseMetrics, err := isolateVisibleSemanticSemiAdditiveBases(input)
	if err != nil {
		return err
	}
	byName := semanticPlanNodeIndexes(input.Nodes)
	for i, node := range input.Nodes {
		semiAdditive, ok := node.(semanticplan.SemiAdditiveNode)
		if !ok {
			continue
		}
		spec := &semiAdditive.Spec
		if err := requireSemiAdditiveEvaluationKind(semiAdditive, spec.Aggregation); err != nil {
			return err
		}
		baseIndex, ok := byName[spec.BaseMetric]
		if !ok {
			return semiAdditivePlanningError(semiAdditive.Base.ID, "semi-additive base metric evaluation node is missing")
		}
		base, source := input.Nodes[baseIndex].(semanticplan.SourceAggregateNode)
		if !source {
			for _, groupingName := range spec.WindowGroupings {
				if _, err := hiddenWindowGrouping(q, groupingName); err != nil {
					return err
				}
			}
			continue
		}

		group, err := hiddenSemanticGroup(q, spec.NonAdditiveDimension, "non-additive time dimension")
		if err != nil {
			return err
		}
		if queriedSemiAdditiveTimeWindow(semiAdditive.Base.OutputGrain, spec.NonAdditiveDimension) {
			group.Name = semiAdditiveRawOrderName(group.Field.Name)
			group.Grain = nil
			group.CustomCalendar = nil
		}
		if !containsGroup(base.Base.OutputGrain, group.Name) {
			base.Base.OutputGrain = append(base.Base.OutputGrain, group)
		}
		if spec.TieBreakDimension != "" {
			tieBreak, err := hiddenSemanticGroup(q, spec.TieBreakDimension, "tie-break dimension")
			if err != nil {
				return err
			}
			if !containsGroup(base.Base.OutputGrain, tieBreak.Name) {
				base.Base.OutputGrain = append(base.Base.OutputGrain, tieBreak)
			}
		}
		for _, groupingName := range spec.WindowGroupings {
			windowGrouping, err := hiddenWindowGrouping(q, groupingName)
			if err != nil {
				return err
			}
			if !containsGroup(base.Base.OutputGrain, windowGrouping.Name) {
				base.Base.OutputGrain = append(base.Base.OutputGrain, windowGrouping)
			}
		}
		semanticMetric := base.Base.ID
		if original, ok := privateBaseMetrics[base.Base.ID]; ok {
			semanticMetric = original
		}
		base, err = refreshSemanticSemiAdditiveBaseSource(q, input, base, semanticMetric)
		if err != nil {
			return err
		}
		refreshSemanticPlanNodeDimensions(&base.Base)
		input.Nodes[baseIndex] = base
		input.Nodes[i] = semiAdditive
	}
	return refreshSemanticPlanNodeInputGrains(input)
}

func isolateVisibleSemanticSemiAdditiveBases(input *ConstructionInput) (map[string]string, error) {
	private := map[string]string{}
	if input == nil || len(input.Nodes) == 0 {
		return private, nil
	}
	requested := make(map[string]struct{}, len(input.Requested))
	for _, name := range input.Requested {
		requested[name] = struct{}{}
	}
	byName := semanticplan.NodesByID(input.Nodes)
	out := make([]semanticplan.SemanticPlanNode, 0, len(input.Nodes)+1)
	for _, node := range input.Nodes {
		semiAdditive, isSemiAdditive := node.(semanticplan.SemiAdditiveNode)
		if isSemiAdditive {
			baseName := semiAdditive.Spec.BaseMetric
			base, ok := byName[baseName]
			_, visible := requested[baseName]
			baseNode, source := base.(semanticplan.SourceAggregateNode)
			if ok && visible && source {
				clones, err := semanticplan.CloneNodes([]semanticplan.SemanticPlanNode{baseNode})
				if err != nil {
					return nil, err
				}
				clone := clones[0].(semanticplan.SourceAggregateNode)
				clone.Base.ID = semiAdditivePrivateBaseName(semiAdditive.Base.ID, baseName)
				clone.MetricState.Metrics = []string{clone.Base.ID}
				clone.MetricState.ShareGroup = ""
				private[clone.Base.ID] = baseName
				out = append(out, clone)

				spec := semanticplan.CloneSemiAdditiveMetricSpec(semiAdditive.Spec)
				spec.BaseMetric = clone.Base.ID
				semiAdditive.Spec = spec
				for i := range semiAdditive.Base.Inputs {
					if semiAdditive.Base.Inputs[i].NodeID == baseName {
						semiAdditive.Base.Inputs[i].NodeID = clone.Base.ID
					}
				}
				node = semiAdditive
			}
		}
		out = append(out, node)
	}
	input.Nodes = out
	return private, nil
}

func refreshSemanticSemiAdditiveBaseSource(q *resolver.SemanticQuerySpec, input *ConstructionInput, base semanticplan.SourceAggregateNode, metricName string) (semanticplan.SourceAggregateNode, error) {
	requirement, err := constructedMetricSourceRequirement(input, metricName)
	if err != nil {
		return semanticplan.SourceAggregateNode{}, semiAdditivePlanningError(metricName, "semi-additive base metric source requirement is not indexed")
	}
	root, joins, required, populationEvidence, err := PlanSourceEvaluation(q.Model, metricName, base.Expression, requirement, base.Base.OutputGrain, semanticPlanNodePreAggregationPredicates(base))
	if err != nil {
		return semanticplan.SourceAggregateNode{}, err
	}
	dataset := q.Model.Datasets[root]
	if dataset == nil {
		return semanticplan.SourceAggregateNode{}, semiAdditivePlanningError(metricName, "semi-additive base metric root dataset is missing")
	}
	base.Source.SourceRoots = []string{root}
	base.Source.Root = semanticplan.DatasetRef{Name: dataset.Name, Source: dataset.Source}
	base.Source.Joins = append([]semanticplan.Join(nil), joins...)
	base.Source.RequiredDatasets = append([]string(nil), required...)
	base.Source.PopulationPreservationEvidence = semanticplan.ClonePopulationPreservationEvidence(populationEvidence)
	return base, nil
}

func semanticPlanNodeIndexes(nodes []semanticplan.SemanticPlanNode) map[string]int {
	out := make(map[string]int, len(nodes))
	for i, node := range nodes {
		out[node.NodeBase().ID] = i
	}
	return out
}

func refreshSemanticPlanNodeInputGrains(input *ConstructionInput) error {
	if input == nil {
		return nil
	}
	byName := semanticPlanNodeIndexes(input.Nodes)
	for i, node := range input.Nodes {
		base := node.NodeBase()
		for j := range base.Inputs {
			dependency, ok := byName[base.Inputs[j].NodeID]
			if !ok {
				continue
			}
			base.Inputs[j].Grain = cloneGroups(input.Nodes[dependency].NodeBase().OutputGrain)
		}
		updated, err := semanticplan.WithNodeBase(node, base)
		if err != nil {
			return err
		}
		input.Nodes[i] = updated
	}
	return nil
}

func refreshSemanticPlanNodeDimensions(base *semanticplan.SemanticPlanNodeBase) {
	base.Dimensions = base.Dimensions[:0]
	for _, group := range base.OutputGrain {
		base.Dimensions = append(base.Dimensions, group.Name)
	}
}

// constructedMetricSourceRequirement returns the source-only facts captured for
// one source metric during evaluation.MetricEvaluationPlan -> semanticplan.SemanticPlan construction.
// It cannot answer metric dependency questions by construction: those facts are
// absent from the type and remain owned by evaluation.MetricEvaluationPlan.
func constructedMetricSourceRequirement(input *ConstructionInput, metric string) (SourceRequirement, error) {
	if input != nil {
		if requirement, ok := input.SourceRequirements[metric]; ok {
			requirement.Datasets = append([]string(nil), requirement.Datasets...)
			return requirement, nil
		}
	}
	return SourceRequirement{}, &serrors.Error{
		Code:    serrors.ErrInternalInvariant,
		Message: "metric source requirement was not resolved during semantic construction",
		Details: map[string]any{"metric": metric},
	}
}
