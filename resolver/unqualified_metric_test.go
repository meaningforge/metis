package resolver_test

import (
	"context"
	"testing"

	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/resolver"
)

func TestResolveUnqualifiedMetricFieldsInMultiDatasetModel(t *testing.T) {
	const modelYAML = `
version: "0.2.0.dev0"
semantic_model:
  - name: sales
    datasets:
      - name: orders
        source: sales.orders
        fields:
          - name: customer_id
            datatype: String
            expression:
              dialects: [{dialect: ANSI_SQL, expression: customer_id}]
          - name: amount
            datatype: Decimal
            expression:
              dialects: [{dialect: ANSI_SQL, expression: amount}]
          - name: status
            datatype: String
            expression:
              dialects: [{dialect: ANSI_SQL, expression: status}]
      - name: customer
        source: sales.customer
        fields:
          - name: customer_id
            datatype: String
            expression:
              dialects: [{dialect: ANSI_SQL, expression: customer_id}]
          - name: region
            datatype: String
            expression:
              dialects: [{dialect: ANSI_SQL, expression: region}]
    relationships:
      - name: orders_to_customer
        from: orders
        to: customer
        from_columns: [customer_id]
        to_columns: [customer_id]
    metrics:
      - name: paid_revenue
        datatype: Decimal
        expression:
          dialects:
            - {dialect: ANSI_SQL, expression: "SUM(CASE WHEN status = 'paid' THEN amount END)"}
`
	doc, err := ossie.NewLoader().Load([]byte(modelYAML))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := manifest.BuildProjectManifest("unqualified-metric-test", doc)
	if err != nil {
		t.Fatal(err)
	}
	r := resolver.New(manifest.NewStore(snapshot))
	resolved, err := r.Resolve(context.Background(), query.SemanticQuery{
		Project: "unqualified-metric-test",
		Model:   "sales",
		Metrics: []query.MetricRef{{Name: "paid_revenue"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.RootDataset != "orders" {
		t.Fatalf("root dataset = %q, want orders", resolved.RootDataset)
	}
	if len(resolved.Metrics) != 1 || len(resolved.Metrics[0].Datasets) != 1 || resolved.Metrics[0].Datasets[0] != "orders" {
		t.Fatalf("unexpected metric datasets: %#v", resolved.Metrics)
	}
}
