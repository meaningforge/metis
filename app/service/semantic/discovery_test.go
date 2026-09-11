package semantic_test

import (
	"context"
	"testing"

	service "github.com/meaningforge/metis/app/service/semantic"
	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
)

const model = `
version: "0.2.0.dev0"
semantic_model:
  - name: sales
    description: Sales analytics
    datasets:
      - name: orders
        source: sales.orders
        fields:
          - name: amount
            datatype: Decimal
            expression:
              dialects: [{dialect: ANSI_SQL, expression: amount}]
          - name: region
            datatype: String
            description: Customer sales region
            expression:
              dialects: [{dialect: ANSI_SQL, expression: region}]
            dimension: {}
    metrics:
      - name: total_revenue
        description: Total sales revenue
        expression:
          dialects: [{dialect: ANSI_SQL, expression: SUM(orders.amount)}]
`

func newService(t *testing.T) *service.DiscoveryService {
	t.Helper()
	doc, err := ossie.NewLoader().Load([]byte(model))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := manifest.BuildProjectManifest("finance", doc)
	if err != nil {
		t.Fatal(err)
	}
	return service.NewDiscoveryService(manifest.NewStore(snapshot)).WithProjectAuthorizer(service.AllAccessProjectAuthorizer{})
}

func TestDiscoveryService(t *testing.T) {
	svc := newService(t)
	result, err := svc.SearchSemantics(context.Background(), service.SearchSemanticsRequest{Project: "finance", Query: "revenue"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Matches) == 0 || result.Matches[0].Name != "total_revenue" || result.Matches[0].Project != "finance" {
		t.Fatalf("unexpected search result: %#v", result.Matches)
	}
	metric, err := svc.GetMetric(context.Background(), service.GetMetricRequest{Project: "finance", Model: "sales", Metric: "total_revenue"})
	if err != nil {
		t.Fatal(err)
	}
	if metric.Name != "total_revenue" {
		t.Fatalf("metric = %q", metric.Name)
	}
	compatibility, err := svc.GetMetricDimensions(context.Background(), service.GetMetricDimensionsRequest{Project: "finance", Model: "sales", Metric: "total_revenue"})
	if err != nil {
		t.Fatal(err)
	}
	if len(compatibility.Dimensions) != 1 || compatibility.Dimensions[0].Status != service.CompatibilityCompatible {
		t.Fatalf("metric dimensions = %#v", compatibility.Dimensions)
	}
	dimension, err := svc.GetDimension(context.Background(), service.GetDimensionRequest{Project: "finance", Model: "sales", Dimension: "region"})
	if err != nil {
		t.Fatal(err)
	}
	if dimension.Dataset != "orders" || dimension.Field.Name != "region" {
		t.Fatalf("unexpected dimension: %#v", dimension)
	}
}
