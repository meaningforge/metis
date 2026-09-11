package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	service "github.com/meaningforge/metis/app/service/semantic"
	"github.com/meaningforge/metis/serrors"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestEncodeToolErrorUsesSharedPayload(t *testing.T) {
	err := encodeToolError(&serrors.Error{
		Code:        serrors.ErrAmbiguousField,
		Message:     "field is ambiguous",
		Details:     map[string]any{"field": "region"},
		Suggestions: []string{"customer.region"},
	})
	if err == nil {
		t.Fatal("expected encoded tool error")
	}
	var payload serrors.Payload
	if decodeErr := json.Unmarshal([]byte(err.Error()), &payload); decodeErr != nil {
		t.Fatalf("tool error is not JSON: %v", decodeErr)
	}
	if payload.Code != string(serrors.ErrAmbiguousField) || payload.Message != "field is ambiguous" {
		t.Fatalf("payload = %#v", payload)
	}
	if payload.CallerAction != serrors.CallerActionChangeRequest {
		t.Fatalf("caller_action = %q", payload.CallerAction)
	}
	if len(payload.Suggestions) != 1 || payload.Suggestions[0] != "customer.region" {
		t.Fatalf("suggestions = %#v", payload.Suggestions)
	}
}

func TestEncodeToolErrorPreservesUnsupportedExtensionDiagnostics(t *testing.T) {
	err := encodeToolError(&serrors.Error{
		Code:    serrors.ErrUnsupportedSemanticExtension,
		Message: "semantic-critical extension is unsupported by the selected compile target",
		Details: map[string]any{
			"extension_identity": "acme/fiscal_calendar/metric",
			"capability":         "fiscal_calendar",
			"version":            "1",
			"engine":             "metis-native",
			"dialect":            "clickhouse",
			"reason":             "version_incompatible",
		},
	})
	if err == nil {
		t.Fatal("expected encoded tool error")
	}

	var payload serrors.Payload
	if decodeErr := json.Unmarshal([]byte(err.Error()), &payload); decodeErr != nil {
		t.Fatalf("tool error is not JSON: %v", decodeErr)
	}
	if payload.Code != string(serrors.ErrUnsupportedSemanticExtension) || payload.Details["reason"] != "version_incompatible" {
		t.Fatalf("payload = %#v", payload)
	}
	if _, leaked := payload.Details["data"]; leaked {
		t.Fatalf("raw extension payload leaked into MCP error: %#v", payload.Details)
	}
}

func TestEncodeToolErrorPreservesNil(t *testing.T) {
	if err := encodeToolError(nil); err != nil {
		t.Fatalf("nil error = %v", err)
	}
}

func TestMCPToolErrorReturnsSharedPayload(t *testing.T) {
	ctx := context.Background()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := newServer(service.NewDiscoveryService(nil).WithProjectAuthorizer(service.AllAccessProjectAuthorizer{}), nil, nil, nil).Connect(ctx, serverTransport, nil)
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

	result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name: ToolListMetrics,
		Arguments: map[string]any{
			"project_id": "finance",
			"search":     []string{"revenue"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError || len(result.Content) != 1 {
		t.Fatalf("tool result = %#v", result)
	}
	content, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("content = %T, want text", result.Content[0])
	}
	var payload serrors.Payload
	if err := json.Unmarshal([]byte(content.Text), &payload); err != nil {
		t.Fatalf("MCP error content is not shared JSON payload: %v", err)
	}
	if payload.Code != string(serrors.ErrInternal) || payload.Message != "agent semantic service is not configured" {
		t.Fatalf("payload = %#v", payload)
	}
	if payload.CallerAction != serrors.CallerActionReportDefect {
		t.Fatalf("caller_action = %q", payload.CallerAction)
	}
	for _, forbidden := range []string{"exception.stacktrace", "metis.error.code", "trace_id", "span_id"} {
		if strings.Contains(content.Text, forbidden) {
			t.Fatalf("observability detail %q leaked into MCP error payload: %s", forbidden, content.Text)
		}
	}
}

func TestMCPProjectAuthorizationUsesSharedDenialPayload(t *testing.T) {
	ctx := context.Background()
	deny := service.ProjectAuthorizerFunc(func(context.Context, service.ProjectAuthorizationRequest) service.ProjectAuthorizationDecision {
		return service.ProjectAuthorizationDecision{Effect: service.ProjectAuthorizationDeny, Reason: service.ProjectAuthorizationReasonPolicyDenied}
	})
	discovery := service.NewDiscoveryService(nil).WithProjectAuthorizer(deny)
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := newServer(discovery, nil, nil, nil).Connect(ctx, serverTransport, nil)
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

	result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: ToolListMetrics, Arguments: map[string]any{"project_id": "finance"}})
	if err != nil {
		t.Fatal(err)
	}
	content, ok := result.Content[0].(*mcp.TextContent)
	if !result.IsError || !ok {
		t.Fatalf("tool result = %#v", result)
	}
	var payload serrors.Payload
	if err := json.Unmarshal([]byte(content.Text), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Code != string(serrors.ErrProjectAccessDenied) || payload.CallerAction != serrors.CallerActionChangeRequest || payload.Message != "project action is not authorized" || len(payload.Details) != 2 || payload.Details["project_id"] != "finance" || payload.Details["action"] != string(service.ProjectActionDiscover) {
		t.Fatalf("payload = %#v", payload)
	}
}
