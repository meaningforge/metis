// This deliberately external host uses only the documented Core integration API.
// Its fixed allow/deny doubles test injection; they are not an enterprise policy product.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/meaningforge/metis/app/auth"
	"github.com/meaningforge/metis/app/bootstrap"
	"github.com/meaningforge/metis/app/hosting"
	"github.com/meaningforge/metis/app/service/policy"
	runtimeservice "github.com/meaningforge/metis/app/service/runtime"
	semantic "github.com/meaningforge/metis/app/service/semantic"
	"github.com/meaningforge/metis/app/service/source"
	"github.com/meaningforge/metis/execution"
	"github.com/meaningforge/metis/serrors"
)

type identity struct{}

func (identity) Verify(_ context.Context, token string) (*auth.Principal, error) {
	if token != "test-host-token" {
		return nil, auth.ErrUnauthorized
	}
	return &auth.Principal{TenantID: "example", SubjectID: "alice", Scopes: []string{auth.ScopeSemanticCompile, auth.ScopeSemanticRead}}, nil
}

type decision struct{ calls int }

func (d *decision) Evaluate(_ context.Context, r policy.Request) (policy.Decision, error) {
	if r.Principal.SubjectID != "alice" || r.Principal.TenantID != "example" || r.ProjectID != "demo" || len(r.Sources) == 0 {
		panic("host identity or workload lost")
	}
	d.calls++
	return policy.Decision{Effect: policy.Denied}, nil
}
func main() {
	adapter := &decision{}
	cfg, err := execution.LoadProjectConfig([]byte(`semantic_sources:
  sales:
    path: models/*.yaml
`))
	if err != nil {
		panic(err)
	}
	model := []byte(`version: "0.2.0.dev0"
semantic_model:
  - name: sales
    datasets:
      - name: orders
        source: warehouse.orders
        fields:
          - name: amount
            datatype: Decimal
            expression:
              dialects: [{dialect: ANSI_SQL, expression: orders.amount}]
    metrics:
      - name: total_revenue
        datatype: Decimal
        expression:
          dialects: [{dialect: ANSI_SQL, expression: SUM(orders.amount)}]
`)
	documents := []source.SourceDocument{{Source: "sales", Path: "models/sales.yaml", Content: model}}
	r, err := bootstrap.NewRuntime(context.Background(), bootstrap.RuntimeInput{Projects: map[string]bootstrap.ProjectInput{"demo": {Config: cfg, Documents: documents}}}, bootstrap.WithProjectAuthorizer(semantic.ScopeProjectAuthorizer{}), bootstrap.WithDataAccessPolicy(adapter))
	if err != nil {
		panic(err)
	}
	defer r.Close(context.Background())
	h, err := hosting.NewHTTPHandler(r, hosting.HTTPOptions{Verifier: identity{}})
	if err != nil {
		panic(err)
	}
	if _, err := hosting.NewHTTPHandler(r, hosting.HTTPOptions{}); err == nil {
		panic("missing verifier accepted")
	}
	// A private host owns its HTTP API and authentication middleware. It does
	// not import REST/MCP transports, call them, or recreate semantic logic.
	privateAPI := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		principal, err := (identity{}).Verify(req.Context(), strings.TrimPrefix(req.Header.Get("Authorization"), "Bearer "))
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var input semantic.CompileRequest
		if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		ctx := r.Generations.Pin(auth.WithPrincipal(req.Context(), principal))
		current, _, err := runtimeservice.FromContext(ctx, input.Query.Project)
		if err == nil {
			_, err = current.Compile.Compile(ctx, input)
		}
		var semanticError *serrors.Error
		if errors.As(err, &semanticError) && semanticError.Code == serrors.ErrDataAccessDenied {
			http.Error(w, string(semanticError.Code), http.StatusForbidden)
			return
		}
		panic(fmt.Sprintf("private API did not enforce injected policy: %v", err))
	})
	call := func(path, body, token string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Accept", "application/json, text/event-stream")
		if token != "" {
			request.Header.Set("Authorization", "Bearer "+token)
		}
		response := httptest.NewRecorder()
		if path == "/private-api/compile" {
			privateAPI.ServeHTTP(response, request)
		} else {
			h.ServeHTTP(response, request)
		}
		return response
	}
	restBody := `{"dialect":"DUCKDB","query":{"project":"demo","model":"sales","metrics":[{"name":"total_revenue"}]}}`
	mcpBody := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"compile_sql","arguments":{"project_id":"demo","dialect":"DUCKDB","output_metrics":["metric:sales.total_revenue"]}}}`
	for round := 0; round < 2; round++ {
		for _, input := range []struct{ path, body string }{{"/v1/compile-sql", restBody}, {"/mcp", mcpBody}, {"/private-api/compile", restBody}} {
			denied := call(input.path, input.body, "")
			if denied.Code != http.StatusUnauthorized {
				panic(fmt.Sprintf("missing identity: %s %d", input.path, denied.Code))
			}
			before := adapter.calls
			result := call(input.path, input.body, "test-host-token")
			if !strings.Contains(result.Body.String(), "DATA_ACCESS_DENIED") || adapter.calls != before+1 {
				panic(fmt.Sprintf("policy injection failed: %s %d %s calls=%d", input.path, result.Code, result.Body.String(), adapter.calls))
			}
		}
		if round == 0 {
			documents[0].Content = []byte(strings.ReplaceAll(string(model), "total_revenue", "updated_revenue"))
			next, err := source.LoadProjectDocuments("demo", cfg, documents)
			if err != nil {
				panic(err)
			}
			installer := auth.WithPrincipal(context.Background(), &auth.Principal{SubjectID: "installer", Scopes: []string{auth.ScopeSemanticActivate}})
			if _, err := r.Generations.Replace(installer, "demo", next.Manifest, next.Bundle.ContentDigest, r.Current("demo").ID); err != nil {
				panic(err)
			}
			restBody = strings.ReplaceAll(restBody, "total_revenue", "updated_revenue")
			mcpBody = strings.ReplaceAll(mcpBody, "total_revenue", "updated_revenue")
		}
	}
	for _, path := range []string{"/admin/v1/projects/demo/git/status", "/admin/v1/projects/demo/runtime/activate", "/hooks/v1/git/legacy"} {
		if result := call(path, `{}`, "test-host-token"); result.Code != http.StatusNotFound {
			panic("standalone handler exposed unsupported endpoint: " + path)
		}
	}
	fmt.Println("external host: in-memory construction and replacement preserve policy in standalone transports and an independent private API; management routes absent")
}
