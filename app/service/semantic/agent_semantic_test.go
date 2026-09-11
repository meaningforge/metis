package semantic_test

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"

	service "github.com/meaningforge/metis/app/service/semantic"
	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/serrors"
	"github.com/meaningforge/metis/tests/conformance/fixtures"
)

type projectCapabilityProvider map[string][]service.AgentProjectCapability

func (p projectCapabilityProvider) ProjectCapabilities(_ context.Context, project string) []service.AgentProjectCapability {
	return append([]service.AgentProjectCapability(nil), p[project]...)
}

func TestAgentSemanticListProjectsReportsDeploymentCapabilities(t *testing.T) {
	surface := service.NewAgentSemanticService(newService(t), projectCapabilityProvider{
		"finance": {service.AgentCapabilityQueryMetrics, "future_dynamic_value", service.AgentCapabilityCompileSQL, service.AgentCapabilityQueryMetrics},
	})

	projects, err := surface.ListProjects(context.Background(), service.ListProjectsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(projects.Projects) != 1 || !slices.Equal(projects.Projects[0].Capabilities, []service.AgentProjectCapability{
		service.AgentCapabilityCompileSQL,
		service.AgentCapabilityQueryMetrics,
	}) {
		t.Fatalf("projects = %#v", projects.Projects)
	}
}

func TestAgentSemanticFunnel(t *testing.T) {
	surface := service.NewAgentSemanticService(newService(t))

	projects, err := surface.ListProjects(context.Background(), service.ListProjectsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(projects.Projects) != 1 || projects.Projects[0].ProjectID != "finance" || projects.Projects[0].Name != "finance" || !slices.Equal(projects.Projects[0].Capabilities, []service.AgentProjectCapability{service.AgentCapabilityCompileSQL}) {
		t.Fatalf("projects = %#v", projects)
	}
	models, err := surface.ListModels(context.Background(), service.ListModelsRequest{ProjectID: "finance"})
	if err != nil {
		t.Fatal(err)
	}
	if len(models.Models) != 1 || models.Models[0].Name != "sales" || models.Models[0].Ref != "model:sales" {
		t.Fatalf("models = %#v", models)
	}

	unscopedMetrics, err := surface.ListMetrics(context.Background(), service.ListMetricsRequest{ProjectID: "finance", Search: []string{"revenue"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(unscopedMetrics.Metrics) != 1 || len(unscopedMetrics.Metrics[0].Dimensions) != 0 {
		t.Fatalf("unscoped metrics should omit dimension fan-out: %#v", unscopedMetrics)
	}
	metrics, err := surface.ListMetrics(context.Background(), service.ListMetricsRequest{ProjectID: "finance", Models: []string{models.Models[0].Ref}, Search: []string{"revenue"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(metrics.Metrics) != 1 || metrics.Metrics[0].Ref != "metric:sales.total_revenue" || metrics.Metrics[0].Model != "sales" || metrics.Metrics[0].ModelRef != "model:sales" || len(metrics.Metrics[0].Dimensions) == 0 {
		t.Fatalf("metrics = %#v", metrics)
	}

	dimensions, err := surface.GetDimensions(context.Background(), service.GetDimensionsRequest{
		ProjectID: "finance",
		Metrics:   []string{metrics.Metrics[0].Ref},
		Search:    []string{"region"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(dimensions.Dimensions) != 1 || dimensions.Dimensions[0].Ref != "dimension:sales.orders.region" || dimensions.Dimensions[0].ModelRef != "model:sales" || dimensions.Dimensions[0].Dataset != "orders" || dimensions.Dimensions[0].Type != service.AgentGroupByDimension {
		t.Fatalf("dimensions = %#v", dimensions)
	}
	detail, err := surface.GetDimension(context.Background(), service.AgentGetDimensionRequest{ProjectID: "finance", Ref: dimensions.Dimensions[0].Ref})
	if err != nil || detail.Dimension.DataType == "" || len(detail.Dimension.ValidGrains) != 0 {
		t.Fatalf("dimension detail = %#v, err = %v", detail, err)
	}

	compiledRequest, err := surface.BuildCompileRequest(service.AgentCompileRequest{
		ProjectID:     "finance",
		Dialect:       "DORIS",
		OutputMetrics: []string{metrics.Metrics[0].Ref},
		GroupBy: []service.AgentGroupByParam{{
			Name: dimensions.Dimensions[0].Ref,
			Type: service.AgentGroupByDimension,
		}},
		OrderBy: []service.AgentOrderByParam{{Name: metrics.Metrics[0].Ref, Descending: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if compiledRequest.Query.Project != "finance" || compiledRequest.Query.Model != "sales" {
		t.Fatalf("compiled request identity = %#v", compiledRequest.Query)
	}
	if len(compiledRequest.Query.Metrics) != 1 || compiledRequest.Query.Metrics[0].Name != "total_revenue" {
		t.Fatalf("compiled metrics = %#v", compiledRequest.Query.Metrics)
	}
	if len(compiledRequest.Query.Dimensions) != 1 || compiledRequest.Query.Dimensions[0].Name != "orders.region" || compiledRequest.Query.Dimensions[0].Grain != nil {
		t.Fatalf("compiled dimensions = %#v", compiledRequest.Query.Dimensions)
	}
	if len(compiledRequest.Query.OrderBy) != 1 || compiledRequest.Query.OrderBy[0].Field != "total_revenue" || compiledRequest.Query.OrderBy[0].Direction != query.SortDesc {
		t.Fatalf("compiled order_by = %#v", compiledRequest.Query.OrderBy)
	}
}

func TestAgentSemanticDimensionsDescribeCustomTimeGrainContract(t *testing.T) {
	const customCalendarModel = `
version: "0.2.0.dev0"
semantic_model:
  - name: fiscal_dense
    custom_extensions:
      - vendor_name: METIS
        data: '{"kind":"custom_calendar","dataset":"calendar","base_time_dimension":"calendar_day","grains":[{"name":"fiscal_week","bucket_dimension":"fiscal_week_start","ordinal_dimension":"fiscal_week_index","dense_mapping":true}]}'
    datasets:
      - name: orders
        source: analytics.dense_orders
        fields:
          - {name: order_day, datatype: Date, expression: {dialects: [{dialect: ANSI_SQL, expression: order_day}]}, dimension: {is_time: true}}
          - {name: revenue, datatype: Decimal, expression: {dialects: [{dialect: ANSI_SQL, expression: revenue}]}}
      - name: calendar
        source: analytics.dense_calendar
        primary_key: [calendar_day]
        fields:
          - {name: calendar_day, datatype: Date, expression: {dialects: [{dialect: ANSI_SQL, expression: calendar_day}]}, dimension: {is_time: true}}
          - {name: fiscal_week_start, datatype: Date, expression: {dialects: [{dialect: ANSI_SQL, expression: fiscal_week_start}]}, dimension: {}}
          - {name: fiscal_week_index, datatype: Integer, expression: {dialects: [{dialect: ANSI_SQL, expression: fiscal_week_index}]}, dimension: {}}
    relationships:
      - {name: orders_to_calendar, from: orders, to: calendar, from_columns: [order_day], to_columns: [calendar_day]}
    metrics:
      - name: revenue
        datatype: Decimal
        expression: {dialects: [{dialect: ANSI_SQL, expression: "SUM(revenue)"}]}
        custom_extensions:
          - vendor_name: METIS
            data: '{"kind":"time_binding","time_dimension":"calendar_day"}'
      - name: previous_fiscal_week_revenue
        description: Prior revenue while retaining otherwise empty reporting periods.
        datatype: Decimal
        expression: {dialects: [{dialect: ANSI_SQL, expression: revenue}]}
        custom_extensions:
          - vendor_name: METIS
            data: '{"kind":"time_offset","base_metric":"revenue","time_dimension":"calendar_day","offset":{"count":-1,"unit":"fiscal_week"}}'
`
	doc, err := ossie.NewLoader().Load([]byte(customCalendarModel))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := manifest.BuildProjectManifest("finance", doc)
	if err != nil {
		t.Fatal(err)
	}
	surface := service.NewAgentSemanticService(service.NewDiscoveryService(manifest.NewStore(snapshot))).WithProjectAuthorizer(service.AllAccessProjectAuthorizer{})
	metrics, err := surface.ListMetrics(context.Background(), service.ListMetricsRequest{
		ProjectID: "finance",
		Search:    []string{"empty reporting periods"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(metrics.Metrics) != 1 || metrics.Metrics[0].Ref != "metric:fiscal_dense.previous_fiscal_week_revenue" {
		t.Fatalf("description search metrics = %#v", metrics.Metrics)
	}
	result, err := surface.GetDimensions(context.Background(), service.GetDimensionsRequest{
		ProjectID: "finance",
		Metrics:   []string{"metric:fiscal_dense.previous_fiscal_week_revenue"},
		Search:    []string{"fiscal_week"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Dimensions) == 0 || result.Dimensions[0].Ref != "dimension:fiscal_dense.calendar.calendar_day" {
		t.Fatalf("dimensions = %#v", result.Dimensions)
	}
	if len(result.Dimensions[0].CanonicalGroupings) != 1 {
		t.Fatalf("canonical groupings = %#v", result.Dimensions[0].CanonicalGroupings)
	}
	canonical := result.Dimensions[0].CanonicalGroupings[0]
	if canonical.Grain != query.TimeGrain("fiscal_week") || canonical.GroupBy.Name != result.Dimensions[0].Ref || canonical.GroupBy.Type != service.AgentGroupByTimeDimension || canonical.GroupBy.Grain == nil || *canonical.GroupBy.Grain != canonical.Grain || canonical.PhysicalBucket != "dimension:fiscal_dense.calendar.fiscal_week_start" {
		t.Fatalf("canonical fiscal-week grouping = %#v", canonical)
	}
	dimensionResult, err := surface.GetDimension(context.Background(), service.AgentGetDimensionRequest{ProjectID: "finance", Ref: result.Dimensions[0].Ref})
	if err != nil {
		t.Fatal(err)
	}
	dimension := dimensionResult.Dimension
	wantGrains := []query.TimeGrain{
		query.TimeGrainYear,
		query.TimeGrainQuarter,
		query.TimeGrainMonth,
		query.TimeGrainWeek,
		query.TimeGrainDay,
		query.TimeGrainHour,
		query.TimeGrain("fiscal_week"),
	}
	if dimension.Type != service.AgentGroupByTimeDimension || !slices.Equal(dimension.ValidGrains, wantGrains) {
		t.Fatalf("dimension metadata = %#v", dimension)
	}
	if len(dimension.SemanticEvidence) != 1 || dimension.SemanticEvidence[0].Effect != service.AgentSemanticEffectPreservesEmptyPeriods || !slices.Equal(dimension.SemanticEvidence[0].Grains, []query.TimeGrain{"fiscal_week"}) {
		t.Fatalf("semantic evidence = %#v", dimension.SemanticEvidence)
	}

	grain := dimension.ValidGrains[len(dimension.ValidGrains)-1]
	compiled, err := surface.BuildCompileRequest(service.AgentCompileRequest{
		ProjectID:     "finance",
		Dialect:       "DORIS",
		OutputMetrics: []string{"metric:fiscal_dense.previous_fiscal_week_revenue"},
		GroupBy: []service.AgentGroupByParam{{
			Name:  dimension.Ref,
			Type:  dimension.Type,
			Grain: &grain,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(compiled.Query.Dimensions) != 1 || compiled.Query.Dimensions[0].Name != "calendar.calendar_day" || compiled.Query.Dimensions[0].Grain == nil || *compiled.Query.Dimensions[0].Grain != query.TimeGrain("fiscal_week") {
		t.Fatalf("compiled dimensions = %#v", compiled.Query.Dimensions)
	}

	_, err = surface.BuildCompileRequest(service.AgentCompileRequest{
		ProjectID:     "finance",
		Dialect:       "DORIS",
		OutputMetrics: []string{"metric:fiscal_dense.previous_fiscal_week_revenue"},
		GroupBy: []service.AgentGroupByParam{{
			Name: "dimension:fiscal_dense.calendar.fiscal_week_start", Type: service.AgentGroupByTimeDimension, Grain: &grain,
		}},
	})
	var semanticErr *serrors.Error
	if !errors.As(err, &semanticErr) || semanticErr.Code != serrors.ErrIncompatibleQueryGrain {
		t.Fatalf("wrong custom bucket error = %v, want INCOMPATIBLE_QUERY_GRAIN", err)
	}
	remediation, ok := semanticErr.Details["remediation"].(map[string]any)
	if !ok {
		t.Fatalf("remediation = %#v", semanticErr.Details["remediation"])
	}
	repaired, ok := remediation["group_by"].(service.AgentGroupByParam)
	if !ok || repaired.Name != result.Dimensions[0].Ref || repaired.Type != service.AgentGroupByTimeDimension || repaired.Grain == nil || *repaired.Grain != grain {
		t.Fatalf("structured group_by remediation = %#v", remediation["group_by"])
	}
	if remediation["do_not_group_by"] != "dimension:fiscal_dense.calendar.fiscal_week_start" || remediation["physical_bucket"] != "dimension:fiscal_dense.calendar.fiscal_week_start" {
		t.Fatalf("structured bucket remediation = %#v", remediation)
	}
}

func TestAgentSemanticDimensionsExposeConversionGrouping(t *testing.T) {
	doc, err := ossie.NewLoader().Load(fixtures.ConversionModelYAML)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := manifest.BuildProjectManifest(fixtures.ConformanceProject, doc)
	if err != nil {
		t.Fatal(err)
	}
	surface := service.NewAgentSemanticService(service.NewDiscoveryService(manifest.NewStore(snapshot))).WithProjectAuthorizer(service.AllAccessProjectAuthorizer{})
	metrics, err := surface.ListMetrics(context.Background(), service.ListMetricsRequest{
		ProjectID: fixtures.ConformanceProject,
		Search:    []string{"share"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(metrics.Metrics) == 0 || metrics.Metrics[0].Ref != "metric:conversion.signup_to_purchase_rate" {
		t.Fatalf("conversion metrics = %#v", metrics.Metrics)
	}
	dimensions, err := surface.GetDimensions(context.Background(), service.GetDimensionsRequest{
		ProjectID: fixtures.ConformanceProject,
		Metrics:   []string{"metric:conversion.signup_to_purchase_rate"},
		Search:    []string{"campaign"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(dimensions.Dimensions) != 1 || dimensions.Dimensions[0].Ref != "dimension:conversion.signups.campaign" {
		t.Fatalf("conversion dimensions = %#v", dimensions.Dimensions)
	}
}

func TestAgentSemanticIntegerTimeLabelDoesNotAdvertiseOrAcceptGrains(t *testing.T) {
	const modelYAML = `
version: "0.2.0.dev0"
semantic_model:
  - name: retail
    datasets:
      - name: dates
        source: analytics.dates
        fields:
          - {name: year_number, datatype: Integer, expression: {dialects: [{dialect: ANSI_SQL, expression: year_number}]}, dimension: {is_time: true}}
          - {name: amount, datatype: Decimal, expression: {dialects: [{dialect: ANSI_SQL, expression: amount}]}}
    metrics:
      - {name: revenue, datatype: Decimal, expression: {dialects: [{dialect: ANSI_SQL, expression: "SUM(amount)"}]}}
`
	doc, err := ossie.NewLoader().Load([]byte(modelYAML))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := manifest.BuildProjectManifest("finance", doc)
	if err != nil {
		t.Fatal(err)
	}
	surface := service.NewAgentSemanticService(service.NewDiscoveryService(manifest.NewStore(snapshot))).WithProjectAuthorizer(service.AllAccessProjectAuthorizer{})
	detail, err := surface.GetDimension(context.Background(), service.AgentGetDimensionRequest{ProjectID: "finance", Ref: "dimension:retail.dates.year_number"})
	if err != nil {
		t.Fatal(err)
	}
	if detail.Dimension.Type != service.AgentGroupByTimeDimension || len(detail.Dimension.ValidGrains) != 0 {
		t.Fatalf("integer time label metadata = %#v", detail.Dimension)
	}
	grain := query.TimeGrainYear
	_, err = surface.BuildCompileRequest(service.AgentCompileRequest{
		ProjectID: "finance", Dialect: "DUCKDB", OutputMetrics: []string{"metric:retail.revenue"},
		GroupBy: []service.AgentGroupByParam{{Name: detail.Dimension.Ref, Type: detail.Dimension.Type, Grain: &grain}},
	})
	var semanticErr *serrors.Error
	if !errors.As(err, &semanticErr) || semanticErr.Code != serrors.ErrIncompatibleQueryGrain {
		t.Fatalf("integer time grain error = %#v", err)
	}
}

func TestAgentSemanticDimensionsArePaginated(t *testing.T) {
	const modelYAML = `
version: "0.2.0.dev0"
semantic_model:
  - name: retail
    datasets:
      - name: sales
        source: analytics.sales
        fields:
          - {name: brand, datatype: String, expression: {dialects: [{dialect: ANSI_SQL, expression: brand}]}, dimension: {}}
          - {name: region, datatype: String, expression: {dialects: [{dialect: ANSI_SQL, expression: region}]}, dimension: {}}
`
	doc, err := ossie.NewLoader().Load([]byte(modelYAML))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := manifest.BuildProjectManifest("finance", doc)
	if err != nil {
		t.Fatal(err)
	}
	surface := service.NewAgentSemanticService(service.NewDiscoveryService(manifest.NewStore(snapshot))).WithProjectAuthorizer(service.AllAccessProjectAuthorizer{})
	limit := 1
	first, err := surface.GetDimensions(context.Background(), service.GetDimensionsRequest{ProjectID: "finance", Model: "model:retail", Limit: &limit})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Dimensions) != 1 || !first.Truncated || first.NextCursor == "" {
		t.Fatalf("first dimension page = %#v", first)
	}
	second, err := surface.GetDimensions(context.Background(), service.GetDimensionsRequest{ProjectID: "finance", Model: "model:retail", Limit: &limit, Cursor: first.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Dimensions) != 1 || second.Dimensions[0].Ref == first.Dimensions[0].Ref {
		t.Fatalf("second dimension page = %#v", second)
	}
	_, err = surface.GetDimensions(context.Background(), service.GetDimensionsRequest{ProjectID: "finance", Model: "model:retail", Search: []string{"region"}, Limit: &limit, Cursor: first.NextCursor})
	var semanticErr *serrors.Error
	if !errors.As(err, &semanticErr) || semanticErr.Code != serrors.ErrInvalidQuery {
		t.Fatalf("cursor reused with different search error = %#v", err)
	}
}

func TestAgentSemanticMetricsExposeDefinitionEquivalenceWithoutSelecting(t *testing.T) {
	const modelYAML = `
version: "0.2.0.dev0"
semantic_model:
  - name: retail
    datasets:
      - name: sales
        source: analytics.sales
        fields:
          - {name: amount, datatype: Decimal, expression: {dialects: [{dialect: ANSI_SQL, expression: amount}]}}
    metrics:
      - {name: total_sales, datatype: Decimal, expression: {dialects: [{dialect: ANSI_SQL, expression: "SUM(sales.amount)"}]}}
      - {name: sales_by_brand, datatype: Decimal, expression: {dialects: [{dialect: ANSI_SQL, expression: "SUM(sales.amount)"}]}}
`
	doc, err := ossie.NewLoader().Load([]byte(modelYAML))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := manifest.BuildProjectManifest("finance", doc)
	if err != nil {
		t.Fatal(err)
	}
	surface := service.NewAgentSemanticService(service.NewDiscoveryService(manifest.NewStore(snapshot))).WithProjectAuthorizer(service.AllAccessProjectAuthorizer{})
	result, err := surface.ListMetrics(context.Background(), service.ListMetricsRequest{ProjectID: "finance"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Metrics) != 2 || !result.SelectionRequired {
		t.Fatalf("metrics = %#v", result)
	}
	for _, metric := range result.Metrics {
		if len(metric.DefinitionEquivalentRefs) != 1 || metric.DefinitionEquivalentRefs[0] == metric.Ref {
			t.Fatalf("definition equivalence for %s = %#v", metric.Ref, metric.DefinitionEquivalentRefs)
		}
	}
}

func TestAgentSemanticListMetricsSearchIsDeterministicNameSubstring(t *testing.T) {
	surface := service.NewAgentSemanticService(newService(t))
	metrics, err := surface.ListMetrics(context.Background(), service.ListMetricsRequest{ProjectID: "finance", Search: []string{"TOTAL_REV"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(metrics.Metrics) != 1 || metrics.Metrics[0].Name != "total_revenue" {
		t.Fatalf("metrics = %#v", metrics)
	}
}

func TestAgentSemanticSearchTermCountIsBounded(t *testing.T) {
	surface := service.NewAgentSemanticService(newService(t))
	terms := make([]string, 21)
	for i := range terms {
		terms[i] = "term-" + strconv.Itoa(i)
	}
	_, err := surface.ListMetrics(context.Background(), service.ListMetricsRequest{ProjectID: "finance", Search: terms})
	var semanticErr *serrors.Error
	if !errors.As(err, &semanticErr) || semanticErr.Code != serrors.ErrInvalidQuery {
		t.Fatalf("search bound error = %#v", err)
	}
}

func TestAgentSemanticListMetricsRanksSpecificMultiTermMatchFirst(t *testing.T) {
	doc, err := ossie.NewLoader().Load(fixtures.CustomOffsetToGrainDenseModelYAML)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := manifest.BuildProjectManifest("finance", doc)
	if err != nil {
		t.Fatal(err)
	}
	surface := service.NewAgentSemanticService(service.NewDiscoveryService(manifest.NewStore(snapshot))).WithProjectAuthorizer(service.AllAccessProjectAuthorizer{})
	metrics, err := surface.ListMetrics(context.Background(), service.ListMetricsRequest{
		ProjectID: "finance",
		Search:    []string{"revenue", "fiscal year", "start"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(metrics.Metrics) < 2 || metrics.Metrics[0].Ref != "metric:custom_offset_to_grain_dense.revenue_at_fiscal_year_start" {
		t.Fatalf("ranked metrics = %#v", metrics.Metrics)
	}
}

func TestAgentSemanticListModelsScoresMultiTermMatchWithinOneAsset(t *testing.T) {
	loader := ossie.NewLoader()
	customOffset, err := loader.Load(fixtures.CustomOffsetToGrainDenseModelYAML)
	if err != nil {
		t.Fatal(err)
	}
	fiscalDense, err := loader.Load(fixtures.CustomCalendarDenseModelYAML)
	if err != nil {
		t.Fatal(err)
	}
	customOffset.SemanticModel = append(customOffset.SemanticModel, fiscalDense.SemanticModel...)
	snapshot, err := manifest.BuildProjectManifest("finance", customOffset)
	if err != nil {
		t.Fatal(err)
	}
	surface := service.NewAgentSemanticService(service.NewDiscoveryService(manifest.NewStore(snapshot))).WithProjectAuthorizer(service.AllAccessProjectAuthorizer{})
	models, err := surface.ListModels(context.Background(), service.ListModelsRequest{
		ProjectID: "finance",
		Search:    []string{"revenue", "fiscal"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(models.Models) < 2 || models.Models[0].Ref != "model:custom_offset_to_grain_dense" {
		t.Fatalf("ranked models = %#v", models.Models)
	}
}

func TestAgentSemanticProgressiveListAndDetailContracts(t *testing.T) {
	doc, err := ossie.NewLoader().Load(fixtures.ConversionModelYAML)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := manifest.BuildProjectManifest(fixtures.ConformanceProject, doc)
	if err != nil {
		t.Fatal(err)
	}
	surface := service.NewAgentSemanticService(service.NewDiscoveryService(manifest.NewStore(snapshot))).WithProjectAuthorizer(service.AllAccessProjectAuthorizer{})
	limit := 1
	metrics, err := surface.ListMetrics(context.Background(), service.ListMetricsRequest{ProjectID: fixtures.ConformanceProject, Limit: &limit})
	if err != nil {
		t.Fatal(err)
	}
	if len(metrics.Metrics) != 1 || !metrics.Truncated {
		t.Fatalf("bounded metrics = %#v", metrics)
	}
	if !metrics.DetailsOmitted || metrics.Metrics[0].Description != "" {
		t.Fatalf("truncated metric list retained detail = %#v", metrics.Metrics[0])
	}
	model, err := surface.GetModel(context.Background(), service.AgentGetModelRequest{ProjectID: fixtures.ConformanceProject, Ref: "model:conversion"})
	if err != nil || len(model.Model.Datasets) == 0 || len(model.Model.Metrics) == 0 || len(model.Model.Dimensions) == 0 {
		t.Fatalf("model inventory = %#v, err = %v", model, err)
	}
	detail, err := surface.GetMetric(context.Background(), service.AgentGetMetricRequest{ProjectID: fixtures.ConformanceProject, Ref: "metric:conversion.signup_to_purchase_rate"})
	if err != nil || detail.Metric.Ref != "metric:conversion.signup_to_purchase_rate" {
		t.Fatalf("metric detail = %#v, err = %v", detail, err)
	}
	if len(detail.Metric.SemanticConstraints) != 1 || detail.Metric.SemanticConstraints[0].Kind != service.MetricConstraintConversion || detail.Metric.SemanticConstraints[0].Conversion == nil {
		t.Fatalf("metric semantic constraints = %#v", detail.Metric.SemanticConstraints)
	}
	if !slices.Equal(detail.Metric.SemanticKinds, []service.MetricSemanticConstraintKind{service.MetricConstraintConversion}) {
		t.Fatalf("metric semantic kinds = %#v", detail.Metric.SemanticKinds)
	}
	focused, err := surface.ListMetrics(context.Background(), service.ListMetricsRequest{ProjectID: fixtures.ConformanceProject, Search: []string{"signup_to_purchase_rate"}})
	if err != nil || len(focused.Metrics) != 1 || !slices.Equal(focused.Metrics[0].SemanticKinds, []service.MetricSemanticConstraintKind{service.MetricConstraintConversion}) {
		t.Fatalf("focused metric semantic kinds = %#v, err = %v", focused, err)
	}
	if len(focused.Metrics[0].SemanticConstraints) != 1 || focused.Metrics[0].SemanticConstraints[0].Conversion == nil {
		t.Fatalf("focused metric selection constraints = %#v", focused.Metrics[0].SemanticConstraints)
	}
	invalid := 51
	_, err = surface.ListModels(context.Background(), service.ListModelsRequest{ProjectID: fixtures.ConformanceProject, Limit: &invalid})
	var semanticErr *serrors.Error
	if !errors.As(err, &semanticErr) || semanticErr.Code != serrors.ErrInvalidQuery {
		t.Fatalf("invalid list limit error = %#v", err)
	}
}

func TestAgentSemanticFocusedMetricsExposeSemiAdditiveSelectionDifferences(t *testing.T) {
	doc, err := ossie.NewLoader().Load(fixtures.SemiAdditiveEdgesModelYAML)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := manifest.BuildProjectManifest(fixtures.ConformanceProject, doc)
	if err != nil {
		t.Fatal(err)
	}
	surface := service.NewAgentSemanticService(service.NewDiscoveryService(manifest.NewStore(snapshot))).WithProjectAuthorizer(service.AllAccessProjectAuthorizer{})
	limit := 2
	metrics, err := surface.ListMetrics(context.Background(), service.ListMetricsRequest{
		ProjectID: fixtures.ConformanceProject,
		Search:    []string{"inventory_balance", "inventory_window_sum"},
		Limit:     &limit,
	})
	if err != nil {
		t.Fatal(err)
	}
	byName := make(map[string]service.AgentMetricSummary, len(metrics.Metrics))
	for _, metric := range metrics.Metrics {
		byName[metric.Name] = metric
	}
	plain := byName["inventory_balance"]
	windowed := byName["inventory_window_sum"]
	if len(plain.SemanticConstraints) != 1 || plain.SemanticConstraints[0].SemiAdditive == nil {
		t.Fatalf("plain inventory constraints = %#v", plain.SemanticConstraints)
	}
	if len(windowed.SemanticConstraints) != 1 || windowed.SemanticConstraints[0].SemiAdditive == nil {
		t.Fatalf("windowed inventory constraints = %#v", windowed.SemanticConstraints)
	}
	if got := plain.SemanticConstraints[0].SemiAdditive.WindowGroupings; len(got) != 0 {
		t.Fatalf("plain inventory window groupings = %v, want none", got)
	}
	semi := windowed.SemanticConstraints[0].SemiAdditive
	if !slices.Equal(semi.WindowGroupings, []string{"warehouse"}) || semi.RollupAggregation != "sum" {
		t.Fatalf("windowed inventory selection evidence = %#v", semi)
	}
	if !metrics.SelectionRequired || !slices.Contains(metrics.SelectionDifferences, service.AgentMetricSelectionName) || !slices.Contains(metrics.SelectionDifferences, service.AgentMetricSelectionSemanticConstraints) {
		t.Fatalf("semi-additive candidate differences = %#v", metrics.SelectionDifferences)
	}
	byGrouping, err := surface.ListMetrics(context.Background(), service.ListMetricsRequest{
		ProjectID: fixtures.ConformanceProject,
		Search:    []string{"warehouse"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(byGrouping.Metrics) != 3 {
		t.Fatalf("warehouse-scoped metrics = %#v, want three governed rollups", byGrouping.Metrics)
	}
	for _, metric := range byGrouping.Metrics {
		if len(metric.SemanticConstraints) != 1 || metric.SemanticConstraints[0].SemiAdditive == nil || !slices.Equal(metric.SemanticConstraints[0].SemiAdditive.WindowGroupings, []string{"warehouse"}) {
			t.Fatalf("warehouse search returned unrelated metric = %#v", metric)
		}
	}
}

func TestAgentSemanticConstraintMatchedMetricsRetainSelectionEvidenceOnLargePages(t *testing.T) {
	loader := ossie.NewLoader()
	var combined *ossie.Document
	for i := range 3 {
		body := []byte(strings.Replace(string(fixtures.SemiAdditiveEdgesModelYAML), "name: semi_additive_edges", "name: semi_additive_edges_"+strconv.Itoa(i), 1))
		doc, err := loader.Load(body)
		if err != nil {
			t.Fatal(err)
		}
		if combined == nil {
			combined = doc
			continue
		}
		combined.SemanticModel = append(combined.SemanticModel, doc.SemanticModel...)
	}
	snapshot, err := manifest.BuildProjectManifest(fixtures.ConformanceProject, combined)
	if err != nil {
		t.Fatal(err)
	}
	surface := service.NewAgentSemanticService(service.NewDiscoveryService(manifest.NewStore(snapshot))).WithProjectAuthorizer(service.AllAccessProjectAuthorizer{})
	metrics, err := surface.ListMetrics(context.Background(), service.ListMetricsRequest{
		ProjectID: fixtures.ConformanceProject,
		Search:    []string{"warehouse"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !metrics.Truncated || len(metrics.Metrics) == 0 {
		t.Fatalf("warehouse page did not preserve its large-result boundary: %#v", metrics)
	}
	for _, metric := range metrics.Metrics {
		if len(metric.SemanticConstraints) != 1 || metric.SemanticConstraints[0].SemiAdditive == nil || !slices.Equal(metric.SemanticConstraints[0].SemiAdditive.WindowGroupings, []string{"warehouse"}) {
			t.Fatalf("constraint-matched metric omitted selection evidence: %#v", metric)
		}
	}
}

func TestAgentSemanticListMetricsMarksExactCrossModelNamesAsSelectionRequired(t *testing.T) {
	loader := ossie.NewLoader()
	first, err := loader.Load(fixtures.SemiAdditiveEdgesModelYAML)
	if err != nil {
		t.Fatal(err)
	}
	second, err := loader.Load([]byte(strings.Replace(string(fixtures.SemiAdditiveEdgesModelYAML), "name: semi_additive_edges", "name: another_inventory_model", 1)))
	if err != nil {
		t.Fatal(err)
	}
	first.SemanticModel = append(first.SemanticModel, second.SemanticModel...)
	snapshot, err := manifest.BuildProjectManifest(fixtures.ConformanceProject, first)
	if err != nil {
		t.Fatal(err)
	}
	surface := service.NewAgentSemanticService(service.NewDiscoveryService(manifest.NewStore(snapshot))).WithProjectAuthorizer(service.AllAccessProjectAuthorizer{})
	metrics, err := surface.ListMetrics(context.Background(), service.ListMetricsRequest{
		ProjectID: fixtures.ConformanceProject,
		Search:    []string{"inventory_balance"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !metrics.SelectionRequired {
		t.Fatalf("multi-candidate metric lookup did not require selection: %#v", metrics)
	}
	if !slices.Contains(metrics.SelectionDifferences, service.AgentMetricSelectionModel) {
		t.Fatalf("cross-model selection differences = %#v", metrics.SelectionDifferences)
	}
	if got, want := metrics.AmbiguousNames, []service.AgentMetricNameAmbiguity{{
		Name: "inventory_balance",
		Refs: []string{"metric:another_inventory_model.inventory_balance", "metric:semi_additive_edges.inventory_balance"},
	}}; !slices.EqualFunc(got, want, func(left, right service.AgentMetricNameAmbiguity) bool {
		return left.Name == right.Name && slices.Equal(left.Refs, right.Refs)
	}) {
		t.Fatalf("metric-name ambiguity = %#v, want %#v", got, want)
	}
}

func TestAgentSemanticListMetricsUsesOnlyExplicitAuthoredAliasesAsStrongMatches(t *testing.T) {
	const model = `
version: "0.2.0.dev0"
semantic_model:
  - name: governed_sales
    datasets:
      - name: orders
        source: analytics.orders
        fields:
          - {name: amount, datatype: Decimal, expression: {dialects: [{dialect: ANSI_SQL, expression: amount}]}}
    metrics:
      - name: gross_merchandise_value
        datatype: Decimal
        expression: {dialects: [{dialect: ANSI_SQL, expression: "SUM(amount)"}]}
        custom_extensions:
          - vendor_name: METIS
            data: '{"kind":"agent_discovery","aliases":["GMV","merchandise sales"]}'
      - name: gmv_context_only
        description: GMV appears here only as descriptive context.
        datatype: Decimal
        expression: {dialects: [{dialect: ANSI_SQL, expression: "MAX(amount)"}]}
`
	doc, err := ossie.NewLoader().Load([]byte(model))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := manifest.BuildProjectManifest("finance", doc)
	if err != nil {
		t.Fatal(err)
	}
	surface := service.NewAgentSemanticService(service.NewDiscoveryService(manifest.NewStore(snapshot))).WithProjectAuthorizer(service.AllAccessProjectAuthorizer{})
	metrics, err := surface.ListMetrics(context.Background(), service.ListMetricsRequest{ProjectID: "finance", Search: []string{"GMV"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(metrics.Metrics) != 2 || metrics.Metrics[0].Ref != "metric:governed_sales.gross_merchandise_value" {
		t.Fatalf("authored alias ranking = %#v", metrics.Metrics)
	}
	if got, want := metrics.Metrics[0].Aliases, []string{"GMV", "merchandise sales"}; !slices.Equal(got, want) {
		t.Fatalf("authored aliases = %v, want %v", got, want)
	}
	if !slices.Contains(metrics.SelectionDifferences, service.AgentMetricSelectionAliases) {
		t.Fatalf("alias selection difference = %#v", metrics.SelectionDifferences)
	}
}

func TestAgentSemanticListMetricsRejectsMalformedAuthoredAliases(t *testing.T) {
	const model = `
version: "0.2.0.dev0"
semantic_model:
  - name: governed_sales
    datasets:
      - name: orders
        source: analytics.orders
        fields:
          - {name: amount, datatype: Decimal, expression: {dialects: [{dialect: ANSI_SQL, expression: amount}]}}
    metrics:
      - name: revenue
        datatype: Decimal
        expression: {dialects: [{dialect: ANSI_SQL, expression: "SUM(amount)"}]}
        custom_extensions:
          - vendor_name: METIS
            data: '{"kind":"agent_discovery","aliases":"revenue total"}'
`
	_, err := ossie.NewLoader().Load([]byte(model))
	var semanticErr *serrors.Error
	if !errors.As(err, &semanticErr) || semanticErr.Code != serrors.ErrInvalidModel {
		t.Fatalf("malformed alias error = %#v", err)
	}
}

func TestAgentSemanticCompileRejectsUnqualifiedMetricSelection(t *testing.T) {
	surface := service.NewAgentSemanticService(newService(t))
	_, err := surface.BuildCompileRequest(service.AgentCompileRequest{
		ProjectID:     "finance",
		Dialect:       "duckdb",
		OutputMetrics: []string{"total_revenue"},
	})
	var semanticErr *serrors.Error
	if !errors.As(err, &semanticErr) || semanticErr.Code != serrors.ErrInvalidQuery || semanticErr.Message != "selected metrics must use canonical metric refs" {
		t.Fatalf("unqualified metric error = %#v", err)
	}
}

func TestAgentSemanticBroadMetricIdentityListHonorsEncodedBudget(t *testing.T) {
	var model strings.Builder
	model.WriteString(`version: "0.2.0.dev0"
semantic_model:
  - name: many_metrics
    datasets:
      - name: facts
        source: analytics.facts
        fields:
          - {name: value, datatype: Decimal, expression: {dialects: [{dialect: ANSI_SQL, expression: value}]}}
    metrics:
`)
	for i := 0; i < 40; i++ {
		model.WriteString("      - name: metric_" + strconv.Itoa(i) + "_with_a_descriptive_identity\n")
		model.WriteString("        datatype: Decimal\n")
		model.WriteString("        expression: {dialects: [{dialect: ANSI_SQL, expression: SUM(value)}]}\n")
	}
	doc, err := ossie.NewLoader().Load([]byte(model.String()))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := manifest.BuildProjectManifest("finance", doc)
	if err != nil {
		t.Fatal(err)
	}
	surface := service.NewAgentSemanticService(service.NewDiscoveryService(manifest.NewStore(snapshot))).WithProjectAuthorizer(service.AllAccessProjectAuthorizer{})
	limit := 50
	result, err := surface.ListMetrics(context.Background(), service.ListMetricsRequest{ProjectID: "finance", Limit: &limit})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Truncated || !result.DetailsOmitted || len(result.Metrics) >= 40 {
		t.Fatalf("budgeted broad metrics = %#v", result)
	}
}

func TestAgentSemanticListMetricsScopesAcrossSelectedModels(t *testing.T) {
	loader := ossie.NewLoader()
	commerce, err := loader.Load(fixtures.CommerceModelYAML)
	if err != nil {
		t.Fatal(err)
	}
	conversion, err := loader.Load(fixtures.ConversionModelYAML)
	if err != nil {
		t.Fatal(err)
	}
	distinct, err := loader.Load(fixtures.DistinctValuesModelYAML)
	if err != nil {
		t.Fatal(err)
	}
	commerce.SemanticModel = append(commerce.SemanticModel, conversion.SemanticModel...)
	commerce.SemanticModel = append(commerce.SemanticModel, distinct.SemanticModel...)
	snapshot, err := manifest.BuildProjectManifest(fixtures.ConformanceProject, commerce)
	if err != nil {
		t.Fatal(err)
	}
	surface := service.NewAgentSemanticService(service.NewDiscoveryService(manifest.NewStore(snapshot))).WithProjectAuthorizer(service.AllAccessProjectAuthorizer{})
	limit := 50
	unscoped, err := surface.ListMetrics(context.Background(), service.ListMetricsRequest{
		ProjectID: fixtures.ConformanceProject,
		Search:    []string{"revenue"},
		Limit:     &limit,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, metric := range unscoped.Metrics {
		if len(metric.Dimensions) != 0 {
			t.Fatalf("unscoped metric %s fanned out dimensions: %#v", metric.Ref, metric.Dimensions)
		}
	}
	result, err := surface.ListMetrics(context.Background(), service.ListMetricsRequest{
		ProjectID: fixtures.ConformanceProject,
		Models:    []string{"model:commerce", "model:conversion", "model:commerce"},
		Limit:     &limit,
	})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, metric := range result.Metrics {
		if metric.Model == "distinct_values" {
			t.Fatalf("unselected model leaked metric %#v", metric)
		}
		if len(metric.Dimensions) != 0 {
			t.Fatalf("multi-model metric %s fanned out dimensions: %#v", metric.Ref, metric.Dimensions)
		}
		seen[metric.Model] = true
	}
	if !seen["commerce"] || !seen["conversion"] {
		t.Fatalf("selected model metrics = %#v", result.Metrics)
	}
}

func TestAgentSemanticMetricSearchDoesNotFanOutModelContext(t *testing.T) {
	surface := service.NewAgentSemanticService(newService(t))
	metrics, err := surface.ListMetrics(context.Background(), service.ListMetricsRequest{ProjectID: "finance", Search: []string{"analytics"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(metrics.Metrics) != 0 {
		t.Fatalf("model-description search fanned out metrics = %#v", metrics.Metrics)
	}
}

func TestAgentSemanticListProjectsFiltersCanonicalIDs(t *testing.T) {
	surface := service.NewAgentSemanticService(newService(t))
	result, err := surface.ListProjects(context.Background(), service.ListProjectsRequest{ProjectIDs: []string{"missing", "finance", "finance"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Projects) != 1 || result.Projects[0].ProjectID != "finance" {
		t.Fatalf("projects=%#v", result.Projects)
	}
}

func TestAgentSemanticDimensionsRejectMultipleAnchors(t *testing.T) {
	surface := service.NewAgentSemanticService(newService(t))
	_, err := surface.GetDimensions(context.Background(), service.GetDimensionsRequest{
		ProjectID: "finance",
		Model:     "model:sales",
		Metrics:   []string{"metric:sales.total_revenue"},
	})
	var semanticErr *serrors.Error
	if !errors.As(err, &semanticErr) || semanticErr.Code != serrors.ErrInvalidQuery {
		t.Fatalf("error = %#v", err)
	}
}

func TestAgentSemanticProjectAuthorizationFailsClosed(t *testing.T) {
	surface := service.NewAgentSemanticService(newService(t)).WithProjectAuthorizer(service.ProjectAuthorizerFunc(func(context.Context, service.ProjectAuthorizationRequest) service.ProjectAuthorizationDecision {
		return service.ProjectAuthorizationDecision{Effect: service.ProjectAuthorizationDeny, Reason: service.ProjectAuthorizationReasonPolicyDenied}
	}))
	projects, err := surface.ListProjects(context.Background(), service.ListProjectsRequest{})
	if err != nil || len(projects.Projects) != 0 {
		t.Fatalf("projects=%#v err=%v", projects, err)
	}
	_, err = surface.ListMetrics(context.Background(), service.ListMetricsRequest{ProjectID: "finance"})
	var semanticErr *serrors.Error
	if !errors.As(err, &semanticErr) || semanticErr.Code != serrors.ErrProjectAccessDenied {
		t.Fatalf("err=%#v", err)
	}
}

func TestAgentSemanticCapabilitiesIntersectRuntimeAndAuthorizedActions(t *testing.T) {
	authorizer := service.ProjectAuthorizerFunc(func(_ context.Context, req service.ProjectAuthorizationRequest) service.ProjectAuthorizationDecision {
		if req.ProjectID == "finance" && (req.Action == service.ProjectActionDiscover || req.Action == service.ProjectActionCompile) {
			return service.ProjectAuthorizationDecision{Effect: service.ProjectAuthorizationAllow, Reason: service.ProjectAuthorizationReasonScopeGranted}
		}
		return service.ProjectAuthorizationDecision{Effect: service.ProjectAuthorizationDeny, Reason: service.ProjectAuthorizationReasonPolicyDenied}
	})
	surface := service.NewAgentSemanticService(newService(t), projectCapabilityProvider{
		"finance": {service.AgentCapabilityCompileSQL, service.AgentCapabilityQueryMetrics},
	}).WithProjectAuthorizer(authorizer)
	projects, err := surface.ListProjects(context.Background(), service.ListProjectsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(projects.Projects) != 1 || !slices.Equal(projects.Projects[0].Capabilities, []service.AgentProjectCapability{service.AgentCapabilityCompileSQL}) {
		t.Fatalf("projects = %#v", projects.Projects)
	}
}
