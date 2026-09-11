package manifest_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/serrors"
)

const derivedMetricModel = `
version: "0.2.0.dev0"
semantic_model:
  - name: sales
    datasets:
      - name: orders
        source: sales.orders
        fields:
          - name: revenue_amount
            datatype: Decimal
            expression:
              dialects: [{dialect: ANSI_SQL, expression: revenue_amount}]
          - name: cost_amount
            datatype: Decimal
            expression:
              dialects: [{dialect: ANSI_SQL, expression: cost_amount}]
          - name: order_id
            datatype: String
            expression:
              dialects: [{dialect: ANSI_SQL, expression: order_id}]
    metrics:
      - name: revenue
        datatype: Decimal
        expression:
          dialects: [{dialect: ANSI_SQL, expression: "SUM(revenue_amount)"}]
      - name: cost
        datatype: Decimal
        expression:
          dialects: [{dialect: ANSI_SQL, expression: "SUM(cost_amount)"}]
      - name: orders_count
        datatype: Integer
        expression:
          dialects: [{dialect: ANSI_SQL, expression: "COUNT(order_id)"}]
      - name: gross_margin
        datatype: Decimal
        expression:
          dialects:
            - {dialect: ANSI_SQL, expression: "revenue - cost"}
            - {dialect: CLICKHOUSE, expression: "revenue - cost"}
      - name: average_revenue
        datatype: Decimal
        expression:
          dialects: [{dialect: ANSI_SQL, expression: "revenue / orders_count"}]
      - name: margin_per_order
        datatype: Decimal
        expression:
          dialects: [{dialect: ANSI_SQL, expression: "gross_margin / orders_count"}]
`

func TestMetricDependencyGraphExpandsDerivedAndRatioMetrics(t *testing.T) {
	doc, err := ossie.NewLoader().Load([]byte(derivedMetricModel))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := manifest.BuildProjectManifest("test", doc)
	if err != nil {
		t.Fatal(err)
	}
	project, _ := snapshot.Project("test")
	model, _ := project.Model("sales")

	margin, ok := model.MetricDependency("gross_margin")
	if !ok {
		t.Fatal("gross_margin dependency not indexed")
	}
	if !reflect.DeepEqual(margin.Metrics, []string{"cost", "revenue"}) {
		t.Fatalf("direct metric dependencies = %#v", margin.Metrics)
	}
	wantRefs := []manifest.SemanticReference{
		{Dataset: "orders", Field: "cost_amount"},
		{Dataset: "orders", Field: "revenue_amount"},
	}
	if !reflect.DeepEqual(margin.References, wantRefs) {
		t.Fatalf("transitive references = %#v, want %#v", margin.References, wantRefs)
	}
	if len(margin.DirectReferences) != 0 || len(margin.DirectDatasets) != 0 {
		t.Fatalf("derived metric direct field dependencies = %#v / %#v", margin.DirectReferences, margin.DirectDatasets)
	}
	if !reflect.DeepEqual(margin.Datasets, []string{"orders"}) {
		t.Fatalf("transitive datasets = %#v", margin.Datasets)
	}
	revenue, _ := model.MetricDependency("revenue")
	if !reflect.DeepEqual(revenue.DirectReferences, []manifest.SemanticReference{{Dataset: "orders", Field: "revenue_amount"}}) {
		t.Fatalf("source metric direct references = %#v", revenue.DirectReferences)
	}

	order, err := model.MetricDependencyGraph.EvaluationOrder("margin_per_order", "average_revenue")
	if err != nil {
		t.Fatal(err)
	}
	wantOrder := []string{"cost", "revenue", "gross_margin", "orders_count", "margin_per_order", "average_revenue"}
	if !reflect.DeepEqual(order, wantOrder) {
		t.Fatalf("evaluation order = %#v, want %#v", order, wantOrder)
	}
	sources, err := model.MetricSources("margin_per_order")
	if err != nil {
		t.Fatal(err)
	}
	wantSources := []manifest.MetricSource{
		{Metric: "cost", Dataset: "orders"},
		{Metric: "orders_count", Dataset: "orders"},
		{Metric: "revenue", Dataset: "orders"},
	}
	if !reflect.DeepEqual(sources, wantSources) {
		t.Fatalf("metric sources = %#v, want %#v", sources, wantSources)
	}
}

