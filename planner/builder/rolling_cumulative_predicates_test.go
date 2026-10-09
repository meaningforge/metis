package builder

import (
	"testing"

	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/query"
)

func TestRollingCumulativeWidensOnlyDependencyReadRange(t *testing.T) {
	grain := query.TimeGrainMonth
	field := &ossie.Field{Name: "order_date"}
	predicate := semanticplan.Predicate{Dataset: "orders", Field: field, Filter: query.Filter{Field: "order_date", Operator: query.FilterGTE, Value: "2026-03-01"}}
	post := semanticplan.PostEvaluationPredicate{Name: "order_date", Filter: predicate.Filter}
	graph := semanticPlanForTest(semanticplan.SemanticPlan{

		Output: semanticplan.SemanticOutputContract{Predicates: []semanticplan.PostEvaluationPredicate{post}}}, []semanticNodeFixture{
		{ID: "revenue", Kind: semanticplan.SemanticPlanNodeSourceAggregate, Node: semanticplan.SourceAggregateNode{}},
		{
			ID:     "rolling_revenue",
			Kind:   semanticplan.SemanticPlanNodeCumulativeWindow,
			Inputs: []semanticNodeInputFixture{{NodeID: "revenue"}},
			Node: semanticplan.CumulativeWindowNode{Spec: ossie.CumulativeMetricSpec{
				TimeDimension: "order_date",
				Window:        ossie.CumulativeWindow{Type: "rolling", Count: 3, Unit: "month"},
			}},
		},
	})
	groups := []semanticplan.GroupBy{{Name: "order_date", Dataset: "orders", Field: field, Grain: &grain}}
	if err := ApplyRollingCumulativeReadRanges(graph, groups, []semanticplan.Predicate{predicate}); err != nil {
		t.Fatal(err)
	}
	got := semanticPlanNodePredicates(graph.Nodes[0])
	if len(got) != 1 || got[0].Filter.Value != "2026-01-01" {
		t.Fatalf("read predicates = %#v, want lower bound widened by two months", got)
	}
	if graph.Output.Predicates[0].Filter.Value != "2026-03-01" {
		t.Fatalf("visible predicate changed: %#v", graph.Output.Predicates)
	}
}

func TestRollingCumulativeRangeSupportsHourTimestamp(t *testing.T) {
	grain := query.TimeGrainHour
	field := &ossie.Field{Name: "event_time"}
	predicate := semanticplan.Predicate{Dataset: "events", Field: field, Filter: query.Filter{Field: "event_time", Operator: query.FilterGTE, Value: "2026-03-01T10:00:00Z"}}
	graph := semanticPlanForTest(semanticplan.SemanticPlan{}, []semanticNodeFixture{
		{ID: "events_total", Kind: semanticplan.SemanticPlanNodeSourceAggregate, Node: semanticplan.SourceAggregateNode{}},
		{
			ID:     "rolling_events",
			Kind:   semanticplan.SemanticPlanNodeCumulativeWindow,
			Inputs: []semanticNodeInputFixture{{NodeID: "events_total"}},
			Node: semanticplan.CumulativeWindowNode{Spec: ossie.CumulativeMetricSpec{
				TimeDimension: "event_time",
				Window:        ossie.CumulativeWindow{Type: "rolling", Count: 3, Unit: "hour"},
			}},
		},
	})
	groups := []semanticplan.GroupBy{{Name: "event_time", Dataset: "events", Field: field, Grain: &grain}}
	if err := ApplyRollingCumulativeReadRanges(graph, groups, []semanticplan.Predicate{predicate}); err != nil {
		t.Fatal(err)
	}
	got := semanticPlanNodePredicates(graph.Nodes[0])
	if len(got) != 1 || got[0].Filter.Value != "2026-03-01T08:00:00Z" {
		t.Fatalf("hourly widened predicates = %#v", got)
	}
}

