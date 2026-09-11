package compiler_test

import (
	"strings"
	"testing"

	"github.com/meaningforge/metis/query"
)

func TestCustomDenseCalendarCompilerConformance(t *testing.T) {
	grain := query.TimeGrain("fiscal_week")
	query := query.SemanticQuery{
		Metrics:    []query.MetricRef{{Name: "previous_fiscal_week_revenue"}},
		Dimensions: []query.DimensionRef{{Name: "day", Grain: &grain}},
	}
	for _, dialect := range []string{"DUCKDB", "DORIS", "CLICKHOUSE"} {
		dialect := dialect
		t.Run(dialect, func(t *testing.T) {
			sqlQuery, err := compileModel(t, []byte(customCalendarOffsetCompilerModel), "fiscal_offset", query, dialect)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"__sparse", "__periods", "__metis_dense_ordinal", "COALESCE", "fiscal_week_start", "fiscal_week_index"} {
				if !strings.Contains(sqlQuery.SQL, want) {
					t.Fatalf("%s custom dense SQL missing %q:\n%s", dialect, want, sqlQuery.SQL)
				}
			}
			unquoted := strings.NewReplacer("\"", "", "`", "").Replace(sqlQuery.SQL)
			if !strings.Contains(unquoted, "analytics.calendar") {
				t.Fatalf("%s custom dense SQL lost calendar source:\n%s", dialect, sqlQuery.SQL)
			}
			periodsPos := strings.Index(sqlQuery.SQL, "__periods")
			offsetPos := strings.Index(sqlQuery.SQL, "__metis_calendar_periods")
			if periodsPos < 0 || offsetPos < 0 || periodsPos > offsetPos {
				t.Fatalf("%s dense period expansion must precede custom offset evaluation:\n%s", dialect, sqlQuery.SQL)
			}
			upper := strings.ToUpper(sqlQuery.SQL)
			if strings.Contains(upper, "DATE_TRUNC") || strings.Contains(upper, "INTERVAL") || strings.Contains(sqlQuery.SQL, "toStartOf") {
				t.Fatalf("%s custom dense SQL fell back to built-in calendar semantics:\n%s", dialect, sqlQuery.SQL)
			}
		})
	}
}
