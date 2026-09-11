package builder

import (
	"reflect"
	"testing"

	"github.com/meaningforge/metis/ossie"
)

func TestBuildJoinTreeUsesStableRootConnectedOrder(t *testing.T) {
	joins, err := BuildJoinTree("orders", []*ossie.Relationship{
		{Name: "orders_to_store", From: "orders", To: "store"},
		{Name: "orders_to_customer", From: "orders", To: "customer"},
		{Name: "customer_to_region", From: "customer", To: "region"},
	})
	if err != nil {
		t.Fatalf("BuildJoinTree() error = %v", err)
	}
	got := make([]string, 0, len(joins))
	for _, join := range joins {
		got = append(got, join.Relationship.Name)
	}
	want := []string{"orders_to_customer", "orders_to_store", "customer_to_region"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("join order = %v, want %v", got, want)
	}
}

func TestBuildJoinTreeRejectsRelationshipOutsideRootComponent(t *testing.T) {
	_, err := BuildJoinTree("orders", []*ossie.Relationship{
		{Name: "orders_to_customer", From: "orders", To: "customer"},
		{Name: "product_to_supplier", From: "product", To: "supplier"},
	})
	if err == nil {
		t.Fatal("BuildJoinTree() succeeded for a disconnected relationship")
	}
}
