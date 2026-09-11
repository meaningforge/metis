package agentquery_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/meaningforge/metis/app/auth"
	metismcp "github.com/meaningforge/metis/app/mcp"
	"github.com/meaningforge/metis/app/rest"
	"github.com/meaningforge/metis/app/service/policy"
	service "github.com/meaningforge/metis/app/service/semantic"
	"github.com/meaningforge/metis/compiler"
	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner"
	"github.com/meaningforge/metis/renderer"
	"github.com/meaningforge/metis/renderer/builtin"
	"github.com/meaningforge/metis/resolver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type transportDenyPolicy struct{ calls int }

func (p *transportDenyPolicy) Evaluate(context.Context, policy.Request) (policy.Decision, error) {
	p.calls++
	return policy.Decision{Effect: policy.Denied}, nil
}

func TestDataPolicyDenialRESTAndMCPParity(t *testing.T) {
	doc, err := ossie.NewLoader().Load([]byte(model))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := manifest.BuildProjectManifest("finance", doc)
	if err != nil {
		t.Fatal(err)
	}
	store := manifest.NewStore(snapshot)
	discovery := service.NewDiscoveryService(store)
	registry, err := renderer.NewRegistry(builtin.Renderers()...)
	if err != nil {
		t.Fatal(err)
	}
	adapter := &transportDenyPolicy{}
	compile := service.NewCompileService(resolver.New(store), planner.New(), compiler.NewCompiler(registry)).WithDiscovery(discovery).WithDataAccessPolicy(adapter)
	ctx := auth.WithPrincipal(context.Background(), &auth.Principal{TenantID: "tenant", SubjectID: "subject", Scopes: []string{auth.ScopeAll}})
	gin.SetMode(gin.TestMode)
	router := gin.New()
	rest.RegisterCompileRoutes(router.Group("/v1"), compile)
	req := httptest.NewRequest(http.MethodPost, "/v1/compile-sql", bytes.NewBufferString(`{"dialect":"DUCKDB","query":{"project":"finance","model":"sales","metrics":[{"name":"total_revenue"}]}}`)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("REST=%d %s", rec.Code, rec.Body.String())
	}
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := metismcp.NewServer(discovery, compile).Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	clientSession, err := mcp.NewClient(&mcp.Implementation{Name: "parity", Version: "1"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clientSession.Close()
	result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: metismcp.ToolCompile, Arguments: map[string]any{"project_id": "finance", "dialect": "DUCKDB", "output_metrics": []string{"metric:sales.total_revenue"}}})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError || len(result.Content) != 1 || adapter.calls != 2 {
		t.Fatalf("MCP=%#v evaluations=%d", result, adapter.calls)
	}
	content, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("content=%T", result.Content[0])
	}
	var restValue, mcpValue any
	if err := json.Unmarshal(rec.Body.Bytes(), &restValue); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(content.Text), &mcpValue); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(restValue, mcpValue) {
		t.Fatalf("REST=%#v MCP=%#v", restValue, mcpValue)
	}
}
