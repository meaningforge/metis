package optimizer_test

import (
	"context"
	"testing"

	"github.com/meaningforge/metis/planner/optimizer"
	"github.com/meaningforge/metis/planner/semanticplan"
)

func TestSourceScanFusionGroupsBySemanticSubplanIdentity(t *testing.T) {
	day := []semanticplan.GroupBy{{Dataset: "calendar", Name: "day"}}
	month := []semanticplan.GroupBy{{Dataset: "calendar", Name: "month"}}
	root := semanticplan.DatasetRef{Name: "orders", Source: "warehouse.orders"}
	input := &semanticplan.SemanticPlan{Nodes: []semanticplan.SemanticPlanNode{
		sourceNodeWithIdentity("revenue", root, []string{"orders", "orders"}, []string{"payments", "orders"}, day, "stale_a"),
		sourceNodeWithIdentity("refunds", root, []string{"orders"}, []string{"orders", "payments", "payments"}, day, "stale_b"),
		sourceNodeWithIdentity("monthly_revenue", root, []string{"orders"}, []string{"orders", "payments"}, month, "stale_c"),
	}}

	optimized, err := optimizer.NewCanonical(optimizer.SourceScanFusionRule{}).Optimize(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if got := optimized.Nodes[0].(semanticplan.SourceAggregateNode).MetricState.ShareGroup; got != "source_001" {
		t.Fatalf("revenue share group = %q", got)
	}
	if got := optimized.Nodes[1].(semanticplan.SourceAggregateNode).MetricState.ShareGroup; got != "source_001" {
		t.Fatalf("refunds share group = %q", got)
	}
	if got := optimized.Nodes[2].(semanticplan.SourceAggregateNode).MetricState.ShareGroup; got != "" {
		t.Fatalf("monthly_revenue share group = %q, want empty", got)
	}
	if got := input.Nodes[0].(semanticplan.SourceAggregateNode).MetricState.ShareGroup; got != "stale_a" {
		t.Fatalf("optimizer mutated input share group: %q", got)
	}
}

func TestSourceScanFusionStageNamingFollowsDeterministicEvaluationOrder(t *testing.T) {
	rootA := semanticplan.DatasetRef{Name: "orders", Source: "warehouse.orders"}
	rootB := semanticplan.DatasetRef{Name: "payments", Source: "warehouse.payments"}
	input := &semanticplan.SemanticPlan{Nodes: []semanticplan.SemanticPlanNode{
		sourceNodeWithIdentity("orders_a", rootA, nil, nil, nil, ""),
		sourceNodeWithIdentity("payments_a", rootB, nil, nil, nil, ""),
		sourceNodeWithIdentity("orders_b", rootA, nil, nil, nil, ""),
		sourceNodeWithIdentity("payments_b", rootB, nil, nil, nil, ""),
	}}

	optimized, err := optimizer.NewCanonical(optimizer.SourceScanFusionRule{}).Optimize(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"source_001", "source_002", "source_001", "source_002"}
	for i := range want {
		node := optimized.Nodes[i].(semanticplan.SourceAggregateNode)
		if got := node.MetricState.ShareGroup; got != want[i] {
			t.Fatalf("stage %q share group = %q, want %q", node.Base.ID, got, want[i])
		}
	}
}

func sourceNodeWithIdentity(id string, root semanticplan.DatasetRef, sourceRoots, requiredDatasets []string, outputGrain []semanticplan.GroupBy, shareGroup string) semanticplan.SourceAggregateNode {
	return semanticplan.SourceAggregateNode{
		Base: semanticplan.SemanticPlanNodeBase{ID: id, OutputGrain: outputGrain},
		Source: semanticplan.SemanticSourceState{
			Root:             root,
			SourceRoots:      sourceRoots,
			RequiredDatasets: requiredDatasets,
		},
		MetricState: semanticplan.SemanticMetricState{ShareGroup: shareGroup},
	}
}
