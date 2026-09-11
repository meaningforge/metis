package optimizer_test

import (
	"context"
	"testing"

	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner/optimizer"
	"github.com/meaningforge/metis/planner/semanticplan"
)

func TestUnusedJoinEliminationKeepsPlanDatasetClosure(t *testing.T) {
	ordersToCustomer := &ossie.Relationship{Name: "orders_to_customer", From: "orders", To: "customer"}
	ordersToProduct := &ossie.Relationship{Name: "orders_to_product", From: "orders", To: "product"}
	ordersToWarehouse := &ossie.Relationship{Name: "orders_to_warehouse", From: "orders", To: "warehouse"}
	input := &semanticplan.SemanticPlan{
		Root:      semanticplan.DatasetRef{Name: "orders", Source: "orders"},
		Requested: []string{"visible_metric", "hidden_filter_metric"},

		Output: semanticplan.SemanticOutputContract{
			Predicates: []semanticplan.PostEvaluationPredicate{{Name: "hidden_filter_metric"}},
		},
		Joins: []semanticplan.Join{
			{Relationship: ordersToCustomer, FromDataset: "orders", ToDataset: "customer"},
			{Relationship: ordersToProduct, FromDataset: "orders", ToDataset: "product"},
			{Relationship: ordersToWarehouse, FromDataset: "orders", ToDataset: "warehouse"},
		},
		Projections: []semanticplan.Projection{{Name: "visible_metric", Kind: semanticplan.ProjectionMetric}},
		Nodes: []semanticplan.SemanticPlanNode{
			semanticplan.SourceAggregateNode{Base: semanticplan.SemanticPlanNodeBase{ID: "visible_metric"}, Source: semanticplan.SemanticSourceState{RequiredDatasets: []string{"orders", "customer"}}},
			semanticplan.SourceAggregateNode{Base: semanticplan.SemanticPlanNodeBase{ID: "hidden_filter_base"}, Source: semanticplan.SemanticSourceState{RequiredDatasets: []string{"orders", "product"}}},
			semanticplan.PostAggregateNode{Base: semanticplan.SemanticPlanNodeBase{ID: "hidden_filter_metric", Inputs: []semanticplan.SemanticPlanNodeInput{{NodeID: "hidden_filter_base"}}}},
			semanticplan.SourceAggregateNode{Base: semanticplan.SemanticPlanNodeBase{ID: "unrelated_metric"}, Source: semanticplan.SemanticSourceState{RequiredDatasets: []string{"orders", "warehouse"}}},
		},
	}

	optimized, err := optimizer.NewCanonical(optimizer.UnusedJoinEliminationRule{}).Optimize(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if len(optimized.Joins) != 2 {
		t.Fatalf("optimized joins = %#v, want customer and product joins", optimized.Joins)
	}
	if optimized.Joins[0].Relationship != ordersToCustomer || optimized.Joins[1].Relationship != ordersToProduct {
		t.Fatalf("optimized joins = %#v, want customer and product joins", optimized.Joins)
	}
	if len(input.Joins) != 3 {
		t.Fatalf("optimizer mutated input joins: %#v", input.Joins)
	}
}
