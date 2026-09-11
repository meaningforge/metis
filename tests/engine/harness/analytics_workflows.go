package harness

import (
	"context"
	"fmt"
	"math/big"
	"testing"

	attributionanalytics "github.com/meaningforge/metis/analytics/attribution"
	comparisonanalytics "github.com/meaningforge/metis/analytics/comparison"
	"github.com/meaningforge/metis/app/auth"
	service "github.com/meaningforge/metis/app/service/semantic"
	"github.com/meaningforge/metis/query"
	enginefixture "github.com/meaningforge/metis/tests/engine/fixture"
)

const (
	AnalyticsProject = enginefixture.AnalyticsProject
	AnalyticsModel   = enginefixture.AnalyticsModel
)

// AnalyticsWorkflowRuntime adapts an executable production Backend to the
// shared public workflow contract. It deliberately contains no engine-specific
// query or expected result.
type AnalyticsWorkflowRuntime struct {
	Name            string
	AttributeMetric func(context.Context, service.MetricAttributionQuery) (*attributionanalytics.AttributeMetricResult, error)
	CompareMetrics  func(context.Context, service.MetricComparisonQuery) (*comparisonanalytics.Result, error)
}

type QueryMetricsRuntime struct {
	Name         string
	QueryMetrics func(context.Context, service.QueryMetricsRequest) (*service.QueryMetricsResult, error)
}

// RunQueryMetricsContract proves a real production Backend executes governed
// semantic intent and normalizes temporal and exact-decimal output types.
func RunQueryMetricsContract(t *testing.T, runtime QueryMetricsRuntime) {
	t.Helper()
	if runtime.Name == "" || runtime.QueryMetrics == nil {
		t.Fatal("query_metrics runtime requires a name and workflow function")
	}
	ctx := auth.WithPrincipal(context.Background(), &auth.Principal{Scopes: []string{auth.ScopeSemanticExecute}})
	month := query.TimeGrainMonth
	result, err := runtime.QueryMetrics(ctx, service.QueryMetricsRequest{Query: query.SemanticQuery{
		Project: AnalyticsProject, Model: AnalyticsModel,
		Metrics: []query.MetricRef{{Name: "total_revenue"}},
		Dimensions: []query.DimensionRef{
			{Name: "events.event_time", Grain: &month},
			{Name: "events.region"},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Count != 2 || len(result.Rows) != 2 || len(result.Schema.Columns) != 3 {
		t.Fatalf("query_metrics result = %#v", result)
	}
	rows := map[string][]any{}
	for _, row := range result.Rows {
		rows[fmt.Sprint(row[0])] = row
	}
	if row := rows["2026-01-01T00:00:00"]; len(row) != 3 || row[1] != "west" || !equalDecimalText(row[2], "5.25") {
		t.Fatalf("query_metrics January row = %#v", row)
	}
}

func equalDecimalText(value any, expected string) bool {
	actualNumber, actualOK := new(big.Rat).SetString(fmt.Sprint(value))
	expectedNumber, expectedOK := new(big.Rat).SetString(expected)
	return actualOK && expectedOK && actualNumber.Cmp(expectedNumber) == 0
}

// RunAnalyticsWorkflowContract verifies the closed attribute_metric and
// compare_metrics workflows against the canonical semantic model and dataset.
// A caller must load fixture.AnalyticsWorkflows and AnalyticsModelYAML first.
func RunAnalyticsWorkflowContract(t *testing.T, runtime AnalyticsWorkflowRuntime) {
	t.Helper()
	if runtime.Name == "" || runtime.AttributeMetric == nil || runtime.CompareMetrics == nil {
		t.Fatal("analytics workflow runtime requires a name and both workflow functions")
	}
	ctx := auth.WithPrincipal(context.Background(), &auth.Principal{Scopes: []string{auth.ScopeSemanticExecute}})

	t.Run(runtime.Name+"/attribute_metric/additive", func(t *testing.T) {
		result, err := runtime.AttributeMetric(ctx, attributionRequest("metric:"+enginefixture.AnalyticsModel+".total_revenue"))
		if err != nil {
			t.Fatal(err)
		}
		if result.Strategy != attributionanalytics.AttributionStrategyAdditive || len(result.Dimensions) != 1 || result.Dimensions[0].Additive == nil || result.Dimensions[0].Additive.Summary.TotalDelta != "2" {
			t.Fatalf("attribute_metric additive result = %#v", result)
		}
	})

	t.Run(runtime.Name+"/attribute_metric/ratio", func(t *testing.T) {
		result, err := runtime.AttributeMetric(ctx, attributionRequest("metric:"+enginefixture.AnalyticsModel+".conversion_rate"))
		if err != nil {
			t.Fatal(err)
		}
		if result.Strategy != attributionanalytics.AttributionStrategyRatio || len(result.Dimensions) != 1 || result.Dimensions[0].Ratio == nil || result.Dimensions[0].Ratio.Summary.RatioDelta == nil || *result.Dimensions[0].Ratio.Summary.RatioDelta != "0.25" {
			t.Fatalf("attribute_metric ratio result = %#v", result)
		}
	})

	t.Run(runtime.Name+"/compare_metrics", func(t *testing.T) {
		result, err := runtime.CompareMetrics(ctx, service.MetricComparisonQuery{
			ProjectID: enginefixture.AnalyticsProject,
			Metrics: []string{
				"metric:" + enginefixture.AnalyticsModel + ".total_revenue",
				"metric:" + enginefixture.AnalyticsModel + ".sessions",
			},
			TimeDimension: analyticsTimeDimension(),
			Baseline:      comparisonanalytics.Period{Start: "2026-01-01T00:00:00Z", End: "2026-02-01T00:00:00Z"},
			Current:       comparisonanalytics.Period{Start: "2026-02-01T00:00:00Z", End: "2026-03-01T00:00:00Z"},
			Dimensions:    []string{analyticsDimension()},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Rows) != 1 || len(result.Rows[0].Values) != 2 {
			t.Fatalf("compare_metrics result = %#v", result)
		}
		values := result.Rows[0].Values
		if values[0].Metric != "metric:"+enginefixture.AnalyticsModel+".sessions" || values[0].PercentChange == nil || *values[0].PercentChange != "100" || values[1].Delta == nil || *values[1].Delta != "2" {
			t.Fatalf("compare_metrics result = %#v", result)
		}
	})
}

func attributionRequest(metric string) service.MetricAttributionQuery {
	return service.MetricAttributionQuery{
		ProjectID:     enginefixture.AnalyticsProject,
		Metric:        metric,
		TimeDimension: analyticsTimeDimension(),
		Baseline:      attributionanalytics.AttributionPeriod{Start: "2026-01-01T00:00:00Z", End: "2026-02-01T00:00:00Z"},
		Current:       attributionanalytics.AttributionPeriod{Start: "2026-02-01T00:00:00Z", End: "2026-03-01T00:00:00Z"},
		Dimensions:    []string{analyticsDimension()},
	}
}

func analyticsTimeDimension() string {
	return "dimension:" + enginefixture.AnalyticsModel + ".events.event_time"
}

func analyticsDimension() string {
	return "dimension:" + enginefixture.AnalyticsModel + ".events.region"
}
