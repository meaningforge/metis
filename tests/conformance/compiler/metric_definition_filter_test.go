package compiler_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/renderer/sql"
)

const metricDefinitionFilterCompilerModel = `
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

const postAggregateDefinitionFilterCompilerModel = `
version: "0.2.0.dev0"
semantic_model:
  - name: sales
    datasets:
      - name: orders
        source: sales.orders
        fields:
          - name: segment
            datatype: String
            dimension: {}
            expression: {dialects: [{dialect: ANSI_SQL, expression: orders.segment}]}
          - name: amount
            datatype: Decimal
            expression: {dialects: [{dialect: ANSI_SQL, expression: orders.amount}]}
          - name: order_count_value
            datatype: Integer
            expression: {dialects: [{dialect: ANSI_SQL, expression: orders.order_count_value}]}
    metrics:
      - name: revenue
        datatype: Decimal
        expression: {dialects: [{dialect: ANSI_SQL, expression: "SUM(orders.amount)"}]}
      - name: order_count
        datatype: Integer
        expression: {dialects: [{dialect: ANSI_SQL, expression: "SUM(orders.order_count_value)"}]}
      - name: average_order_value
        datatype: Decimal
        expression: {dialects: [{dialect: ANSI_SQL, expression: "revenue / order_count"}]}
        custom_extensions:
          - vendor_name: METIS
            data: '{"kind":"definition_filter","stage":"post_aggregation","filters":[{"field":"average_order_value","operator":"gte","value":50}]}'
`

func TestMetricDefinitionFilterCompilerConformance(t *testing.T) {
	for _, dialect := range []string{"DUCKDB", "DORIS", "CLICKHOUSE"} {
		t.Run(dialect, func(t *testing.T) {
			query, err := compileModel(t, []byte(metricDefinitionFilterCompilerModel), "sales", query.SemanticQuery{
				Metrics: []query.MetricRef{{Name: "gold_revenue"}},
			}, dialect)
			if err != nil {
				t.Fatal(err)
			}
			lowerSQL := strings.ToLower(query.SQL)
			if !strings.Contains(lowerSQL, "customers") || !strings.Contains(lowerSQL, "tier") {
				t.Fatalf("%s SQL does not carry definition-filter relationship predicate:\n%s", dialect, query.SQL)
			}
			if !strings.Contains(lowerSQL, "join") {
				t.Fatalf("%s SQL does not join definition-filter dataset:\n%s", dialect, query.SQL)
			}
			if !strings.Contains(lowerSQL, "sum") || !strings.Contains(lowerSQL, "amount") {
				t.Fatalf("%s SQL does not preserve owning metric aggregate:\n%s", dialect, query.SQL)
			}
			if !containsParameter(query.Parameters, "gold") {
				t.Fatalf("%s parameters = %#v, want definition-filter value gold", dialect, query.Parameters)
			}
		})
	}
}

func TestMetricDefinitionFilterComposesWithQueryFilterInSQL(t *testing.T) {
	query, err := compileModel(t, []byte(metricDefinitionFilterCompilerModel), "sales", query.SemanticQuery{
		Metrics: []query.MetricRef{{Name: "gold_revenue"}},
		Filters: []query.Filter{{Field: "amount", Operator: query.FilterGT, Value: 100}},
	}, "CLICKHOUSE")
	if err != nil {
		t.Fatal(err)
	}
	lowerSQL := strings.ToLower(query.SQL)
	if !strings.Contains(lowerSQL, "tier") || !strings.Contains(lowerSQL, "amount") {
		t.Fatalf("ClickHouse SQL does not compose intrinsic and query filters:\n%s", query.SQL)
	}
	if !containsParameter(query.Parameters, "gold") || !containsParameter(query.Parameters, 100) {
		t.Fatalf("parameters = %#v, want gold and 100", query.Parameters)
	}
}

func TestPostAggregateDefinitionFilterCompilerConformance(t *testing.T) {
	for _, dialect := range []string{"DUCKDB", "DORIS", "CLICKHOUSE"} {
		t.Run(dialect, func(t *testing.T) {
			query, err := compileModel(t, []byte(postAggregateDefinitionFilterCompilerModel), "sales", query.SemanticQuery{
				Metrics:    []query.MetricRef{{Name: "average_order_value"}},
				Dimensions: []query.DimensionRef{{Name: "segment"}},
			}, dialect)
			if err != nil {
				t.Fatal(err)
			}
			lowerSQL := strings.ToLower(query.SQL)
			if !strings.Contains(lowerSQL, "average_order_value") || !strings.Contains(lowerSQL, "revenue") || !strings.Contains(lowerSQL, "order_count") {
				t.Fatalf("%s SQL does not preserve derived metric staging:\n%s", dialect, query.SQL)
			}
			if !strings.Contains(lowerSQL, "where") {
				t.Fatalf("%s SQL does not lower intrinsic post-aggregate predicate after evaluation:\n%s", dialect, query.SQL)
			}
			if !containsParameter(query.Parameters, 50) {
				t.Fatalf("%s parameters = %#v, want intrinsic post-aggregate threshold 50", dialect, query.Parameters)
			}
		})
	}
}

func TestPostAggregateDefinitionFilterComposesWithQueryMetricFilterInSQL(t *testing.T) {
	query, err := compileModel(t, []byte(postAggregateDefinitionFilterCompilerModel), "sales", query.SemanticQuery{
		Metrics:    []query.MetricRef{{Name: "average_order_value"}},
		Dimensions: []query.DimensionRef{{Name: "segment"}},
		Filters:    []query.Filter{{Field: "average_order_value", Operator: query.FilterLTE, Value: 100}},
	}, "CLICKHOUSE")
	if err != nil {
		t.Fatal(err)
	}
	if !containsParameter(query.Parameters, 50) || !containsParameter(query.Parameters, 100) {
		t.Fatalf("parameters = %#v, want intrinsic 50 and query-time 100 thresholds", query.Parameters)
	}
	if strings.Count(strings.ToUpper(query.SQL), "WHERE") < 1 {
		t.Fatalf("ClickHouse SQL does not carry composed post-evaluation predicates:\n%s", query.SQL)
	}
}

func containsParameter(parameters []sql.QueryParameter, want any) bool {
	wantText := fmt.Sprint(want)
	for _, parameter := range parameters {
		if fmt.Sprint(parameter.Value) == wantText {
			return true
		}
	}
	return false
}
