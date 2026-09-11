package planner_test

import (
	"context"
	"testing"

	"github.com/meaningforge/metis/expression"
	"github.com/meaningforge/metis/planner/conversion"
	"github.com/meaningforge/metis/planner/optimizer"
	"github.com/meaningforge/metis/planner/semanticplan"
)

func TestDerivedMetricsReuseCommonDependencyEvaluation(t *testing.T) {
	nodes := []semanticplan.SemanticPlanNode{
		sharedDependencySourceStage("revenue", "SUM(orders.amount)", ""),
		sharedDependencyDerivedStage("double_revenue", "revenue", "revenue * 2"),
		sharedDependencyDerivedStage("triple_revenue", "revenue", "revenue * 3"),
	}
	requested := []string{"double_revenue", "triple_revenue"}
	input := &semanticplan.SemanticPlan{
		Root:        semanticplan.DatasetRef{Name: "orders", Source: "analytics.orders"},
		Projections: []semanticplan.Projection{{Name: "double_revenue", Kind: semanticplan.ProjectionMetric, Expression: expression.NewResolvedExpression("ANSI_SQL", "revenue * 2")}, {Name: "triple_revenue", Kind: semanticplan.ProjectionMetric, Expression: expression.NewResolvedExpression("ANSI_SQL", "revenue * 3")}},
		Requested:   requested,
		Nodes:       nodes,
	}

	sourceGroups := 0
	for _, node := range input.Nodes {
		if node.NodeBase().ID == "revenue" {
			sourceGroups++
		}
	}
	if sourceGroups != 1 {
		t.Fatalf("revenue evaluation stages = %d, want one shared dependency", sourceGroups)
	}

	stmt, err := conversion.BuildSQLPlan(input, mustRenderer(t, "DUCKDB"))
	if err != nil {
		t.Fatal(err)
	}
	if got := len(stmt.Blocks); got != 4 {
		t.Fatalf("block count = %d, want one dependency stage, two derived stages, and the root", got)
	}
	revenueStages := 0
	for _, block := range stmt.Blocks {
		for _, projection := range block.Projections {
			if projection.Alias == "revenue" {
				revenueStages++
				break
			}
		}
	}
	if revenueStages != 1 {
		t.Fatalf("CTEs materializing revenue = %d, want one common dependency stage", revenueStages)
	}
}

func TestDefaultOptimizerRemovesStaleSharedStagesAfterPruning(t *testing.T) {
	nodes := []semanticplan.SemanticPlanNode{
		sharedDependencySourceStage("revenue", "SUM(orders.amount)", "stale"),
		sharedDependencySourceStage("cost", "SUM(orders.cost)", "stale"),
		sharedDependencySourceStage("unused", "SUM(orders.tax)", "stale"),
		sharedDependencyDerivedStage("margin", "revenue", "revenue - cost", semanticplan.SemanticPlanNodeInput{NodeID: "cost"}),
	}
	requested := []string{"margin"}
	input := &semanticplan.SemanticPlan{
		Root:        semanticplan.DatasetRef{Name: "orders", Source: "analytics.orders"},
		Projections: []semanticplan.Projection{{Name: "margin", Kind: semanticplan.ProjectionMetric, Expression: expression.NewResolvedExpression("ANSI_SQL", "revenue - cost")}},
		Requested:   requested,
		Nodes:       nodes,
	}

	optimized, err := optimizer.Default().Optimize(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if len(optimized.Nodes) != 3 {
		t.Fatalf("evaluation nodes = %#v, want unused node pruned", optimized.Nodes)
	}
	first := optimized.Nodes[0].(semanticplan.SourceAggregateNode).MetricState.ShareGroup
	second := optimized.Nodes[1].(semanticplan.SourceAggregateNode).MetricState.ShareGroup
	if first == "" || first != second {
		t.Fatalf("surviving source nodes were not fused: %#v", optimized.Nodes)
	}
	stmt, err := conversion.BuildSQLPlan(optimized, mustRenderer(t, "DUCKDB"))
	if err != nil {
		t.Fatal(err)
	}
	if got := len(stmt.Blocks); got != 3 {
		t.Fatalf("optimized block count = %d, want shared source, derived stage, and root", got)
	}
	for _, block := range stmt.Blocks {
		for _, projection := range block.Projections {
			if projection.Alias == "unused" {
				t.Fatalf("stale intermediate column survived pruning in block %q", block.ID)
			}
		}
	}
}

func sharedDependencySourceStage(name, expressionSQL, shareGroup string) semanticplan.SourceAggregateNode {
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
		MetricState: semanticplan.SemanticMetricState{ShareGroup: shareGroup},
		Expression:  expression.NewResolvedExpression("ANSI_SQL", expressionSQL),
	}
}

func sharedDependencyDerivedStage(name, firstInput, expressionSQL string, extraInputs ...semanticplan.SemanticPlanNodeInput) semanticplan.PostAggregateNode {
	inputs := []semanticplan.SemanticPlanNodeInput{{NodeID: firstInput}}
	inputs = append(inputs, extraInputs...)
	return semanticplan.PostAggregateNode{
		Base: semanticplan.SemanticPlanNodeBase{
			ID:       name,
			Boundary: semanticplan.SemanticPlanNodeBoundaryPostAggregate,
			PredicateBoundaryEvidence: semanticplan.SemanticPredicateBoundaryEvidence{
				Movement: semanticplan.SemanticPredicateBoundaryBlocked,
				Proof:    semanticplan.SemanticPredicateProofNodeSemantics,
			},
			Inputs: inputs,
		},
		Expression: expression.NewResolvedExpression("ANSI_SQL", expressionSQL),
	}
}
