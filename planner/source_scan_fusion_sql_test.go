package planner_test

import (
	"context"
	"testing"

	"github.com/meaningforge/metis/expression"
	"github.com/meaningforge/metis/planner/conversion"
	"github.com/meaningforge/metis/planner/optimizer"
	"github.com/meaningforge/metis/planner/semanticplan"
)

func TestSourceScanFusionReducesCompatibleAggregateCTEs(t *testing.T) {
	sourceGroup := func(name, expressionSQL string) semanticplan.SourceAggregateNode {
		return semanticplan.SourceAggregateNode{
			Base: semanticplan.SemanticPlanNodeBase{
				ID:       name,
				Boundary: semanticplan.SemanticPlanNodeBoundarySourceAggregate,
				PredicateBoundaryEvidence: semanticplan.SemanticPredicateBoundaryEvidence{
					Movement: semanticplan.SemanticPredicateBoundaryAllowedWithProof,
					Proof:    semanticplan.SemanticPredicateProofDatasetReachability,
				},
			},
			Source: semanticplan.SemanticSourceState{
				Root:             semanticplan.DatasetRef{Name: "orders", Source: "warehouse.orders"},
				SourceRoots:      []string{"orders"},
				RequiredDatasets: []string{"orders"},
			},
			Expression: expression.NewResolvedExpression("ANSI_SQL", expressionSQL),
		}
	}
	// Two source metrics feeding a derived one. Fusion matters exactly here:
	// the derived stage forces composition, so the sources become blocks, and
	// fusion is what stops them being two scans of the same rows.
	derived := semanticplan.PostAggregateNode{
		Base: semanticplan.SemanticPlanNodeBase{
			ID:       "margin",
			Boundary: semanticplan.SemanticPlanNodeBoundaryPostAggregate,
			PredicateBoundaryEvidence: semanticplan.SemanticPredicateBoundaryEvidence{
				Movement: semanticplan.SemanticPredicateBoundaryBlocked,
				Proof:    semanticplan.SemanticPredicateProofNodeSemantics,
			},
			Inputs: []semanticplan.SemanticPlanNodeInput{{NodeID: "revenue"}, {NodeID: "cost"}},
		},
		MetricState: semanticplan.SemanticMetricState{Metrics: []string{"margin"}},
		Expression:  expression.NewResolvedExpression("ANSI_SQL", "revenue - cost"),
	}
	nodes := []semanticplan.SemanticPlanNode{
		sourceGroup("revenue", "SUM(orders.amount)"),
		sourceGroup("cost", "SUM(orders.cost)"),
		derived,
	}
	requested := []string{"margin"}
	input := &semanticplan.SemanticPlan{
		Root: semanticplan.DatasetRef{Name: "orders", Source: "analytics.orders"},
		Projections: []semanticplan.Projection{
			{Name: "margin", Kind: semanticplan.ProjectionMetric, Expression: expression.NewResolvedExpression("ANSI_SQL", "revenue - cost")},
		},
		Requested: requested,
		Nodes:     nodes,
	}

	unoptimizedPlan, err := conversion.BuildSQLPlan(input, mustRenderer(t, "DUCKDB"))
	if err != nil {
		t.Fatal(err)
	}
	if got := len(unoptimizedPlan.Blocks); got != 4 {
		t.Fatalf("unoptimized block count = %d, want two independent aggregate stages, the derived one, and root", got)
	}

	optimized, err := optimizer.NewCanonical(optimizer.SourceScanFusionRule{}).Optimize(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	optimizedPlan, err := conversion.BuildSQLPlan(optimized, mustRenderer(t, "DUCKDB"))
	if err != nil {
		t.Fatal(err)
	}
	if got := len(optimizedPlan.Blocks); got != 3 {
		t.Fatalf("optimized block count = %d, want one shared aggregate stage, the derived one, and root", got)
	}
	first := optimized.Nodes[0].(semanticplan.SourceAggregateNode).MetricState.ShareGroup
	second := optimized.Nodes[1].(semanticplan.SourceAggregateNode).MetricState.ShareGroup
	if first == "" || first != second {
		t.Fatalf("shared source nodes = %#v", optimized.Nodes)
	}
	root := optimizedPlan.Blocks[len(optimizedPlan.Blocks)-1]
	if len(root.Joins) != 0 {
		t.Fatalf("optimized outer joins = %#v, want no join between fused metrics", root.Joins)
	}
}
