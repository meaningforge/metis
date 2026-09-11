package command

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	appmcp "github.com/meaningforge/metis/app/mcp"
	"github.com/meaningforge/metis/cmd/s2sbench/bench/fixtures"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func canonicalModels(t *testing.T) []fixtures.Definition {
	t.Helper()
	models, err := fixtures.CanonicalSemanticModels()
	if err != nil {
		t.Fatal(err)
	}
	return models
}

func TestSemanticProjectRuntimeUsesRequestedFixedPort(t *testing.T) {
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	if err := probe.Close(); err != nil {
		t.Fatal(err)
	}
	runtime, err := newSemanticProjectRuntime(t.TempDir(), port, canonicalModels(t))
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close(context.Background())
	mcp, _, _, err := runtime.CurrentEndpoint()
	if err != nil {
		t.Fatal(err)
	}
	if want := fmt.Sprintf(":%d", port); !strings.HasSuffix(mcp.URL, want) {
		t.Fatalf("MCP URL = %q, want suffix %q", mcp.URL, want)
	}
}

func TestSemanticProjectRuntimeRejectsFixedPortConflictBeforeCollection(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	if _, err := newSemanticProjectRuntime(t.TempDir(), port, canonicalModels(t)); err == nil || !strings.Contains(err.Error(), "reserve benchmark MCP listener") {
		t.Fatalf("error = %v, want early fixed-port conflict", err)
	}
}

func TestSemanticProjectRuntimeLoadsAllModelsOnceAndScopesBearerToAgent(t *testing.T) {
	const callerToken = "caller-owned-token"
	t.Setenv(s2sbenchMCPTokenEnv, callerToken)
	root := t.TempDir()
	models := canonicalModels(t)
	runtime, err := newSemanticProjectRuntime(root, 0, models)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close(context.Background())

	mcp, project, environment, err := runtime.CurrentEndpoint()
	if err != nil {
		t.Fatal(err)
	}
	if project != fixtures.ConformanceProject {
		t.Fatalf("project=%q, want %q", project, fixtures.ConformanceProject)
	}
	if mcp.Name != "metis" || mcp.URL == "" || mcp.BearerTokenEnvVar != s2sbenchMCPTokenEnv {
		t.Fatalf("unexpected MCP endpoint: %#v", mcp)
	}
	token := environment[s2sbenchMCPTokenEnv]
	if token == "" || token == callerToken {
		t.Fatalf("request-scoped token=%q", token)
	}
	if got := os.Getenv(s2sbenchMCPTokenEnv); got != callerToken {
		t.Fatalf("runtime changed caller environment to %q", got)
	}

	entries, err := os.ReadDir(filepath.Join(root, "models"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != len(models) {
		t.Fatalf("materialized models=%d, want %d", len(entries), len(models))
	}
	manifest, err := os.ReadFile(filepath.Join(root, "project.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, model := range models {
		name := sanitizeWorkspaceComponent(model.Model) + ".ossie.yaml"
		if !strings.Contains(string(manifest), "./models/"+name) {
			t.Fatalf("semantic project manifest omits canonical model %q", model.Model)
		}
	}

	client := &http.Client{Timeout: time.Second}
	response, err := client.Get(mcp.URL)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status=%d, want %d", response.StatusCode, http.StatusUnauthorized)
	}
	request, err := http.NewRequest(http.MethodGet, mcp.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized {
		t.Fatal("authenticated MCP request was rejected")
	}

	secondMCP, secondProject, secondEnvironment, err := runtime.CurrentEndpoint()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(secondMCP, mcp) || secondProject != project || secondEnvironment[s2sbenchMCPTokenEnv] != token {
		t.Fatal("immutable semantic endpoint changed between questions")
	}
}

func TestSemanticProjectRuntimeExposesDimensionValuesAsUnavailableCompileOnlySmoke(t *testing.T) {
	runtime, err := newSemanticProjectRuntime(t.TempDir(), 0, canonicalModels(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { runtime.Close(context.Background()) })

	endpoint, _, environment, err := runtime.CurrentEndpoint()
	if err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "s2sbench-dimension-values-smoke", Version: "1"}, nil)
	transport := &mcp.StreamableClientTransport{
		Endpoint: endpoint.URL,
		HTTPClient: &http.Client{Transport: s2sbenchBearerRoundTripper{
			base: http.DefaultTransport, token: environment[s2sbenchMCPTokenEnv],
		}},
		DisableStandaloneSSE: true,
	}
	session, err := client.Connect(context.Background(), transport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })

	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tool := range tools.Tools {
		found = found || tool.Name == appmcp.ToolGetDimensionValues
	}
	if !found {
		t.Fatal("S2SBench Metis runtime omitted get_dimension_values")
	}
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: appmcp.ToolGetDimensionValues,
		Arguments: map[string]any{
			"dimension": "dimension:sales.orders.region",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError || !strings.Contains(result.Content[0].(*mcp.TextContent).Text, "QUERY_EXECUTION_UNAVAILABLE") {
		t.Fatalf("compile-only get_dimension_values result = %#v", result)
	}
}

type s2sbenchBearerRoundTripper struct {
	base  http.RoundTripper
	token string
}

func (transport s2sbenchBearerRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	request = request.Clone(request.Context())
	request.Header.Set("Authorization", "Bearer "+transport.token)
	return transport.base.RoundTrip(request)
}
