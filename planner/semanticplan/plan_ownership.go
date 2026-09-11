package semanticplan

import (
	"fmt"

	"github.com/meaningforge/metis/ossie"
)

// OwnNodes clones canonical typed nodes and attaches the shared-grain evidence
// selected for this plan. The caller retains ownership of every input.
func OwnNodes(nodes []SemanticPlanNode, sharedGrain *SharedGrainResolution) ([]SemanticPlanNode, error) {
	out, err := CloneNodes(nodes)
	if err != nil || sharedGrain == nil {
		return out, err
	}
	shared := make(map[string]MetricSharedGrainEvidence, len(sharedGrain.Metrics))
	for _, evidence := range sharedGrain.Metrics {
		shared[evidence.Metric] = CloneMetricSharedGrainEvidence(evidence)
	}
	for i, node := range out {
		evidence, ok := shared[node.NodeBase().ID]
		if !ok {
			continue
		}
		metric, ok := NodeMetricState(node)
		if !ok {
			continue
		}
		owned := evidence
		metric.SharedGrainEvidence = &owned
		updated, err := WithNodeMetricState(node, metric)
		if err != nil {
			return nil, err
		}
		out[i] = updated
	}
	return out, nil
}

// InstallOwnedDAG installs an independently owned typed DAG, its output
// contract, and any plan-level calendar domains. It does not construct source
// semantics or choose a physical lowering strategy.
func InstallOwnedDAG(plan *SemanticPlan, requested []string, nodes []SemanticPlanNode, grain []GroupBy, post []PostEvaluationPredicate, sharedGrain *SharedGrainResolution) error {
	if plan == nil {
		return fmt.Errorf("semantic plan is required")
	}
	owned, err := OwnNodes(nodes, sharedGrain)
	if err != nil {
		return err
	}
	plan.DenseCalendar = CloneDenseCalendarPlan(plan.DenseCalendar)
	plan.CustomDenseCalendar = CloneCustomDenseCalendarPlan(plan.CustomDenseCalendar)
	if err := SetOwnedDAG(plan, requested, owned, grain, post); err != nil {
		return err
	}
	return ValidateSemanticPlanDAG(plan)
}

// NodeMetric returns the metric declaration carried by a metric-bearing node.
// Source selection nodes intentionally carry no metric declaration.
func NodeMetric(node SemanticPlanNode) *ossie.Metric {
	switch node := node.(type) {
	case SourceAggregateNode:
		return node.Metric
	case PostAggregateNode:
		return node.Metric
	case JoinAggregatesNode:
		return node.Metric
	case CrossJoinAggregatesNode:
		return node.Metric
	case CumulativeWindowNode:
		return node.Metric
	case TimeOffsetNode:
		return node.Metric
	case OffsetToGrainNode:
		return node.Metric
	case ConversionNode:
		return node.Metric
	case SemiAdditiveNode:
		return node.Metric
	default:
		return nil
	}
}
