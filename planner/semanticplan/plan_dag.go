package semanticplan

import "fmt"

// SemanticPlan owns its DAG directly through concrete typed
// SemanticPlanNode values.

// SetOwnedDAG installs an already constructed, owned typed-node DAG and the
// query-wide output semantics. No compatibility projection is involved at the
// storage seam.
func SetOwnedDAG(plan *SemanticPlan, requested []string, nodes []SemanticPlanNode, grain []GroupBy, post []PostEvaluationPredicate) error {
	if plan == nil {
		return nil
	}
	owned, err := CloneNodes(nodes)
	if err != nil {
		return fmt.Errorf("install semantic plan typed nodes: %w", err)
	}
	plan.Requested = append([]string(nil), requested...)
	plan.Nodes = owned
	plan.Output = BuildOutputContract(plan, grain, post)
	return nil
}

// ValidateOwnedDAG proves the plan's own typed nodes are a DAG using only
// canonical node state.
func ValidateOwnedDAG(plan *SemanticPlan) error {
	if plan == nil || len(plan.Nodes) == 0 {
		return nil
	}

	nodes := make(map[string]struct{}, len(plan.Nodes))
	produced := make(map[string]struct{}, len(plan.Nodes))
	for index, node := range plan.Nodes {
		if err := ValidateNode(node); err != nil {
			return fmt.Errorf("plan node at index %d: %w", index, err)
		}
		base := node.NodeBase()
		if base.ID == "" {
			return fmt.Errorf("plan node at index %d has an empty id", index)
		}
		if _, exists := nodes[base.ID]; exists {
			return fmt.Errorf("plan node %q is duplicated", base.ID)
		}
		nodes[base.ID] = struct{}{}
		if base.Boundary == "" {
			return fmt.Errorf("plan node %q has an unsupported kind %q", base.ID, node.Kind())
		}

		for _, input := range base.Inputs {
			if _, ok := nodes[input.NodeID]; !ok {
				return fmt.Errorf("plan node %q depends on %q, which is not an earlier node", base.ID, input.NodeID)
			}
		}
		produced[base.ID] = struct{}{}
		for _, metric := range semanticPlanNodeMetrics(node) {
			produced[metric] = struct{}{}
		}
		for _, dimension := range base.Dimensions {
			produced[dimension] = struct{}{}
		}
	}

	for _, requested := range plan.Requested {
		if requested == "" {
			continue
		}
		if _, ok := produced[requested]; !ok {
			return fmt.Errorf("plan requests output %q that no node produces", requested)
		}
	}
	return nil
}

func semanticPlanNodeMetrics(node SemanticPlanNode) []string {
	state, ok := NodeMetricState(node)
	if !ok {
		return nil
	}
	return state.Metrics
}
