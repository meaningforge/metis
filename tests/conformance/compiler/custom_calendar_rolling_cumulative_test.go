package compiler_test

import (
	"strings"
	"testing"

	"github.com/meaningforge/metis/query"
)

const customCalendarRollingCompilerModel = `
version: "0.2.0.dev0"
semantic_model:
  - name: fiscal_rolling
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
          - name: region
            datatype: String
            expression: {dialects: [{dialect: ANSI_SQL, expression: region}]}
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
      - name: rolling_3_fiscal_week_revenue
        datatype: Decimal
        expression: {dialects: [{dialect: ANSI_SQL, expression: revenue}]}
        custom_extensions:
          - vendor_name: METIS
            data: '{"kind":"cumulative","base_metric":"revenue","time_dimension":"day","window":{"type":"rolling","count":3,"unit":"fiscal_week"}}'
`

func TestCustomCalendarRollingCumulativeCompilerConformance(t *testing.T) {
	grain := query.TimeGrain("fiscal_week")
	query := query.SemanticQuery{
		Metrics: []query.MetricRef{{Name: "rolling_3_fiscal_week_revenue"}},
		Dimensions: []query.DimensionRef{
			{Name: "day", Grain: &grain},
			{Name: "region"},
		},
	}
	for _, dialect := range []string{"DUCKDB", "DORIS", "CLICKHOUSE"} {
		dialect := dialect
		t.Run(dialect, func(t *testing.T) {
			sqlQuery, err := compileModel(t, []byte(customCalendarRollingCompilerModel), "fiscal_rolling", query, dialect)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"__sparse", "__periods", "__metis_dense_ordinal", "fiscal_week_start", "fiscal_week_index", "ROWS BETWEEN 2 PRECEDING AND CURRENT ROW"} {
				if !strings.Contains(sqlQuery.SQL, want) {
					t.Fatalf("%s custom rolling SQL missing %q:\n%s", dialect, want, sqlQuery.SQL)
				}
			}
			upper := strings.ToUpper(sqlQuery.SQL)
			if strings.Contains(upper, "DATE_TRUNC") || strings.Contains(upper, "INTERVAL") || strings.Contains(sqlQuery.SQL, "toStartOf") {
				t.Fatalf("%s custom rolling SQL fell back to built-in calendar semantics:\n%s", dialect, sqlQuery.SQL)
			}
			if !strings.Contains(sqlQuery.SQL, "PARTITION BY") || !strings.Contains(sqlQuery.SQL, "region") {
				t.Fatalf("%s custom rolling SQL lost non-time window partition:\n%s", dialect, sqlQuery.SQL)
			}
		})
	}
}
