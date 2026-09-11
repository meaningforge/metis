package builder

import (
	"testing"

	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner/evaluation"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/resolver"
)

func TestSplitEvaluationPredicatesConsumesCumulativeMetricPlanSpec(t *testing.T) {
	plan := &evaluation.MetricEvaluationPlan{Nodes: []evaluation.MetricEvaluationNode{{
		ID:   "revenue_ytd",
		Kind: evaluation.MetricEvaluationCumulative,
		Spec: evaluation.MetricEvaluationSpec{Cumulative: &evaluation.CumulativeMetricEvaluationSpec{
			Spec: ossie.CumulativeMetricSpec{TimeDimension: "day_id"},
		}},
	}}}
	field := &ossie.Field{Name: "day_id"}
	groups := []semanticplan.GroupBy{{Name: "calendar.day_id", Dataset: "calendar", Field: field}}
	predicates := []semanticplan.Predicate{{
		Filter:  query.Filter{Field: "calendar.day_id"},
		Dataset: "calendar",
		Field:   field,
	}}

	pre, post, err := SplitEvaluationPredicates(plan, groups, predicates)
	if err != nil {
		t.Fatal(err)
	}
	if len(pre) != 0 {
		t.Fatalf("pre-aggregation predicates = %#v, want none", pre)
	}
	if len(post) != 1 || post[0].Name != "calendar.day_id" {
		t.Fatalf("post-evaluation predicates = %#v, want cumulative time predicate", post)
	}
}

func TestSplitEvaluationPredicatesFailsClosedOnMissingCumulativeSpec(t *testing.T) {
	plan := &evaluation.MetricEvaluationPlan{Nodes: []evaluation.MetricEvaluationNode{{
		ID:   "revenue_ytd",
		Kind: evaluation.MetricEvaluationCumulative,
	}}}

	if _, _, err := SplitEvaluationPredicates(plan, nil, nil); err == nil {
		t.Fatal("cumulative metric node without its typed spec was accepted")
	}
}

func TestLowerMetricEvaluationPlanDistinguishesMetricFreeBypassFromMissingPlan(t *testing.T) {
	model := &manifest.ModelIndex{}
	metricQuery := &resolver.ResolvedSemanticQuery{
		Model:   model,
		Metrics: []resolver.ResolvedMetric{{Name: "revenue"}},
	}
	if _, err := lowerMetricEvaluationPlan(metricQuery, nil, true, nil, nil); err == nil {
		t.Fatal("metric-bearing query reached lowering without evaluation.MetricEvaluationPlan")
	}

	metricFreeQuery := &resolver.ResolvedSemanticQuery{Model: model}
	construction, err := lowerMetricEvaluationPlan(metricFreeQuery, nil, false, nil, nil)
	if err != nil {
		t.Fatalf("metric-free lowering bypass failed: %v", err)
	}
	if len(construction.Nodes) != 0 {
		t.Fatalf("metric-free lowering fabricated nodes: %#v", construction.Nodes)
	}
}
