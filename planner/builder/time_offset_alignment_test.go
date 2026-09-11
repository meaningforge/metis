package builder

import (
	"reflect"
	"testing"

	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/query"
)

func TestTimeOffsetReadRangeCoversAllRequestedUnits(t *testing.T) {
	timeField := &ossie.Field{Name: "order_date"}
	predicate := semanticplan.Predicate{
		Filter:  query.Filter{Field: "order_date", Operator: query.FilterBetween, Value: []string{"2026-03-01", "2026-06-30"}},
		Dataset: "orders",
		Field:   timeField,
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
			ID:     "previous_two_months_revenue",
			Kind:   semanticplan.SemanticPlanNodeTimeOffset,
			Inputs: []semanticNodeInputFixture{{NodeID: "revenue"}},
			Node: semanticplan.TimeOffsetNode{Spec: ossie.TimeOffsetMetricSpec{
				Kind:          ossie.MetricExtensionTimeOffset,
				BaseMetric:    "revenue",
				TimeDimension: "order_date",
				Offset:        ossie.TimeOffset{Count: -2, Unit: "month"},
			}},
		},
		{
			ID:     "previous_year_revenue",
			Kind:   semanticplan.SemanticPlanNodeTimeOffset,
			Inputs: []semanticNodeInputFixture{{NodeID: "revenue"}},
			Node: semanticplan.TimeOffsetNode{Spec: ossie.TimeOffsetMetricSpec{
				Kind:          ossie.MetricExtensionTimeOffset,
				BaseMetric:    "revenue",
				TimeDimension: "order_date",
				Offset:        ossie.TimeOffset{Count: -1, Unit: "year"},
			}},
		},
	})
	groups := []semanticplan.GroupBy{{Name: "order_date", Dataset: "orders", Field: timeField}}

	if err := ApplyTimeOffsetReadRanges(graph, groups, []semanticplan.Predicate{predicate}); err != nil {
		t.Fatal(err)
	}

	got := semanticPlanNodePredicates(graph.Nodes[0])[0].Filter.Value
	want := []string{"2025-03-01", "2026-06-30"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("source read range = %#v, want %#v", got, want)
	}
	if len(graph.Output.Predicates) != 1 || !reflect.DeepEqual(graph.Output.Predicates[0].Filter.Value, predicate.Filter.Value) {
		t.Fatalf("visible output range = %#v, want original query range %#v", graph.Output.Predicates, predicate.Filter.Value)
	}
}
