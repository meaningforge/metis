package semanticplan

import (
	"github.com/meaningforge/metis/expression"
	"github.com/meaningforge/metis/ossie"
)

// NodeMetricExpression returns the metric and resolved-expression evidence
// owned by a metric-bearing semantic node.
func NodeMetricExpression(node SemanticPlanNode) (*ossie.Metric, expression.ResolvedExpression, bool) {
	switch node := node.(type) {
	case SourceAggregateNode:
		return node.Metric, node.Expression, true
	case PostAggregateNode:
		return node.Metric, node.Expression, true
	case JoinAggregatesNode:
		return node.Metric, node.Expression, true
	case CrossJoinAggregatesNode:
		return node.Metric, node.Expression, true
	case CumulativeWindowNode:
		return node.Metric, node.Expression, true
	case TimeOffsetNode:
		return node.Metric, node.Expression, true
	case OffsetToGrainNode:
		return node.Metric, node.Expression, true
	case ConversionNode:
		return node.Metric, node.Expression, true
	case SemiAdditiveNode:
		return node.Metric, node.Expression, true
	default:
		return nil, expression.ResolvedExpression{}, false
	}
}
