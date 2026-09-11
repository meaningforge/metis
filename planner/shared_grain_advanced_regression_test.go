package planner_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/meaningforge/metis/planner"
	"github.com/meaningforge/metis/planner/conversion"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/query"
)

func TestSharedGrainRegressionAcrossProductionMetricKinds(t *testing.T) {
	month := query.TimeGrainMonth
	tests := []struct {
		name       string
		model      string
		query      query.SemanticQuery
		wantMetric string
		lower      bool
	}{
		{name: "ordinary", model: metricEvaluationModel, query: query.SemanticQuery{Model: "finance", Metrics: []query.MetricRef{{Name: "revenue"}}, Dimensions: []query.DimensionRef{{Name: "calendar.day_id", Grain: &month}}}, wantMetric: "revenue", lower: true},
		{name: "derived multi-root", model: metricEvaluationModel, query: query.SemanticQuery{Model: "finance", Metrics: []query.MetricRef{{Name: "gross_margin"}}, Dimensions: []query.DimensionRef{{Name: "calendar.day_id", Grain: &month}}}, wantMetric: "gross_margin", lower: true},
		{name: "conversion", model: conversionEvaluationModel, query: query.SemanticQuery{Model: "commerce", Metrics: []query.MetricRef{{Name: "signup_to_purchase_rate"}}}, wantMetric: "signup_to_purchase_rate"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolved := resolveMetricEvaluation(t, tt.model, tt.query)
			plan, err := planner.New().Plan(context.Background(), resolved, mustRenderer(t, "DUCKDB"))
			if err != nil {
				t.Fatal(err)
			}
			if plan.SharedGrain == nil {
				t.Fatalf("plan missing shared-grain evidence: %#v", plan)
			}
			semanticOutputGrain := plan.Groups
			if len(semanticPlanNodeFixturesForTest(plan)) != 0 {
				semanticOutputGrain = plan.Output.Grain
			}
			if !reflect.DeepEqual(plan.SharedGrain.Grain, semanticOutputGrain) {
				t.Fatalf("shared grain = %#v, semantic output grain = %#v", plan.SharedGrain.Grain, semanticOutputGrain)
			}
			if len(plan.SharedGrain.Metrics) != 1 || plan.SharedGrain.Metrics[0].Metric != tt.wantMetric {
				t.Fatalf("metric evidence = %#v", plan.SharedGrain.Metrics)
			}
			if !tt.lower {
				return
			}
			grainKey := plan.SharedGrain.GrainKey
			metrics := append([]semanticplan.MetricSharedGrainEvidence(nil), plan.SharedGrain.Metrics...)
			if _, err := conversion.BuildSQLPlan(plan, mustRenderer(t, "DUCKDB")); err != nil {
				t.Fatal(err)
			}
			if plan.SharedGrain.GrainKey != grainKey || !reflect.DeepEqual(plan.SharedGrain.Metrics, metrics) {
				t.Fatalf("lowering changed semantic shared-grain evidence: %#v", plan.SharedGrain)
			}
		})
	}
}

func TestSharedGrainMultiSourceDrillAcrossSurvivesLowering(t *testing.T) {
	month := query.TimeGrainMonth
	resolved := resolveMetricEvaluation(t, metricEvaluationModel, query.SemanticQuery{Model: "finance", Metrics: []query.MetricRef{{Name: "revenue"}, {Name: "cost"}}, Dimensions: []query.DimensionRef{{Name: "calendar.day_id", Grain: &month}}})
	plan, err := planner.New().Plan(context.Background(), resolved, mustRenderer(t, "DUCKDB"))
	if err != nil {
		t.Fatal(err)
	}
	if plan.SharedGrain == nil || len(semanticPlanNodeFixturesForTest(plan)) == 0 {
		t.Fatal("shared grain evidence or canonical graph is nil")
	}
	gotMetrics := make([]string, 0, len(plan.SharedGrain.Metrics))
	for _, evidence := range plan.SharedGrain.Metrics {
		gotMetrics = append(gotMetrics, evidence.Metric)
	}
	if want := []string{"cost", "revenue"}; !reflect.DeepEqual(gotMetrics, want) {
		t.Fatalf("shared metric evidence = %#v, want %#v", gotMetrics, want)
	}
	stmt, err := conversion.BuildSQLPlan(plan, mustRenderer(t, "DUCKDB"))
	if err != nil {
		t.Fatal(err)
	}
	root := stmt.Blocks[len(stmt.Blocks)-1]
	if len(stmt.Blocks) != 3 || len(root.Joins) != 1 || root.Joins[0].Kind != "full_outer" {
		t.Fatalf("drill-across lowering = %#v", stmt)
	}
	if !reflect.DeepEqual(plan.SharedGrain.Grain, plan.Output.Grain) {
		t.Fatalf("lowering changed shared semantic grain: shared=%#v output=%#v", plan.SharedGrain.Grain, plan.Output.Grain)
	}
}
