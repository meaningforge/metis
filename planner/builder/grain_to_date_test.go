package builder

import (
	"errors"
	"testing"

	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/serrors"
)

func TestGrainToDateRequiresFinerQueryGrain(t *testing.T) {
	month := query.TimeGrainMonth
	year := query.TimeGrainYear
	field := &ossie.Field{Name: "order_date"}
	nodes := []semanticplan.SemanticPlanNode{semanticplan.CumulativeWindowNode{Base: semanticplan.SemanticPlanNodeBase{ID: "ytd_revenue"}, Spec: ossie.CumulativeMetricSpec{
		TimeDimension: "order_date",
		Window:        ossie.CumulativeWindow{Type: "grain_to_date", Unit: "year"},
	},
	}}
	if err := ValidateGrainToDateQueryGrains(nodes, []semanticplan.GroupBy{{Name: "order_date", Field: field, Grain: &month}}); err != nil {
		t.Fatal(err)
	}
	err := ValidateGrainToDateQueryGrains(nodes, []semanticplan.GroupBy{{Name: "order_date", Field: field, Grain: &year}})
	var apiErr *serrors.Error
	if !errors.As(err, &apiErr) || apiErr.Code != serrors.ErrIncompatibleQueryGrain {
		t.Fatalf("error = %#v", err)
	}
}

func TestGrainToDateWidensReadToResetStart(t *testing.T) {
	month := query.TimeGrainMonth
	field := &ossie.Field{Name: "order_date"}
	predicate := semanticplan.Predicate{Dataset: "orders", Field: field, Filter: query.Filter{Field: "order_date", Operator: query.FilterGTE, Value: "2026-03-01"}}
	owned := predicate
	post := semanticplan.PostEvaluationPredicate{Name: "order_date", Filter: predicate.Filter}
	graph := semanticPlanForTest(semanticplan.SemanticPlan{
		Output: semanticplan.SemanticOutputContract{Predicates: []semanticplan.PostEvaluationPredicate{post}}}, []semanticNodeFixture{
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
			ID:     "ytd_revenue",
			Kind:   semanticplan.SemanticPlanNodeCumulativeWindow,
			Inputs: []semanticNodeInputFixture{{NodeID: "revenue"}},
			Node: semanticplan.CumulativeWindowNode{Base: semanticplan.SemanticPlanNodeBase{ID: "ytd_revenue"}, Spec: ossie.CumulativeMetricSpec{
				TimeDimension: "order_date",
				Window:        ossie.CumulativeWindow{Type: "grain_to_date", Unit: "year"},
			}},
		},
	})
	groups := []semanticplan.GroupBy{{Name: "order_date", Dataset: "orders", Field: field, Grain: &month}}
	if err := ApplyGrainToDateReadRanges(graph, groups, []semanticplan.Predicate{predicate}); err != nil {
		t.Fatal(err)
	}
	got := semanticPlanNodePredicates(graph.Nodes[0])
	if len(got) != 1 || got[0].Filter.Operator != query.FilterGTE || got[0].Filter.Value != "2026-01-01" {
		t.Fatalf("read predicates = %#v", got)
	}
	if graph.Output.Predicates[0].Filter.Value != "2026-03-01" {
		t.Fatalf("visible predicate changed: %#v", graph.Output.Predicates)
	}
}

func TestGrainToDateDoesNotNarrowExistingRollingRead(t *testing.T) {
	field := &ossie.Field{Name: "order_date"}
	existing := semanticplan.Predicate{Dataset: "orders", Field: field, Filter: query.Filter{Field: "order_date", Operator: query.FilterGTE, Value: "2025-12-01"}}
	plan := &semanticplan.SemanticPlan{Nodes: []semanticplan.SemanticPlanNode{semanticplan.SourceAggregateNode{Base: semanticplan.SemanticPlanNodeBase{
		ID: "revenue",
		Predicates: []semanticplan.SemanticPlanNodePredicate{{
			Scope:       semanticplan.SemanticPredicatePreAggregation,
			OwnerNodeID: "revenue",
			Proof:       semanticplan.SemanticPredicateProofSourceOwnership,
			Predicate:   &existing,
		}},
	}}}}
	MergeHistoricalReadPredicate(plan, "revenue", semanticplan.Predicate{Dataset: "orders", Field: field, Filter: query.Filter{Field: "order_date", Operator: query.FilterGTE, Value: "2026-01-01"}})
	got := semanticPlanNodePredicates(plan.Nodes[0])
	if len(got) != 1 || got[0].Filter.Value != "2025-12-01" {
		t.Fatalf("existing wider lower bound was narrowed to %#v", got)
	}
}
