package compiler_test

import (
	"strings"
	"testing"

	"github.com/meaningforge/metis/query"
)

const customCalendarGrainToDateCompilerModel = `
version: "0.2.0.dev0"
semantic_model:
  - name: fiscal_gtd
    custom_extensions:
      - vendor_name: METIS
        data: '{"kind":"custom_calendar","dataset":"calendar","base_time_dimension":"day","grains":[{"name":"fiscal_week","bucket_dimension":"fiscal_week_start","ordinal_dimension":"fiscal_week_index","dense_mapping":true,"parent_grain":"fiscal_quarter"},{"name":"fiscal_quarter","bucket_dimension":"fiscal_quarter_start","ordinal_dimension":"fiscal_quarter_index","dense_mapping":true}]}'
    datasets:
      - name: calendar
        source: analytics.calendar
        fields:
          - {name: day, datatype: Date, expression: {dialects: [{dialect: ANSI_SQL, expression: day}]}, dimension: {is_time: true}}
          - {name: fiscal_week_start, datatype: Date, expression: {dialects: [{dialect: ANSI_SQL, expression: fiscal_week_start}]}, dimension: {}}
          - {name: fiscal_week_index, datatype: Integer, expression: {dialects: [{dialect: ANSI_SQL, expression: fiscal_week_index}]}, dimension: {}}
          - {name: fiscal_quarter_start, datatype: Date, expression: {dialects: [{dialect: ANSI_SQL, expression: fiscal_quarter_start}]}, dimension: {}}
          - {name: fiscal_quarter_index, datatype: Integer, expression: {dialects: [{dialect: ANSI_SQL, expression: fiscal_quarter_index}]}, dimension: {}}
          - {name: region, datatype: String, expression: {dialects: [{dialect: ANSI_SQL, expression: region}]}, dimension: {}}
          - {name: revenue, datatype: Decimal, expression: {dialects: [{dialect: ANSI_SQL, expression: revenue}]}}
    metrics:
      - name: revenue
        datatype: Decimal
        expression: {dialects: [{dialect: ANSI_SQL, expression: "SUM(calendar.revenue)"}]}
        custom_extensions:
          - vendor_name: METIS
            data: '{"kind":"time_binding","time_dimension":"day"}'
      - name: fiscal_quarter_to_date_revenue
        datatype: Decimal
        expression: {dialects: [{dialect: ANSI_SQL, expression: revenue}]}
        custom_extensions:
          - vendor_name: METIS
            data: '{"kind":"cumulative","base_metric":"revenue","time_dimension":"day","window":{"type":"grain_to_date","unit":"fiscal_quarter"}}'
`

func TestCustomCalendarGrainToDateCompilerConformance(t *testing.T) {
	grain := query.TimeGrain("fiscal_week")
	query := query.SemanticQuery{
		Metrics:    []query.MetricRef{{Name: "fiscal_quarter_to_date_revenue"}},
		Dimensions: []query.DimensionRef{{Name: "day", Grain: &grain}, {Name: "region"}},
	}
	for _, dialect := range []string{"DUCKDB", "DORIS", "CLICKHOUSE"} {
		dialect := dialect
		t.Run(dialect, func(t *testing.T) {
			sqlQuery, err := compileModel(t, []byte(customCalendarGrainToDateCompilerModel), "fiscal_gtd", query, dialect)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"__metis_custom_gtd_periods", "fiscal_week_start", "fiscal_week_index", "fiscal_quarter_start", "ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW"} {
				if !strings.Contains(sqlQuery.SQL, want) {
					t.Fatalf("%s custom grain-to-date SQL missing %q:\n%s", dialect, want, sqlQuery.SQL)
				}
			}
			upper := strings.ToUpper(sqlQuery.SQL)
			if strings.Contains(upper, "DATE_TRUNC") || strings.Contains(upper, "INTERVAL") || strings.Contains(sqlQuery.SQL, "toStartOf") {
				t.Fatalf("%s custom grain-to-date SQL fell back to built-in calendar semantics:\n%s", dialect, sqlQuery.SQL)
			}
			if !strings.Contains(sqlQuery.SQL, "PARTITION BY") || !strings.Contains(sqlQuery.SQL, "region") || !strings.Contains(sqlQuery.SQL, "__metis_reset_bucket") {
				t.Fatalf("%s custom grain-to-date SQL lost reset/non-time partitioning:\n%s", dialect, sqlQuery.SQL)
			}
		})
	}
}
