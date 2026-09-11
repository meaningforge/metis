package compiler_test

import (
	"strings"
	"testing"

	"github.com/meaningforge/metis/tests/conformance/scenarios"
)

func TestCumulativeQuarterAggregatesBeforeWindow(t *testing.T) {
	scenario, ok := scenarios.ByName("cumulative_metric_by_quarter_and_region")
	if !ok {
		t.Fatal("missing cumulative quarter scenario")
	}
	_, sqlQuery, err := compile(t, scenario.Query, "DORIS")
	if err != nil {
		t.Fatal(err)
	}

	groupBy := strings.Index(sqlQuery.SQL, "GROUP BY")
	window := strings.Index(sqlQuery.SQL, "OVER (")
	if groupBy < 0 || window < 0 {
		t.Fatalf("expected grouped source aggregate followed by cumulative window:\n%s", sqlQuery.SQL)
	}
	if groupBy > window {
		t.Fatalf("cumulative window appears before requested-grain aggregation:\n%s", sqlQuery.SQL)
	}
	if !strings.Contains(sqlQuery.SQL, "quarter") && !strings.Contains(sqlQuery.SQL, "QUARTER") {
		t.Fatalf("quarter grain lowering missing:\n%s", sqlQuery.SQL)
	}
	if !strings.Contains(sqlQuery.SQL, "PARTITION BY") || !strings.Contains(sqlQuery.SQL, "region") {
		t.Fatalf("region partition missing from cumulative quarter query:\n%s", sqlQuery.SQL)
	}
}
