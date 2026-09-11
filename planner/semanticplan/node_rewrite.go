package semanticplan

import "fmt"

func NodesByID(nodes []SemanticPlanNode) map[string]SemanticPlanNode {
	out := make(map[string]SemanticPlanNode, len(nodes))
	for _, node := range nodes {
		if node != nil {
			out[node.NodeBase().ID] = node
		}
	}
	return out
}

func WithNodeBase(node SemanticPlanNode, base SemanticPlanNodeBase) (SemanticPlanNode, error) {
	switch value := node.(type) {
	case SourceAggregateNode:
		value.Base = base
		return value, nil
	case PostAggregateNode:
		value.Base = base
		return value, nil
	case JoinAggregatesNode:
		value.Base = base
		return value, nil
	case CrossJoinAggregatesNode:
		value.Base = base
		return value, nil
	case CumulativeWindowNode:
		value.Base = base
		return value, nil
	case TimeOffsetNode:
		value.Base = base
		return value, nil
	case OffsetToGrainNode:
		value.Base = base
		return value, nil
	case ConversionNode:
		value.Base = base
		return value, nil
	case SemiAdditiveNode:
		value.Base = base
		return value, nil
	case SourceSelectionNode:
		value.Base = base
		return value, nil
	case AdditiveAttributionNode:
		value.Base = base
		return value, nil
	case RatioAttributionNode:
		value.Base = base
		return value, nil
	default:
		return nil, fmt.Errorf("unsupported semantic plan node %T", node)
	}
}

func NodeSourceState(node SemanticPlanNode) (SemanticSourceState, bool) {
	switch value := node.(type) {
	case SourceAggregateNode:
		return value.Source, true
	case PostAggregateNode:
		return value.Source, true
	case JoinAggregatesNode:
		return value.Source, true
	case CrossJoinAggregatesNode:
		return value.Source, true
	case CumulativeWindowNode:
		return value.Source, true
	case TimeOffsetNode:
		return value.Source, true
	case OffsetToGrainNode:
		return value.Source, true
	case ConversionNode:
		return value.Source, true
	case SemiAdditiveNode:
		return value.Source, true
	case SourceSelectionNode:
		return value.Source, true
	default:
		return SemanticSourceState{}, false
	}
}

func WithNodeSourceState(node SemanticPlanNode, state SemanticSourceState) (SemanticPlanNode, error) {
	switch value := node.(type) {
	case SourceAggregateNode:
		value.Source = state
		return value, nil
	case PostAggregateNode:
		value.Source = state
		return value, nil
	case JoinAggregatesNode:
		value.Source = state
		return value, nil
	case CrossJoinAggregatesNode:
		value.Source = state
		return value, nil
	case CumulativeWindowNode:
		value.Source = state
		return value, nil
	case TimeOffsetNode:
		value.Source = state
		return value, nil
	case OffsetToGrainNode:
		value.Source = state
		return value, nil
	case ConversionNode:
		value.Source = state
		return value, nil
	case SemiAdditiveNode:
		value.Source = state
		return value, nil
	case SourceSelectionNode:
		value.Source = state
		return value, nil
	default:
		return nil, fmt.Errorf("semantic plan node %T has no source state", node)
	}
}

func NodeMetricState(node SemanticPlanNode) (SemanticMetricState, bool) {
	switch value := node.(type) {
	case SourceAggregateNode:
		return value.MetricState, true
	case PostAggregateNode:
		return value.MetricState, true
	case JoinAggregatesNode:
		return value.MetricState, true
	case CrossJoinAggregatesNode:
		return value.MetricState, true
	case CumulativeWindowNode:
		return value.MetricState, true
	case TimeOffsetNode:
		return value.MetricState, true
	case OffsetToGrainNode:
		return value.MetricState, true
	case ConversionNode:
		return value.MetricState, true
	case SemiAdditiveNode:
		return value.MetricState, true
	case AdditiveAttributionNode:
		return value.MetricState, true
	case RatioAttributionNode:
		return value.MetricState, true
	default:
		return SemanticMetricState{}, false
	}
}

func WithNodeMetricState(node SemanticPlanNode, state SemanticMetricState) (SemanticPlanNode, error) {
	switch value := node.(type) {
	case SourceAggregateNode:
		value.MetricState = state
		return value, nil
	case PostAggregateNode:
		value.MetricState = state
		return value, nil
	case JoinAggregatesNode:
		value.MetricState = state
		return value, nil
	case CrossJoinAggregatesNode:
		value.MetricState = state
		return value, nil
	case CumulativeWindowNode:
		value.MetricState = state
		return value, nil
	case TimeOffsetNode:
		value.MetricState = state
		return value, nil
	case OffsetToGrainNode:
		value.MetricState = state
		return value, nil
	case ConversionNode:
		value.MetricState = state
		return value, nil
	case SemiAdditiveNode:
		value.MetricState = state
		return value, nil
	case AdditiveAttributionNode:
		value.MetricState = state
		return value, nil
	case RatioAttributionNode:
		value.MetricState = state
		return value, nil
	default:
		return nil, fmt.Errorf("semantic plan node %T has no metric state", node)
	}
}
