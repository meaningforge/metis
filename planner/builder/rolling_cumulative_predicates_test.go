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
