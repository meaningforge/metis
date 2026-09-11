package attribution_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner/attribution"
	"github.com/meaningforge/metis/planner/evaluation"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/renderer"
	"github.com/meaningforge/metis/renderer/builtin"
	"github.com/meaningforge/metis/renderer/sql"
	"github.com/meaningforge/metis/resolver"
)

const metricAttributionModel = `
version: "0.2.0.dev0"
semantic_model:
  - name: attribution
    datasets:
      - name: events
        source: analytics.events
        fields:
          - name: amount
            datatype: Decimal
            expression:
              dialects: [{dialect: ANSI_SQL, expression: events.amount}]
          - name: converted
            datatype: Integer
            expression:
              dialects: [{dialect: ANSI_SQL, expression: events.converted}]
          - name: sessions
            datatype: Integer
            expression:
              dialects: [{dialect: ANSI_SQL, expression: events.sessions}]
          - name: customer_id
            datatype: String
            expression:
              dialects: [{dialect: ANSI_SQL, expression: events.customer_id}]
    metrics:
      - name: revenue
        datatype: Decimal
        expression:
          dialects: [{dialect: ANSI_SQL, expression: "SUM(events.amount)"}]
      - name: event_count
        datatype: Integer
        expression:
          dialects: [{dialect: ANSI_SQL, expression: "COUNT(*)"}]
      - name: peak_amount
        datatype: Decimal
        expression:
          dialects: [{dialect: ANSI_SQL, expression: "MAX(events.amount)"}]
      - name: average_amount
        datatype: Decimal
        expression:
          dialects: [{dialect: ANSI_SQL, expression: "AVG(events.amount)"}]
      - name: unique_customers
        datatype: Integer
        expression:
          dialects: [{dialect: ANSI_SQL, expression: "COUNT(DISTINCT events.customer_id)"}]
      - name: converted
        datatype: Integer
        expression:
          dialects: [{dialect: ANSI_SQL, expression: "SUM(events.converted)"}]
      - name: sessions
        datatype: Integer
        expression:
          dialects: [{dialect: ANSI_SQL, expression: "SUM(events.sessions)"}]
      - name: conversion_rate
        datatype: Decimal
        expression:
          dialects: [{dialect: ANSI_SQL, expression: "converted / sessions"}]
      - name: unique_customer_rate
        datatype: Decimal
        expression:
          dialects: [{dialect: ANSI_SQL, expression: "unique_customers / sessions"}]
      - name: revenue_less_events
        datatype: Decimal
        expression:
          dialects: [{dialect: ANSI_SQL, expression: "revenue - event_count"}]
`

