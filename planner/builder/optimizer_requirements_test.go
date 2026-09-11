package builder

import (
	"testing"

	"github.com/meaningforge/metis/planner/optimizer"
	"github.com/meaningforge/metis/planner/semanticplan"
)

func TestCollectSemanticRequirementsRejectsMissingRequestedMetricStage(t *testing.T) {
	// A request no stage produces is caught by plan validation, which owns the
	// rule that every requested output has a producer. The requirement
	// collector walks stage dependencies, and a plan with a stage to walk is
	// what makes an unproducible request visible to it.
	plan := semanticPlanForTest(semanticplan.SemanticPlan{
		Requested: []string{"missing_metric"},
	}, []semanticNodeFixture{{
		ID:       "revenue",
		Kind:     semanticplan.SemanticPlanNodeSourceAggregate,
		Boundary: semanticplan.SemanticPlanNodeBoundarySourceAggregate,
		Metrics:  []string{"revenue"},
		Node:     semanticplan.SourceAggregateNode{Base: semanticplan.SemanticPlanNodeBase{ID: "revenue"}},
	}})

	_, err := optimizer.CollectRequirements(plan)
	if err == nil {
		t.Fatal("expected missing semantic stage error")
	}
}

func TestCollectSemanticRequirementsUsesPlanOwnedStages(t *testing.T) {
	plan := semanticPlanForTest(semanticplan.SemanticPlan{
		Projections: []semanticplan.Projection{{
			Name: "margin",
			Kind: semanticplan.ProjectionMetric,
		}},
	}, []semanticNodeFixture{
		{
			ID:               "revenue",
			Kind:             semanticplan.SemanticPlanNodeSourceAggregate,
			RequiredDatasets: []string{"orders"},
			Node:             semanticplan.SourceAggregateNode{Base: semanticplan.SemanticPlanNodeBase{ID: "revenue"}},
		},
		{
			ID:               "margin",
			Kind:             semanticplan.SemanticPlanNodePostAggregate,
			Inputs:           []semanticNodeInputFixture{{NodeID: "revenue"}},
			RequiredDatasets: []string{"orders"},
			Node:             semanticplan.PostAggregateNode{Base: semanticplan.SemanticPlanNodeBase{ID: "margin"}},
		},
	})

	requirements, err := optimizer.CollectRequirements(plan)
	if err != nil {
		t.Fatalf("plan-owned stage requirements failed: %v", err)
	}
	if !requirements.RequiresNode("margin") {
		t.Fatal("missing output stage requirement")
	}
	if !requirements.RequiresNode("revenue") {
		t.Fatal("missing dependency stage requirement")
	}
	if !requirements.RequiresDataset("orders") {
		t.Fatal("missing required dataset")
	}
}
