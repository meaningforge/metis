package compiler_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/meaningforge/metis/query"
)

func TestTimeOffsetDenseCalendarSeparatesOutputAndReadRanges(t *testing.T) {
	grain := query.TimeGrainMonth
	q := query.SemanticQuery{
		Metrics:    []query.MetricRef{{Name: "previous_month_revenue"}},
		Dimensions: []query.DimensionRef{{Name: "order_date", Grain: &grain}},
		Filters: []query.Filter{
			{Field: "order_date", Operator: query.FilterGTE, Value: "2026-06-01"},
			{Field: "order_date", Operator: query.FilterLTE, Value: "2026-08-31"},
		},
	}
	plan, _ := compileWithTimeSpine(t, q, "DORIS")
	if plan.DenseCalendar == nil {
		t.Fatal("dense calendar plan is nil")
	}
	if plan.DenseCalendar.QueryTimeDimension != "order_date" {
		t.Fatalf("query time dimension = %q, want order_date", plan.DenseCalendar.QueryTimeDimension)
	}
	if len(plan.DenseCalendar.OutputPredicates) != 2 {
		t.Fatalf("output predicates = %#v", plan.DenseCalendar.OutputPredicates)
	}
	if len(plan.DenseCalendar.ReadPredicates) != 2 {
		t.Fatalf("read predicates = %#v", plan.DenseCalendar.ReadPredicates)
	}
	if reflect.DeepEqual(plan.DenseCalendar.OutputPredicates, plan.DenseCalendar.ReadPredicates) {
		t.Fatalf("time-offset dependency read range was not widened: %#v", plan.DenseCalendar.ReadPredicates)
	}
}

func TestTimeOffsetDenseCalendarLowersAcrossTargets(t *testing.T) {
	for _, dialect := range []string{"DUCKDB", "DORIS", "CLICKHOUSE"} {
		dialect := dialect
		t.Run(dialect, func(t *testing.T) {
			grain := query.TimeGrainMonth
			q := query.SemanticQuery{
				Metrics:    []query.MetricRef{{Name: "previous_month_revenue"}},
				Dimensions: []query.DimensionRef{{Name: "order_date", Grain: &grain}},
				Filters: []query.Filter{
					{Field: "order_date", Operator: query.FilterGTE, Value: "2026-06-01"},
					{Field: "order_date", Operator: query.FilterLTE, Value: "2026-08-31"},
				},
			}
			_, sqlQuery := compileWithTimeSpine(t, q, dialect)
			for _, fragment := range []string{"__calendar", "COALESCE", "previous_month_revenue", "WHERE"} {
				if !strings.Contains(sqlQuery.SQL, fragment) {
					t.Fatalf("%s dense time-offset SQL missing %q:\n%s", dialect, fragment, sqlQuery.SQL)
				}
			}
			calendarPos := strings.Index(sqlQuery.SQL, "__calendar")
			offsetPos := strings.Index(sqlQuery.SQL, "previous_month_revenue")
			if calendarPos < 0 || offsetPos < 0 || calendarPos > offsetPos {
				t.Fatalf("%s calendar densification must precede time-offset evaluation:\n%s", dialect, sqlQuery.SQL)
			}
		})
	}
}
