package builder

import (
	"context"
	"reflect"
	"testing"

	"github.com/meaningforge/metis/planner/optimizer"
	"github.com/meaningforge/metis/planner/semanticplan"
)

func TestStagedPlanOptimizerNormalizesCanonicalGraphWithoutMutatingCaller(t *testing.T) {
	graph := *semanticPlanForTest(semanticplan.SemanticPlan{}, canonicalNodeFixtures([]semanticNodeFixture{
		{
			ID:                        "revenue",
			Kind:                      semanticplan.SemanticPlanNodeSourceAggregate,
			Boundary:                  semanticplan.SemanticPlanNodeBoundarySourceAggregate,
			PredicateBoundaryEvidence: semanticplan.PredicateBoundaryEvidence(semanticplan.SemanticPlanNodeBoundarySourceAggregate),
			Root:                      semanticplan.DatasetRef{Name: "orders", Source: "analytics.orders"},
			SourceRoots:               []string{"orders", "orders"},
			RequiredDatasets:          []string{"orders", "orders"},
		},
		{
			ID:                        "double_revenue",
			Kind:                      semanticplan.SemanticPlanNodePostAggregate,
			Boundary:                  semanticplan.SemanticPlanNodeBoundaryPostAggregate,
			PredicateBoundaryEvidence: semanticplan.PredicateBoundaryEvidence(semanticplan.SemanticPlanNodeBoundaryPostAggregate),
			Inputs:                    []semanticNodeInputFixture{{NodeID: "revenue"}},
			Node:                      semanticplan.PostAggregateNode{},
		},
	}))
	plan := semanticPlanForTest(semanticplan.SemanticPlan{
		Root:      semanticplan.DatasetRef{Name: "orders", Source: "analytics.orders"},
		Requested: graph.Requested,
	}, semanticPlanNodeFixturesForTest(&graph))

	optimized, err := optimizer.NewCanonical(optimizer.SemanticSetNormalizationRule{}).OptimizeSemanticPlan(context.Background(), plan)
	if err != nil {
		t.Fatalf("optimize staged graph: %v", err)
	}
	optimizedStages := semanticPlanNodeFixturesForTest(optimized)
	if got, want := optimizedStages[0].RequiredDatasets, []string{"orders"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("required datasets = %#v, want %#v", got, want)
	}
	if got, want := optimizedStages[0].SourceRoots, []string{"orders"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("source roots = %#v, want %#v", got, want)
	}
	if got := semanticPlanNodeFixturesForTest(plan)[0].RequiredDatasets; len(got) != 2 || got[1] != "orders" {
		t.Fatalf("optimizer mutated caller-owned canonical graph: %#v", got)
	}
}

func TestStagedPlanOptimizerFusesCanonicalSourceStages(t *testing.T) {
	boundary := semanticplan.PredicateBoundaryEvidence(semanticplan.SemanticPlanNodeBoundarySourceAggregate)
	graph := *semanticPlanForTest(semanticplan.SemanticPlan{}, canonicalNodeFixtures([]semanticNodeFixture{
		{
			ID:                        "revenue",
			Kind:                      semanticplan.SemanticPlanNodeSourceAggregate,
			Boundary:                  semanticplan.SemanticPlanNodeBoundarySourceAggregate,
			PredicateBoundaryEvidence: boundary,
			Root:                      semanticplan.DatasetRef{Name: "orders", Source: "analytics.orders"},
			SourceRoots:               []string{"orders"},
			RequiredDatasets:          []string{"orders"},
		},
		{
			ID:                        "cost",
			Kind:                      semanticplan.SemanticPlanNodeSourceAggregate,
			Boundary:                  semanticplan.SemanticPlanNodeBoundarySourceAggregate,
			PredicateBoundaryEvidence: boundary,
			Root:                      semanticplan.DatasetRef{Name: "orders", Source: "analytics.orders"},
			SourceRoots:               []string{"orders"},
			RequiredDatasets:          []string{"orders"},
		},
		{
			ID:                        "margin",
			Kind:                      semanticplan.SemanticPlanNodePostAggregate,
			Boundary:                  semanticplan.SemanticPlanNodeBoundaryPostAggregate,
			PredicateBoundaryEvidence: semanticplan.PredicateBoundaryEvidence(semanticplan.SemanticPlanNodeBoundaryPostAggregate),
			Inputs:                    []semanticNodeInputFixture{{NodeID: "revenue"}, {NodeID: "cost"}},
			Node:                      semanticplan.PostAggregateNode{},
		},
	}))
	plan := semanticPlanForTest(semanticplan.SemanticPlan{Root: semanticplan.DatasetRef{Name: "orders", Source: "analytics.orders"}, Requested: graph.Requested}, semanticPlanNodeFixturesForTest(&graph))

	optimized, err := optimizer.Default().OptimizeSemanticPlan(context.Background(), plan)
	if err != nil {
		t.Fatalf("optimize staged graph: %v", err)
	}
	optimizedStages := semanticPlanNodeFixturesForTest(optimized)
	if optimizedStages[0].ShareGroup == "" {
		t.Fatal("expected canonical source stages to be fused")
	}
	if optimizedStages[0].ShareGroup != optimizedStages[1].ShareGroup {
		t.Fatalf("share groups differ: %#v", optimizedStages)
	}
}
