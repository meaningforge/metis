package compiler_test

import (
	"strings"
	"testing"

	"github.com/meaningforge/metis/query"
)

const offsetToGrainCompilerModel = `
version: "0.2.0.dev0"
semantic_model:
  - name: offset_to_grain
    datasets:
      - name: orders
        source: analytics.orders
        fields:
          - {name: order_date, datatype: Date, expression: {dialects: [{dialect: ANSI_SQL, expression: order_date}]}, dimension: {is_time: true}}
          - {name: region, datatype: String, expression: {dialects: [{dialect: ANSI_SQL, expression: region}]}, dimension: {}}
          - {name: revenue, datatype: Decimal, expression: {dialects: [{dialect: ANSI_SQL, expression: revenue}]}}
    metrics:
      - name: revenue
        datatype: Decimal
        expression: {dialects: [{dialect: ANSI_SQL, expression: "SUM(orders.revenue)"}]}
      - name: revenue_at_year_start
        datatype: Decimal
        expression: {dialects: [{dialect: ANSI_SQL, expression: revenue}]}
        custom_extensions:
          - vendor_name: METIS
            data: '{"kind":"offset_to_grain","base_metric":"revenue","time_dimension":"order_date","grain":"year"}'
`

func TestOffsetToGrainCompilerConformance(t *testing.T) {
	grain := query.TimeGrainMonth
	query := query.SemanticQuery{
		Metrics:    []query.MetricRef{{Name: "revenue_at_year_start"}},
		Dimensions: []query.DimensionRef{{Name: "order_date", Grain: &grain}, {Name: "region"}},
	}
	for _, dialect := range []string{"DUCKDB", "DORIS", "CLICKHOUSE"} {
		dialect := dialect
		t.Run(dialect, func(t *testing.T) {
			sqlQuery, err := compileModel(t, []byte(offsetToGrainCompilerModel), "offset_to_grain", query, dialect)
			if err != nil {
				t.Fatal(err)
			}
			upper := strings.ToUpper(sqlQuery.SQL)
			for _, want := range []string{"FIRST_VALUE", "PARTITION BY", "ORDER BY"} {
				if !strings.Contains(upper, want) {
					t.Fatalf("%s offset-to-grain SQL missing %q:\n%s", dialect, want, sqlQuery.SQL)
				}
			}
			if !strings.Contains(strings.ToLower(sqlQuery.SQL), "region") {
				t.Fatalf("%s SQL lost non-time partition:\n%s", dialect, sqlQuery.SQL)
			}
			if strings.Contains(upper, "INTERVAL") {
				t.Fatalf("%s offset-to-grain incorrectly lowered as N-period shift:\n%s", dialect, sqlQuery.SQL)
			}
		})
	}
}

const customOffsetToGrainCompilerModel = `
version: "0.2.0.dev0"
semantic_model:
  - name: fiscal_offset_to_grain
    custom_extensions:
      - vendor_name: METIS
        data: '{"kind":"custom_calendar","dataset":"calendar","base_time_dimension":"day","grains":[{"name":"fiscal_week","bucket_dimension":"fiscal_week_start","ordinal_dimension":"fiscal_week_index","dense_mapping":true,"parent_grain":"fiscal_year"},{"name":"fiscal_year","bucket_dimension":"fiscal_year_start","ordinal_dimension":"fiscal_year_index","dense_mapping":true}]}'
    datasets:
      - name: calendar
        source: analytics.calendar
        fields:
          - {name: day, datatype: Date, expression: {dialects: [{dialect: ANSI_SQL, expression: day}]}, dimension: {is_time: true}}
          - {name: fiscal_week_start, datatype: Date, expression: {dialects: [{dialect: ANSI_SQL, expression: fiscal_week_start}]}, dimension: {}}
          - {name: fiscal_week_index, datatype: Integer, expression: {dialects: [{dialect: ANSI_SQL, expression: fiscal_week_index}]}, dimension: {}}
          - {name: fiscal_year_start, datatype: Date, expression: {dialects: [{dialect: ANSI_SQL, expression: fiscal_year_start}]}, dimension: {}}
          - {name: fiscal_year_index, datatype: Integer, expression: {dialects: [{dialect: ANSI_SQL, expression: fiscal_year_index}]}, dimension: {}}
          - {name: region, datatype: String, expression: {dialects: [{dialect: ANSI_SQL, expression: region}]}, dimension: {}}
          - {name: revenue, datatype: Decimal, expression: {dialects: [{dialect: ANSI_SQL, expression: revenue}]}}
    metrics:
      - name: revenue
        datatype: Decimal
        expression: {dialects: [{dialect: ANSI_SQL, expression: "SUM(calendar.revenue)"}]}
      - name: revenue_at_fiscal_year_start
        datatype: Decimal
        expression: {dialects: [{dialect: ANSI_SQL, expression: revenue}]}
        custom_extensions:
          - vendor_name: METIS
            data: '{"kind":"offset_to_grain","base_metric":"revenue","time_dimension":"day","grain":"fiscal_year"}'
`

func TestCustomOffsetToGrainCompilerConformance(t *testing.T) {
	grain := query.TimeGrain("fiscal_week")
	query := query.SemanticQuery{
		Metrics:    []query.MetricRef{{Name: "revenue_at_fiscal_year_start"}},
		Dimensions: []query.DimensionRef{{Name: "day", Grain: &grain}, {Name: "region"}},
	}
	for _, dialect := range []string{"DUCKDB", "DORIS", "CLICKHOUSE"} {
		dialect := dialect
		t.Run(dialect, func(t *testing.T) {
			sqlQuery, err := compileModel(t, []byte(customOffsetToGrainCompilerModel), "fiscal_offset_to_grain", query, dialect)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"__metis_offset_to_grain_periods", "fiscal_week_start", "fiscal_week_index", "fiscal_year_start", "__metis_boundary_bucket", "__metis_query_ordinal", "__periods", "__sparse", "__metis_dense_ordinal"} {
				if !strings.Contains(sqlQuery.SQL, want) {
					t.Fatalf("%s custom offset-to-grain SQL missing %q:\n%s", dialect, want, sqlQuery.SQL)
				}
			}
			upper := strings.ToUpper(sqlQuery.SQL)
			if !strings.Contains(upper, "COALESCE") || !strings.Contains(sqlQuery.SQL, "0") {
				t.Fatalf("%s custom offset-to-grain SQL did not zero-fill the dense boundary domain:\n%s", dialect, sqlQuery.SQL)
			}
			if strings.Contains(upper, "INTERVAL") || strings.Contains(upper, "DATE_TRUNC") || strings.Contains(sqlQuery.SQL, "toStartOf") {
				t.Fatalf("%s custom offset-to-grain SQL fell back to built-in calendar semantics:\n%s", dialect, sqlQuery.SQL)
			}
		})
	}
}
