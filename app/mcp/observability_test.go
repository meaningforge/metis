package mcp

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/meaningforge/metis/app/observability"
	service "github.com/meaningforge/metis/app/service/semantic"
	"github.com/meaningforge/metis/serrors"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestObservabilityDoesNotExpandAgentFacingMCPTools(t *testing.T) {
	ctx := context.Background()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := newServer(service.NewDiscoveryService(nil), service.NewCompileService(nil, nil, nil), observability.NewRecorder(), observability.NewTracing(nil)).Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clientSession.Close()
	initialized := clientSession.InitializeResult()
	if initialized == nil {
		t.Fatalf("MCP server instructions = %#v", initialized)
	}
	for _, guidance := range []string{"Discover only missing identities", "never selects one by ranking", "every requested output", "For custom calendars"} {
		if !strings.Contains(initialized.Instructions, guidance) {
			t.Fatalf("MCP server instructions lack %q: %s", guidance, initialized.Instructions)
		}
	}

	result, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		ToolSearchOntologyConcepts: true,
		ToolResolveOntologyConcept: true,
		"list_projects":            true,
		"list_models":              true,
		"get_model":                true,
		"list_metrics":             true,
		"get_metric":               true,
		"get_dimensions":           true,
		"get_dimension":            true,
		"get_relationships":        true,
		"compile_sql":              true,
	}
	if len(result.Tools) != len(want) {
		t.Fatalf("observed MCP tools = %d, want %d", len(result.Tools), len(want))
	}
	for _, tool := range result.Tools {
		if !want[tool.Name] {
			t.Fatalf("observability leaked Agent-facing MCP tool %q", tool.Name)
		}
		for _, forbidden := range []string{"trace", "observability", "telemetry"} {
			if strings.Contains(tool.Name, forbidden) {
				t.Fatalf("observability vocabulary leaked in MCP tool %q", tool.Name)
			}
		}
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint || !tool.Annotations.IdempotentHint || tool.Annotations.OpenWorldHint == nil || *tool.Annotations.OpenWorldHint {
			t.Fatalf("tool %q annotations = %#v, want read-only idempotent closed-world", tool.Name, tool.Annotations)
		}
		if tool.Name == ToolGetRelationships && !strings.Contains(tool.Description, "point-in-time") {
			t.Fatalf("get_relationships description = %q", tool.Description)
		}
		if tool.Name == ToolListMetrics && (!strings.Contains(tool.Description, "retrieval-only") || !strings.Contains(tool.Description, "selection_required") || !strings.Contains(tool.Description, "automatically")) {
			t.Fatalf("list_metrics description lacks non-authoritative selection guidance: %q", tool.Description)
		}
	}
	for _, deleted := range []string{
		"search_semantics", "get_metric_dimensions", "list_dimensions",
		"get_semantic_context", "validate_query", "explain_query", "compile", "query_metrics",
	} {
		legacy, callErr := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: deleted, Arguments: map[string]any{}})
		if callErr == nil && (legacy == nil || !legacy.IsError) {
			t.Fatalf("deleted legacy tool %q unexpectedly callable: result=%#v err=%v", deleted, legacy, callErr)
		}
	}
}

func TestObservedMCPVocabularyMatchesToolRegistry(t *testing.T) {
	for tool, method := range map[string]observability.MCPMethod{
		ToolListProjects:           observability.MCPMethodListProjects,
		ToolSearchOntologyConcepts: observability.MCPMethodSearchOntologyConcepts,
		ToolResolveOntologyConcept: observability.MCPMethodResolveOntologyConcept,
		ToolListModels:             observability.MCPMethodListModels,
		ToolGetModel:               observability.MCPMethodGetModel,
		ToolListMetrics:            observability.MCPMethodListMetrics,
		ToolGetMetric:              observability.MCPMethodGetMetric,
		ToolGetDimensions:          observability.MCPMethodGetDimensions,
		ToolGetDimension:           observability.MCPMethodGetDimension,
		ToolGetDimensionValues:     observability.MCPMethodGetDimensionValues,
		ToolGetRelationships:       observability.MCPMethodGetRelationships,
		ToolCompile:                observability.MCPMethodCompile,
	} {
		if tool != string(method) {
			t.Fatalf("tool %q does not match observation method %q", tool, method)
		}
	}
}

func TestObserveToolRecordsRegisteredMethodAndCompileChild(t *testing.T) {
	memory := observability.NewMemorySink(10)
	exporter := tracetest.NewInMemoryExporter()
	provider := trace.NewTracerProvider(trace.WithSyncer(exporter))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	tracing := observability.NewTracing(provider)
	handler := observeTool(observability.MCPMethodCompile, observability.NewRecorder(memory), tracing,
		func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, *struct{}, error) {
			_, compileSpan := tracing.Start(ctx, observability.OperationCompile)
			compileSpan.End()
			return nil, nil, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "invalid query"}
		})

	_, _, err := handler(context.Background(), nil, struct{}{})
	if err == nil {
		t.Fatal("observed handler error is nil")
	}
	var encoded encodedToolError
	if !errors.As(err, &encoded) {
		t.Fatalf("handler error type = %T, want encodedToolError", err)
	}
	observations := memory.Observations()
	if len(observations) != 1 {
		t.Fatalf("MCP observations = %#v", observations)
	}
	got := observations[0].(observability.MCPObservation)
	if got.Method != observability.MCPMethodCompile || got.Result != observability.ResultError {
		t.Fatalf("MCP observation = %#v", got)
	}

	spans := exporter.GetSpans()
	if len(spans) != 2 {
		t.Fatalf("MCP spans = %#v", spans)
	}
	byName := make(map[string]tracetest.SpanStub, len(spans))
	for _, span := range spans {
		byName[span.Name] = span
	}
	if got, want := byName[string(observability.OperationCompile)].Parent.SpanID(), byName[string(observability.OperationMCP)].SpanContext.SpanID(); got != want {
		t.Fatalf("compile parent = %s, want MCP span %s", got, want)
	}
	if events := byName[string(observability.OperationMCP)].Events; len(events) != 1 || events[0].Name != "exception" {
		t.Fatalf("MCP exception events = %#v", events)
	}
}

func TestObserveToolRecordsPanicAndPreservesRecoverySemantics(t *testing.T) {
	memory := observability.NewMemorySink(10)
	exporter := tracetest.NewInMemoryExporter()
	provider := trace.NewTracerProvider(trace.WithSyncer(exporter))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	tracing := observability.NewTracing(provider)
	handler := observeTool(observability.MCPMethodCompile, observability.NewRecorder(memory), tracing,
		func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, *struct{}, error) {
			panic("compiler defect")
		})

	func() {
		defer func() {
			if recovered := recover(); recovered != "compiler defect" {
				t.Fatalf("recovered panic = %#v", recovered)
			}
		}()
		_, _, _ = handler(context.Background(), nil, struct{}{})
	}()

	observations := memory.Observations()
	if len(observations) != 1 || observations[0].(observability.MCPObservation).Result != observability.ResultError {
		t.Fatalf("panic observations = %#v", observations)
	}
	spans := exporter.GetSpans()
	if len(spans) != 1 || len(spans[0].Events) != 1 || spans[0].Events[0].Name != "exception" {
		t.Fatalf("panic span = %#v", spans)
	}
}
