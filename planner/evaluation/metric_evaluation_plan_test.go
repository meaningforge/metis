package evaluation_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/meaningforge/metis/planner/evaluation"
	"github.com/meaningforge/metis/query"
)

func TestBuildMetricEvaluationPlanMaterializesDerivedDependencyDAG(t *testing.T) {
	resolved := resolveMetricEvaluation(t, metricEvaluationModel, query.SemanticQuery{
		Model: "finance",
		Metrics: []query.MetricRef{
			{Name: "contribution_margin"},
		},
	})

	plan, err := evaluation.BuildMetricEvaluationPlan(resolved)
	if err != nil {
		t.Fatal(err)
	}
	if plan == nil {
		t.Fatal("metric-bearing query returned no evaluation.MetricEvaluationPlan")
	}

	wantRoots := []evaluation.MetricEvaluationRoot{{
		Metric: "contribution_margin",
		Role:   evaluation.MetricEvaluationRoleOutput,
	}}
	if !reflect.DeepEqual(plan.Roots, wantRoots) {
		t.Fatalf("roots = %#v, want %#v", plan.Roots, wantRoots)
	}

	wantIDs := []string{"discounts", "revenue", "contribution_margin"}
	gotIDs := make([]string, len(plan.Nodes))
	for i := range plan.Nodes {
		gotIDs[i] = plan.Nodes[i].ID
	}
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Fatalf("node order = %#v, want %#v", gotIDs, wantIDs)
	}
	for _, node := range plan.Nodes[:2] {
		if node.Kind != evaluation.MetricEvaluationSource || node.Spec.Source == nil || len(node.Inputs) != 0 {
			t.Fatalf("source node = %#v", node)
		}
	}
	derived := plan.Nodes[2]
	if derived.Kind != evaluation.MetricEvaluationDerived || derived.Spec.Derived == nil {
		t.Fatalf("derived node = %#v", derived)
	}
	wantInputs := []evaluation.MetricEvaluationInput{{Metric: "discounts"}, {Metric: "revenue"}}
	if !reflect.DeepEqual(derived.Inputs, wantInputs) {
		t.Fatalf("derived inputs = %#v, want %#v", derived.Inputs, wantInputs)
	}
}

func TestBuildMetricEvaluationPlanKeepsDistinctRootRoles(t *testing.T) {
	resolved := resolveMetricEvaluation(t, metricEvaluationModel, query.SemanticQuery{
		Model:   "finance",
		Metrics: []query.MetricRef{{Name: "revenue"}},
		Filters: []query.Filter{{Field: "revenue", Operator: query.FilterGT, Value: float64(0)}},
		OrderBy: []query.OrderBy{{Field: "revenue", Direction: query.SortDesc}},
	})

	plan, err := evaluation.BuildMetricEvaluationPlan(resolved)
	if err != nil {
		t.Fatal(err)
	}
	want := []evaluation.MetricEvaluationRoot{
		{Metric: "revenue", Role: evaluation.MetricEvaluationRoleOutput},
		{Metric: "revenue", Role: evaluation.MetricEvaluationRolePredicate},
		{Metric: "revenue", Role: evaluation.MetricEvaluationRoleOrder},
	}
	if !reflect.DeepEqual(plan.Roots, want) {
		t.Fatalf("roots = %#v, want %#v", plan.Roots, want)
	}
}

func TestBuildMetricEvaluationPlanBypassesMetricFreeQuery(t *testing.T) {
	resolved := resolveMetricEvaluation(t, metricEvaluationModel, query.SemanticQuery{
		Model:      "finance",
		Dimensions: []query.DimensionRef{{Name: "calendar.day_id"}},
	})
	plan, err := evaluation.BuildMetricEvaluationPlan(resolved)
	if err != nil {
		t.Fatal(err)
	}
	if plan != nil {
		t.Fatalf("metric-free query fabricated evaluation.MetricEvaluationPlan: %#v", plan)
	}
}

func TestValidateMetricEvaluationPlanRejectsKindSpecMismatch(t *testing.T) {
	resolved := resolveMetricEvaluation(t, metricEvaluationModel, query.SemanticQuery{
		Model:   "finance",
		Metrics: []query.MetricRef{{Name: "revenue"}},
	})
	plan, err := evaluation.BuildMetricEvaluationPlan(resolved)
	if err != nil {
		t.Fatal(err)
	}
	plan.Nodes[0].Kind = evaluation.MetricEvaluationDerived
	if err := evaluation.ValidateMetricEvaluationPlan(plan); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("validation error = %v, want kind/spec mismatch", err)
	}
}

func TestValidateMetricEvaluationPlanRejectsNonTopologicalInput(t *testing.T) {
	resolved := resolveMetricEvaluation(t, metricEvaluationModel, query.SemanticQuery{
		Model:   "finance",
		Metrics: []query.MetricRef{{Name: "contribution_margin"}},
	})
	plan, err := evaluation.BuildMetricEvaluationPlan(resolved)
	if err != nil {
		t.Fatal(err)
	}
	plan.Nodes[0].Inputs = []evaluation.MetricEvaluationInput{{Metric: "contribution_margin"}}
	if err := evaluation.ValidateMetricEvaluationPlan(plan); err == nil || !strings.Contains(err.Error(), "not an earlier node") {
		t.Fatalf("validation error = %v, want topological-order failure", err)
	}
}

func TestValidateMetricEvaluationPlanRejectsDuplicateInput(t *testing.T) {
	resolved := resolveMetricEvaluation(t, metricEvaluationModel, query.SemanticQuery{
		Model:   "finance",
		Metrics: []query.MetricRef{{Name: "contribution_margin"}},
	})
	plan, err := evaluation.BuildMetricEvaluationPlan(resolved)
	if err != nil {
		t.Fatal(err)
	}
	plan.Nodes[2].Inputs = append(plan.Nodes[2].Inputs, plan.Nodes[2].Inputs[0])
	if err := evaluation.ValidateMetricEvaluationPlan(plan); err == nil || !strings.Contains(err.Error(), "duplicate input") {
		t.Fatalf("validation error = %v, want duplicate-input failure", err)
	}
}
