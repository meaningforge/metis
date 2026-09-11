package agentcontext_test

import (
	"context"
	"strings"
	"testing"

	service "github.com/meaningforge/metis/app/service/semantic"
	"github.com/meaningforge/metis/compiler/artifact"
	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/query"
)

func TestAgentProjectMetricDimensionCompileJourney(t *testing.T) {
	runtime := runtimeForFixture(t, "commerce.ossie.yaml")
	discovery := service.NewDiscoveryService(manifest.NewStore(runtime.SemanticManifest)).WithProjectAuthorizer(service.AllAccessProjectAuthorizer{})
	surface := service.NewAgentSemanticService(discovery)

	projects, err := surface.ListProjects(context.Background(), service.ListProjectsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(projects.Projects) == 0 || projects.Projects[0].Name != conformanceProject {
		t.Fatalf("projects=%#v", projects)
	}

	metrics, err := surface.ListMetrics(context.Background(), service.ListMetricsRequest{
		ProjectID: conformanceProject,
		Search:    []string{"revenue"},
	})
	if err != nil {
		t.Fatal(err)
	}
	metricRef := findMetricRef(metrics.Metrics, "revenue")
	if metricRef == "" {
		t.Fatalf("metrics=%#v", metrics)
	}

	dimensions, err := surface.GetDimensions(context.Background(), service.GetDimensionsRequest{
		ProjectID: conformanceProject,
		Metrics:   []string{metricRef},
		Search:    []string{"region"},
	})
	if err != nil {
		t.Fatal(err)
	}
	dimensionRef := findDimensionRef(dimensions.Dimensions, "customer.region")
	if dimensionRef == "" {
		t.Fatalf("dimensions=%#v", dimensions)
	}

	compileRequest, err := surface.BuildCompileRequest(service.AgentCompileRequest{
		ProjectID:     conformanceProject,
		Dialect:       "DORIS",
		OutputMetrics: []string{metricRef},
		GroupBy:       []service.AgentGroupByParam{{Name: dimensionRef, Type: service.AgentGroupByDimension}},
	})
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := runtime.Compile.Compile(context.Background(), compileRequest)
	if err != nil {
		t.Fatal(err)
	}
	physical := compiled.PhysicalQuery
	if physical.SQL == "" || physical.Dialect != "DORIS" {
		t.Fatalf("physical query=%#v", compiled.PhysicalQuery)
	}
}

func TestExplorationQuestionUsesMetricListing(t *testing.T) {
	runtime := runtimeForFixture(t, "commerce.ossie.yaml")
	discovery := service.NewDiscoveryService(manifest.NewStore(runtime.SemanticManifest)).WithProjectAuthorizer(service.AllAccessProjectAuthorizer{})
	metrics, err := service.NewAgentSemanticService(discovery).ListMetrics(context.Background(), service.ListMetricsRequest{
		ProjectID: conformanceProject,
		Search:    []string{"revenue"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(metrics.Metrics) == 0 || findMetricRef(metrics.Metrics, "revenue") == "" {
		t.Fatalf("metrics=%#v", metrics)
	}
}

func TestMetricFilterDoesNotProjectFilterOnlyMetric(t *testing.T) {
	runtime := runtimeForFixture(t, "commerce.ossie.yaml")
	surface := service.NewAgentSemanticService(service.NewDiscoveryService(manifest.NewStore(runtime.SemanticManifest))).WithProjectAuthorizer(service.AllAccessProjectAuthorizer{})
	req, err := surface.BuildCompileRequest(service.AgentCompileRequest{
		ProjectID:     conformanceProject,
		Dialect:       "DORIS",
		OutputMetrics: []string{"metric:commerce.revenue"},
		GroupBy: []service.AgentGroupByParam{{
			Name: "dimension:commerce.orders.status",
			Type: service.AgentGroupByDimension,
		}},
		Filters: []query.Filter{{
			Field: "metric:commerce.contribution_margin", Operator: query.FilterBetween, Value: []int{100, 10000},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := runtime.Compile.Compile(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if got := outputColumnNames(compiled.OutputSchema.Columns); !slicesEqual(got, []string{"orders.status", "revenue"}) {
		t.Fatalf("output columns = %v, want status and revenue only", got)
	}
}

func TestMetricFreeTemporalJourneyUsesModelScopedDimensions(t *testing.T) {
	runtime := runtimeForFixture(t, "temporal_relationship.ossie.yaml")
	surface := service.NewAgentSemanticService(service.NewDiscoveryService(manifest.NewStore(runtime.SemanticManifest))).WithProjectAuthorizer(service.AllAccessProjectAuthorizer{})
	models, err := surface.ListModels(context.Background(), service.ListModelsRequest{ProjectID: conformanceProject})
	if err != nil {
		t.Fatal(err)
	}
	modelRef := ""
	for _, model := range models.Models {
		if model.Name == "temporal_relationship" {
			modelRef = model.Ref
		}
	}
	if modelRef != "model:temporal_relationship" {
		t.Fatalf("temporal model ref = %q", modelRef)
	}
	discovered, err := surface.GetDimensions(context.Background(), service.GetDimensionsRequest{
		ProjectID: conformanceProject,
		Model:     modelRef,
		Search:    []string{"order_id", "customer_tier"},
	})
	if err != nil {
		t.Fatal(err)
	}
	orderID := findDimensionRef(discovered.Dimensions, "orders.order_id")
	customerTier := findDimensionRef(discovered.Dimensions, "customer_history.customer_tier")
	if orderID == "" || customerTier == "" {
		t.Fatalf("metric-free dimensions = %#v", discovered.Dimensions)
	}
	relationships, err := surface.GetRelationships(context.Background(), service.AgentGetRelationshipsRequest{
		ProjectID: conformanceProject, Model: modelRef, Datasets: []string{"orders", "customer_history"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(relationships.Relationships) != 1 || len(relationships.Relationships[0].FromColumns) == 0 || len(relationships.Relationships[0].ToColumns) == 0 || len(relationships.Relationships[0].SemanticEvidence) != 1 || relationships.Relationships[0].SemanticEvidence[0].Effect != service.AgentSemanticEffectPointInTime {
		t.Fatalf("relationship evidence = %#v", relationships.Relationships)
	}
	req, err := surface.BuildCompileRequest(service.AgentCompileRequest{
		ProjectID: conformanceProject,
		Dialect:   "DORIS",
		Model:     modelRef,
		GroupBy: []service.AgentGroupByParam{
			{Name: orderID, Type: service.AgentGroupByDimension},
			{Name: customerTier, Type: service.AgentGroupByDimension},
		},
		Filters: []query.Filter{{Field: orderID, Operator: query.FilterEQ, Value: "o_current"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if req.Query.Model != "temporal_relationship" || len(req.Query.Metrics) != 0 {
		t.Fatalf("metric-free compile request = %#v", req.Query)
	}
	compiled, err := runtime.Compile.Compile(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if got := outputColumnNames(compiled.OutputSchema.Columns); !slicesEqual(got, []string{"orders.order_id", "customer_history.customer_tier"}) {
		t.Fatalf("output columns = %v", got)
	}
	physical := compiled.PhysicalQuery
	if !strings.Contains(physical.SQL, "valid_from") || !strings.Contains(physical.SQL, "valid_to") {
		t.Fatalf("temporal physical query = %#v", compiled.PhysicalQuery)
	}
}

func TestMetricFreeDiscoveryExposesAuthoredValueEncoding(t *testing.T) {
	runtime := runtimeForFixture(t, "distinct_values.ossie.yaml")
	surface := service.NewAgentSemanticService(service.NewDiscoveryService(manifest.NewStore(runtime.SemanticManifest))).WithProjectAuthorizer(service.AllAccessProjectAuthorizer{})
	discovered, err := surface.GetDimensions(context.Background(), service.GetDimensionsRequest{
		ProjectID: conformanceProject,
		Model:     "model:distinct_values",
		Search:    []string{"country"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(discovered.Dimensions) != 1 || discovered.Dimensions[0].Name != "orders.country" {
		t.Fatalf("country value-encoding discovery = %#v", discovered.Dimensions)
	}
	detail, err := surface.GetDimension(context.Background(), service.AgentGetDimensionRequest{ProjectID: conformanceProject, Ref: discovered.Dimensions[0].Ref})
	if err != nil || !strings.Contains(detail.Dimension.Description, "Japan is JP") {
		t.Fatalf("country value-encoding detail = %#v, err = %v", detail, err)
	}
}

func outputColumnNames(columns []artifact.OutputColumn) []string {
	out := make([]string, 0, len(columns))
	for _, column := range columns {
		out = append(out, column.Name)
	}
	return out
}

func slicesEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func findMetricRef(metrics []service.AgentMetricSummary, name string) string {
	for _, metric := range metrics {
		if metric.Name == name {
			return metric.Ref
		}
	}
	return ""
}

func findDimensionRef(dimensions []service.AgentDimensionSummary, name string) string {
	for _, dimension := range dimensions {
		if dimension.Name == name {
			return dimension.Ref
		}
	}
	return ""
}
