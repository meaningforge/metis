package manifest_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/meaningforge/metis/expression"
	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/serrors"
)

func TestDefaultExpressionAnalyzerUsesNativeParser(t *testing.T) {
	doc, err := ossie.NewLoader().Load([]byte(dependencyModel))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := manifest.BuildProjectManifest("test", doc)
	if err != nil {
		t.Fatal(err)
	}
	project, _ := snapshot.Project("test")
	model, _ := project.Model("sales")
	paid, ok := model.MetricDependency("paid_revenue")
	if !ok {
		t.Fatal("paid_revenue dependency not indexed")
	}
	if paid.Inference != "native-parser" {
		t.Fatalf("inference = %q", paid.Inference)
	}
	want := []manifest.SemanticReference{{Dataset: "orders", Field: "amount"}, {Dataset: "orders", Field: "status"}}
	if !reflect.DeepEqual(paid.References, want) {
		t.Fatalf("refs = %#v, want %#v", paid.References, want)
	}
}

func TestNativeAnalyzerStoresBoundTypedExpressions(t *testing.T) {
	doc, err := ossie.NewLoader().Load([]byte(dependencyModel))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := manifest.BuildProjectManifest("test", doc)
	if err != nil {
		t.Fatal(err)
	}
	project, _ := snapshot.Project("test")
	model, _ := project.Model("sales")
	analysis, ok := model.MetricAnalysis("paid_revenue")
	if !ok {
		t.Fatal("paid_revenue analysis not indexed")
	}
	if len(analysis.Expressions) != 2 {
		t.Fatalf("expressions = %d, want 2", len(analysis.Expressions))
	}
	for _, analyzed := range analysis.Expressions {
		if analyzed.Bound.Expr == nil || analyzed.Typed.Expr == nil {
			t.Fatalf("%s expression was not retained", analyzed.Dialect)
		}
		if analyzed.Typed.Type != expression.TypeDecimal || analyzed.Typed.Aggregation != expression.AggregationAggregate {
			t.Fatalf("%s typed expression = %#v", analyzed.Dialect, analyzed.Typed)
		}
		if got := analyzed.Bound.Symbols(); len(got) != 2 {
			t.Fatalf("%s symbols = %#v, want amount and status", analyzed.Dialect, got)
		}
	}
}

const ambiguousNativeModel = `
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
      - name: refunds
        source: sales.refunds
        fields:
          - name: amount
            datatype: Decimal
            expression:
              dialects: [{dialect: ANSI_SQL, expression: amount}]
    metrics:
      - name: total_amount
        datatype: Decimal
        expression:
          dialects:
            - {dialect: ANSI_SQL, expression: "SUM(amount)"}
`

func TestNativeAnalyzerRejectsAmbiguousBareReference(t *testing.T) {
	doc, err := ossie.NewLoader().Load([]byte(ambiguousNativeModel))
	if err != nil {
		t.Fatal(err)
	}
	_, err = manifest.BuildProjectManifest("test", doc)
	if err == nil {
		t.Fatal("expected ambiguous expression reference error")
	}
	var apiErr *serrors.Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %T %v", err, err)
	}
	if apiErr.Code != serrors.ErrAmbiguousExpressionReference {
		t.Fatalf("code = %s", apiErr.Code)
	}
}

func TestNativeAnalyzerBindsCurrentMetricNameToSourceField(t *testing.T) {
	doc, err := ossie.NewLoader().Load([]byte(`
version: "0.2.0.dev0"
semantic_model:
  - name: sales
    datasets:
      - name: orders
        source: sales.orders
        fields:
          - name: revenue
            datatype: Decimal
            expression:
              dialects: [{dialect: ANSI_SQL, expression: revenue}]
    metrics:
      - name: revenue
        datatype: Decimal
        expression:
          dialects: [{dialect: ANSI_SQL, expression: "SUM(revenue)"}]
`))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := manifest.BuildProjectManifest("test", doc)
	if err != nil {
		t.Fatal(err)
	}
	project, _ := snapshot.Project("test")
	model, _ := project.Model("sales")
	dependency, ok := model.MetricDependency("revenue")
	if !ok {
		t.Fatal("revenue dependency not indexed")
	}
	want := []manifest.SemanticReference{{Dataset: "orders", Field: "revenue"}}
	if !reflect.DeepEqual(dependency.References, want) {
		t.Fatalf("refs = %#v, want %#v", dependency.References, want)
	}
	if len(dependency.Metrics) != 0 {
		t.Fatalf("metric dependencies = %#v, want none", dependency.Metrics)
	}
}
