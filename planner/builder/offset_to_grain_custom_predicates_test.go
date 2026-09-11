package builder

import (
	"testing"

	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/query"
)

func TestCustomOffsetToGrainFilteredReadUsesScopedFullHistory(t *testing.T) {
	day := &ossie.Field{Name: "day"}
	predicate := semanticplan.Predicate{Dataset: "calendar", Field: day, Filter: query.Filter{Field: "day", Operator: query.FilterGTE, Value: "2026-03-01"}}
	revenuePredicate := predicate
	costPredicate := predicate
	graph := semanticPlanForTest(semanticplan.SemanticPlan{}, []semanticNodeFixture{
		{
			ID:   "revenue",
			Kind: semanticplan.SemanticPlanNodeSourceAggregate,
			Predicates: []semanticNodePredicateFixture{{
				Scope: semanticplan.SemanticPredicatePreAggregation, OwnerNodeID: "revenue", Proof: semanticplan.SemanticPredicateProofSourceOwnership, Predicate: &revenuePredicate,
			}},
			Node: semanticplan.SourceAggregateNode{Base: semanticplan.SemanticPlanNodeBase{ID: "revenue"}},
		},
		{
			ID:   "cost",
			Kind: semanticplan.SemanticPlanNodeSourceAggregate,
			Predicates: []semanticNodePredicateFixture{{
				Scope: semanticplan.SemanticPredicatePreAggregation, OwnerNodeID: "cost", Proof: semanticplan.SemanticPredicateProofSourceOwnership, Predicate: &costPredicate,
			}},
			Node: semanticplan.SourceAggregateNode{Base: semanticplan.SemanticPlanNodeBase{ID: "cost"}},
		},
		{
			ID:     "revenue_at_fiscal_year_start",
			Kind:   semanticplan.SemanticPlanNodeOffsetToGrain,
			Inputs: []semanticNodeInputFixture{{NodeID: "revenue"}},
			Node: semanticplan.OffsetToGrainNode{
				Base:       semanticplan.SemanticPlanNodeBase{ID: "revenue_at_fiscal_year_start"},
				Spec:       ossie.OffsetToGrainMetricSpec{BaseMetric: "revenue", TimeDimension: "day", Grain: "fiscal_year"},
				OffsetPlan: &semanticplan.OffsetToGrainPlan{CustomCalendar: true},
			},
		},
	})
	groups := []semanticplan.GroupBy{{Name: "day@fiscal_week", Dataset: "calendar", Field: day, CustomCalendar: &semanticplan.CustomCalendarGrouping{Dataset: "calendar", BaseTimeField: day, Grain: query.TimeGrain("fiscal_week")}}}
	if err := ApplyOffsetToGrainReadRanges(graph, groups, []semanticplan.Predicate{predicate}); err != nil {
		t.Fatal(err)
	}
	if got := semanticPlanNodePredicates(graph.Nodes[0]); len(got) != 0 {
		t.Fatalf("revenue source predicates = %#v, want full-history read", got)
	}
	if got := semanticPlanNodePredicates(graph.Nodes[1]); len(got) != 1 {
		t.Fatalf("unrelated cost predicates = %#v, want original filter", got)
	}
	if len(graph.Output.Predicates) != 1 || graph.Output.Predicates[0].Filter.Value != "2026-03-01" {
		t.Fatalf("post predicates = %#v", graph.Output.Predicates)
	}
}

func TestCustomOffsetToGrainFilteredReadRejectsNonRangeOperator(t *testing.T) {
	day := &ossie.Field{Name: "day"}
	predicate := semanticplan.Predicate{Dataset: "calendar", Field: day, Filter: query.Filter{Field: "day", Operator: query.FilterEQ, Value: "2026-03-01"}}
	owned := predicate
	graph := semanticPlanForTest(semanticplan.SemanticPlan{}, []semanticNodeFixture{
		{
			ID:   "revenue",
			Kind: semanticplan.SemanticPlanNodeSourceAggregate,
			Predicates: []semanticNodePredicateFixture{{
				Scope: semanticplan.SemanticPredicatePreAggregation, OwnerNodeID: "revenue", Proof: semanticplan.SemanticPredicateProofSourceOwnership, Predicate: &owned,
			}},
			Node: semanticplan.SourceAggregateNode{Base: semanticplan.SemanticPlanNodeBase{ID: "revenue"}},
		},
		{
			ID:     "boundary",
			Kind:   semanticplan.SemanticPlanNodeOffsetToGrain,
			Inputs: []semanticNodeInputFixture{{NodeID: "revenue"}},
			Node: semanticplan.OffsetToGrainNode{
				Base:       semanticplan.SemanticPlanNodeBase{ID: "boundary"},
				Spec:       ossie.OffsetToGrainMetricSpec{BaseMetric: "revenue", TimeDimension: "day", Grain: "fiscal_year"},
				OffsetPlan: &semanticplan.OffsetToGrainPlan{CustomCalendar: true},
			},
		},
	})
	groups := []semanticplan.GroupBy{{Name: "day@fiscal_week", Dataset: "calendar", Field: day, CustomCalendar: &semanticplan.CustomCalendarGrouping{Dataset: "calendar", BaseTimeField: day, Grain: query.TimeGrain("fiscal_week")}}}
	if err := ApplyOffsetToGrainReadRanges(graph, groups, []semanticplan.Predicate{predicate}); err == nil {
		t.Fatal("expected non-range custom offset-to-grain filter rejection")
	}
}
