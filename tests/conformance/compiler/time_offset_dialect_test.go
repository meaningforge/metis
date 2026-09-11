package compiler_test

import (
	"strings"
	"testing"

	"github.com/meaningforge/metis/tests/conformance/scenarios"
)

func TestTimeOffsetDialectRendering(t *testing.T) {
	cases := []struct {
		dialect  string
		scenario string
		want     string
	}{
		{dialect: "DUCKDB", scenario: "time_offset_previous_month", want: "INTERVAL '1' MONTH"},
		{dialect: "DUCKDB", scenario: "time_offset_previous_quarter", want: "INTERVAL '1' QUARTER"},
		{dialect: "DUCKDB", scenario: "time_offset_previous_year", want: "INTERVAL '1' YEAR"},
		{dialect: "DORIS", scenario: "time_offset_previous_month", want: "INTERVAL 1 MONTH"},
		{dialect: "DORIS", scenario: "time_offset_previous_quarter", want: "INTERVAL 3 MONTH"},
		{dialect: "DORIS", scenario: "time_offset_previous_year", want: "INTERVAL 1 YEAR"},
		{dialect: "CLICKHOUSE", scenario: "time_offset_previous_month", want: "addMonths("},
		{dialect: "CLICKHOUSE", scenario: "time_offset_previous_quarter", want: "addQuarters("},
		{dialect: "CLICKHOUSE", scenario: "time_offset_previous_year", want: "addYears("},
	}

	for _, tc := range cases {
		t.Run(tc.dialect+"/"+tc.scenario, func(t *testing.T) {
			scenario, ok := scenarios.ByName(tc.scenario)
			if !ok {
				t.Fatalf("scenario %q not found", tc.scenario)
			}
			_, sqlQuery, err := compile(t, scenario.Query, tc.dialect)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(sqlQuery.SQL, tc.want) {
				t.Fatalf("%s SQL missing %q:\n%s", tc.dialect, tc.want, sqlQuery.SQL)
			}
		})
	}
}
