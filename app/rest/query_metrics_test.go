package rest

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestQueryMetricsRejectsPhysicalPlacementFieldsAndKeepsUnavailableContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	v1 := router.Group("/v1")
	RegisterQueryMetricsRoutes(v1, nil)
	RegisterDimensionValuesRoutes(v1, nil)
	RegisterAttributeMetricRoutes(v1, nil)
	RegisterCompareMetricsRoutes(v1, nil)

	physical := httptest.NewRecorder()
	router.ServeHTTP(physical, httptest.NewRequest(http.MethodPost, "/v1/query-metrics", strings.NewReader(`{"query":{"metrics":[{"name":"revenue"}]},"dialect":"DORIS"}`)))
	if physical.Code != http.StatusBadRequest || !strings.Contains(physical.Body.String(), "INVALID_QUERY") {
		t.Fatalf("physical placement response = %d %s", physical.Code, physical.Body.String())
	}

	unavailable := httptest.NewRecorder()
	router.ServeHTTP(unavailable, httptest.NewRequest(http.MethodPost, "/v1/query-metrics", strings.NewReader(`{"query":{"metrics":[{"name":"revenue"}]}}`)))
	if unavailable.Code != http.StatusUnprocessableEntity || !strings.Contains(unavailable.Body.String(), "QUERY_EXECUTION_UNAVAILABLE") {
		t.Fatalf("compile-only response = %d %s", unavailable.Code, unavailable.Body.String())
	}

	dimensionPhysical := httptest.NewRecorder()
	router.ServeHTTP(dimensionPhysical, httptest.NewRequest(http.MethodPost, "/v1/dimension-values", strings.NewReader(`{"dimension":"dimension:sales.orders.region","dialect":"DORIS"}`)))
	if dimensionPhysical.Code != http.StatusBadRequest || !strings.Contains(dimensionPhysical.Body.String(), "INVALID_QUERY") {
		t.Fatalf("dimension value physical placement response = %d %s", dimensionPhysical.Code, dimensionPhysical.Body.String())
	}
	dimensionUnavailable := httptest.NewRecorder()
	router.ServeHTTP(dimensionUnavailable, httptest.NewRequest(http.MethodPost, "/v1/dimension-values", strings.NewReader(`{"dimension":"dimension:sales.orders.region"}`)))
	if dimensionUnavailable.Code != http.StatusUnprocessableEntity || !strings.Contains(dimensionUnavailable.Body.String(), "QUERY_EXECUTION_UNAVAILABLE") {
		t.Fatalf("dimension value compile-only response = %d %s", dimensionUnavailable.Code, dimensionUnavailable.Body.String())
	}

	attributionPhysical := httptest.NewRecorder()
	router.ServeHTTP(attributionPhysical, httptest.NewRequest(http.MethodPost, "/v1/attribute-metric", strings.NewReader(`{"metric":"metric:sales.revenue","time_dimension":"dimension:sales.orders.time","baseline":{"start":"2026-01-01T00:00:00Z","end":"2026-02-01T00:00:00Z"},"current":{"start":"2026-02-01T00:00:00Z","end":"2026-03-01T00:00:00Z"},"dimensions":["dimension:sales.orders.region"],"dialect":"DORIS"}`)))
	if attributionPhysical.Code != http.StatusBadRequest || !strings.Contains(attributionPhysical.Body.String(), "INVALID_QUERY") {
		t.Fatalf("attribution physical placement response = %d %s", attributionPhysical.Code, attributionPhysical.Body.String())
	}
	attributionUnavailable := httptest.NewRecorder()
	router.ServeHTTP(attributionUnavailable, httptest.NewRequest(http.MethodPost, "/v1/attribute-metric", strings.NewReader(`{"metric":"metric:sales.revenue","time_dimension":"dimension:sales.orders.time","baseline":{"start":"2026-01-01T00:00:00Z","end":"2026-02-01T00:00:00Z"},"current":{"start":"2026-02-01T00:00:00Z","end":"2026-03-01T00:00:00Z"},"dimensions":["dimension:sales.orders.region"]}`)))
	if attributionUnavailable.Code != http.StatusUnprocessableEntity || !strings.Contains(attributionUnavailable.Body.String(), "QUERY_EXECUTION_UNAVAILABLE") {
		t.Fatalf("attribution compile-only response = %d %s", attributionUnavailable.Code, attributionUnavailable.Body.String())
	}

	comparisonPhysical := httptest.NewRecorder()
	router.ServeHTTP(comparisonPhysical, httptest.NewRequest(http.MethodPost, "/v1/compare-metrics", strings.NewReader(`{"metrics":["metric:sales.revenue"],"time_dimension":"dimension:sales.orders.time","baseline":{"start":"2026-01-01T00:00:00Z","end":"2026-02-01T00:00:00Z"},"current":{"start":"2026-02-01T00:00:00Z","end":"2026-03-01T00:00:00Z"},"dialect":"DORIS"}`)))
	if comparisonPhysical.Code != http.StatusBadRequest || !strings.Contains(comparisonPhysical.Body.String(), "INVALID_QUERY") {
		t.Fatalf("comparison physical placement response = %d %s", comparisonPhysical.Code, comparisonPhysical.Body.String())
	}
	comparisonUnavailable := httptest.NewRecorder()
	router.ServeHTTP(comparisonUnavailable, httptest.NewRequest(http.MethodPost, "/v1/compare-metrics", strings.NewReader(`{"metrics":["metric:sales.revenue"],"time_dimension":"dimension:sales.orders.time","baseline":{"start":"2026-01-01T00:00:00Z","end":"2026-02-01T00:00:00Z"},"current":{"start":"2026-02-01T00:00:00Z","end":"2026-03-01T00:00:00Z"}}`)))
	if comparisonUnavailable.Code != http.StatusUnprocessableEntity || !strings.Contains(comparisonUnavailable.Body.String(), "QUERY_EXECUTION_UNAVAILABLE") {
		t.Fatalf("comparison compile-only response = %d %s", comparisonUnavailable.Code, comparisonUnavailable.Body.String())
	}

	legacy := httptest.NewRecorder()
	router.ServeHTTP(legacy, httptest.NewRequest(http.MethodPost, "/v1/compile", nil))
	if legacy.Code != http.StatusNotFound {
		t.Fatalf("legacy compile route status = %d", legacy.Code)
	}
}
