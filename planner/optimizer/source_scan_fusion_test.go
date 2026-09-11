package optimizer_test

import (
	"context"
	"testing"

	"github.com/meaningforge/metis/planner/optimizer"
	"github.com/meaningforge/metis/planner/semanticplan"
)

func sourceAggregateNode(id, root, sourceRoot string) semanticplan.SourceAggregateNode {
	return semanticplan.SourceAggregateNode{
		Base: semanticplan.SemanticPlanNodeBase{ID: id},
		Source: semanticplan.SemanticSourceState{
			Root:             semanticplan.DatasetRef{Name: root, Source: "warehouse." + root},
			SourceRoots:      []string{sourceRoot},
			RequiredDatasets: []string{root},
		},
	}
}

func TestSourceScanFusionRequiresEquivalentSourceRoots(t *testing.T) {
	input := &semanticplan.SemanticPlan{Nodes: []semanticplan.SemanticPlanNode{
		sourceAggregateNode("revenue", "orders", "orders"),
		sourceAggregateNode("refunds", "orders", "refund_orders"),
	}}

	optimized, err := optimizer.NewCanonical(optimizer.SourceScanFusionRule{}).Optimize(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range optimized.Nodes {
		node := raw.(semanticplan.SourceAggregateNode)
		if node.MetricState.ShareGroup != "" {
			t.Fatalf("node %q received share group %q despite distinct source roots", node.Base.ID, node.MetricState.ShareGroup)
		}
	}
}

func TestSourceScanFusionSharesEquivalentSourceRoots(t *testing.T) {
	input := &semanticplan.SemanticPlan{Nodes: []semanticplan.SemanticPlanNode{
		sourceAggregateNode("revenue", "orders", "orders"),
		sourceAggregateNode("refunds", "orders", "orders"),
	}}

	optimized, err := optimizer.NewCanonical(optimizer.SourceScanFusionRule{}).Optimize(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	first := optimized.Nodes[0].(semanticplan.SourceAggregateNode).MetricState.ShareGroup
	second := optimized.Nodes[1].(semanticplan.SourceAggregateNode).MetricState.ShareGroup
	if first == "" || first != second {
		t.Fatalf("share groups = %q, %q, want one compatible shared group", first, second)
	}
}

func TestSourceScanFusionKeepsDeterministicNodeNamingAndOrdering(t *testing.T) {
	input := &semanticplan.SemanticPlan{Nodes: []semanticplan.SemanticPlanNode{
		sourceAggregateNode("revenue", "orders", "orders"),
		sourceAggregateNode("units", "inventory", "inventory"),
		sourceAggregateNode("refunds", "orders", "orders"),
		sourceAggregateNode("returns", "inventory", "inventory"),
	}}

	optimizer := optimizer.NewCanonical(optimizer.SourceScanFusionRule{})
	first, err := optimizer.Optimize(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := optimizer.Optimize(context.Background(), first)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"source_001", "source_002", "source_001", "source_002"}
	for i := range want {
		firstNode := first.Nodes[i].(semanticplan.SourceAggregateNode)
		if firstNode.MetricState.ShareGroup != want[i] {
			t.Fatalf("first node %q share group = %q, want %q", firstNode.Base.ID, firstNode.MetricState.ShareGroup, want[i])
		}
		secondNode := second.Nodes[i].(semanticplan.SourceAggregateNode)
		if secondNode.MetricState.ShareGroup != want[i] {
			t.Fatalf("second node %q share group = %q, want %q", secondNode.Base.ID, secondNode.MetricState.ShareGroup, want[i])
		}
	}
}
