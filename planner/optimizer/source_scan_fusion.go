package optimizer

import (
	"fmt"

	"github.com/meaningforge/metis/planner/semanticplan"
)

// fuseSourceScans assigns deterministic share groups to source-aggregate nodes with the
// same sharable source-scan work. The caller decides whether the plan's chosen
// physical strategy permits source-scan reuse.
func fuseSourceScans(plan *semanticplan.SemanticPlan) (bool, error) {
	if plan == nil {
		return false, nil
	}
	previous := make(map[int]string)
	groups := make([][]int, 0)
	groupByIdentity := make(map[string]int)
	for i, node := range plan.Nodes {
		sourceNode, ok := node.(semanticplan.SourceAggregateNode)
		if !ok {
			continue
		}
		previous[i] = sourceNode.MetricState.ShareGroup
		sourceNode.MetricState.ShareGroup = ""
		plan.Nodes[i] = sourceNode

		identity, sharable, err := semanticplan.SharableSourceScanIdentityNode(sourceNode)
		if err != nil {
			return false, fmt.Errorf("source scan fusion: %w", err)
		}
		if !sharable {
			continue
		}
		if groupIndex, ok := groupByIdentity[identity]; ok {
			groups[groupIndex] = append(groups[groupIndex], i)
			continue
		}
		groupByIdentity[identity] = len(groups)
		groups = append(groups, []int{i})
	}

	shareIndex := 0
	for _, group := range groups {
		if len(group) < 2 {
			continue
		}
		shareIndex++
		name := fmt.Sprintf("source_%03d", shareIndex)
		for _, nodeIndex := range group {
			sourceNode := plan.Nodes[nodeIndex].(semanticplan.SourceAggregateNode)
			sourceNode.MetricState.ShareGroup = name
			plan.Nodes[nodeIndex] = sourceNode
		}
	}
	for i, before := range previous {
		after := plan.Nodes[i].(semanticplan.SourceAggregateNode).MetricState.ShareGroup
		if before != after {
			return true, nil
		}
	}
	return false, nil
}
