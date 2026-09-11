package manifest_test

import (
	"testing"

	"github.com/meaningforge/metis/expression"
	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
)

const aggregationPropertyModel = `
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
          - name: customer_id
            datatype: String
            expression:
              dialects: [{dialect: ANSI_SQL, expression: customer_id}]
    metrics:
      - name: revenue
        datatype: Decimal
        expression:
          dialects: [{dialect: ANSI_SQL, expression: "SUM(orders.amount)"}]
      - name: unique_customers
        datatype: Integer
        expression:
          dialects: [{dialect: ANSI_SQL, expression: "COUNT(DISTINCT orders.customer_id)"}]
`

func analyzedAggregationProperties(t *testing.T, metric string) []expression.AggregationProperties {
	t.Helper()
	doc, err := ossie.NewLoader().Load([]byte(aggregationPropertyModel))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := manifest.BuildProjectManifest("test", doc)
	if err != nil {
		t.Fatal(err)
	}
	project, _ := snapshot.Project("test")
	model, _ := project.Model("sales")
	analysis, ok := model.MetricAnalysis(metric)
	if !ok {
		t.Fatalf("metric %q is not analyzed", metric)
	}
	if len(analysis.Expressions) == 0 {
		t.Fatalf("metric %q has no analyzed expressions", metric)
	}
	return analysis.Expressions[0].AggregationProperties
}

func TestSemanticManifestRecordsAggregationPropertyEvidence(t *testing.T) {
	props := analyzedAggregationProperties(t, "revenue")
	if len(props) != 1 {
		t.Fatalf("collected %d properties, want 1", len(props))
	}
	if !props[0].Derived {
		t.Fatal("Derived = false, want true")
	}
	if props[0].Duplicate != expression.DuplicateSensitive || props[0].Rollup != expression.RollupDistributive {
		t.Fatalf("axes = %s/%s", props[0].Duplicate, props[0].Rollup)
	}
	if props[0].Merge != "SUM" {
		t.Fatalf("merge = %q, want SUM", props[0].Merge)
	}
}

func TestSemanticManifestRecordsDistinctCountAsInvariantAndHolistic(t *testing.T) {
	props := analyzedAggregationProperties(t, "unique_customers")
	if len(props) != 1 {
		t.Fatalf("collected %d properties, want 1", len(props))
	}
	if props[0].Duplicate != expression.DuplicateInvariant {
		t.Fatalf("duplicate = %s, want %s", props[0].Duplicate, expression.DuplicateInvariant)
	}
	if props[0].Rollup != expression.RollupHolistic {
		t.Fatalf("rollup = %s, want %s", props[0].Rollup, expression.RollupHolistic)
	}
}
