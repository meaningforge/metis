package manifest_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/serrors"
)

const dependencyModel = `
version: "0.2.0.dev0"
semantic_model:
  - name: sales
    datasets:
      - name: orders
        source: sales.orders
        fields:
          - name: amount
            datatype: Decimal
            expression:
              dialects: [{dialect: ANSI_SQL, expression: amount}]
          - name: status
            datatype: String
            expression:
              dialects: [{dialect: ANSI_SQL, expression: status}]
          - name: customer_id
            datatype: String
            expression:
              dialects: [{dialect: ANSI_SQL, expression: customer_id}]
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
            dimension: {}
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
            - {dialect: CLICKHOUSE, expression: "sumIf(amount, status = 'paid')"}
      - name: apac_revenue
        datatype: Decimal
        expression:
          dialects:
            - {dialect: ANSI_SQL, expression: "SUM(CASE WHEN customer.region = 'APAC' THEN orders.amount END)"}
            - {dialect: CLICKHOUSE, expression: "sumIf(orders.amount, customer.region = 'APAC')"}
`

const inconsistentDependencyModel = `
version: "0.2.0.dev0"
semantic_model:
  - name: sales
    datasets:
      - name: orders
        source: sales.orders
        fields:
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
          - name: region
            datatype: String
            expression:
              dialects: [{dialect: ANSI_SQL, expression: region}]
    metrics:
      - name: revenue
        datatype: Decimal
        expression:
          dialects:
            - {dialect: ANSI_SQL, expression: "SUM(orders.amount)"}
            - {dialect: CLICKHOUSE, expression: "sumIf(orders.amount, customer.region = 'APAC')"}
`

func TestBuildProjectManifestIndexesMetricDependencies(t *testing.T) {
	doc, err := ossie.NewLoader().Load([]byte(dependencyModel))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := manifest.BuildProjectManifest("test", doc)
	if err != nil {
		t.Fatal(err)
	}
	project, err := snapshot.Project("test")
	if err != nil {
		t.Fatal(err)
	}
	model, err := project.Model("sales")
	if err != nil {
		t.Fatal(err)
	}

	paid, ok := model.MetricDependency("paid_revenue")
	if !ok {
		t.Fatal("paid_revenue dependency not indexed")
	}
	if !reflect.DeepEqual(paid.Datasets, []string{"orders"}) {
		t.Fatalf("paid_revenue datasets = %#v, want [orders]", paid.Datasets)
	}
	if paid.Inference != "native-parser" {
		t.Fatalf("paid_revenue inference = %q", paid.Inference)
	}

	apac, ok := model.MetricDependency("apac_revenue")
	if !ok {
		t.Fatal("apac_revenue dependency not indexed")
	}
	if !reflect.DeepEqual(apac.Datasets, []string{"customer", "orders"}) {
		t.Fatalf("apac_revenue datasets = %#v, want [customer orders]", apac.Datasets)
	}
	wantRefs := []manifest.SemanticReference{{Dataset: "customer", Field: "region"}, {Dataset: "orders", Field: "amount"}}
	if !reflect.DeepEqual(apac.References, wantRefs) {
		t.Fatalf("apac_revenue refs = %#v, want %#v", apac.References, wantRefs)
	}
	if apac.Inference != "native-parser" {
		t.Fatalf("apac_revenue inference = %q", apac.Inference)
	}
}

func TestBuildProjectManifestRejectsInconsistentDialectReferences(t *testing.T) {
	doc, err := ossie.NewLoader().Load([]byte(inconsistentDependencyModel))
	if err != nil {
		t.Fatal(err)
	}
	_, err = manifest.BuildProjectManifest("test", doc)
	if err == nil {
		t.Fatal("expected inconsistent expression reference error")
	}
	var metisErr *serrors.Error
	if !errors.As(err, &metisErr) || metisErr.Code != serrors.ErrInconsistentExpressionReferences {
		t.Fatalf("error = %#v, want %s", err, serrors.ErrInconsistentExpressionReferences)
	}
}
