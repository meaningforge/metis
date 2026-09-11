package evaluation_test

import (
	"reflect"
	"testing"

	"github.com/meaningforge/metis/planner/evaluation"
	"github.com/meaningforge/metis/query"
)

func TestCloneMetricEvaluationPlanOwnsMutablePlanState(t *testing.T) {
	resolved := resolveMetricEvaluation(t, metricEvaluationModel, query.SemanticQuery{
		Model:   "finance",
		Metrics: []query.MetricRef{{Name: "contribution_margin"}},
	})
	plan, err := evaluation.BuildMetricEvaluationPlan(resolved)
	if err != nil {
		t.Fatal(err)
	}
	clone := evaluation.CloneMetricEvaluationPlan(plan)
	if clone == nil || !reflect.DeepEqual(plan, clone) {
		t.Fatalf("clone = %#v, want deep-equal plan", clone)
	}

	clone.Roots[0].Metric = "changed"
	clone.Nodes[2].Inputs[0].Metric = "changed"
	clone.Nodes[0].Metric.Name = "changed"
	if plan.Roots[0].Metric == "changed" {
		t.Fatal("root slice aliases clone")
	}
	if plan.Nodes[2].Inputs[0].Metric == "changed" {
		t.Fatal("input slice aliases clone")
	}
	if plan.Nodes[0].Metric.Name == "changed" {
		t.Fatal("metric pointer aliases clone")
	}
}

func TestFingerprintMetricEvaluationPlanIsDeterministicAndRootCanonical(t *testing.T) {
	resolved := resolveMetricEvaluation(t, metricEvaluationModel, query.SemanticQuery{
		Model:   "finance",
		Metrics: []query.MetricRef{{Name: "revenue"}},
		Filters: []query.Filter{{Field: "revenue", Operator: query.FilterGT, Value: float64(0)}},
	})
	plan, err := evaluation.BuildMetricEvaluationPlan(resolved)
	if err != nil {
		t.Fatal(err)
	}
	first, err := evaluation.FingerprintMetricEvaluationPlan(plan)
	if err != nil {
		t.Fatal(err)
	}
	second, err := evaluation.FingerprintMetricEvaluationPlan(evaluation.CloneMetricEvaluationPlan(plan))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("fingerprint changed across clone: %q != %q", first, second)
	}

	reordered := evaluation.CloneMetricEvaluationPlan(plan)
	for left, right := 0, len(reordered.Roots)-1; left < right; left, right = left+1, right-1 {
		reordered.Roots[left], reordered.Roots[right] = reordered.Roots[right], reordered.Roots[left]
	}
	canonical, err := evaluation.FingerprintMetricEvaluationPlan(reordered)
	if err != nil {
		t.Fatal(err)
	}
	if first != canonical {
		t.Fatalf("set-like root order changed fingerprint: %q != %q", first, canonical)
	}
}

func TestFingerprintMetricEvaluationPlanIsSensitiveToMetricMeaning(t *testing.T) {
	resolved := resolveMetricEvaluation(t, metricEvaluationModel, query.SemanticQuery{
		Model:   "finance",
		Metrics: []query.MetricRef{{Name: "contribution_margin"}},
	})
	plan, err := evaluation.BuildMetricEvaluationPlan(resolved)
	if err != nil {
		t.Fatal(err)
	}
	base, err := evaluation.FingerprintMetricEvaluationPlan(plan)
	if err != nil {
		t.Fatal(err)
	}
	changed := evaluation.CloneMetricEvaluationPlan(plan)
	changed.Nodes[2].Expression.Source += " + 0"
	got, err := evaluation.FingerprintMetricEvaluationPlan(changed)
	if err != nil {
		t.Fatal(err)
	}
	if got == base {
		t.Fatal("metric expression change did not change fingerprint")
	}
}

func TestExplainMetricEvaluationPlanReportsOnlyMetricEvaluationShape(t *testing.T) {
	resolved := resolveMetricEvaluation(t, metricEvaluationModel, query.SemanticQuery{
		Model:   "finance",
		Metrics: []query.MetricRef{{Name: "contribution_margin"}},
	})
	plan, err := evaluation.BuildMetricEvaluationPlan(resolved)
	if err != nil {
		t.Fatal(err)
	}
	explanation, err := evaluation.ExplainMetricEvaluationPlan(plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(explanation.Roots) != 1 || explanation.Roots[0].Metric != "contribution_margin" {
		t.Fatalf("explain roots = %#v", explanation.Roots)
	}
	if len(explanation.Nodes) != 3 {
		t.Fatalf("explain nodes = %#v", explanation.Nodes)
	}
	derived := explanation.Nodes[2]
	if derived.Metric != "contribution_margin" || derived.Kind != evaluation.MetricEvaluationDerived {
		t.Fatalf("derived explanation = %#v", derived)
	}
	if !reflect.DeepEqual(derived.Inputs, []string{"discounts", "revenue"}) {
		t.Fatalf("derived explain inputs = %#v", derived.Inputs)
	}
}
