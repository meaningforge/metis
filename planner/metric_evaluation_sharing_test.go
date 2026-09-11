package planner_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/meaningforge/metis/planner"
	"github.com/meaningforge/metis/planner/conversion"
	"github.com/meaningforge/metis/planner/evaluation"
	"github.com/meaningforge/metis/query"
)

func TestMetricEvaluationNodesShareProvenSourceAggregation(t *testing.T) {
	resolved := resolveMetricEvaluation(t, metricEvaluationModel, query.SemanticQuery{
		Model:   "finance",
		Metrics: []query.MetricRef{{Name: "contribution_margin"}},
	})

	evaluationPlan, err := evaluation.BuildMetricEvaluationPlan(resolved)
	if err != nil {
		t.Fatal(err)
	}
	if got := metricEvaluationNodeIDs(evaluationPlan); !reflect.DeepEqual(got, []string{"discounts", "revenue", "contribution_margin"}) {
		t.Fatalf("metric evaluation identities = %v", got)
	}

	semanticPlan, err := planner.New().Plan(context.Background(), resolved, mustRenderer(t, "DUCKDB"))
	if err != nil {
		t.Fatal(err)
	}
	nodes := semanticPlanNodeFixturesForTest(semanticPlan)
	groups := sharedSourceMetricGroups(nodes)
	if len(groups) != 1 || !reflect.DeepEqual(groups[0], []string{"discounts", "revenue"}) {
		t.Fatalf("shared source aggregation proof = %#v", groups)
	}

	sqlPlan, err := conversion.BuildSQLPlan(semanticPlan, mustRenderer(t, "DUCKDB"))
	if err != nil {
		t.Fatal(err)
	}
	if len(sqlPlan.Blocks) != 3 || len(sqlPlan.Blocks[0].Projections) != 2 {
		t.Fatalf("two metric identities did not share one aggregation block: %#v", sqlPlan)
	}
}

func TestMetricEvaluationPassthroughReuseAvoidsRedundantBranch(t *testing.T) {
	resolved := resolveMetricEvaluation(t, metricEvaluationModel, query.SemanticQuery{
		Model: "finance",
		Metrics: []query.MetricRef{
			{Name: "contribution_margin"},
			{Name: "revenue"},
		},
	})

	evaluationPlan, err := evaluation.BuildMetricEvaluationPlan(resolved)
	if err != nil {
		t.Fatal(err)
	}
	if got := metricEvaluationNodeIDs(evaluationPlan); !reflect.DeepEqual(got, []string{"discounts", "revenue", "contribution_margin"}) {
		t.Fatalf("metric evaluation identities = %v", got)
	}
	if !reflect.DeepEqual(evaluationPlan.Roots, []evaluation.MetricEvaluationRoot{
		{Metric: "contribution_margin", Role: evaluation.MetricEvaluationRoleOutput},
		{Metric: "revenue", Role: evaluation.MetricEvaluationRoleOutput},
	}) {
		t.Fatalf("metric evaluation roots = %#v", evaluationPlan.Roots)
	}

	semanticPlan, err := planner.New().Plan(context.Background(), resolved, mustRenderer(t, "DUCKDB"))
	if err != nil {
		t.Fatal(err)
	}
	revenueNodes := 0
	for _, node := range semanticPlanNodeFixturesForTest(semanticPlan) {
		if node.ID == "revenue" {
			revenueNodes++
		}
	}
	if revenueNodes != 1 {
		t.Fatalf("revenue semantic nodes = %d, want one reusable dependency", revenueNodes)
	}

	sqlPlan, err := conversion.BuildSQLPlan(semanticPlan, mustRenderer(t, "DUCKDB"))
	if err != nil {
		t.Fatal(err)
	}
	if len(sqlPlan.Blocks) != 3 {
		t.Fatalf("passthrough output introduced a redundant branch: %#v", sqlPlan)
	}
	revenueComputations := 0
	for _, block := range sqlPlan.Blocks[:len(sqlPlan.Blocks)-1] {
		for _, projection := range block.Projections {
			if projection.Alias == "revenue" {
				revenueComputations++
			}
		}
	}
	if revenueComputations != 1 {
		t.Fatalf("revenue computations = %d, want one reusable computation", revenueComputations)
	}
}

func metricEvaluationNodeIDs(plan *evaluation.MetricEvaluationPlan) []string {
	ids := make([]string, len(plan.Nodes))
	for i, node := range plan.Nodes {
		ids[i] = node.ID
	}
	return ids
}
