package rest_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	service "github.com/meaningforge/metis/app/service/semantic"
)

func TestSemanticContextEndpointUsesRouteScope(t *testing.T) {
	body := []byte(`{"project":"ignored","model":"ignored","metrics":["total_revenue"]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/finance/models/sales/semantic-context", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	newHandler(t).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var result service.SemanticContext
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.Project != "finance" || result.Model != "sales" {
		t.Fatalf("scope = %s/%s", result.Project, result.Model)
	}
	if len(result.Metrics) != 1 || result.Metrics[0].Name != "total_revenue" {
		t.Fatalf("metrics = %#v", result.Metrics)
	}
}

func TestSemanticContextEndpointRejectsEmptyFocus(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/finance/models/sales/semantic-context", bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	newHandler(t).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
