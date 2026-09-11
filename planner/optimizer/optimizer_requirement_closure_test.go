package optimizer_test

import (
	"context"
	"testing"

	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner/optimizer"
	"github.com/meaningforge/metis/planner/semanticplan"
)

func TestUnusedJoinEliminationPrunesUnrelatedJoinsWithMetricSort(t *testing.T) {
	ordersToCustomer := &ossie.Relationship{Name: "orders_to_customer", From: "orders", To: "customer"}
	customerToProduct := &ossie.Relationship{Name: "customer_to_product", From: "customer", To: "product"}
	input := &semanticplan.SemanticPlan{
		Root: semanticplan.DatasetRef{Name: "orders", Source: "orders"},
		Joins: []semanticplan.Join{
			{Relationship: ordersToCustomer, FromDataset: "orders", ToDataset: "customer"},
			{Relationship: customerToProduct, FromDataset: "customer", ToDataset: "product"},
		},
		Projections: []semanticplan.Projection{{Name: "region", Kind: semanticplan.ProjectionDimension, Dataset: "customer"}},
		Sorts:       []semanticplan.Sort{{Name: "revenue", Kind: semanticplan.SortMetric}},
	}

	optimized, err := optimizer.NewCanonical(optimizer.UnusedJoinEliminationRule{}).Optimize(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if len(optimized.Joins) != 1 || optimized.Joins[0].Relationship != ordersToCustomer {
		t.Fatalf("optimized joins = %#v, want only orders_to_customer", optimized.Joins)
	}
	if len(input.Joins) != 2 {
		t.Fatalf("optimizer mutated input joins: %#v", input.Joins)
	}
}
