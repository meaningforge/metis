package optimizer_test

import (
	"context"
	"testing"

	"github.com/meaningforge/metis/planner/optimizer"
	"github.com/meaningforge/metis/planner/semanticplan"
)

func TestMetricProjectionPruningKeepsHiddenFilterMetricDependencyClosure(t *testing.T) {
	input := &semanticplan.SemanticPlan{
		Projections: []semanticplan.Projection{{Name: "visible_metric", Kind: semanticplan.ProjectionMetric}},
		Requested:   []string{"visible_metric", "hidden_filter_metric"},
		Nodes: []semanticplan.SemanticPlanNode{
			semanticplan.SourceAggregateNode{Base: semanticplan.SemanticPlanNodeBase{ID: "visible_metric"}},
			semanticplan.SourceAggregateNode{Base: semanticplan.SemanticPlanNodeBase{ID: "hidden_filter_base"}},
			semanticplan.PostAggregateNode{Base: semanticplan.SemanticPlanNodeBase{ID: "hidden_filter_metric", Inputs: []semanticplan.SemanticPlanNodeInput{{NodeID: "hidden_filter_base"}}}},
			semanticplan.SourceAggregateNode{Base: semanticplan.SemanticPlanNodeBase{ID: "unrelated_metric"}},
		},
	}

	optimized, err := optimizer.NewCanonical(optimizer.MetricProjectionPruningRule{}).Optimize(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}

	got := make([]string, 0, len(optimized.Nodes))
	for _, node := range optimized.Nodes {
		got = append(got, node.NodeBase().ID)
	}
	want := []string{"visible_metric", "hidden_filter_base", "hidden_filter_metric"}
	if len(got) != len(want) {
		t.Fatalf("optimized semantic stages = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("optimized semantic stages = %v, want %v", got, want)
		}
	}
	if len(input.Nodes) != 4 {
		t.Fatalf("optimizer mutated input semantic nodes: %#v", input.Nodes)
	}
}
