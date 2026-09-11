package compiler_test

import (
	"strings"
	"testing"

	"github.com/meaningforge/metis/query"
)

const customCalendarSemiAdditiveCompilerModel = `
version: "0.2.0.dev0"
semantic_model:
  - name: fiscal_inventory
    custom_extensions:
      - vendor_name: METIS
        data: '{"kind":"custom_calendar","dataset":"inventory","base_time_dimension":"snapshot_date","grains":[{"name":"fiscal_week","bucket_dimension":"fiscal_week_start","ordinal_dimension":"fiscal_week_index","dense_mapping":true}]}'
    datasets:
      - name: inventory
        source: analytics.inventory_snapshot
        fields:
          - name: snapshot_date
            datatype: Date
            expression: {dialects: [{dialect: ANSI_SQL, expression: snapshot_date}]}
            dimension: {is_time: true}
          - name: fiscal_week_start
            datatype: Date
            expression: {dialects: [{dialect: ANSI_SQL, expression: fiscal_week_start}]}
            dimension: {}
          - name: fiscal_week_index
            datatype: Integer
            expression: {dialects: [{dialect: ANSI_SQL, expression: fiscal_week_index}]}
            dimension: {}
          - name: quantity
            datatype: Decimal
            expression: {dialects: [{dialect: ANSI_SQL, expression: quantity}]}
    metrics:
      - name: inventory_quantity
        datatype: Decimal
        expression: {dialects: [{dialect: ANSI_SQL, expression: "SUM(quantity)"}]}
      - name: inventory_balance
        datatype: Decimal
        expression: {dialects: [{dialect: ANSI_SQL, expression: inventory_quantity}]}
        custom_extensions:
          - vendor_name: METIS
            data: '{"kind":"semi_additive","base_metric":"inventory_quantity","non_additive_dimension":"snapshot_date","aggregation":"last"}'
`

func TestCustomCalendarSemiAdditiveCompilerConformance(t *testing.T) {
	grain := query.TimeGrain("fiscal_week")
	query := query.SemanticQuery{
		Metrics:    []query.MetricRef{{Name: "inventory_balance"}},
		Dimensions: []query.DimensionRef{{Name: "snapshot_date", Grain: &grain}},
	}
	for _, dialect := range []string{"DUCKDB", "DORIS", "CLICKHOUSE"} {
		dialect := dialect
		t.Run(dialect, func(t *testing.T) {
			sqlQuery, err := compileModel(t, []byte(customCalendarSemiAdditiveCompilerModel), "fiscal_inventory", query, dialect)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"fiscal_week_start", "__metis_ordered_snapshot_date", "snapshot_date"} {
				if !strings.Contains(sqlQuery.SQL, want) {
					t.Fatalf("%s custom semi-additive SQL missing %q:\n%s", dialect, want, sqlQuery.SQL)
				}
			}
			upper := strings.ToUpper(sqlQuery.SQL)
			if strings.Contains(upper, "DATE_TRUNC") || strings.Contains(sqlQuery.SQL, "toStartOf") {
				t.Fatalf("%s custom semi-additive SQL fell back to built-in calendar semantics:\n%s", dialect, sqlQuery.SQL)
			}
		})
	}
}
