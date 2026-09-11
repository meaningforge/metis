package rest_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	service "github.com/meaningforge/metis/app/service/semantic"
)

func TestSearchEndpointSupportsKindModelAndLimit(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/projects/finance/semantics/search?q=revenue&model=sales&kind=metric&limit=1", nil)
	rec := httptest.NewRecorder()
	newHandler(t).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body service.SearchResult
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Matches) != 1 || body.Matches[0].Kind != service.AssetMetric || body.Matches[0].Model != "sales" {
		t.Fatalf("matches = %#v", body.Matches)
	}
}

func TestSearchEndpointSupportsMultipleKinds(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/projects/finance/semantics/search?q=revenue&kind=metric,ontology_concept&limit=10", nil)
	rec := httptest.NewRecorder()
	newHandler(t).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body service.SearchResult
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Matches) != 2 {
		t.Fatalf("matches = %#v", body.Matches)
	}
	for _, match := range body.Matches {
		if match.Kind != service.AssetMetric && match.Kind != service.AssetOntologyConcept {
			t.Fatalf("unexpected kind in %#v", match)
		}
	}
}

func TestSearchEndpointRejectsInvalidLimit(t *testing.T) {
	for _, value := range []string{"many", "101"} {
		req := httptest.NewRequest(http.MethodGet, "/v1/projects/finance/semantics/search?q=revenue&limit="+value, nil)
		rec := httptest.NewRecorder()
		newHandler(t).ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("limit=%s status=%d body=%s", value, rec.Code, rec.Body.String())
		}
	}
}
