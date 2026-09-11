package semantic

import (
	"context"
	"reflect"
	"testing"

	"github.com/meaningforge/metis/manifest"
)

func TestMetricsDimensionsAggregatesCompatibleAndUnreachableEvidence(t *testing.T) {
	result, err := newService(t).GetMetricsDimensions(context.Background(), GetMetricsDimensionsRequest{
		Project: "finance",
		Model:   "sales",
		Metrics: []string{"total_revenue", "total_cost"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := result.Metrics, []string{"total_revenue", "total_cost"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("metrics = %v, want %v", got, want)
	}

	byName := map[string]MetricSetDimensionCompatibility{}
	for _, dimension := range result.Dimensions {
		byName[dimension.Qualified] = dimension
	}
	region := byName["customer.region"]
	if region.Status != CompatibilityCompatible || len(region.PerMetric) != 2 {
		t.Fatalf("region = %#v", region)
	}
	if region.PerMetric[0].Metric != "total_revenue" || region.PerMetric[1].Metric != "total_cost" {
		t.Fatalf("region per-metric order = %#v", region.PerMetric)
	}
	category := byName["inventory.category"]
	if category.Status != CompatibilityUnreachable || len(category.PerMetric) != 2 {
		t.Fatalf("category = %#v", category)
	}
}

func TestMetricsDimensionsAggregateFailsClosedOnAmbiguousMetric(t *testing.T) {
	doc := loadDoc(t)
	model := &doc.SemanticModel[0]
	duplicateRoute := model.Relationships[0]
	duplicateRoute.Name = "orders_to_customer_alternate"
	model.Relationships = append(model.Relationships, duplicateRoute)
	snapshot, err := manifest.BuildProjectManifest("finance", doc)
	if err != nil {
		t.Fatal(err)
	}

	result, err := NewDiscoveryService(manifest.NewStore(snapshot)).WithProjectAuthorizer(AllAccessProjectAuthorizer{}).GetMetricsDimensions(context.Background(), GetMetricsDimensionsRequest{
		Project: "finance",
		Model:   "sales",
		Metrics: []string{"total_revenue", "total_cost"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, dimension := range result.Dimensions {
		if dimension.Qualified != "customer.region" {
			continue
		}
		if dimension.Status != CompatibilityAmbiguous {
			t.Fatalf("region status = %q, want %q", dimension.Status, CompatibilityAmbiguous)
		}
		return
	}
	t.Fatal("customer.region compatibility not found")
}

func TestMetricsDimensionsDeduplicatesMetricsPreservingOrder(t *testing.T) {
	result, err := newService(t).GetMetricsDimensions(context.Background(), GetMetricsDimensionsRequest{
		Project: "finance",
		Model:   "sales",
		Metrics: []string{"total_cost", "total_revenue", "total_cost"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := result.Metrics, []string{"total_cost", "total_revenue"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("metrics = %v, want %v", got, want)
	}
}
