package semanticplan

import "fmt"

// CloneNodes deep-clones the canonical typed DAG directly.
func CloneNodes(nodes []SemanticPlanNode) ([]SemanticPlanNode, error) {
	if len(nodes) == 0 {
		return nil, nil
	}
	out := make([]SemanticPlanNode, 0, len(nodes))
	for _, node := range nodes {
		if err := ValidateNode(node); err != nil {
			return nil, err
		}
		base := CloneNodeBase(node.NodeBase())
		switch node := node.(type) {
		case SourceAggregateNode:
			node.Base, node.Source, node.MetricState = base, CloneSourceState(node.Source), CloneMetricState(node.MetricState)
			node.Metric, node.Expression = cloneMetric(node.Metric), cloneResolvedExpression(node.Expression)
			out = append(out, node)
		case PostAggregateNode:
			node.Base, node.Source, node.MetricState = base, cloneSemanticSourceState(node.Source), cloneSemanticMetricState(node.MetricState)
			node.Metric, node.Expression = cloneMetric(node.Metric), cloneResolvedExpression(node.Expression)
			out = append(out, node)
		case JoinAggregatesNode:
			node.Base, node.Source, node.MetricState = base, cloneSemanticSourceState(node.Source), cloneSemanticMetricState(node.MetricState)
			node.Metric, node.Expression = cloneMetric(node.Metric), cloneResolvedExpression(node.Expression)
			out = append(out, node)
		case CrossJoinAggregatesNode:
			node.Base, node.Source, node.MetricState = base, cloneSemanticSourceState(node.Source), cloneSemanticMetricState(node.MetricState)
			node.Metric, node.Expression = cloneMetric(node.Metric), cloneResolvedExpression(node.Expression)
			out = append(out, node)
		case CumulativeWindowNode:
			node.Base, node.Source, node.MetricState = base, cloneSemanticSourceState(node.Source), cloneSemanticMetricState(node.MetricState)
			node.Metric, node.Expression = cloneMetric(node.Metric), cloneResolvedExpression(node.Expression)
			node.CustomCalendarRolling = cloneCustomCalendarCumulativePlan(node.CustomCalendarRolling)
			node.CustomCalendarGrainToDate = cloneCustomCalendarGrainToDatePlan(node.CustomCalendarGrainToDate)
			out = append(out, node)
		case TimeOffsetNode:
			node.Base, node.Source, node.MetricState = base, cloneSemanticSourceState(node.Source), cloneSemanticMetricState(node.MetricState)
			node.Metric, node.Expression = cloneMetric(node.Metric), cloneResolvedExpression(node.Expression)
			node.CustomCalendar = cloneCustomCalendarOffsetPlan(node.CustomCalendar)
			out = append(out, node)
		case OffsetToGrainNode:
			node.Base, node.Source, node.MetricState = base, cloneSemanticSourceState(node.Source), cloneSemanticMetricState(node.MetricState)
			node.Metric, node.Expression = cloneMetric(node.Metric), cloneResolvedExpression(node.Expression)
			node.OffsetPlan = cloneOffsetToGrainPlan(node.OffsetPlan)
			out = append(out, node)
		case ConversionNode:
			node.Base, node.Source, node.MetricState = base, cloneSemanticSourceState(node.Source), cloneSemanticMetricState(node.MetricState)
			node.Metric, node.Expression = cloneMetric(node.Metric), cloneResolvedExpression(node.Expression)
			node.Spec, node.Conversion, node.PhysicalInputs = cloneConversionMetricSpec(node.Spec), cloneConversionPlan(node.Conversion), cloneConversionPhysicalInputPlan(node.PhysicalInputs)
			out = append(out, node)
		case SemiAdditiveNode:
			node.Base, node.Source, node.MetricState = base, cloneSemanticSourceState(node.Source), cloneSemanticMetricState(node.MetricState)
			node.Metric, node.Expression, node.Spec = cloneMetric(node.Metric), cloneResolvedExpression(node.Expression), cloneSemiAdditiveMetricSpec(node.Spec)
			out = append(out, node)
		case SourceSelectionNode:
			node.Base, node.Source = base, CloneSourceState(node.Source)
			out = append(out, node)
		case AdditiveAttributionNode:
			node.Base, node.MetricState = base, CloneMetricState(node.MetricState)
			out = append(out, node)
		case RatioAttributionNode:
			node.Base, node.MetricState = base, cloneSemanticMetricState(node.MetricState)
			out = append(out, node)
		default:
			return nil, fmt.Errorf("unsupported semantic plan node %T", node)
		}
	}
	return out, nil
}

func CloneNodeBase(in SemanticPlanNodeBase) SemanticPlanNodeBase {
	in.Inputs = append([]SemanticPlanNodeInput(nil), in.Inputs...)
	for i := range in.Inputs {
		in.Inputs[i].Grain = append([]GroupBy(nil), in.Inputs[i].Grain...)
	}
	in.Dimensions = append([]string(nil), in.Dimensions...)
	in.OutputGrain = append([]GroupBy(nil), in.OutputGrain...)
	in.Predicates = append([]SemanticPlanNodePredicate(nil), in.Predicates...)
	for i := range in.Predicates {
		if in.Predicates[i].Predicate != nil {
			predicate := *in.Predicates[i].Predicate
			in.Predicates[i].Predicate = &predicate
		}
		if in.Predicates[i].Post != nil {
			post := *in.Predicates[i].Post
			in.Predicates[i].Post = &post
		}
	}
	in.ExtensionEvidence = append(in.ExtensionEvidence[:0:0], in.ExtensionEvidence...)
	return in
}

func CloneSourceState(in SemanticSourceState) SemanticSourceState {
	in.SourceRoots = append([]string(nil), in.SourceRoots...)
	in.RequiredDatasets = append([]string(nil), in.RequiredDatasets...)
	in.Joins = append([]Join(nil), in.Joins...)
	in.PopulationPreservationEvidence = ClonePopulationPreservationEvidence(in.PopulationPreservationEvidence)
	return in
}

func ClonePopulationPreservationEvidence(in []PopulationPreservationEvidence) []PopulationPreservationEvidence {
	out := append([]PopulationPreservationEvidence(nil), in...)
	for i := range out {
		out[i].Obligations = append([]PopulationPreservationObligationEvidence(nil), in[i].Obligations...)
		if in[i].Admission != nil {
			admission := *in[i].Admission
			out[i].Admission = &admission
		}
	}
	return out
}

func CloneMetricState(in SemanticMetricState) SemanticMetricState {
	in.Metrics = append([]string(nil), in.Metrics...)
	if in.SharedGrainEvidence != nil {
		shared := CloneMetricSharedGrainEvidence(*in.SharedGrainEvidence)
		in.SharedGrainEvidence = &shared
	}
	return in
}

func NodeBaseID(node SemanticPlanNode) string {
	if node == nil {
		return ""
	}
	return node.NodeBase().ID
}

func cloneSemanticSourceState(in SemanticSourceState) SemanticSourceState {
	return CloneSourceState(in)
}

func cloneSemanticMetricState(in SemanticMetricState) SemanticMetricState {
	return CloneMetricState(in)
}
