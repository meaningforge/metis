package compiler_test

import (
	"strings"
	"testing"

	"github.com/meaningforge/metis/query"
)

const customCalendarCompilerModel = `
version: "0.2.0.dev0"
semantic_model:
  - name: fiscal
    custom_extensions:
      - vendor_name: METIS
        data: '{"kind":"custom_calendar","dataset":"calendar","base_time_dimension":"day","grains":[{"name":"fiscal_week","bucket_dimension":"fiscal_week_start","ordinal_dimension":"fiscal_week_index"}]}'
    datasets:
      - name: calendar
        source: analytics.calendar
        fields:
          - name: day
            datatype: Date
            expression: {dialects: [{dialect: ANSI_SQL, expression: day}]}
            dimension: {}
          - name: fiscal_week_start
            datatype: Date
            expression: {dialects: [{dialect: ANSI_SQL, expression: fiscal_week_start}]}
            dimension: {}
          - name: fiscal_week_index
            datatype: Integer
            expression: {dialects: [{dialect: ANSI_SQL, expression: fiscal_week_index}]}
            dimension: {}
          - name: revenue
            datatype: Decimal
            expression: {dialects: [{dialect: ANSI_SQL, expression: revenue}]}
    metrics:
      - name: total_revenue
        datatype: Decimal
        expression: {dialects: [{dialect: ANSI_SQL, expression: "SUM(revenue)"}]}
`

func TestCustomCalendarGroupingCompilerConformance(t *testing.T) {
	grain := query.TimeGrain("fiscal_week")
	query := query.SemanticQuery{
		Metrics:    []query.MetricRef{{Name: "total_revenue"}},
		Dimensions: []query.DimensionRef{{Name: "day", Grain: &grain}},
	}
	for _, dialect := range []string{"DUCKDB", "DORIS", "CLICKHOUSE"} {
		t.Run(dialect, func(t *testing.T) {
			sqlQuery, err := compileModel(t, []byte(customCalendarCompilerModel), "fiscal", query, dialect)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(sqlQuery.SQL, "fiscal_week_start") {
				t.Fatalf("%s SQL missing declared custom bucket:\n%s", dialect, sqlQuery.SQL)
			}
			if strings.Contains(strings.ToUpper(sqlQuery.SQL), "DATE_TRUNC") || strings.Contains(sqlQuery.SQL, "toStartOf") {
				t.Fatalf("%s SQL incorrectly lowered custom calendar through built-in time truncation:\n%s", dialect, sqlQuery.SQL)
			}
			if !strings.Contains(strings.ToUpper(sqlQuery.SQL), "GROUP BY") {
				t.Fatalf("%s SQL missing custom calendar grouping:\n%s", dialect, sqlQuery.SQL)
			}
		})
	}
}