func attributionPlanForMetric(t *testing.T, metric string) attribution.MetricAttributionPlan {
	t.Helper()
	resolved := resolveMetricEvaluation(t, metricAttributionModel, query.SemanticQuery{
		Model:   "attribution",
		Metrics: []query.MetricRef{{Name: metric}},
	})
	evaluation, err := evaluation.BuildMetricEvaluationPlan(resolved)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := attribution.BuildMetricAttributionPlan(evaluation, resolved.Model, metric)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func resolveMetricEvaluation(t *testing.T, modelText string, semanticQuery query.SemanticQuery) *resolver.ResolvedSemanticQuery {
	t.Helper()
	doc, err := ossie.NewLoader().Load([]byte(modelText))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := manifest.BuildProjectManifest("test", doc)
	if err != nil {
		t.Fatal(err)
	}
	semanticQuery.Project = "test"
	resolved, err := resolver.New(manifest.NewStore(snapshot)).ResolveForRenderer(context.Background(), semanticQuery, mustRenderer(t, "DORIS"))
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func mustRenderer(t *testing.T, dialect string) renderer.Renderer {
	t.Helper()
	registry, err := renderer.NewRegistry(builtin.Renderers()...)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := registry.Resolve(sql.SQLDialect(dialect))
	if err != nil {
		t.Fatal(err)
	}
	return selected
}

func TestMetricAttributionPlanProjectsAdditiveSemanticManifestProof(t *testing.T) {
	for _, metric := range []string{"revenue", "event_count", "revenue_less_events"} {
		t.Run(metric, func(t *testing.T) {
			plan := attributionPlanForMetric(t, metric)
			if plan.Exactness != attribution.MetricAttributionExact || plan.Strategy != semanticplan.MetricAttributionAdditiveContribution {
				t.Fatalf("plan = %#v", plan)
			}
			if plan.Reconciliation != semanticplan.MetricAttributionReconcileSegmentDelta {
				t.Fatalf("reconciliation = %q", plan.Reconciliation)
			}
			want := []attribution.MetricAttributionComponent{{Metric: metric, Role: attribution.MetricAttributionValue}}
			if !reflect.DeepEqual(plan.Components, want) {
				t.Fatalf("components = %#v, want %#v", plan.Components, want)
			}
		})
	}
}

func TestMetricAttributionPlanProjectsUnsupportedSemanticManifestProof(t *testing.T) {
	for _, metric := range []string{"peak_amount", "average_amount", "unique_customers", "unique_customer_rate"} {
		t.Run(metric, func(t *testing.T) {
			plan := attributionPlanForMetric(t, metric)
			if plan.Exactness != attribution.MetricAttributionUnsupported || plan.Reason != attribution.MetricAttributionReasonUnsupportedDecomposition {
				t.Fatalf("plan = %#v", plan)
			}
			if plan.Detail == "" {
				t.Fatalf("unsupported plan has no canonical decomposition detail: %#v", plan)
			}
		})
	}
}

func TestMetricAttributionPlanBuildsExactRatioMixRateRecipe(t *testing.T) {
	plan := attributionPlanForMetric(t, "conversion_rate")
	if plan.Exactness != attribution.MetricAttributionExact || plan.Strategy != semanticplan.MetricAttributionRatioMixRate {
		t.Fatalf("plan = %#v", plan)
	}
	if plan.Reconciliation != semanticplan.MetricAttributionReconcileMixRate {
		t.Fatalf("reconciliation = %q", plan.Reconciliation)
	}
	want := []attribution.MetricAttributionComponent{
		{Metric: "converted", Role: attribution.MetricAttributionNumerator},
		{Metric: "sessions", Role: attribution.MetricAttributionDenominator},
	}
	if !reflect.DeepEqual(plan.Components, want) {
		t.Fatalf("components = %#v, want %#v", plan.Components, want)
	}
}

func TestValidateMetricAttributionPlanRejectsUnsupportedPlanWithExecutableSemantics(t *testing.T) {
	plan := attribution.MetricAttributionPlan{
		Metric:         "unique_customers",
		Exactness:      attribution.MetricAttributionUnsupported,
		Strategy:       semanticplan.MetricAttributionAdditiveContribution,
		Reconciliation: semanticplan.MetricAttributionReconcileSegmentDelta,
		Reason:         attribution.MetricAttributionReasonUnsupportedDecomposition,
		Detail:         "aggregation is not additive",
	}
	if err := attribution.ValidateMetricAttributionPlan(plan); err == nil {
		t.Fatal("unsupported attribution plan with executable semantics unexpectedly validated")
	}
}

func TestValidateMetricAttributionPlanRejectsRatioWithDuplicateComponents(t *testing.T) {
	plan := attribution.MetricAttributionPlan{
		Metric:    "conversion_rate",
		Exactness: attribution.MetricAttributionExact,
		Strategy:  semanticplan.MetricAttributionRatioMixRate,
		Components: []attribution.MetricAttributionComponent{
			{Metric: "sessions", Role: attribution.MetricAttributionNumerator},
			{Metric: "sessions", Role: attribution.MetricAttributionDenominator},
		},
		Reconciliation: semanticplan.MetricAttributionReconcileMixRate,
	}
	if err := attribution.ValidateMetricAttributionPlan(plan); err == nil {
		t.Fatal("ratio attribution plan with duplicate components unexpectedly validated")
	}
}
