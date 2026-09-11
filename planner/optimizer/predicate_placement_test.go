package optimizer_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/meaningforge/metis/expression"
	"github.com/meaningforge/metis/planner/optimizer"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/query"
)

func TestPredicatePushdownConsumesAdvancedPredicateOnlyAfterPlannerPlacementProof(t *testing.T) {
	predicate := semanticplan.Predicate{
		Dataset:    "orders",
		Expression: expression.NewResolvedExpression("ANSI_SQL", "status"),
		Filter:     query.Filter{Field: "orders.status", Operator: query.FilterEQ, Value: "paid"},
	}
	input := &semanticplan.SemanticPlan{
		Predicates: []semanticplan.Predicate{predicate},
		Nodes: []semanticplan.SemanticPlanNode{
			semanticplan.SourceAggregateNode{Base: semanticplan.SemanticPlanNodeBase{
				ID:       "revenue",
				Boundary: semanticplan.SemanticPlanNodeBoundarySourceAggregate,
				Predicates: []semanticplan.SemanticPlanNodePredicate{{
					Scope:       semanticplan.SemanticPredicatePreAggregation,
					OwnerNodeID: "revenue",
					Proof:       semanticplan.SemanticPredicateProofSourceOwnership,
					Predicate:   &predicate,
				}},
			}},
			semanticplan.CumulativeWindowNode{Base: semanticplan.SemanticPlanNodeBase{
				ID:       "rolling_revenue",
				Boundary: semanticplan.SemanticPlanNodeBoundarySemanticIsolation,
				PredicateBoundaryEvidence: semanticplan.SemanticPredicateBoundaryEvidence{
					Movement: semanticplan.SemanticPredicateBoundaryBlocked,
					Proof:    semanticplan.SemanticPredicateProofNodeSemantics,
				},
				Inputs: []semanticplan.SemanticPlanNodeInput{{NodeID: "revenue"}},
			}},
		},
	}

	optimized, err := optimizer.NewCanonical(optimizer.PredicatePushdownRule{}).Optimize(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if len(optimized.Predicates) != 0 {
		t.Fatalf("top-level predicate was retained after placement proof: %#v", optimized.Predicates)
	}
	if got := optimized.Nodes[0].NodeBase().Predicates; len(got) != 1 || got[0].Predicate == nil || !reflect.DeepEqual(*got[0].Predicate, predicate) {
		t.Fatalf("source predicate placement changed: %#v", got)
	}
	if len(input.Predicates) != 1 || len(input.Nodes[0].NodeBase().Predicates) != 1 {
		t.Fatal("optimizer mutated predicate-placement input")
	}
}

func TestPredicatePushdownLeavesAdvancedPredicateInPlaceWhenPlacementProofIsMissing(t *testing.T) {
	predicate := semanticplan.Predicate{
		Dataset:    "orders",
		Expression: expression.NewResolvedExpression("ANSI_SQL", "status"),
		Filter:     query.Filter{Field: "orders.status", Operator: query.FilterEQ, Value: "paid"},
	}
	input := &semanticplan.SemanticPlan{
		Predicates: []semanticplan.Predicate{predicate},
		Nodes: []semanticplan.SemanticPlanNode{
			semanticplan.SourceAggregateNode{Base: semanticplan.SemanticPlanNodeBase{ID: "revenue", Boundary: semanticplan.SemanticPlanNodeBoundarySourceAggregate}},
			semanticplan.CumulativeWindowNode{Base: semanticplan.SemanticPlanNodeBase{
				ID:       "rolling_revenue",
				Boundary: semanticplan.SemanticPlanNodeBoundarySemanticIsolation,
				PredicateBoundaryEvidence: semanticplan.SemanticPredicateBoundaryEvidence{
					Movement: semanticplan.SemanticPredicateBoundaryBlocked,
					Proof:    semanticplan.SemanticPredicateProofNodeSemantics,
				},
				Inputs: []semanticplan.SemanticPlanNodeInput{{NodeID: "revenue"}},
			}},
		},
	}

	optimized, err := optimizer.NewCanonical(optimizer.PredicatePushdownRule{}).Optimize(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(optimized.Predicates, []semanticplan.Predicate{predicate}) {
		t.Fatalf("top-level predicate = %#v, want unchanged", optimized.Predicates)
	}
	if len(optimized.Nodes[0].NodeBase().Predicates) != 0 {
		t.Fatalf("optimizer invented advanced predicate placement: %#v", optimized.Nodes[0].NodeBase().Predicates)
	}
	if got := optimized.OptimizationTrace; len(got) != 1 || got[0].Changed {
		t.Fatalf("optimization trace = %#v, want unchanged predicate_pushdown", got)
	}
}
