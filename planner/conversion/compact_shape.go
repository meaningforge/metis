package conversion

import (
	"fmt"

	"github.com/meaningforge/metis/planner/semanticplan"
)

// sqlQueryShape is the plan-owned input for one compact query block.
type sqlQueryShape struct {
	Root        semanticplan.DatasetRef
	Joins       []semanticplan.Join
	Projections []semanticplan.Projection
	Predicates  []semanticplan.Predicate
	Groups      []semanticplan.GroupBy
	Sorts       []semanticplan.Sort
	Limit       *int
}

// planOwnedQueryShape derives compact SQL shape from canonical typed nodes plus
// the output contract.
func planOwnedQueryShape(plan *semanticplan.SemanticPlan) (sqlQueryShape, error) {
	if plan == nil || len(plan.Nodes) == 0 {
		return sqlQueryShape{}, fmt.Errorf("semantic plan owns no nodes to lower")
	}
	if strategy := LoweringStrategyForPlan(plan); strategy != SemanticLoweringCompact {
		return sqlQueryShape{}, fmt.Errorf("plan lowers %s, which is not a single scan", strategy)
	}
	scan := plan.Nodes[0]
	if err := semanticplan.ValidateNode(scan); err != nil {
		return sqlQueryShape{}, err
	}
	base := scan.NodeBase()
	source, ok := semanticplan.NodeSourceState(scan)
	if !ok {
		return sqlQueryShape{}, fmt.Errorf("semantic plan node %q has no source state", base.ID)
	}
	return sqlQueryShape{
		Root:        source.Root,
		Joins:       append([]semanticplan.Join(nil), source.Joins...),
		Predicates:  nodePlacedPredicates(scan),
		Groups:      append([]semanticplan.GroupBy(nil), base.OutputGrain...),
		Projections: append([]semanticplan.Projection(nil), plan.Output.Projections...),
		Sorts:       append([]semanticplan.Sort(nil), plan.Output.OrderBy...),
		Limit:       plan.Output.Limit,
	}, nil
}
