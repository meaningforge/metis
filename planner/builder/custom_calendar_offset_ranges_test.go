package builder

import (
	"testing"

	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/query"
)

func TestCustomCalendarOffsetRangeKeepsVisibleFilterAndReadsHistoricalPeriods(t *testing.T) {
	baseTime := &ossie.Field{Name: "day"}
	bucket := &ossie.Field{Name: "fiscal_week_start"}
	predicate := semanticplan.Predicate{
		Filter:  query.Filter{Field: "day", Operator: query.FilterBetween, Value: []string{"2026-02-02", "2026-02-15"}},
		Dataset: "calendar",
		Field:   baseTime,
	}
	owned := predicate
	graph := semanticPlanForTest(semanticplan.SemanticPlan{}, []semanticNodeFixture{
		{
			ID:   "revenue",
			Kind: semanticplan.SemanticPlanNodeSourceAggregate,
			Predicates: []semanticNodePredicateFixture{{
				Scope:       semanticplan.SemanticPredicatePreAggregation,
				OwnerNodeID: "revenue",
				Proof:       semanticplan.SemanticPredicateProofSourceOwnership,
				Predicate:   &owned,
			}},
			Node: semanticplan.SourceAggregateNode{Base: semanticplan.SemanticPlanNodeBase{ID: "revenue"}},
		},
		{
			ID:     "previous_fiscal_week_revenue",
			Kind:   semanticplan.SemanticPlanNodeTimeOffset,
			Inputs: []semanticNodeInputFixture{{NodeID: "revenue"}},
			Node: semanticplan.TimeOffsetNode{
				Base: semanticplan.SemanticPlanNodeBase{ID: "previous_fiscal_week_revenue"},
				Spec: ossie.TimeOffsetMetricSpec{
					TimeDimension: "day",
					Offset:        ossie.TimeOffset{Count: -1, Unit: "fiscal_week"},
				},
			},
		},
	})
	groups := []semanticplan.GroupBy{{
		Name:    "day",
		Dataset: "calendar",
		Field:   bucket,
		CustomCalendar: &semanticplan.CustomCalendarGrouping{
			Grain:         query.TimeGrain("fiscal_week"),
			Dataset:       "calendar",
			BaseTimeField: baseTime,
			BucketField:   bucket,
		},
	}}

	if err := ApplyTimeOffsetReadRanges(graph, groups, []semanticplan.Predicate{predicate}); err != nil {
		t.Fatal(err)
	}
	if got := len(semanticPlanNodeFixturesForTest(graph)[0].Predicates); got != 0 {
		t.Fatalf("source predicates = %d, want 0 so custom offset can read historical periods", got)
	}
	if got := len(graph.Output.Predicates); got != 1 {
		t.Fatalf("post-evaluation predicates = %d, want 1", got)
	}
	post := graph.Output.Predicates[0]
	if post.Name != "day" {
		t.Fatalf("post-evaluation predicate = %#v, want day", post)
	}
	if post.Filter.Operator != query.FilterBetween {
		t.Fatalf("post-evaluation operator = %q, want between", post.Filter.Operator)
	}
}

func TestCustomCalendarOffsetRangeRequiresResolvedCustomGroup(t *testing.T) {
	baseTime := &ossie.Field{Name: "day"}
	predicate := semanticplan.Predicate{
		Filter:  query.Filter{Field: "day", Operator: query.FilterGTE, Value: "2026-02-02"},
		Dataset: "calendar",
		Field:   baseTime,
	}
	graph := semanticPlanForTest(semanticplan.SemanticPlan{}, []semanticNodeFixture{{
		ID:   "previous_fiscal_week_revenue",
		Kind: semanticplan.SemanticPlanNodeTimeOffset,
		Node: semanticplan.TimeOffsetNode{
			Base: semanticplan.SemanticPlanNodeBase{ID: "previous_fiscal_week_revenue"},
			Spec: ossie.TimeOffsetMetricSpec{
				TimeDimension: "day",
				Offset:        ossie.TimeOffset{Count: -1, Unit: "fiscal_week"},
			},
		},
	}})

	if err := ApplyTimeOffsetReadRanges(graph, nil, []semanticplan.Predicate{predicate}); err == nil {
		t.Fatal("expected missing custom query grain to fail")
	}
}

func TestCustomCalendarOffsetRangeRejectsNonRangeFilter(t *testing.T) {
	baseTime := &ossie.Field{Name: "day"}
	bucket := &ossie.Field{Name: "fiscal_week_start"}
	predicate := semanticplan.Predicate{
		Filter:  query.Filter{Field: "day", Operator: query.FilterEQ, Value: "2026-02-02"},
		Dataset: "calendar",
		Field:   baseTime,
	}
	graph := semanticPlanForTest(semanticplan.SemanticPlan{}, []semanticNodeFixture{{
		ID:   "previous_fiscal_week_revenue",
		Kind: semanticplan.SemanticPlanNodeTimeOffset,
		Node: semanticplan.TimeOffsetNode{
			Base: semanticplan.SemanticPlanNodeBase{ID: "previous_fiscal_week_revenue"},
			Spec: ossie.TimeOffsetMetricSpec{
				TimeDimension: "day",
				Offset:        ossie.TimeOffset{Count: -1, Unit: "fiscal_week"},
			},
		},
	}})
	groups := []semanticplan.GroupBy{{
		Name:    "day",
		Dataset: "calendar",
		Field:   bucket,
		CustomCalendar: &semanticplan.CustomCalendarGrouping{
			Grain:         query.TimeGrain("fiscal_week"),
			Dataset:       "calendar",
			BaseTimeField: baseTime,
			BucketField:   bucket,
		},
	}}

	if err := ApplyTimeOffsetReadRanges(graph, groups, []semanticplan.Predicate{predicate}); err == nil {
		t.Fatal("expected non-range custom time-relative filter to fail")
	}
}
