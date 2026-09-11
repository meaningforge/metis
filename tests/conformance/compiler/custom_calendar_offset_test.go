package compiler_test

import (
	"strings"
	"testing"

	"github.com/meaningforge/metis/query"
)

const customCalendarOffsetCompilerModel = `
version: "0.2.0.dev0"
semantic_model:
  - name: fiscal_offset
    custom_extensions:
      - vendor_name: METIS
        data: '{"kind":"custom_calendar","dataset":"calendar","base_time_dimension":"day","grains":[{"name":"fiscal_week","bucket_dimension":"fiscal_week_start","ordinal_dimension":"fiscal_week_index","dense_mapping":true}]}'
    datasets:
      - name: calendar
        source: analytics.calendar
        fields:
          - name: day
            datatype: Date
            expression: {dialects: [{dialect: ANSI_SQL, expression: day}]}
            dimension: {is_time: true}
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
      - name: revenue
        datatype: Decimal
        expression: {dialects: [{dialect: ANSI_SQL, expression: "SUM(calendar.revenue)"}]}
        custom_extensions:
          - vendor_name: METIS
            data: '{"kind":"time_binding","time_dimension":"day"}'
      - name: previous_fiscal_week_revenue
        datatype: Decimal
        expression: {dialects: [{dialect: ANSI_SQL, expression: revenue}]}
        custom_extensions:
          - vendor_name: METIS
            data: '{"kind":"time_offset","base_metric":"revenue","time_dimension":"day","offset":{"count":-1,"unit":"fiscal_week"}}'
`

func TestCustomCalendarOffsetCompilerConformance(t *testing.T) {
	grain := query.TimeGrain("fiscal_week")
	query := query.SemanticQuery{
		Metrics:    []query.MetricRef{{Name: "previous_fiscal_week_revenue"}},
		Dimensions: []query.DimensionRef{{Name: "day", Grain: &grain}},
	}
	for _, dialect := range []string{"DUCKDB", "DORIS", "CLICKHOUSE"} {
		t.Run(dialect, func(t *testing.T) {
			sqlQuery, err := compileModel(t, []byte(customCalendarOffsetCompilerModel), "fiscal_offset", query, dialect)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"__metis_calendar_periods", "fiscal_week_start", "fiscal_week_index"} {
				if !strings.Contains(sqlQuery.SQL, want) {
					t.Fatalf("%s SQL missing %q:\n%s", dialect, want, sqlQuery.SQL)
				}
			}
			unquoted := strings.NewReplacer("\"", "", "`", "").Replace(sqlQuery.SQL)
			if !strings.Contains(unquoted, "analytics.calendar") {
				t.Fatalf("%s SQL lost custom calendar source:\n%s", dialect, sqlQuery.SQL)
			}
			upper := strings.ToUpper(sqlQuery.SQL)
			if strings.Contains(upper, "INTERVAL") || strings.Contains(sqlQuery.SQL, "addWeeks(") || strings.Contains(sqlQuery.SQL, "addMonths(") {
				t.Fatalf("%s SQL incorrectly interpreted custom grain as a dialect calendar interval:\n%s", dialect, sqlQuery.SQL)
			}
			if !strings.Contains(sqlQuery.SQL, "+ 1") {
				t.Fatalf("%s SQL missing ordinal movement for previous fiscal period:\n%s", dialect, sqlQuery.SQL)
			}
		})
	}
}
