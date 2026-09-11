package semanticplan

import (
	"reflect"
	"testing"

	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/query"
)

func TestInstallOwnedDAGOwnsNodesAndCalendarDomains(t *testing.T) {
	field := &ossie.Field{Name: "order_date"}
	plan := &SemanticPlan{DenseCalendar: &DenseCalendarPlan{
		Dataset:   DatasetRef{Name: "time_spine"},
		TimeField: field,
		Grain:     query.TimeGrainDay,
	}}
	node := SourceAggregateNode{Base: SemanticPlanNodeBase{
		ID:                        "revenue",
		Boundary:                  SemanticPlanNodeBoundarySourceAggregate,
		PredicateBoundaryEvidence: PredicateBoundaryEvidence(SemanticPlanNodeBoundarySourceAggregate),
	}, Source: SemanticSourceState{SourceRoots: []string{"orders"}}}
	if err := InstallOwnedDAG(plan, []string{"revenue"}, []SemanticPlanNode{node}, nil, nil, nil); err != nil {
		t.Fatalf("install owned DAG: %v", err)
	}
	if plan.DenseCalendar.TimeField == field {
		t.Fatal("calendar field aliases pre-install state")
	}
	node.Source.SourceRoots[0] = "changed"
	if got := plan.Nodes[0].(SourceAggregateNode).Source.SourceRoots[0]; got != "orders" {
		t.Fatalf("installed node aliases construction input: %q", got)
	}
}

func TestStructuralObservationReadsTypedPlanState(t *testing.T) {
	plan := &SemanticPlan{
		Joins: []Join{{FromDataset: "orders", ToDataset: "customers"}},
		Nodes: []SemanticPlanNode{
			SourceAggregateNode{
				Base:        SemanticPlanNodeBase{ID: "revenue"},
				Source:      SemanticSourceState{Joins: []Join{{FromDataset: "orders", ToDataset: "customers"}}},
				MetricState: SemanticMetricState{Metrics: []string{"revenue"}, ShareGroup: "source_001"},
			},
			TimeOffsetNode{Base: SemanticPlanNodeBase{ID: "prior"}},
		},
	}
	summary := SummarizeSemanticPlan(plan)
	if got, want := summary, (PlanShapeSummary{Joins: 1, EvaluationNodes: 2, SourceEvaluationNodes: 1, DerivedEvaluationNodes: 1, AdvancedEvaluationNodes: 1, SourceGroups: 1, SourceGroupMetrics: 1, EvaluationJoins: 1}); !reflect.DeepEqual(got, want) {
		t.Fatalf("summary = %#v, want %#v", got, want)
	}
	if err := ValidateStructuralNonRegression(plan, plan); err != nil {
		t.Fatalf("identical plan regressed structurally: %v", err)
	}
}
