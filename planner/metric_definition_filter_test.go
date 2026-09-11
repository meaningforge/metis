package planner_test

import (
	"context"
	"testing"

	"github.com/meaningforge/metis/planner"
	"github.com/meaningforge/metis/query"
)

const metricDefinitionFilterModel = `
version: "0.2.0.dev0"
semantic_model:
  - name: sales
    datasets:
      - name: orders
        source: sales.orders
        fields:
          - name: customer_id
            datatype: String
            expression: {dialects: [{dialect: ANSI_SQL, expression: orders.customer_id}]}
          - name: amount
            datatype: Decimal
            expression: {dialects: [{dialect: ANSI_SQL, expression: orders.amount}]}
      - name: customers
        source: sales.customers
        primary_key: [customer_id]
        fields:
          - name: customer_id
            datatype: String
            expression: {dialects: [{dialect: ANSI_SQL, expression: customers.customer_id}]}
          - name: tier
            datatype: String
            expression: {dialects: [{dialect: ANSI_SQL, expression: customers.tier}]}
    relationships:
      - name: orders_to_customers
        from: orders
        to: customers
        from_columns: [customer_id]
        to_columns: [customer_id]
    metrics:
      - name: gold_revenue
        datatype: Decimal
        expression: {dialects: [{dialect: ANSI_SQL, expression: "SUM(orders.amount)"}]}
        custom_extensions:
          - vendor_name: METIS
            data: '{"kind":"definition_filter","filters":[{"field":"tier","operator":"eq","value":"gold"}]}'
`

const postAggregateDefinitionFilterModel = `
version: "0.2.0.dev0"
semantic_model:
  - name: sales
    datasets:
      - name: orders
        source: sales.orders
        fields:
          - name: amount
            datatype: Decimal
            expression: {dialects: [{dialect: ANSI_SQL, expression: orders.amount}]}
          - name: order_count
            datatype: Integer
            expression: {dialects: [{dialect: ANSI_SQL, expression: orders.order_count}]}
    metrics:
      - name: revenue
        datatype: Decimal
        expression: {dialects: [{dialect: ANSI_SQL, expression: "SUM(orders.amount)"}]}
      - name: orders
        datatype: Integer
        expression: {dialects: [{dialect: ANSI_SQL, expression: "SUM(orders.order_count)"}]}
      - name: average_order_value
        datatype: Decimal
        expression: {dialects: [{dialect: ANSI_SQL, expression: "revenue / orders"}]}
        custom_extensions:
          - vendor_name: METIS
            data: '{"kind":"definition_filter","stage":"post_aggregation","filters":[{"field":"average_order_value","operator":"gte","value":50}]}'
`

func TestMetricDefinitionFilterIsOwnedBySourceMetric(t *testing.T) {
	resolved := resolveMetricEvaluation(t, metricDefinitionFilterModel, query.SemanticQuery{
		Model: "sales", Metrics: []query.MetricRef{{Name: "gold_revenue"}},
	})
	plan, err := planner.New().Plan(context.Background(), resolved, mustRenderer(t, "DUCKDB"))
	if err != nil {
		t.Fatal(err)
	}
	graph := plan
	if len(semanticPlanNodeFixturesForTest(graph)) != 1 {
		t.Fatalf("stages = %#v", semanticPlanNodeFixturesForTest(graph))
	}
	stage := semanticPlanNodeFixturesForTest(graph)[0]
	if len(stage.Predicates) != 1 || stage.Predicates[0].Predicate == nil {
		t.Fatalf("predicates = %#v", stage.Predicates)
	}
	predicate := *stage.Predicates[0].Predicate
	if predicate.Dataset != "customers" || predicate.Filter.Field != "tier" || predicate.Expression.Source != "customers.tier" {
		t.Fatalf("predicate = %#v", predicate)
	}
	if len(stage.Joins) != 1 || stage.Joins[0].ToDataset != "customers" {
		t.Fatalf("joins = %#v", stage.Joins)
	}
}

func TestMetricDefinitionFilterComposesWithQueryPredicate(t *testing.T) {
	resolved := resolveMetricEvaluation(t, metricDefinitionFilterModel, query.SemanticQuery{
		Model:   "sales",
		Metrics: []query.MetricRef{{Name: "gold_revenue"}},
		Filters: []query.Filter{{Field: "amount", Operator: query.FilterGT, Value: 0}},
	})
	plan, err := planner.New().Plan(context.Background(), resolved, mustRenderer(t, "DUCKDB"))
	if err != nil {
		t.Fatal(err)
	}
	stage := semanticPlanNodeFixturesForTest(plan)[0]
	if len(stage.Predicates) != 2 {
		t.Fatalf("predicates = %#v", stage.Predicates)
	}
}

func TestPostAggregateDefinitionFilterIsOwnedByDerivedMetricOutput(t *testing.T) {
	resolved := resolveMetricEvaluation(t, postAggregateDefinitionFilterModel, query.SemanticQuery{
		Model: "sales", Metrics: []query.MetricRef{{Name: "average_order_value"}},
	})
	plan, err := planner.New().Plan(context.Background(), resolved, mustRenderer(t, "DUCKDB"))
	if err != nil {
		t.Fatal(err)
	}
	graph := plan
	if len(graph.Output.Predicates) != 1 {
		t.Fatalf("post predicates = %#v", graph.Output.Predicates)
	}
	predicate := graph.Output.Predicates[0]
	if predicate.Name != "average_order_value" || predicate.Filter.Field != "average_order_value" || predicate.Filter.Operator != query.FilterGTE || predicate.Filter.Value != float64(50) {
		t.Fatalf("post predicate = %#v", predicate)
	}
	for _, stage := range semanticPlanNodeFixturesForTest(graph) {
		if len(stage.Predicates) != 0 {
			t.Fatalf("post filter leaked into source stage %q: %#v", stage.ID, stage.Predicates)
		}
	}
}

func TestPostAggregateDefinitionFilterComposesWithQueryMetricPredicate(t *testing.T) {
	resolved := resolveMetricEvaluation(t, postAggregateDefinitionFilterModel, query.SemanticQuery{
		Model:   "sales",
		Metrics: []query.MetricRef{{Name: "average_order_value"}},
		Filters: []query.Filter{{Field: "average_order_value", Operator: query.FilterLTE, Value: 100}},
	})
	plan, err := planner.New().Plan(context.Background(), resolved, mustRenderer(t, "DUCKDB"))
	if err != nil {
		t.Fatal(err)
	}
	if got := len(plan.Output.Predicates); got != 2 {
		t.Fatalf("post predicates = %d, want 2", got)
	}
}
