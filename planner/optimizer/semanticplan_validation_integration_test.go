package optimizer_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/meaningforge/metis/expression"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner/optimizer"
	"github.com/meaningforge/metis/planner/semanticplan"
)

func TestDefaultOptimizerReachesDeterministicFixedPoint(t *testing.T) {
	ordersToCustomer := &ossie.Relationship{Name: "orders_to_customer", From: "orders", To: "customer"}
	customerToProduct := &ossie.Relationship{Name: "customer_to_product", From: "customer", To: "product"}
	input := &semanticplan.SemanticPlan{
		Root:        semanticplan.DatasetRef{Name: "orders", Source: "orders"},
		Joins:       []semanticplan.Join{{Relationship: ordersToCustomer, FromDataset: "orders", ToDataset: "customer"}, {Relationship: ordersToCustomer, FromDataset: "orders", ToDataset: "customer"}, {Relationship: customerToProduct, FromDataset: "customer", ToDataset: "product"}},
		Projections: []semanticplan.Projection{{Name: "region", Kind: semanticplan.ProjectionDimension, Dataset: "customer", Expression: expression.NewResolvedExpression("ANSI_SQL", "region")}},
	}
	before := append([]semanticplan.Join(nil), input.Joins...)
	engine := optimizer.Default()
	first, err := engine.Optimize(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := engine.Optimize(context.Background(), first)
	if err != nil {
		t.Fatal(err)
	}
	first.OptimizationTrace = nil
	second.OptimizationTrace = nil
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("optimizer did not reach fixed point:\nfirst=%#v\nsecond=%#v", first, second)
	}
	if !reflect.DeepEqual(input.Joins, before) {
		t.Fatalf("optimizer mutated input joins: %#v", input.Joins)
	}
}
