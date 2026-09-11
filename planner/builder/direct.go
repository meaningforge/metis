package builder

import (
	"fmt"

	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/resolver"
)

// InstallDirectNodes gives a direct-lowering plan its own typed semantic DAG.
// Metric-free queries receive one source-selection node; metric-bearing queries
// retain the construction nodes supplied by metric evaluation lowering.
func InstallDirectNodes(plan *semanticplan.SemanticPlan, requested []string, nodes []semanticplan.SemanticPlanNode, outputGrain []semanticplan.GroupBy, post []semanticplan.PostEvaluationPredicate, sharedGrain *semanticplan.SharedGrainResolution, q *resolver.SemanticQuerySpec) error {
	if plan == nil {
		return nil
	}
	if len(nodes) == 0 {
		node, ok := BuildSourceSelection(plan, q)
		if !ok {
			return nil
		}
		nodes = []semanticplan.SemanticPlanNode{node}
		requested = append([]string(nil), node.Base.Dimensions...)
		outputGrain = append([]semanticplan.GroupBy(nil), plan.Groups...)
	}
	if err := semanticplan.InstallOwnedDAG(plan, requested, nodes, outputGrain, post, sharedGrain); err != nil {
		return fmt.Errorf("build plan-owned semantic nodes: %w", err)
	}
	return nil
}
