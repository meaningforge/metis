package planner_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/meaningforge/metis/planner"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/query"
)

func TestPlannerAttachesSharedGrainEvidenceForMultiRootMetric(t *testing.T) {
	grain := query.TimeGrainMonth
	resolved := resolveMetricEvaluation(t, metricEvaluationModel, query.SemanticQuery{
		Model:      "finance",
		Metrics:    []query.MetricRef{{Name: "gross_margin"}},
		Dimensions: []query.DimensionRef{{Name: "calendar.day_id", Grain: &grain}},
	})

	plan, err := planner.New().Plan(context.Background(), resolved, mustRenderer(t, "DUCKDB"))
	if err != nil {
		t.Fatal(err)
	}
	if plan.SharedGrain == nil || len(plan.SharedGrain.Metrics) != 1 {
		t.Fatalf("shared grain = %#v", plan.SharedGrain)
	}
	evidence := plan.SharedGrain.Metrics[0]
	if evidence.Metric != "gross_margin" {
		t.Fatalf("metric = %q", evidence.Metric)
	}
	if !reflect.DeepEqual(evidence.RootDatasets, []string{"costs", "orders"}) {
		t.Fatalf("root datasets = %#v", evidence.RootDatasets)
	}
	wantPaths := [][]string{{"costs_to_calendar"}, {"orders_to_calendar"}}
	if !reflect.DeepEqual(evidence.RelationshipPaths, wantPaths) {
		t.Fatalf("relationship paths = %#v, want %#v", evidence.RelationshipPaths, wantPaths)
	}
	if plan.SharedGrain.GrainKey == "" || len(plan.SharedGrain.Grain) != 1 {
		t.Fatalf("shared grain = %#v", plan.SharedGrain)
	}
	if len(semanticPlanNodeFixturesForTest(plan)) == 0 || !reflect.DeepEqual(plan.Requested, []string{"gross_margin"}) {
		t.Fatalf("graph requested metrics = %#v", semanticPlanNodeFixturesForTest(plan))
	}
	var graphEvidence *semanticplan.MetricSharedGrainEvidence
	for i := range semanticPlanNodeFixturesForTest(plan) {
		if semanticPlanNodeFixturesForTest(plan)[i].ID == "gross_margin" {
			graphEvidence = semanticPlanNodeFixturesForTest(plan)[i].SharedGrainEvidence
			break
		}
	}
	if graphEvidence == nil || !reflect.DeepEqual(graphEvidence.RootDatasets, evidence.RootDatasets) || !reflect.DeepEqual(graphEvidence.RelationshipPaths, evidence.RelationshipPaths) {
		t.Fatalf("graph shared-grain evidence = %#v, plan evidence = %#v", graphEvidence, evidence)
	}
}

func TestPlannerSharedGrainEvidenceIsDeterministicForRequestedMetrics(t *testing.T) {
	resolved := resolveMetricEvaluation(t, metricEvaluationModel, query.SemanticQuery{
		Model: "finance",
		Metrics: []query.MetricRef{
			{Name: "revenue"},
			{Name: "discounts"},
		},
	})

	plan, err := planner.New().Plan(context.Background(), resolved, mustRenderer(t, "DUCKDB"))
	if err != nil {
		t.Fatal(err)
	}
	if plan.SharedGrain == nil {
		t.Fatal("shared grain evidence is nil")
	}
	got := make([]string, 0, len(plan.SharedGrain.Metrics))
	for _, evidence := range plan.SharedGrain.Metrics {
		got = append(got, evidence.Metric)
	}
	if want := []string{"discounts", "revenue"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("metric evidence order = %#v, want %#v", got, want)
	}
}
