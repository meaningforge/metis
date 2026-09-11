package compiler_test

import (
	"context"
	"strings"
	"testing"

	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/renderer/sql"
	"github.com/meaningforge/metis/resolver"
	"github.com/meaningforge/metis/tests/conformance/scenarios"
)

func TestClickHouseTimeGrainRendering(t *testing.T) {
	cases := []struct {
		grain query.TimeGrain
		fn    string
	}{
		{query.TimeGrainYear, "toStartOfYear"},
		{query.TimeGrainQuarter, "toStartOfQuarter"},
		{query.TimeGrainMonth, "toStartOfMonth"},
		{query.TimeGrainWeek, "toStartOfWeek"},
		{query.TimeGrainDay, "toStartOfDay"},
		{query.TimeGrainHour, "toStartOfHour"},
	}
	for _, tc := range cases {
		t.Run(string(tc.grain), func(t *testing.T) {
			_, sqlQuery, err := compile(t, scenarios.TimeGrain("time_"+string(tc.grain), tc.grain).Query, "CLICKHOUSE")
			if err != nil {
				t.Fatal(err)
			}
			want := tc.fn + "(orders.order_date)"
			if tc.grain == query.TimeGrainWeek {
				want = tc.fn + "(orders.order_date, 1)"
			}
			if !strings.Contains(sqlQuery.SQL, want) {
				t.Fatalf("ClickHouse SQL missing %q:\n%s", want, sqlQuery.SQL)
			}
			if !strings.Contains(sqlQuery.SQL, "GROUP BY "+want) {
				t.Fatalf("ClickHouse SQL missing grouped time grain %q:\n%s", want, sqlQuery.SQL)
			}
		})
	}
}

func TestClickHouseTargetSpecificExpressionSelection(t *testing.T) {
	model := []byte(`version: "0.2.0.dev0"
semantic_model:
  - name: clickhouse_expression
    datasets:
      - name: orders_ext
        source: analytics.orders_ext
        fields:
          - {name: customer_id, datatype: String, expression: {dialects: [{dialect: ANSI_SQL, expression: customer_id}]}}
          - {name: amount, datatype: Decimal, expression: {dialects: [{dialect: ANSI_SQL, expression: amount}]}}
          - {name: status, datatype: String, expression: {dialects: [{dialect: ANSI_SQL, expression: status}]}}
      - name: customer_ext
        source: analytics.customer_ext
        primary_key: [customer_id]
        fields:
          - {name: customer_id, datatype: String, expression: {dialects: [{dialect: ANSI_SQL, expression: customer_id}]}}
          - {name: region, datatype: String, expression: {dialects: [{dialect: ANSI_SQL, expression: region}]}}
    relationships:
      - name: orders_to_customer
        from: orders_ext
        to: customer_ext
        from_columns: [customer_id]
        to_columns: [customer_id]
    metrics:
      - name: paid_revenue
        datatype: Decimal
        expression:
          dialects:
            - {dialect: ANSI_SQL, expression: "SUM(CASE WHEN status = 'paid' THEN amount END)"}
            - {dialect: CLICKHOUSE, expression: "sumIf(amount, status = 'paid')"}
            - {dialect: DORIS, expression: "SUM(IF(status = 'paid', amount, NULL))"}
      - name: apac_paid_revenue
        datatype: Decimal
        expression:
          dialects:
            - {dialect: ANSI_SQL, expression: "SUM(CASE WHEN customer_ext.region = 'APAC' AND orders_ext.status = 'paid' THEN orders_ext.amount END)"}
            - {dialect: CLICKHOUSE, expression: "sumIf(orders_ext.amount, customer_ext.region = 'APAC' AND orders_ext.status = 'paid')"}
`)

	for _, tc := range []struct {
		metric string
		want   []string
	}{
		{metric: "paid_revenue", want: []string{"sumIf(amount, status = 'paid')", "AS `paid_revenue`"}},
		{metric: "apac_paid_revenue", want: []string{"sumIf(orders_ext.amount, customer_ext.region = 'APAC' AND orders_ext.status = 'paid')", "AS `apac_paid_revenue`", "JOIN `analytics`.`customer_ext` AS `customer_ext`"}},
	} {
		t.Run(tc.metric, func(t *testing.T) {
			sqlQuery, err := compileModel(t, model, "clickhouse_expression", query.SemanticQuery{Metrics: []query.MetricRef{{Name: tc.metric}}}, "CLICKHOUSE")
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range tc.want {
				if !strings.Contains(sqlQuery.SQL, want) {
					t.Fatalf("ClickHouse SQL missing %q:\n%s", want, sqlQuery.SQL)
				}
			}
			if strings.Contains(sqlQuery.SQL, "SUM(CASE WHEN") {
				t.Fatalf("ClickHouse target-specific expression did not win over ANSI fallback:\n%s", sqlQuery.SQL)
			}
		})
	}
}

func TestClickHouseRejectsForeignOnlyExpression(t *testing.T) {
	model := []byte(`version: "0.2.0.dev0"
semantic_model:
  - name: foreign_only
    datasets:
      - name: orders_ext
        source: analytics.orders_ext
        fields:
          - {name: amount, datatype: Decimal, expression: {dialects: [{dialect: ANSI_SQL, expression: amount}]}}
    metrics:
      - name: snowflake_only_metric
        datatype: Decimal
        expression:
          dialects:
            - {dialect: SNOWFLAKE, expression: "SUM(orders_ext.amount)::NUMBER"}
`)

	_, err := compileModel(t, model, "foreign_only", query.SemanticQuery{Metrics: []query.MetricRef{{Name: "snowflake_only_metric"}}}, "CLICKHOUSE")
	if err == nil {
		t.Fatal("foreign-only expression unexpectedly compiled for ClickHouse")
	}
	if !strings.Contains(err.Error(), "UNSUPPORTED_EXPRESSION") && !strings.Contains(strings.ToLower(err.Error()), "unsupported") {
		t.Fatalf("error = %v, want explicit unsupported-expression error", err)
	}
}

func compileModel(t *testing.T, modelYAML []byte, model string, query query.SemanticQuery, dialect string) (sql.SQLRenderResult, error) {
	t.Helper()
	doc, err := ossie.NewLoader().Load(modelYAML)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := manifest.BuildProjectManifest(projectName, doc)
	if err != nil {
		t.Fatal(err)
	}
	query.Project = projectName
	query.Model = model
	renderer := mustRenderer(t, dialect)
	resolved, err := resolver.New(manifest.NewStore(snapshot)).ResolveForRenderer(context.Background(), query, renderer)
	if err != nil {
		return sql.SQLRenderResult{}, err
	}
	plan, err := planner.New().Plan(context.Background(), resolved, renderer)
	if err != nil {
		return sql.SQLRenderResult{}, err
	}
	sqlQuery, err := compilePlan(context.Background(), plan, renderer)
	if err != nil {
		return sql.SQLRenderResult{}, err
	}
	return sqlQuery, nil
}
