package builder

import (
	"fmt"

	"github.com/meaningforge/metis/expression"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner/semanticplan"
)

// canonicalNodeFixtures gives each stage the typed node its kind requires for
// fixtures whose subject is something else entirely -- ordering, predicate
// ownership, explain output, and scan fusion. A stage that declares its own
// node keeps it so evaluation-specific fixtures remain explicit.
func canonicalNodeFixtures(stages []semanticNodeFixture) []semanticNodeFixture {
	out := make([]semanticNodeFixture, 0, len(stages))
	for _, stage := range stages {
		if stage.Node == nil {
			stage.Node = canonicalNodeFor(stage.Kind)
		}
		out = append(out, stage)
	}
	return out
}

func canonicalNodeFor(kind semanticplan.SemanticPlanNodeKind) semanticplan.SemanticPlanNode {
	switch kind {
	case semanticplan.SemanticPlanNodeSourceAggregate:
		return semanticplan.SourceAggregateNode{}
	case semanticplan.SemanticPlanNodePostAggregate:
		return semanticplan.PostAggregateNode{}
	case semanticplan.SemanticPlanNodeJoinAggregates:
		return semanticplan.JoinAggregatesNode{}
	case semanticplan.SemanticPlanNodeCrossJoinAggregates:
		return semanticplan.CrossJoinAggregatesNode{}
	case semanticplan.SemanticPlanNodeCumulativeWindow:
		return semanticplan.CumulativeWindowNode{}
	case semanticplan.SemanticPlanNodeTimeOffset:
		return semanticplan.TimeOffsetNode{}
	case semanticplan.SemanticPlanNodeOffsetToGrain:
		return semanticplan.OffsetToGrainNode{}
	case semanticplan.SemanticPlanNodeConversion:
		return semanticplan.ConversionNode{}
	case semanticplan.SemanticPlanNodeSemiAdditiveLast:
		return semanticplan.SemiAdditiveNode{Spec: ossie.SemiAdditiveMetricSpec{Aggregation: "last"}}
	case semanticplan.SemanticPlanNodeSemiAdditiveFirst:
		return semanticplan.SemiAdditiveNode{Spec: ossie.SemiAdditiveMetricSpec{Aggregation: "first"}}
	default:
		panic(fmt.Sprintf("no canonical semantic node for stage kind %q", kind))
	}
}

// summableBaseStages builds the source-aggregate stages a cumulative stage
// depends on, each carrying a proven SUM merge. Lowering requires that proof,
// so a hand-built graph must supply it the way the planner does.
func summableBaseStages(stage semanticNodeFixture) map[string]semanticNodeFixture {
	out := make(map[string]semanticNodeFixture, len(stage.Inputs))
	for _, input := range stage.Inputs {
		out[input.NodeID] = semanticNodeFixture{
			ID:   input.NodeID,
			Kind: semanticplan.SemanticPlanNodeSourceAggregate,
			Node: semanticplan.SourceAggregateNode{Rollup: semanticplan.RollupContract{
				Function:  "SUM",
				Algebra:   expression.RollupDistributive,
				Merge:     "SUM",
				Mergeable: true,
			}},
		}
	}
	return out
}