func TestMetricSourcesRejectsAmbiguousLeafAggregateRoot(t *testing.T) {
	doc, err := ossie.NewLoader().Load([]byte(`
version: "0.2.0.dev0"
semantic_model:
  - name: finance
    datasets:
      - name: orders
        source: finance.orders
        fields:
          - name: amount
            datatype: Decimal
            expression:
              dialects: [{dialect: ANSI_SQL, expression: orders.amount}]
      - name: costs
        source: finance.costs
        fields:
          - name: amount
            datatype: Decimal
            expression:
              dialects: [{dialect: ANSI_SQL, expression: costs.amount}]
    metrics:
      - name: unsafe_margin
        datatype: Decimal
        expression:
          dialects: [{dialect: ANSI_SQL, expression: "SUM(orders.amount) - SUM(costs.amount)"}]
`))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := manifest.BuildProjectManifest("test", doc)
	if err != nil {
		t.Fatal(err)
	}
	project, _ := snapshot.Project("test")
	model, _ := project.Model("finance")
	_, err = model.MetricSources("unsafe_margin")
	if err == nil {
		t.Fatal("expected ambiguous metric source")
	}
	var apiErr *serrors.Error
	if !errors.As(err, &apiErr) || apiErr.Code != serrors.ErrAmbiguousMetricSource {
		t.Fatalf("error = %#v, want %s", err, serrors.ErrAmbiguousMetricSource)
	}
	want := []string{"costs", "orders"}
	if !reflect.DeepEqual(apiErr.Details["candidate_datasets"], want) {
		t.Fatalf("candidate datasets = %#v, want %#v", apiErr.Details["candidate_datasets"], want)
	}
}

