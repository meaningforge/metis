package compiler_test

import (
	"strings"
	"testing"

	"github.com/meaningforge/metis/query"
)

const distinctValuesCompilerModel = `
version: "0.2.0.dev0"
semantic_model:
  - name: sales
    datasets:
      - name: orders
        source: sales.orders
        fields:
          - name: region
            datatype: String
            dimension: {}
            expression: {dialects: [{dialect: ANSI_SQL, expression: orders.region}]}
          - name: country
            datatype: String
            dimension: {}
            expression: {dialects: [{dialect: ANSI_SQL, expression: orders.country}]}
`

func TestDistinctValuesCompilerConformance(t *testing.T) {
	limit := 20
	for _, dialect := range []string{"DUCKDB", "DORIS", "CLICKHOUSE"} {
		t.Run(dialect, func(t *testing.T) {
			query, err := compileModel(t, []byte(distinctValuesCompilerModel), "sales", query.SemanticQuery{
				Intent:     query.QueryIntentDistinctValues,
				Dimensions: []query.DimensionRef{{Name: "region"}},
				Filters:    []query.Filter{{Field: "country", Operator: query.FilterEQ, Value: "JP"}},
				OrderBy:    []query.OrderBy{{Field: "region", Direction: query.SortAsc}},
				Limit:      &limit,
			}, dialect)
			if err != nil {
				t.Fatal(err)
			}
			upper := strings.ToUpper(query.SQL)
			if !strings.Contains(upper, "GROUP BY") || !strings.Contains(strings.ToLower(query.SQL), "region") {
				t.Fatalf("%s distinct-values SQL must group by requested dimension:\n%s", dialect, query.SQL)
			}
			if !strings.Contains(upper, "ORDER BY") || (!strings.Contains(upper, "LIMIT") && !strings.Contains(upper, "FETCH FIRST")) {
				t.Fatalf("%s distinct-values SQL must preserve ordering and limit:\n%s", dialect, query.SQL)
			}
			if len(query.Parameters) != 1 || query.Parameters[0].Value != "JP" {
				t.Fatalf("%s parameters = %#v, want country filter JP", dialect, query.Parameters)
			}
			if strings.Contains(upper, "SUM(") || strings.Contains(upper, "COUNT(") {
				t.Fatalf("%s distinct-values SQL must not introduce metric aggregation:\n%s", dialect, query.SQL)
			}
		})
	}
}
