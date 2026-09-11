package compiler_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/serrors"
)

func TestTimeOffsetFilterExpandsSourceRangeButPreservesVisibleRange(t *testing.T) {
	grain := query.TimeGrainMonth
	q := query.SemanticQuery{
		Metrics: []query.MetricRef{{Name: "previous_month_revenue"}},
		Dimensions: []query.DimensionRef{
			{Name: "order_date", Grain: &grain},
			{Name: "region"},
		},
		Filters: []query.Filter{
			{Field: "order_date", Operator: query.FilterBetween, Value: []string{"2026-03-01", "2026-06-30"}},
			{Field: "region", Operator: query.FilterEQ, Value: "APAC"},
		},
	}

	plan, sqlQuery, err := compile(t, q, "DORIS")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Nodes) == 0 {
		t.Fatal("plan owns no nodes")
	}
	if got := len(plan.Output.Predicates); got != 1 {
		t.Fatalf("post-evaluation predicates = %d, want 1", got)
	}
	post := plan.Output.Predicates[0]
	if post.Name != "order_date" || !reflect.DeepEqual(post.Filter.Value, []string{"2026-03-01", "2026-06-30"}) {
		t.Fatalf("visible-range predicate = %#v", post)
	}

	base := semanticGraphNodeByID(t, plan.Nodes, "revenue")
	predicates := semanticGraphNodePredicates(base)
	if got := len(predicates); got != 2 {
		t.Fatalf("base pre-aggregation predicates = %d, want expanded time + region", got)
	}
	var timeFilter, regionFilter *semanticplan.Predicate
	for i := range predicates {
		predicate := &predicates[i]
		switch predicate.Filter.Field {
		case "order_date":
			timeFilter = predicate
		case "region":
			regionFilter = predicate
		}
	}
	if timeFilter == nil || !reflect.DeepEqual(timeFilter.Filter.Value, []string{"2026-02-01", "2026-06-30"}) {
		t.Fatalf("expanded source time predicate = %#v, want 2026-02-01..2026-06-30", timeFilter)
	}
	if regionFilter == nil || regionFilter.Filter.Value != "APAC" {
		t.Fatalf("region predicate = %#v", regionFilter)
	}
	if got := len(plan.Predicates); got != 0 {
		t.Fatalf("top-level predicates = %#v, want none after optimizer pushdown", plan.Predicates)
	}
	if strings.Count(sqlQuery.SQL, "WHERE") < 2 {
		t.Fatalf("expected source and final visible-range WHERE clauses:\n%s", sqlQuery.SQL)
	}
	if got := len(sqlQuery.Parameters); got != 5 {
		t.Fatalf("parameters = %d, want 5 (expanded range + region + visible range)", got)
	}
}

func TestTimeOffsetRejectsEqualityTimeFilterInV1(t *testing.T) {
	grain := query.TimeGrainMonth
	q := query.SemanticQuery{
		Metrics:    []query.MetricRef{{Name: "previous_month_revenue"}},
		Dimensions: []query.DimensionRef{{Name: "order_date", Grain: &grain}},
		Filters:    []query.Filter{{Field: "order_date", Operator: query.FilterEQ, Value: "2026-03-01"}},
	}
	_, _, err := compile(t, q, "DORIS")
	if err == nil {
		t.Fatal("expected typed planning error for equality time filter")
	}
	apiErr, ok := err.(*serrors.Error)
	if !ok || apiErr.Code != serrors.ErrUnsupportedTimeFilter {
		t.Fatalf("error = %#v, want %s", err, serrors.ErrUnsupportedTimeFilter)
	}
}