func TestMetricSourcesUsesRelationshipDirectionForCrossDatasetLeafRoot(t *testing.T) {
	doc, err := ossie.NewLoader().Load([]byte(`
version: "0.2.0.dev0"
semantic_model:
  - name: sales
    datasets:
      - name: orders
        source: sales.orders
        fields:
          - {name: customer_id, datatype: String, expression: {dialects: [{dialect: ANSI_SQL, expression: orders.customer_id}]}}
          - {name: amount, datatype: Decimal, expression: {dialects: [{dialect: ANSI_SQL, expression: orders.amount}]}}
      - name: customer
        source: sales.customer
        fields:
          - {name: customer_id, datatype: String, expression: {dialects: [{dialect: ANSI_SQL, expression: customer.customer_id}]}}
    relationships:
      - name: orders_to_customer
        from: orders
        to: customer
        from_columns: [customer_id]
        to_columns: [customer_id]
    metrics:
      - name: average_customer_value
        datatype: Decimal
        expression:
          dialects: [{dialect: ANSI_SQL, expression: "SUM(orders.amount) / COUNT(DISTINCT customer.customer_id)"}]
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
	sources, err := model.MetricSources("average_customer_value")
	if err != nil {
		t.Fatal(err)
	}
	want := []manifest.MetricSource{{Metric: "average_customer_value", Dataset: "orders"}}
	if !reflect.DeepEqual(sources, want) {
		t.Fatalf("metric sources = %#v, want %#v", sources, want)
	}
}

const cyclicMetricModel = `
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
    metrics:
      - name: metric_a
        datatype: Decimal
        expression:
          dialects: [{dialect: ANSI_SQL, expression: "metric_b + 1"}]
      - name: metric_b
        datatype: Decimal
        expression:
          dialects: [{dialect: ANSI_SQL, expression: "metric_c + 1"}]
      - name: metric_c
        datatype: Decimal
        expression:
          dialects: [{dialect: ANSI_SQL, expression: "metric_a + 1"}]
`

func TestMetricDependencyGraphRejectsCycles(t *testing.T) {
	doc, err := ossie.NewLoader().Load([]byte(cyclicMetricModel))
	if err != nil {
		t.Fatal(err)
	}
	_, err = manifest.BuildProjectManifest("test", doc)
	if err == nil {
		t.Fatal("expected metric dependency cycle")
	}
	var apiErr *serrors.Error
	if !errors.As(err, &apiErr) || apiErr.Code != serrors.ErrMetricDependencyCycle {
		t.Fatalf("error = %#v, want %s", err, serrors.ErrMetricDependencyCycle)
	}
	wantCycle := []string{"metric_a", "metric_b", "metric_c", "metric_a"}
	if !reflect.DeepEqual(apiErr.Details["cycle"], wantCycle) {
		t.Fatalf("cycle = %#v, want %#v", apiErr.Details["cycle"], wantCycle)
	}
}

const inconsistentMetricReferenceModel = `
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
    metrics:
      - name: revenue
        datatype: Decimal
        expression:
          dialects: [{dialect: ANSI_SQL, expression: "SUM(amount)"}]
      - name: cost
        datatype: Decimal
        expression:
          dialects: [{dialect: ANSI_SQL, expression: "SUM(amount)"}]
      - name: margin
        datatype: Decimal
        expression:
          dialects:
            - {dialect: ANSI_SQL, expression: "revenue - cost"}
            - {dialect: CLICKHOUSE, expression: "revenue"}
`

func TestMetricDependencyGraphRejectsInconsistentDialectMetricReferences(t *testing.T) {
	doc, err := ossie.NewLoader().Load([]byte(inconsistentMetricReferenceModel))
	if err != nil {
		t.Fatal(err)
	}
	_, err = manifest.BuildProjectManifest("test", doc)
	if err == nil {
		t.Fatal("expected inconsistent metric references")
	}
	var apiErr *serrors.Error
	if !errors.As(err, &apiErr) || apiErr.Code != serrors.ErrInconsistentExpressionReferences {
		t.Fatalf("error = %#v, want %s", err, serrors.ErrInconsistentExpressionReferences)
	}
}

func TestMetricDependencyGraphRejectsMetricReaggregation(t *testing.T) {
	model := `
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
    metrics:
      - name: revenue
        datatype: Decimal
        expression:
          dialects: [{dialect: ANSI_SQL, expression: "SUM(amount)"}]
      - name: invalid_revenue
        datatype: Decimal
        expression:
          dialects: [{dialect: ANSI_SQL, expression: "SUM(revenue)"}]
`
	doc, err := ossie.NewLoader().Load([]byte(model))
	if err != nil {
		t.Fatal(err)
	}
	_, err = manifest.BuildProjectManifest("test", doc)
	if err == nil {
		t.Fatal("expected semantic analysis error")
	}
	var apiErr *serrors.Error
	if !errors.As(err, &apiErr) || apiErr.Code != serrors.ErrSemanticAnalysisFailed {
		t.Fatalf("error = %#v, want %s", err, serrors.ErrSemanticAnalysisFailed)
	}
}

// An unknown metric name means two different things at the same guard. Which
// one it is depends on who wrote the name, and the caller can only act on one
// of them, so the two conditions carry different codes.
func TestUnknownMetricNameDistinguishesRequestFromModelDefinition(t *testing.T) {
	doc, err := ossie.NewLoader().Load([]byte(derivedMetricModel))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := manifest.BuildProjectManifest("test", doc)
	if err != nil {
		t.Fatal(err)
	}
	project, _ := snapshot.Project("test")
	model, _ := project.Model("sales")

	_, err = model.MetricDependencyGraph.EvaluationOrder("reveneu")

	var apiErr *serrors.Error
	if !errors.As(err, &apiErr) || apiErr.Code != serrors.ErrMetricNotFound {
		t.Fatalf("requested unknown metric error = %#v, want %s", err, serrors.ErrMetricNotFound)
	}
	if got := apiErr.Details["metric"]; got != "reveneu" {
		t.Fatalf("details[metric] = %#v, want reveneu", got)
	}
	if _, ok := apiErr.Details["referenced_by"]; ok {
		t.Fatal("a name the request supplied must not be reported as referenced by a metric definition")
	}
}

// The same guard, reached from inside a metric definition, is the model's defect
// and names the metric that referenced it so the author knows where to look.
func TestMetricDefinitionReferencingUndefinedMetricIsAModelCondition(t *testing.T) {
	doc, err := ossie.NewLoader().Load([]byte(`
version: "0.2.0.dev0"
semantic_model:
  - name: sales
    datasets:
      - name: orders
        source: sales.orders
        fields:
          - name: revenue_amount
            datatype: Decimal
            expression:
              dialects: [{dialect: ANSI_SQL, expression: revenue_amount}]
    metrics:
      - name: revenue
        datatype: Decimal
        expression:
          dialects: [{dialect: ANSI_SQL, expression: "SUM(revenue_amount)"}]
      - name: gross_margin
        datatype: Decimal
        expression:
          dialects: [{dialect: ANSI_SQL, expression: "revenue - cost"}]
`))
	if err != nil {
		t.Fatal(err)
	}

	_, err = manifest.BuildProjectManifest("test", doc)

	var apiErr *serrors.Error
	if !errors.As(err, &apiErr) || apiErr.Code != serrors.ErrUnresolvedSemanticReference {
		t.Fatalf("undefined dependency error = %#v, want %s", err, serrors.ErrUnresolvedSemanticReference)
	}
	names, _ := apiErr.Details["names"].([]string)
	if !reflect.DeepEqual(names, []string{"cost"}) {
		t.Fatalf("details[names] = %#v, want [cost]", apiErr.Details["names"])
	}
}
