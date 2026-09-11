//go:build duckdb

package command

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	appmcp "github.com/meaningforge/metis/app/mcp"
	service "github.com/meaningforge/metis/app/service/semantic"
	duckdbfixture "github.com/meaningforge/metis/cmd/s2sbench/bench/duckdbfixture"
	"github.com/meaningforge/metis/cmd/s2sbench/bench/fixtures"
	"github.com/meaningforge/metis/cmd/s2sbench/bench/runner/readiness"
	"github.com/meaningforge/metis/cmd/s2sbench/bench/scenarios"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestExecutableSemanticProjectRuntimeReturnsCanonicalDimensionValues(t *testing.T) {
	ctx := context.Background()
	database := filepath.Join(t.TempDir(), "s2sbench-values.duckdb")
	physical, err := duckdbfixture.New(database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = physical.Close(context.Background()) })
	if err := physical.PrepareFixture(ctx, fixtures.DistinctValues); err != nil {
		t.Fatal(err)
	}

	runtime, err := newExecutableSemanticProjectRuntime(t.TempDir(), 0, canonicalModels(t), database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	endpoint, _, environment, err := runtime.CurrentEndpoint()
	if err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "s2sbench-dimension-values", Version: "1"}, nil)
	transport := &mcp.StreamableClientTransport{
		Endpoint: endpoint.URL,
		HTTPClient: &http.Client{Transport: s2sbenchBearerRoundTripper{
			base: http.DefaultTransport, token: environment[s2sbenchMCPTokenEnv],
		}},
		DisableStandaloneSSE: true,
	}
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: appmcp.ToolGetDimensionValues,
		Arguments: map[string]any{
			"dimension": "dimension:distinct_values.orders.country",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("get_dimension_values returned error: %#v", result.Content)
	}
	payload, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(payload), `"JP"`) {
		t.Fatalf("get_dimension_values payload = %s, want JP", payload)
	}

	execution := &metisExecution{duckdb: physical, runtime: runtime}
	next, ok := scenarios.ByName("aggregation_variants")
	if !ok {
		t.Fatal("aggregation_variants scenario is unavailable")
	}
	if err := execution.Prepare(ctx, next); err != nil {
		t.Fatalf("prepare next fixture after runtime-backed dimension discovery: %v", err)
	}
}

func TestExecutableSemanticProjectRuntimeExposesAllToolsAndCapturesExecutedQuery(t *testing.T) {
	ctx := context.Background()
	database := filepath.Join(t.TempDir(), "s2sbench-query.duckdb")
	physical, err := duckdbfixture.New(database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = physical.Close(context.Background()) })
	scenario, ok := scenarios.ByName("aggregation_variants")
	if !ok {
		t.Fatal("aggregation_variants scenario is unavailable")
	}
	if err := physical.Prepare(ctx, scenario); err != nil {
		t.Fatal(err)
	}

	runtime, err := newExecutableSemanticProjectRuntime(t.TempDir(), 0, canonicalModels(t), database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	endpoint, _, environment, err := runtime.CurrentEndpoint()
	if err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "s2sbench-query-capture", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint: endpoint.URL,
		HTTPClient: &http.Client{Transport: s2sbenchBearerRoundTripper{
			base: http.DefaultTransport, token: environment[s2sbenchMCPTokenEnv],
		}},
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantTools := map[string]bool{
		appmcp.ToolQueryMetrics: true, appmcp.ToolCompile: true,
		appmcp.ToolGetDimensionValues: true, appmcp.ToolAttributeMetric: true,
		appmcp.ToolCompareMetrics: true,
	}
	for _, tool := range tools.Tools {
		delete(wantTools, tool.Name)
	}
	if len(wantTools) != 0 {
		t.Fatalf("executable runtime omitted configured tools: %v", wantTools)
	}

	toolResult, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: appmcp.ToolQueryMetrics,
		Arguments: map[string]any{"output_metrics": []string{
			"metric:commerce.average_order_amount",
			"metric:commerce.minimum_order_amount",
			"metric:commerce.maximum_order_amount",
		}},
	})
	if err != nil || toolResult.IsError {
		t.Fatalf("query_metrics result=%#v error=%v", toolResult, err)
	}
	body, err := json.Marshal(toolResult.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var result service.QueryMetricsResult
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}
	if result.QueryID == "" || result.Count != 1 {
		t.Fatalf("query_metrics payload = %s", body)
	}
	record, err := runtime.captures.decorateAttempt(readiness.AttemptRecord{QueryEvidence: &readiness.QueryEvidence{QueryID: result.QueryID}})
	if err != nil {
		t.Fatal(err)
	}
	if record.ExecutedQuery == nil || record.ExecutedQuery.PhysicalQuery.SQL == "" || len(record.ExecutedQuery.OutputSchema.Columns) != 3 {
		t.Fatalf("executed query evidence = %#v", record.ExecutedQuery)
	}
}
