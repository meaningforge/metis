package hosting_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/meaningforge/metis/app/auth"
	"github.com/meaningforge/metis/app/bootstrap"
	"github.com/meaningforge/metis/app/hosting"
)

func TestStandaloneRuntimeHasNoPlatformSurfaces(t *testing.T) {
	r, err := bootstrap.LoadRuntime("../../examples/demo/metis.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close(context.Background())
	verifier, err := auth.NewStaticAPIKeyVerifier("runtime-test")
	if err != nil {
		t.Fatal(err)
	}
	handler, err := hosting.NewHTTPHandler(r, hosting.HTTPOptions{Verifier: verifier})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/", "/admin/v1/datasources", "/admin/v1/projects/demo/sources", "/admin/v1/projects/demo/sources/apply", "/admin/v1/projects/demo/runtime/activate", "/admin/v1/projects/demo/git/status", "/hooks/v1/git/legacy"} {
		for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete} {
			request := httptest.NewRequest(method, path, strings.NewReader(`{}`))
			request.Header.Set("Authorization", "Bearer runtime-test")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusNotFound {
				t.Fatalf("platform route exposed: %s %s: %d", method, path, response.Code)
			}
		}
	}
	for _, token := range []string{"", "runtime-test"} {
		request := httptest.NewRequest(http.MethodPost, "/v1/compile-sql", strings.NewReader(`{"dialect":"DUCKDB","query":{"project":"demo","model":"sales","metrics":[{"name":"total_revenue"}]}}`))
		request.Header.Set("Content-Type", "application/json")
		if token != "" {
			request.Header.Set("Authorization", "Bearer "+token)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		want := http.StatusOK
		if token == "" {
			want = http.StatusUnauthorized
		}
		if response.Code != want {
			t.Fatalf("runtime compile: %d %s", response.Code, response.Body.String())
		}
	}
}

func TestMetricsOperatorRoute(t *testing.T) {
	runtime, err := bootstrap.LoadRuntime("../../examples/demo/metis.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close(context.Background())
	verifier, err := auth.NewStaticAPIKeyVerifier("runtime-test")
	if err != nil {
		t.Fatal(err)
	}

	withoutMetrics, err := hosting.NewHTTPHandler(runtime, hosting.HTTPOptions{Verifier: verifier})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	withoutMetrics.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("disabled metrics status = %d, want %d", response.Code, http.StatusNotFound)
	}

	metrics := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("operator_metric 1\n"))
	})
	withMetrics, err := hosting.NewHTTPHandler(runtime, hosting.HTTPOptions{Verifier: verifier, Metrics: metrics})
	if err != nil {
		t.Fatal(err)
	}
	response = httptest.NewRecorder()
	withMetrics.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if response.Code != http.StatusOK || response.Body.String() != "operator_metric 1\n" {
		t.Fatalf("operator metrics response = %d %q", response.Code, response.Body.String())
	}

	response = httptest.NewRecorder()
	withMetrics.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/compile-sql", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("protected route without token status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}
