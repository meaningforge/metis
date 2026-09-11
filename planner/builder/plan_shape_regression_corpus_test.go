package builder

import (
	"testing"

	"github.com/meaningforge/metis/planner/semanticplan"
)

type planShapeRegressionCase struct {
	name string
	plan *semanticplan.SemanticPlan
	want semanticplan.PlanShapeSummary
}

func TestPlanShapeRegressionCorpus(t *testing.T) {
	cases := []planShapeRegressionCase{
		{
			name: "simple",
			plan: &semanticplan.SemanticPlan{
				Root:        semanticplan.DatasetRef{Name: "orders", Source: "orders"},
				Projections: []semanticplan.Projection{{Name: "revenue", Kind: semanticplan.ProjectionMetric, Dataset: "orders"}},
			},
			want: semanticplan.PlanShapeSummary{Projections: 1},
		},
		{
			name: "composition",
			plan: stagedTestPlan("orders", []semanticNodeFixture{
				{ID: "revenue", Kind: semanticplan.SemanticPlanNodeSourceAggregate, ShareGroup: "revenue_stage", Metrics: []string{"revenue"}},
				{ID: "cost", Kind: semanticplan.SemanticPlanNodeSourceAggregate, ShareGroup: "cost_stage", Metrics: []string{"cost"}},
				{ID: "margin", Kind: semanticplan.SemanticPlanNodePostAggregate, Inputs: []semanticNodeInputFixture{{NodeID: "revenue"}, {NodeID: "cost"}}},
			}),
			want: semanticplan.PlanShapeSummary{EvaluationNodes: 3, SourceEvaluationNodes: 2, DerivedEvaluationNodes: 1, SourceGroups: 2, SourceGroupMetrics: 2},
		},
		{
			name: "cumulative",
			plan: advancedPlan(semanticplan.SemanticPlanNodeCumulativeWindow, false),
			want: semanticplan.PlanShapeSummary{EvaluationNodes: 2, SourceEvaluationNodes: 1, DerivedEvaluationNodes: 1, AdvancedEvaluationNodes: 1, SourceGroups: 1, SourceGroupMetrics: 1},
		},
		{
			name: "conversion",
			plan: stagedTestPlan("sessions", []semanticNodeFixture{
				{ID: "sessions", Kind: semanticplan.SemanticPlanNodeSourceAggregate, ShareGroup: "sessions_stage", Metrics: []string{"sessions"}},
				{ID: "orders", Kind: semanticplan.SemanticPlanNodeSourceAggregate, ShareGroup: "orders_stage", Metrics: []string{"orders"}},
				{ID: "conversion_rate", Kind: semanticplan.SemanticPlanNodeConversion, Inputs: []semanticNodeInputFixture{{NodeID: "sessions"}, {NodeID: "orders"}}},
			}),
			want: semanticplan.PlanShapeSummary{EvaluationNodes: 3, SourceEvaluationNodes: 2, DerivedEvaluationNodes: 1, AdvancedEvaluationNodes: 1, SourceGroups: 2, SourceGroupMetrics: 2},
		},
		{
			name: "time_offset",
			plan: advancedPlan(semanticplan.SemanticPlanNodeTimeOffset, false),
			want: semanticplan.PlanShapeSummary{EvaluationNodes: 2, SourceEvaluationNodes: 1, DerivedEvaluationNodes: 1, AdvancedEvaluationNodes: 1, SourceGroups: 1, SourceGroupMetrics: 1},
		},
		{
			name: "fill_densification",
			plan: advancedPlan(semanticplan.SemanticPlanNodePostAggregate, true),
			want: semanticplan.PlanShapeSummary{EvaluationNodes: 2, SourceEvaluationNodes: 1, DerivedEvaluationNodes: 1, SourceGroups: 1, SourceGroupMetrics: 1, DenseCalendarDomains: 1},
		},
		{
			name: "grain_to_date",
			plan: func() *semanticplan.SemanticPlan {
				plan := advancedPlan(semanticplan.SemanticPlanNodeCumulativeWindow, false)
				node := plan.Nodes[1].(semanticplan.CumulativeWindowNode)
				node.CustomCalendarGrainToDate = &semanticplan.CustomCalendarGrainToDatePlan{}
				plan.Nodes[1] = node
				return plan
			}(),
			want: semanticplan.PlanShapeSummary{EvaluationNodes: 2, SourceEvaluationNodes: 1, DerivedEvaluationNodes: 1, AdvancedEvaluationNodes: 1, SourceGroups: 1, SourceGroupMetrics: 1, CustomCalendarDomains: 1},
		},
		{
			name: "semi_additive",
			plan: advancedPlan(semanticplan.SemanticPlanNodeSemiAdditiveLast, false),
			want: semanticplan.PlanShapeSummary{EvaluationNodes: 2, SourceEvaluationNodes: 1, DerivedEvaluationNodes: 1, AdvancedEvaluationNodes: 1, SourceGroups: 1, SourceGroupMetrics: 1},
		},
	}

	fingerprints := map[string]string{}
	seen := map[string]string{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := semanticplan.SummarizeSemanticPlan(tc.plan)
			if got != tc.want {
				t.Fatalf("summary = %+v, want %+v", got, tc.want)
			}
			first, err := semanticplan.Fingerprint(tc.plan)
			if err != nil {
				t.Fatalf("fingerprint: %v", err)
			}
			second, err := semanticplan.Fingerprint(tc.plan)
			if err != nil {
				t.Fatalf("repeat fingerprint: %v", err)
			}
			if first != second {
				t.Fatalf("fingerprint is not stable: %q != %q", first, second)
			}
			if other, ok := seen[first]; ok {
				t.Fatalf("fingerprint unexpectedly collides with %s", other)
			}
			seen[first] = tc.name
			fingerprints[tc.name] = first
		})
	}

	if fingerprints["cumulative"] == fingerprints["time_offset"] {
		t.Fatal("advanced plans with equal summaries must still retain distinct canonical fingerprints")
	}
	if fingerprints["cumulative"] == fingerprints["semi_additive"] {
		t.Fatal("advanced evaluation kind must remain represented in the canonical fingerprint")
	}
}

// stagedTestPlan builds a plan the way the planner does: the graph is
// scaffolding, and the plan owns the stages.
//
// These fixtures used to set only plan.Graph, which stopped describing anything
// once shape and lineage started reading plan ownership. That is the honest
// result rather than a reason to make the readers fall back -- a plan that owns
// no stages is not a plan any consumer should be able to summarise.
func stagedTestPlan(root string, stages []semanticNodeFixture) *semanticplan.SemanticPlan {
	plan := semanticPlanForTest(semanticplan.SemanticPlan{
		Root: semanticplan.DatasetRef{Name: root, Source: root},
	}, stages)
	return plan
}

func advancedPlan(kind semanticplan.SemanticPlanNodeKind, dense bool) *semanticplan.SemanticPlan {
	plan := stagedTestPlan("orders", []semanticNodeFixture{
		{ID: "base", Kind: semanticplan.SemanticPlanNodeSourceAggregate, ShareGroup: "orders_stage", Metrics: []string{"base"}},
		{ID: "derived", Kind: kind, Inputs: []semanticNodeInputFixture{{NodeID: "base"}}},
	})
	if dense {
		plan.DenseCalendar = &semanticplan.DenseCalendarPlan{Dataset: semanticplan.DatasetRef{Name: "time_spine", Source: "time_spine"}}
	}
	return plan
}
