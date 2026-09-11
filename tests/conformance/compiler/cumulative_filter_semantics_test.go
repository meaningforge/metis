package compiler_test

import (
	"strings"
	"testing"

	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/query"
)

func TestCumulativeFilterPlacement(t *testing.T) {
	grain := query.TimeGrainMonth
	q := query.SemanticQuery{
		Metrics: []query.MetricRef{{Name: "cumulative_revenue"}},
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
	if post.Name != "order_date" {
		t.Fatalf("post-evaluation predicate = %#v, want order_date", plan.Output.Predicates[0])
	}
	var base *semanticplan.SourceAggregateNode
	for _, node := range plan.Nodes {
		if node.NodeBase().ID != "revenue" {
			continue
		}
		source, ok := node.(semanticplan.SourceAggregateNode)
		if !ok {
			t.Fatalf("revenue node has type %T, want SourceAggregateNode", node)
		}
		base = &source
		break
	}
	if base == nil {
		t.Fatal("base revenue node not found")
	}
	var pre []semanticplan.SemanticPlanNodePredicate
	for _, predicate := range base.Base.Predicates {
		if predicate.Scope == semanticplan.SemanticPredicatePreAggregation && predicate.Predicate != nil {
			pre = append(pre, predicate)
		}
	}
	if got := len(pre); got != 1 {
		t.Fatalf("base pre-aggregation predicates = %d, want only region filter", got)
	}
	if got := pre[0].Predicate.Filter.Field; got != "region" {
		t.Fatalf("base predicate = %q, want region", got)
	}
	if !strings.Contains(sqlQuery.SQL, "ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW") {
		t.Fatalf("cumulative frame missing:\n%s", sqlQuery.SQL)
	}
	if strings.Count(sqlQuery.SQL, "WHERE") != 2 {
		// One WHERE belongs to the base aggregate (region), one belongs to the
		// final visible-range filter (order_date). The time predicate must not be
		// present in the base aggregate.
		t.Fatalf("WHERE count = %d, want 2:\n%s", strings.Count(sqlQuery.SQL, "WHERE"), sqlQuery.SQL)
	}
	if got := len(sqlQuery.Parameters); got != 3 {
		t.Fatalf("parameters = %d, want 3", got)
	}
}
