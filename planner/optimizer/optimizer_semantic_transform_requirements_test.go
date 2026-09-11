package optimizer_test

import (
	"context"
	"testing"

	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner/optimizer"
	"github.com/meaningforge/metis/planner/semanticplan"
)

func TestUnusedJoinEliminationRetainsSemanticTransformDatasets(t *testing.T) {
	tests := []struct {
		name  string
		apply func(*semanticplan.SemanticPlan)
	}{
		{
			name: "dense calendar",
			apply: func(plan *semanticplan.SemanticPlan) {
				plan.DenseCalendar = &semanticplan.DenseCalendarPlan{Dataset: semanticplan.DatasetRef{Name: "calendar", Source: "calendar"}}
			},
		},
		{
			name: "custom dense calendar",
			apply: func(plan *semanticplan.SemanticPlan) {
				plan.CustomDenseCalendar = &semanticplan.CustomDenseCalendarPlan{Dataset: semanticplan.DatasetRef{Name: "calendar", Source: "calendar"}}
			},
		},
		{
			name: "custom calendar offset",
			apply: func(plan *semanticplan.SemanticPlan) {
				plan.Nodes = []semanticplan.SemanticPlanNode{semanticplan.TimeOffsetNode{
					Base: semanticplan.SemanticPlanNodeBase{ID: "revenue"},
					CustomCalendar: &semanticplan.CustomCalendarOffsetPlan{
						Dataset: semanticplan.DatasetRef{Name: "calendar", Source: "calendar"},
					},
				}}
			},
		},
		{
			name: "custom calendar cumulative",
			apply: func(plan *semanticplan.SemanticPlan) {
				plan.Nodes = []semanticplan.SemanticPlanNode{semanticplan.CumulativeWindowNode{
					Base: semanticplan.SemanticPlanNodeBase{ID: "revenue"},
					CustomCalendarRolling: &semanticplan.CustomCalendarCumulativePlan{
						Dataset: semanticplan.DatasetRef{Name: "calendar", Source: "calendar"},
					},
				}}
			},
		},
		{
			name: "custom calendar grain to date",
			apply: func(plan *semanticplan.SemanticPlan) {
				plan.Nodes = []semanticplan.SemanticPlanNode{semanticplan.CumulativeWindowNode{
					Base: semanticplan.SemanticPlanNodeBase{ID: "revenue"},
					CustomCalendarGrainToDate: &semanticplan.CustomCalendarGrainToDatePlan{
						Dataset: semanticplan.DatasetRef{Name: "calendar", Source: "calendar"},
					},
				}}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ordersToCalendar := &ossie.Relationship{Name: "orders_to_calendar", From: "orders", To: "calendar"}
			calendarToUnused := &ossie.Relationship{Name: "calendar_to_unused", From: "calendar", To: "unused"}
			input := &semanticplan.SemanticPlan{
				Root: semanticplan.DatasetRef{Name: "orders", Source: "orders"},
				Joins: []semanticplan.Join{
					{Relationship: ordersToCalendar, FromDataset: "orders", ToDataset: "calendar"},
					{Relationship: calendarToUnused, FromDataset: "calendar", ToDataset: "unused"},
				},
				Projections: []semanticplan.Projection{{Name: "revenue", Kind: semanticplan.ProjectionMetric, Dataset: "orders"}},
			}
			tt.apply(input)

			optimized, err := optimizer.NewCanonical(optimizer.UnusedJoinEliminationRule{}).Optimize(context.Background(), input)
			if err != nil {
				t.Fatal(err)
			}
			if len(optimized.Joins) != 1 || optimized.Joins[0].Relationship != ordersToCalendar {
				t.Fatalf("optimized joins = %#v, want only orders_to_calendar", optimized.Joins)
			}
			if len(input.Joins) != 2 {
				t.Fatalf("optimizer mutated input joins: %#v", input.Joins)
			}
		})
	}
}
