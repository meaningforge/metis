package agentcontext_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/meaningforge/metis/app/mcp"
	"github.com/meaningforge/metis/app/rest"
	service "github.com/meaningforge/metis/app/service/semantic"
	"github.com/meaningforge/metis/compiler/artifact"
	"github.com/meaningforge/metis/execution/backend"
	"github.com/meaningforge/metis/execution/datasource"
	"github.com/meaningforge/metis/execution/driver"
	"github.com/meaningforge/metis/execution/runner"
	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/renderer"
	"github.com/meaningforge/metis/renderer/builtin"
)

func TestMetricListingServiceMCPParity(t *testing.T) {
	runtime := runtimeForFixture(t, "commerce.ossie.yaml")
	discovery := service.NewDiscoveryService(manifest.NewStore(runtime.SemanticManifest)).WithProjectAuthorizer(service.AllAccessProjectAuthorizer{})
	req := service.ListMetricsRequest{ProjectID: conformanceProject, Search: []string{"revenue"}}

	expected, err := service.NewAgentSemanticService(discovery).ListMetrics(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	mcpPayload := mcpToolPayload(t, discovery, runtime.Compile, mcp.ToolListMetrics, req)
	assertPayloadEqual(t, expected, mcpPayload)
}

func TestCompatibleDimensionsServiceMCPParity(t *testing.T) {
	runtime := runtimeForFixture(t, "commerce.ossie.yaml")
	discovery := service.NewDiscoveryService(manifest.NewStore(runtime.SemanticManifest)).WithProjectAuthorizer(service.AllAccessProjectAuthorizer{})
	req := service.GetDimensionsRequest{
		ProjectID: conformanceProject,
		Metrics:   []string{"metric:commerce.revenue"},
		Search:    []string{"region"},
	}

	expected, err := service.NewAgentSemanticService(discovery).GetDimensions(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	mcpPayload := mcpToolPayload(t, discovery, runtime.Compile, mcp.ToolGetDimensions, req)
	assertPayloadEqual(t, expected, mcpPayload)
}

func TestMetricFreeDimensionsServiceMCPParity(t *testing.T) {
	runtime := runtimeForFixture(t, "temporal_relationship.ossie.yaml")
	discovery := service.NewDiscoveryService(manifest.NewStore(runtime.SemanticManifest)).WithProjectAuthorizer(service.AllAccessProjectAuthorizer{})
	req := service.GetDimensionsRequest{
		ProjectID: conformanceProject,
		Model:     "model:temporal_relationship",
		Search:    []string{"order_id", "customer_tier"},
	}
	expected, err := service.NewAgentSemanticService(discovery).GetDimensions(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	mcpPayload := mcpToolPayload(t, discovery, runtime.Compile, mcp.ToolGetDimensions, req)
	assertPayloadEqual(t, expected, mcpPayload)
}

func TestCanonicalCompileRESTAndMCPParity(t *testing.T) {
	runtime := runtimeForFixture(t, "commerce.ossie.yaml")
	discovery := service.NewDiscoveryService(manifest.NewStore(runtime.SemanticManifest)).WithProjectAuthorizer(service.AllAccessProjectAuthorizer{})
	month := query.TimeGrainMonth
	agentRequest := service.AgentCompileRequest{
		ProjectID:     conformanceProject,
		Dialect:       "DORIS",
		OutputMetrics: []string{"metric:commerce.revenue_growth_rate"},
		GroupBy: []service.AgentGroupByParam{{
			Name:  "dimension:commerce.orders.order_date",
			Type:  service.AgentGroupByTimeDimension,
			Grain: &month,
		}},
	}
	internalRequest, err := service.NewAgentSemanticService(discovery).BuildCompileRequest(agentRequest)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := runtime.Compile.Compile(context.Background(), internalRequest)
	if err != nil {
		t.Fatal(err)
	}

	restPayload := restCompile(t, runtime.Compile, internalRequest)
	mcpPayload := mcpToolPayload(t, discovery, runtime.Compile, mcp.ToolCompile, agentRequest)
	assertTransportPayloadEqual(t, expected, restPayload, mcpPayload)
}

func TestCanonicalQueryMetricsRESTAndMCPParity(t *testing.T) {
	runtime := runtimeForFixture(t, "commerce.ossie.yaml")
	discovery := service.NewDiscoveryService(manifest.NewStore(runtime.SemanticManifest)).WithProjectAuthorizer(service.AllAccessProjectAuthorizer{})
	queryMetrics := queryMetricsServiceForParity(t, runtime.Compile)
	agentRequest := service.AgentQueryMetricsRequest{
		ProjectID:     conformanceProject,
		OutputMetrics: []string{"metric:commerce.revenue"},
	}
	internalRequest, err := service.NewAgentSemanticService(discovery).BuildQueryMetricsRequestContext(context.Background(), agentRequest)
	if err != nil {
		t.Fatal(err)
	}

	expected, err := queryMetrics.QueryMetrics(context.Background(), internalRequest)
	if err != nil {
		t.Fatal(err)
	}
	restPayload := restQueryMetrics(t, queryMetrics, internalRequest)
	mcpPayload := mcpQueryMetricsPayload(t, discovery, runtime.Compile, queryMetrics, agentRequest)
	assertQueryMetricsTransportPayloadEqual(t, expected, restPayload, mcpPayload)
}

func TestDimensionValuesServiceRESTAndMCPParity(t *testing.T) {
	runtime := runtimeForFixture(t, "commerce.ossie.yaml")
	discovery := service.NewDiscoveryService(manifest.NewStore(runtime.SemanticManifest)).WithProjectAuthorizer(service.AllAccessProjectAuthorizer{})
	dimensionValues := service.NewDimensionValuesService(runtime.Compile, discovery, queryMetricsParityProjects{}, executionRuntimeForParity(t)).WithProjectAuthorizer(service.AllAccessProjectAuthorizer{})
	request := service.DimensionValuesQuery{
		ProjectID: conformanceProject,
		Dimension: "dimension:commerce.customer.region",
	}
	expected, err := dimensionValues.GetDimensionValues(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	restPayload := restDimensionValues(t, dimensionValues, request)
	mcpPayload := mcpDimensionValuesPayload(t, discovery, runtime.Compile, dimensionValues, request)
	assertDimensionValuesTransportPayloadEqual(t, expected, restPayload, mcpPayload)
}

func TestMetricFreeCompileMCPParity(t *testing.T) {
	runtime := runtimeForFixture(t, "temporal_relationship.ossie.yaml")
	discovery := service.NewDiscoveryService(manifest.NewStore(runtime.SemanticManifest)).WithProjectAuthorizer(service.AllAccessProjectAuthorizer{})
	agentRequest := service.AgentCompileRequest{
		ProjectID: conformanceProject,
		Dialect:   "DORIS",
		Model:     "model:temporal_relationship",
		GroupBy: []service.AgentGroupByParam{
			{Name: "dimension:temporal_relationship.orders.order_id", Type: service.AgentGroupByDimension},
			{Name: "dimension:temporal_relationship.customer_history.customer_tier", Type: service.AgentGroupByDimension},
		},
		Filters: []query.Filter{{
			Field: "dimension:temporal_relationship.orders.order_id", Operator: query.FilterEQ, Value: "o_current",
		}},
	}
	internalRequest, err := service.NewAgentSemanticService(discovery).BuildCompileRequest(agentRequest)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := runtime.Compile.Compile(context.Background(), internalRequest)
	if err != nil {
		t.Fatal(err)
	}
	mcpPayload := mcpToolPayload(t, discovery, runtime.Compile, mcp.ToolCompile, agentRequest)
	assertPayloadEqual(t, expected, mcpPayload)
}

func TestPrimaryToolSchemasExposeOutputRolesAndMetricFreePath(t *testing.T) {
	runtime := runtimeForFixture(t, "temporal_relationship.ossie.yaml")
	discovery := service.NewDiscoveryService(manifest.NewStore(runtime.SemanticManifest)).WithProjectAuthorizer(service.AllAccessProjectAuthorizer{})
	handler := mcp.NewHTTPHandler(discovery, runtime.Compile)
	mcpInitialize(t, handler)
	response := mcpPost(t, handler, map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "tools/list", "params": map[string]any{},
	})
	var envelope struct {
		Result struct {
			Tools []struct {
				Name        string `json:"name"`
				InputSchema struct {
					Properties map[string]any `json:"properties"`
					Required   []string       `json:"required"`
				} `json:"inputSchema"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(mcpJSONPayload(t, response), &envelope); err != nil {
		t.Fatal(err)
	}
	tools := map[string]struct {
		Properties map[string]any
		Required   []string
	}{}
	for _, tool := range envelope.Result.Tools {
		tools[tool.Name] = struct {
			Properties map[string]any
			Required   []string
		}{tool.InputSchema.Properties, tool.InputSchema.Required}
	}
	compileSchema := tools[mcp.ToolCompile]
	if _, ok := compileSchema.Properties["output_metrics"]; !ok {
		t.Fatalf("compile schema omits output_metrics: %#v", compileSchema.Properties)
	}
	if _, ok := compileSchema.Properties["metrics"]; ok {
		t.Fatalf("compile schema still exposes ambiguous metrics: %#v", compileSchema.Properties)
	}
	filterSchema, err := json.Marshal(compileSchema.Properties["filters"])
	if err != nil {
		t.Fatal(err)
	}
	for _, requiredGuidance := range []string{"eq, neq, gt, gte", "between is inclusive", "ISO-8601", "combined with AND"} {
		if !strings.Contains(string(filterSchema), requiredGuidance) {
			t.Fatalf("compile filter schema omits %q: %s", requiredGuidance, filterSchema)
		}
	}
	orderSchema, err := json.Marshal(compileSchema.Properties["order_by"])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(orderSchema), "sort precedence") {
		t.Fatalf("compile order schema omits precedence: %s", orderSchema)
	}
	dimensionSchema := tools[mcp.ToolGetDimensions]
	if _, ok := dimensionSchema.Properties["model"]; !ok {
		t.Fatalf("get_dimensions schema omits model: %#v", dimensionSchema.Properties)
	}
	if slices.Contains(dimensionSchema.Required, "metrics") {
		t.Fatalf("get_dimensions still requires metrics: %#v", dimensionSchema.Required)
	}
}

func restCompile(t *testing.T, compile *service.CompileService, req service.CompileRequest) json.RawMessage {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	group := router.Group("/v1")
	rest.RegisterCompileRoutes(group, compile)

	request := httptest.NewRequest(http.MethodPost, "/v1/compile-sql", bytes.NewReader(mustJSON(t, req)))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("REST compile status = %d body=%s", response.Code, response.Body.String())
	}
	return append(json.RawMessage(nil), response.Body.Bytes()...)
}

func restQueryMetrics(t *testing.T, queryMetrics *service.QueryMetricsService, req service.QueryMetricsRequest) json.RawMessage {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	group := router.Group("/v1")
	rest.RegisterQueryMetricsRoutes(group, queryMetrics)

	request := httptest.NewRequest(http.MethodPost, "/v1/query-metrics", bytes.NewReader(mustJSON(t, req)))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("REST query_metrics status = %d body=%s", response.Code, response.Body.String())
	}
	return append(json.RawMessage(nil), response.Body.Bytes()...)
}

func restDimensionValues(t *testing.T, dimensionValues *service.DimensionValuesService, req service.DimensionValuesQuery) json.RawMessage {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	group := router.Group("/v1")
	rest.RegisterDimensionValuesRoutes(group, dimensionValues)

	request := httptest.NewRequest(http.MethodPost, "/v1/dimension-values", bytes.NewReader(mustJSON(t, req)))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("REST get_dimension_values status = %d body=%s", response.Code, response.Body.String())
	}
	return append(json.RawMessage(nil), response.Body.Bytes()...)
}

func mcpToolPayload(t *testing.T, discovery *service.DiscoveryService, compile *service.CompileService, tool string, arguments any) json.RawMessage {
	t.Helper()
	handler := mcp.NewHTTPHandler(discovery, compile)
	mcpInitialize(t, handler)

	call := map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      tool,
			"arguments": arguments,
		},
	}
	response := mcpPost(t, handler, call)
	var envelope struct {
		Result struct {
			StructuredContent json.RawMessage `json:"structuredContent"`
		} `json:"result"`
		Error any `json:"error"`
	}
	payload := mcpJSONPayload(t, response)
	if err := json.Unmarshal(payload, &envelope); err != nil {
		t.Fatalf("decode MCP %s response: %v body=%s", tool, err, response.Body.String())
	}
	if envelope.Error != nil {
		t.Fatalf("MCP %s returned error: %#v", tool, envelope.Error)
	}
	if len(envelope.Result.StructuredContent) == 0 {
		t.Fatalf("MCP %s omitted structuredContent: %s", tool, response.Body.String())
	}
	return envelope.Result.StructuredContent
}

func mcpQueryMetricsPayload(t *testing.T, discovery *service.DiscoveryService, compile *service.CompileService, queryMetrics *service.QueryMetricsService, arguments any) json.RawMessage {
	t.Helper()
	handler := mcp.NewHTTPHandlerWithQueryMetrics(discovery, compile, queryMetrics)
	mcpInitialize(t, handler)

	call := map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      mcp.ToolQueryMetrics,
			"arguments": arguments,
		},
	}
	response := mcpPost(t, handler, call)
	var envelope struct {
		Result struct {
			StructuredContent json.RawMessage `json:"structuredContent"`
		} `json:"result"`
		Error any `json:"error"`
	}
	payload := mcpJSONPayload(t, response)
	if err := json.Unmarshal(payload, &envelope); err != nil {
		t.Fatalf("decode MCP query_metrics response: %v body=%s", err, response.Body.String())
	}
	if envelope.Error != nil {
		t.Fatalf("MCP query_metrics returned error: %#v", envelope.Error)
	}
	if len(envelope.Result.StructuredContent) == 0 {
		t.Fatalf("MCP query_metrics omitted structuredContent: %s", response.Body.String())
	}
	return envelope.Result.StructuredContent
}

func mcpDimensionValuesPayload(t *testing.T, discovery *service.DiscoveryService, compile *service.CompileService, dimensionValues *service.DimensionValuesService, arguments any) json.RawMessage {
	t.Helper()
	handler := mcp.NewObservedHTTPHandlerWithDimensionValues(discovery, compile, nil, dimensionValues, nil, nil, nil, nil)
	mcpInitialize(t, handler)

	call := map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      mcp.ToolGetDimensionValues,
			"arguments": arguments,
		},
	}
	response := mcpPost(t, handler, call)
	var envelope struct {
		Result struct {
			StructuredContent json.RawMessage `json:"structuredContent"`
		} `json:"result"`
		Error any `json:"error"`
	}
	payload := mcpJSONPayload(t, response)
	if err := json.Unmarshal(payload, &envelope); err != nil {
		t.Fatalf("decode MCP get_dimension_values response: %v body=%s", err, response.Body.String())
	}
	if envelope.Error != nil {
		t.Fatalf("MCP get_dimension_values returned error: %#v", envelope.Error)
	}
	if len(envelope.Result.StructuredContent) == 0 {
		t.Fatalf("MCP get_dimension_values omitted structuredContent: %s", response.Body.String())
	}
	return envelope.Result.StructuredContent
}

func mcpInitialize(t *testing.T, handler http.Handler) {
	t.Helper()
	response := mcpPost(t, handler, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-06-18",
			"capabilities":    map[string]any{},
			"clientInfo": map[string]any{
				"name":    "metis-agent-context-conformance",
				"version": "0",
			},
		},
	})
	var envelope struct {
		Error any `json:"error"`
	}
	if err := json.Unmarshal(mcpJSONPayload(t, response), &envelope); err != nil {
		t.Fatalf("decode MCP initialize response: %v body=%s", err, response.Body.String())
	}
	if envelope.Error != nil {
		t.Fatalf("MCP initialize returned error: %#v", envelope.Error)
	}
}

func mcpPost(t *testing.T, handler http.Handler, message any) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(mustJSON(t, message)))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("MCP status = %d body=%s", response.Code, response.Body.String())
	}
	return response
}

func mcpJSONPayload(t *testing.T, response *httptest.ResponseRecorder) []byte {
	t.Helper()
	body := response.Body.Bytes()
	if !strings.HasPrefix(response.Header().Get("Content-Type"), "text/event-stream") {
		return body
	}
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "data:") {
			payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if payload != "" {
				return []byte(payload)
			}
		}
	}
	t.Fatalf("MCP SSE response omitted JSON-RPC data: %s", response.Body.String())
	return nil
}

func assertPayloadEqual(t *testing.T, expected any, actual json.RawMessage) {
	t.Helper()
	expectedJSON := mustJSON(t, expected)
	var expectedValue any
	var actualValue any
	if err := json.Unmarshal(expectedJSON, &expectedValue); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(actual, &actualValue); err != nil {
		t.Fatalf("decode MCP payload: %v payload=%s", err, actual)
	}
	if !reflect.DeepEqual(actualValue, expectedValue) {
		t.Fatalf("MCP payload differs from service canonical expectation:\nservice=%s\nMCP=%s", expectedJSON, actual)
	}
}

func assertTransportPayloadEqual(t *testing.T, expected any, restPayload, mcpPayload json.RawMessage) {
	t.Helper()
	expectedJSON := mustJSON(t, expected)
	var expectedValue any
	var restValue any
	var mcpValue any
	for _, item := range []struct {
		name  string
		input []byte
		out   *any
	}{
		{name: "service", input: expectedJSON, out: &expectedValue},
		{name: "REST", input: restPayload, out: &restValue},
		{name: "MCP", input: mcpPayload, out: &mcpValue},
	} {
		if err := json.Unmarshal(item.input, item.out); err != nil {
			t.Fatalf("decode %s payload: %v payload=%s", item.name, err, item.input)
		}
	}
	if !reflect.DeepEqual(restValue, expectedValue) {
		t.Fatalf("REST payload differs from service canonical expectation:\nservice=%s\nREST=%s", expectedJSON, restPayload)
	}
	if !reflect.DeepEqual(mcpValue, expectedValue) {
		t.Fatalf("MCP payload differs from service canonical expectation:\nservice=%s\nMCP=%s", expectedJSON, mcpPayload)
	}
}

func assertQueryMetricsTransportPayloadEqual(t *testing.T, expected *service.QueryMetricsResult, restPayload, mcpPayload json.RawMessage) {
	t.Helper()
	var restResult, mcpResult service.QueryMetricsResult
	if err := json.Unmarshal(restPayload, &restResult); err != nil {
		t.Fatalf("decode REST query_metrics payload: %v payload=%s", err, restPayload)
	}
	if err := json.Unmarshal(mcpPayload, &mcpResult); err != nil {
		t.Fatalf("decode MCP query_metrics payload: %v payload=%s", err, mcpPayload)
	}
	for _, result := range []*service.QueryMetricsResult{expected, &restResult, &mcpResult} {
		if result.QueryID == "" {
			t.Fatalf("query_metrics response omitted query_id: %#v", result)
		}
	}
	if expected.QueryID == restResult.QueryID || expected.QueryID == mcpResult.QueryID || restResult.QueryID == mcpResult.QueryID {
		t.Fatalf("query_metrics query_ids must correlate distinct executions: service=%q REST=%q MCP=%q", expected.QueryID, restResult.QueryID, mcpResult.QueryID)
	}
	for _, result := range []*service.QueryMetricsResult{&restResult, &mcpResult} {
		result.QueryID = ""
	}
	expectedCopy := *expected
	expectedCopy.QueryID = ""
	if !reflect.DeepEqual(restResult, expectedCopy) {
		t.Fatalf("REST query_metrics payload differs from service result:\nservice=%#v\nREST=%#v", expectedCopy, restResult)
	}
	if !reflect.DeepEqual(mcpResult, expectedCopy) {
		t.Fatalf("MCP query_metrics payload differs from service result:\nservice=%#v\nMCP=%#v", expectedCopy, mcpResult)
	}
}

func assertDimensionValuesTransportPayloadEqual(t *testing.T, expected *service.DimensionValuesResult, restPayload, mcpPayload json.RawMessage) {
	t.Helper()
	var restResult, mcpResult service.DimensionValuesResult
	if err := json.Unmarshal(restPayload, &restResult); err != nil {
		t.Fatalf("decode REST get_dimension_values payload: %v payload=%s", err, restPayload)
	}
	if err := json.Unmarshal(mcpPayload, &mcpResult); err != nil {
		t.Fatalf("decode MCP get_dimension_values payload: %v payload=%s", err, mcpPayload)
	}
	for _, result := range []*service.DimensionValuesResult{expected, &restResult, &mcpResult} {
		if result.QueryID == "" {
			t.Fatalf("get_dimension_values response omitted query_id: %#v", result)
		}
	}
	if expected.QueryID == restResult.QueryID || expected.QueryID == mcpResult.QueryID || restResult.QueryID == mcpResult.QueryID {
		t.Fatalf("get_dimension_values query_ids must correlate distinct executions: service=%q REST=%q MCP=%q", expected.QueryID, restResult.QueryID, mcpResult.QueryID)
	}
	for _, result := range []*service.DimensionValuesResult{&restResult, &mcpResult} {
		result.QueryID = ""
	}
	expectedCopy := *expected
	expectedCopy.QueryID = ""
	if !reflect.DeepEqual(restResult, expectedCopy) {
		t.Fatalf("REST get_dimension_values payload differs from service result:\nservice=%#v\nREST=%#v", expectedCopy, restResult)
	}
	if !reflect.DeepEqual(mcpResult, expectedCopy) {
		t.Fatalf("MCP get_dimension_values payload differs from service result:\nservice=%#v\nMCP=%#v", expectedCopy, mcpResult)
	}
}

type queryMetricsParityProjects struct{}

func (queryMetricsParityProjects) ResolveProject(explicit string) (string, error) {
	if explicit == "" || explicit == conformanceProject {
		return conformanceProject, nil
	}
	return "", fmt.Errorf("unexpected project %q", explicit)
}

func (queryMetricsParityProjects) DataSourceForProject(project string) (string, bool) {
	return "warehouse", project == conformanceProject
}

type queryMetricsParityDriver struct{}

func (queryMetricsParityDriver) DataSourceType() datasource.Type        { return "doris" }
func (queryMetricsParityDriver) ValidateConfig(map[string]string) error { return nil }
func (queryMetricsParityDriver) OpenDataSource(context.Context, driver.OpenRequest) (driver.Runtime, error) {
	return queryMetricsParityRuntime{}, nil
}

type queryMetricsParityRuntime struct{}

func (queryMetricsParityRuntime) Acquire(context.Context) (driver.Executor, error) {
	return queryMetricsParityExecutor{}, nil
}

func (queryMetricsParityRuntime) Close(context.Context) error { return nil }

type queryMetricsParityExecutor struct{}

func (queryMetricsParityExecutor) Execute(context.Context, *artifact.CompiledQuery) (driver.ResultStream, error) {
	return &queryMetricsParityStream{}, nil
}

func (queryMetricsParityExecutor) Close() error { return nil }

type queryMetricsParityStream struct{ sent bool }

func (s *queryMetricsParityStream) Next(context.Context) ([]any, error) {
	if s.sent {
		return nil, io.EOF
	}
	s.sent = true
	return []any{"12.50"}, nil
}

func (*queryMetricsParityStream) Close() error { return nil }

func queryMetricsServiceForParity(t *testing.T, compile *service.CompileService) *service.QueryMetricsService {
	t.Helper()
	return service.NewQueryMetricsService(compile, queryMetricsParityProjects{}, executionRuntimeForParity(t)).WithProjectAuthorizer(service.AllAccessProjectAuthorizer{})
}

func executionRuntimeForParity(t *testing.T) *runner.Runner {
	t.Helper()
	maxRows, maxBytes := int64(10), int64(1024)
	sources, err := datasource.NewDataSourceRegistry(map[string]datasource.DataSource{
		"warehouse": {
			Type: "doris",
			Policy: datasource.DataSourcePolicy{
				QueryTimeout:   time.Second.String(),
				MaxRows:        &maxRows,
				MaxBytes:       &maxBytes,
				MaxConcurrency: queryMetricsParityIntPointer(1),
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	registry, err := renderer.NewRegistry(builtin.Renderers()...)
	if err != nil {
		t.Fatal(err)
	}
	doris, err := registry.Resolve("DORIS")
	if err != nil {
		t.Fatal(err)
	}
	backends, err := backend.NewBackendRegistry(backend.Backend{Type: "doris", Renderer: doris, DriverFactory: queryMetricsParityDriver{}})
	if err != nil {
		t.Fatal(err)
	}
	return runner.New(sources, backends, nil, nil)
}

func queryMetricsParityIntPointer(value int) *int { return &value }

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
