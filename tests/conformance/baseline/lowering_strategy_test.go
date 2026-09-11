package baseline_test

import (
	"testing"

	"github.com/meaningforge/metis/planner/conversion"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/query"
)

func TestDerivedLoweringStrategyKeysOnNodeStructure(t *testing.T) {
	sourceSelection := semanticplan.SourceSelectionNode{
		Base: semanticplan.SemanticPlanNodeBase{
			ID:       "source_selection",
			Boundary: semanticplan.SemanticPlanNodeBoundarySourceSelection,
		},
		Source: semanticplan.SemanticSourceState{Root: semanticplan.DatasetRef{Name: "customers", Source: "analytics.customers"}},
		Mode:   semanticplan.SourceSelectionGroupedValues,
	}
	sourceAggregate := semanticplan.SourceAggregateNode{
		Base: semanticplan.SemanticPlanNodeBase{
			ID:       "revenue",
			Boundary: semanticplan.SemanticPlanNodeBoundarySourceAggregate,
		},
		Source: semanticplan.SemanticSourceState{Root: semanticplan.DatasetRef{Name: "orders", Source: "analytics.orders"}},
	}
	rootless := sourceAggregate
	rootless.Source.Root = semanticplan.DatasetRef{}
	secondMetric := sourceAggregate
	secondMetric.Base.ID = "cost"
	otherRoot := sourceAggregate
	otherRoot.Base.ID = "sessions"
	otherRoot.Source.Root = semanticplan.DatasetRef{Name: "sessions", Source: "analytics.sessions"}
	isolated := semanticplan.CumulativeWindowNode{
		Base: semanticplan.SemanticPlanNodeBase{
			ID:       "rolling_revenue",
			Boundary: semanticplan.SemanticPlanNodeBoundarySemanticIsolation,
		},
		Source: semanticplan.SemanticSourceState{Root: semanticplan.DatasetRef{Name: "orders", Source: "analytics.orders"}},
	}

	for _, tc := range []struct {
		name string
		plan *semanticplan.SemanticPlan
		want conversion.SemanticLoweringStrategy
	}{
		{"single source selection", &semanticplan.SemanticPlan{Nodes: []semanticplan.SemanticPlanNode{sourceSelection}}, conversion.SemanticLoweringCompact},
		{"single source aggregate", &semanticplan.SemanticPlan{Nodes: []semanticplan.SemanticPlanNode{sourceAggregate}}, conversion.SemanticLoweringCompact},
		{
			"two source nodes sharing a scan",
			&semanticplan.SemanticPlan{Nodes: []semanticplan.SemanticPlanNode{sourceAggregate, secondMetric}},
			conversion.SemanticLoweringCompact,
		},
		{
			"two source nodes on different roots",
			&semanticplan.SemanticPlan{Nodes: []semanticplan.SemanticPlanNode{sourceAggregate, otherRoot}},
			conversion.SemanticLoweringComposed,
		},
		{"isolating boundary", &semanticplan.SemanticPlan{Nodes: []semanticplan.SemanticPlanNode{isolated}}, conversion.SemanticLoweringComposed},
		{"no nodes", &semanticplan.SemanticPlan{}, conversion.SemanticLoweringComposed},
		{
			"node with no source",
			&semanticplan.SemanticPlan{Nodes: []semanticplan.SemanticPlanNode{rootless}},
			conversion.SemanticLoweringComposed,
		},
		{
			"dense calendar",
			&semanticplan.SemanticPlan{Nodes: []semanticplan.SemanticPlanNode{sourceAggregate}, DenseCalendar: &semanticplan.DenseCalendarPlan{}},
			conversion.SemanticLoweringComposed,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := conversion.LoweringStrategyForPlan(tc.plan); got != tc.want {
				t.Errorf("strategy = %q, want %q", got, tc.want)
			}
		})
	}

	withInput := sourceAggregate
	withInput.Base.Inputs = []semanticplan.SemanticPlanNodeInput{{NodeID: "other"}}
	if got := conversion.LoweringStrategyForPlan(&semanticplan.SemanticPlan{Nodes: []semanticplan.SemanticPlanNode{withInput}}); got != conversion.SemanticLoweringComposed {
		t.Errorf("a node with a dependency edge lowered %q", got)
	}

	filtered := sourceAggregate
	filtered.Base.Predicates = []semanticplan.SemanticPlanNodePredicate{{
		Scope:       semanticplan.SemanticPredicatePreAggregation,
		OwnerNodeID: filtered.Base.ID,
		Proof:       semanticplan.SemanticPredicateProofSourceOwnership,
		Predicate:   &semanticplan.Predicate{Filter: query.Filter{Field: "tier", Operator: query.FilterEQ, Value: "gold"}},
	}}
	if got := conversion.LoweringStrategyForPlan(&semanticplan.SemanticPlan{Nodes: []semanticplan.SemanticPlanNode{filtered}}); got != conversion.SemanticLoweringComposed {
		t.Errorf("a node carrying a metric-local predicate lowered %q", got)
	}

	if got := conversion.LoweringStrategyForPlan(&semanticplan.SemanticPlan{
		Nodes:  []semanticplan.SemanticPlanNode{sourceAggregate},
		Output: semanticplan.SemanticOutputContract{Predicates: []semanticplan.PostEvaluationPredicate{{Name: "revenue"}}},
	}); got != conversion.SemanticLoweringComposed {
		t.Errorf("a plan with a final-output predicate lowered %q", got)
	}
}