func TestCustomRollingCumulativeRangeKeepsOutputAndOnlyUpperBoundsSource(t *testing.T) {
	baseTime := &ossie.Field{Name: "calendar_day"}
	bucket := &ossie.Field{Name: "fiscal_week_start"}
	predicate := semanticplan.Predicate{
		Dataset: "calendar",
		Field:   baseTime,
		Filter:  query.Filter{Field: "calendar_day", Operator: query.FilterBetween, Value: []string{"2026-01-19", "2026-01-26"}},
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
			Node: semanticplan.SourceAggregateNode{},
		},
		{
			ID:     "rolling_revenue",
			Kind:   semanticplan.SemanticPlanNodeCumulativeWindow,
			Inputs: []semanticNodeInputFixture{{NodeID: "revenue"}},
			Node: semanticplan.CumulativeWindowNode{
				Spec: ossie.CumulativeMetricSpec{
					TimeDimension: "calendar_day",
					Window:        ossie.CumulativeWindow{Type: "rolling", Count: 3, Unit: "fiscal_week"},
				},
				CustomCalendarRolling: &semanticplan.CustomCalendarCumulativePlan{Grain: query.TimeGrain("fiscal_week")},
			},
		},
	})
	groups := []semanticplan.GroupBy{{
		Name:    "calendar_day",
		Dataset: "calendar",
		Field:   bucket,
		CustomCalendar: &semanticplan.CustomCalendarGrouping{
			Grain:         query.TimeGrain("fiscal_week"),
			Dataset:       "calendar",
			BaseTimeField: baseTime,
			BucketField:   bucket,
		},
	}}

	if err := ApplyRollingCumulativeReadRanges(graph, groups, []semanticplan.Predicate{predicate}); err != nil {
		t.Fatal(err)
	}
	read := semanticPlanNodePredicates(graph.Nodes[0])
	if len(read) != 1 || read[0].Filter.Operator != query.FilterLTE || read[0].Filter.Value != "2026-01-26" {
		t.Fatalf("source predicates = %#v, want only inclusive upper bound", read)
	}
	if len(graph.Output.Predicates) != 1 || graph.Output.Predicates[0].Name != "calendar_day" || graph.Output.Predicates[0].Filter.Operator != query.FilterBetween {
		t.Fatalf("output predicates = %#v, want original visible range", graph.Output.Predicates)
	}
}

func TestCustomRollingCumulativeLowerBoundReadsFullOrdinalHistory(t *testing.T) {
	baseTime := &ossie.Field{Name: "calendar_day"}
	bucket := &ossie.Field{Name: "fiscal_week_start"}
	predicate := semanticplan.Predicate{Dataset: "calendar", Field: baseTime, Filter: query.Filter{Field: "calendar_day", Operator: query.FilterGTE, Value: "2026-01-19"}}
	owned := predicate
	graph := semanticPlanForTest(semanticplan.SemanticPlan{}, []semanticNodeFixture{
		{ID: "revenue", Kind: semanticplan.SemanticPlanNodeSourceAggregate, Predicates: []semanticNodePredicateFixture{{Scope: semanticplan.SemanticPredicatePreAggregation, OwnerNodeID: "revenue", Proof: semanticplan.SemanticPredicateProofSourceOwnership, Predicate: &owned}}, Node: semanticplan.SourceAggregateNode{}},
		{ID: "rolling_revenue", Kind: semanticplan.SemanticPlanNodeCumulativeWindow, Inputs: []semanticNodeInputFixture{{NodeID: "revenue"}}, Node: semanticplan.CumulativeWindowNode{
			Spec:                  ossie.CumulativeMetricSpec{TimeDimension: "calendar_day", Window: ossie.CumulativeWindow{Type: "rolling", Count: 3, Unit: "fiscal_week"}},
			CustomCalendarRolling: &semanticplan.CustomCalendarCumulativePlan{Grain: query.TimeGrain("fiscal_week")},
		}},
	})
	groups := []semanticplan.GroupBy{{Name: "calendar_day", Dataset: "calendar", Field: bucket, CustomCalendar: &semanticplan.CustomCalendarGrouping{Grain: query.TimeGrain("fiscal_week"), Dataset: "calendar", BaseTimeField: baseTime, BucketField: bucket}}}

	if err := ApplyRollingCumulativeReadRanges(graph, groups, []semanticplan.Predicate{predicate}); err != nil {
		t.Fatal(err)
	}
	if got := semanticPlanNodePredicates(graph.Nodes[0]); len(got) != 0 {
		t.Fatalf("source predicates = %#v, want full ordinal history", got)
	}
	if len(graph.Output.Predicates) != 1 || graph.Output.Predicates[0].Filter.Value != "2026-01-19" {
		t.Fatalf("output predicates = %#v", graph.Output.Predicates)
	}
}

func TestCustomRollingCumulativeRejectsNonRangeTimeFilter(t *testing.T) {
	predicate := semanticplan.Predicate{Field: &ossie.Field{Name: "calendar_day"}, Filter: query.Filter{Field: "calendar_day", Operator: query.FilterEQ, Value: "2026-01-19"}}
	if _, err := customRollingReadPredicate(predicate); err == nil {
		t.Fatal("expected non-range custom rolling filter rejection")
	}
}
