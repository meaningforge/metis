package rest_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	service "github.com/meaningforge/metis/app/service/semantic"
)

func TestSemanticSearchRESTSupportsSearchAndEnumeration(t *testing.T) {
	for _, tt := range []struct {
		name string
		body string
		ref  string
	}{
		{name: "search", body: `{"query":"revenue region"}`, ref: "metric:sales.total_revenue"},
		{name: "enumerate", body: `{"kinds":["metric"],"model":"sales"}`, ref: "metric:sales.total_revenue"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/v1/projects/finance/semantic/search", bytes.NewBufferString(tt.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			newHandler(t).ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			var result service.SemanticSearchResult
			if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
				t.Fatal(err)
			}
			if !containsSemanticRef(result.Items, tt.ref) {
				t.Fatalf("result=%#v", result)
			}
		})
	}
}

func containsSemanticRef(items []service.SemanticSearchItem, ref string) bool {
	for _, item := range items {
		if item.Ref == ref {
			return true
		}
	}
	return false
}
