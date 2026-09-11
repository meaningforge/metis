package builder

import (
	"reflect"
	"testing"

	"github.com/meaningforge/metis/planner/semanticplan"
)

func TestSummarizeSemanticPlanIsDeterministicAndNonMutating(t *testing.T) {
	post := semanticplan.PostEvaluationPredicate{Name: "margin"}
	plan := semanticPlanForTest(semanticplan.SemanticPlan{
		Joins:       []semanticplan.Join{{FromDataset: "orders", ToDataset: "customers"}},
		Predicates:  []semanticplan.Predicate{{Dataset: "orders"}},
		Projections: []semanticplan.Projection{{Name: "revenue"}},
		Groups:      []semanticplan.GroupBy{{Name: "region"}},
		Sorts:       []semanticplan.Sort{{Name: "revenue"}},

		Output: semanticplan.SemanticOutputContract{Predicates: []semanticplan.PostEvaluationPredicate{post}},
	}, []semanticNodeFixture{
		{
			ID:         "revenue",
			Kind:       semanticplan.SemanticPlanNodeSourceAggregate,
			ShareGroup: "source_001",
			Metrics:    []string{"revenue", "cost"},
			Joins:      []semanticplan.Join{{FromDataset: "orders", ToDataset: "customers"}},
			Predicates: []semanticNodePredicateFixture{{Scope: semanticplan.SemanticPredicatePreAggregation, Predicate: &semanticplan.Predicate{Dataset: "orders"}}},
		},
		{ID: "rolling_revenue", Kind: semanticplan.SemanticPlanNodeCumulativeWindow, Inputs: []semanticNodeInputFixture{{NodeID: "revenue"}}, Node: semanticplan.CumulativeWindowNode{CustomCalendarRolling: &semanticplan.CustomCalendarCumulativePlan{}}},
		{ID: "previous_period_revenue", Kind: semanticplan.SemanticPlanNodeTimeOffset, Inputs: []semanticNodeInputFixture{{NodeID: "revenue"}}, Node: semanticplan.TimeOffsetNode{CustomCalendar: &semanticplan.CustomCalendarOffsetPlan{}}},
	})
	before := semanticplan.ClonePlan(plan)

	first := semanticplan.SummarizeSemanticPlan(plan)
	second := semanticplan.SummarizeSemanticPlan(plan)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("summary changed across identical observations: first=%#v second=%#v", first, second)
	}
	if !reflect.DeepEqual(plan, before) {
		t.Fatalf("summary mutated plan:\nbefore=%#v\nafter=%#v", before, plan)
	}

	want := semanticplan.PlanShapeSummary{
		Joins:                    1,
		Predicates:               1,
		Projections:              1,
		Groups:                   1,
		Sorts:                    1,
		EvaluationNodes:          3,
		SourceEvaluationNodes:    1,
		DerivedEvaluationNodes:   2,
		AdvancedEvaluationNodes:  2,
		SourceGroups:             1,
		SourceGroupMetrics:       2,
		EvaluationJoins:          1,
		PreAggregationPredicates: 1,
		PostEvaluationPredicates: 1,
		CustomCalendarDomains:    2,
	}
	if !reflect.DeepEqual(first, want) {
		t.Fatalf("summary = %#v, want %#v", first, want)
	}
}

func TestSummarizeSemanticPlanMakesStructuralReductionComparable(t *testing.T) {
	unoptimized := semanticPlanForTest(semanticplan.SemanticPlan{
		Joins: []semanticplan.Join{
			{FromDataset: "orders", ToDataset: "customers"},
			{FromDataset: "orders", ToDataset: "customers"},
		},
	}, []semanticNodeFixture{
		{ID: "revenue", Kind: semanticplan.SemanticPlanNodeSourceAggregate, Metrics: []string{"revenue"}},
		{ID: "cost", Kind: semanticplan.SemanticPlanNodeSourceAggregate, Metrics: []string{"cost"}},
	})

	optimized := semanticplan.ClonePlan(unoptimized)
	optimized.Joins = optimized.Joins[:1]
	mutateSemanticPlanNodeFixturesForTest(optimized, func(stages []semanticNodeFixture) {
		stages[0].ShareGroup = "source_001"
		stages[0].Metrics = []string{"revenue", "cost"}
		stages[1].ShareGroup = "source_001"
		stages[1].Metrics = []string{"cost"}
	})

	before := semanticplan.SummarizeSemanticPlan(unoptimized)
	after := semanticplan.SummarizeSemanticPlan(optimized)
	if before.Joins != 2 || after.Joins != 1 {
		t.Fatalf("join reduction not observable: before=%#v after=%#v", before, after)
	}
	if after.SourceGroups != 1 || after.SourceGroupMetrics != 2 {
		t.Fatalf("source-stage structure not observable: after=%#v", after)
	}
}
