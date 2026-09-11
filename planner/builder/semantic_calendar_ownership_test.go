package builder

import (
	"testing"

	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/query"
)

func TestInstalledPlanDAGOwnsCalendarDomains(t *testing.T) {
	field := &ossie.Field{Name: "order_date"}
	bucket := &ossie.Field{Name: "fiscal_week_start"}
	ordinal := &ossie.Field{Name: "fiscal_week_ordinal"}
	plan := &semanticplan.SemanticPlan{
		DenseCalendar: &semanticplan.DenseCalendarPlan{
			Dataset:            semanticplan.DatasetRef{Name: "time_spine", Source: "analytics.time_spine"},
			TimeField:          field,
			QueryTimeDimension: "order_date",
			Grain:              query.TimeGrainDay,
		},
		CustomDenseCalendar: &semanticplan.CustomDenseCalendarPlan{
			Dataset:            semanticplan.DatasetRef{Name: "calendar", Source: "analytics.calendar"},
			QueryTimeDimension: "order_date",
			Grain:              query.TimeGrain("fiscal_week"),
			BucketField:        bucket,
			BucketExpression:   "calendar.fiscal_week_start",
			OrdinalField:       ordinal,
			OrdinalExpression:  "calendar.fiscal_week_ordinal",
		},
	}

	input := ConstructionInput{
		Nodes: []semanticplan.SemanticPlanNode{semanticplan.SourceAggregateNode{Base: semanticplan.SemanticPlanNodeBase{
			ID:                        "revenue",
			Boundary:                  semanticplan.SemanticPlanNodeBoundarySourceAggregate,
			PredicateBoundaryEvidence: semanticplan.PredicateBoundaryEvidence(semanticplan.SemanticPlanNodeBoundarySourceAggregate),
		}}},
	}
	if err := semanticplan.InstallOwnedDAG(plan, input.Requested, input.Nodes, input.OutputGrain, input.PostEvaluationPredicates, nil); err != nil {
		t.Fatal(err)
	}

	if len(plan.Nodes) == 0 || plan.DenseCalendar == nil || plan.CustomDenseCalendar == nil {
		t.Fatalf("plan calendar ownership missing: %#v", plan)
	}
	if plan.DenseCalendar.TimeField == field {
		t.Fatal("dense calendar graph state aliases construction input field")
	}
	if plan.CustomDenseCalendar.BucketField == bucket || plan.CustomDenseCalendar.OrdinalField == ordinal {
		t.Fatal("custom dense calendar graph state aliases construction input fields")
	}
}

func TestSemanticPlanNodePlannerHandsOutOwnedStages(t *testing.T) {
	plan := semanticPlanForTest(semanticplan.SemanticPlan{}, canonicalNodeFixtures([]semanticNodeFixture{semanticTestNodeFixture(semanticNodeFixture{
		ID:               "revenue",
		Kind:             semanticplan.SemanticPlanNodeSourceAggregate,
		Boundary:         semanticplan.SemanticPlanNodeBoundarySourceAggregate,
		Metrics:          []string{"revenue"},
		RequiredDatasets: []string{"orders"},
	})}))

	if err := semanticplan.ValidateSemanticPlanDAG(plan); err != nil {
		t.Fatal(err)
	}
	stages, err := semanticNodeFixturesFromPlanNodes(plan.Nodes)
	if err != nil {
		t.Fatal(err)
	}
	stages[0].ID = "mutated"
	stages[0].Metrics[0] = "mutated"
	owned := semanticPlanNodeFixturesForTest(plan)
	if owned[0].ID != "revenue" || owned[0].Metrics[0] != "revenue" {
		t.Fatalf("the planner handed out the plan's own stage storage: %#v", owned[0])
	}
}
