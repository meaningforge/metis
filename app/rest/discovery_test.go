package rest_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/meaningforge/metis/app/rest"
	service "github.com/meaningforge/metis/app/service/semantic"
	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/serrors"
)

const modelYAML = `
version: "0.2.0.dev0"
name: Sales ontology
ontology:
  - concept: Revenue
    description: Business revenue amount
    type: ValueType
    extends: [Decimal]
ontology_mappings:
  - name: sales_mapping
    concept_mappings:
      - concept: Revenue
        object_mappings:
          - expression: orders.amount
semantic_model:
  - name: sales
    description: Sales semantics
    datasets:
      - name: orders
        source: analytics.orders
        fields:
          - name: amount
            datatype: Decimal
            expression:
              dialects: [{dialect: ANSI_SQL, expression: orders.amount}]
      - name: customer
        source: analytics.customer
        fields:
          - name: region
            description: Customer sales region
            datatype: String
            expression:
              dialects: [{dialect: ANSI_SQL, expression: customer.region}]
            dimension: {}
    metrics:
      - name: total_revenue
        description: Total recognized revenue
        datatype: Decimal
        expression:
          dialects: [{dialect: ANSI_SQL, expression: SUM(orders.amount)}]
`

func newHandler(t *testing.T) http.Handler {
	t.Helper()
	gin.SetMode(gin.TestMode)
	doc, err := ossie.NewLoader().Load([]byte(modelYAML))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := manifest.BuildProjectManifest("finance", doc)
	if err != nil {
		t.Fatal(err)
	}
	discoveryService := service.NewDiscoveryService(manifest.NewStore(snapshot)).WithProjectAuthorizer(service.AllAccessProjectAuthorizer{})
	router := gin.New()
	v1 := router.Group("/v1")
	rest.RegisterDiscoveryRoutes(v1, discoveryService)
	return router
}

func TestRESTProjectAuthorizationUsesSharedDenialPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	deny := service.ProjectAuthorizerFunc(func(context.Context, service.ProjectAuthorizationRequest) service.ProjectAuthorizationDecision {
		return service.ProjectAuthorizationDecision{Effect: service.ProjectAuthorizationDeny, Reason: service.ProjectAuthorizationReasonPolicyDenied}
	})
	discovery := service.NewDiscoveryService(nil).WithProjectAuthorizer(deny)
	router := gin.New()
	rest.RegisterDiscoveryRoutes(router.Group("/v1"), discovery)
	req := httptest.NewRequest(http.MethodGet, "/v1/projects/finance/models/secret", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var payload serrors.Payload
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.Code != string(serrors.ErrProjectAccessDenied) || payload.CallerAction != serrors.CallerActionChangeRequest || payload.Message != "project action is not authorized" || len(payload.Details) != 2 || payload.Details["project_id"] != "finance" || payload.Details["action"] != string(service.ProjectActionDiscover) {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestSearchEndpoint(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/projects/finance/semantics/search?q=revenue", nil)
	rec := httptest.NewRecorder()
	newHandler(t).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Matches []struct {
			Kind              string   `json:"kind"`
			Project           string   `json:"project"`
			Name              string   `json:"name"`
			MappedExpressions []string `json:"mapped_expressions"`
		} `json:"matches"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	metricFound, ontologyFound := false, false
	for _, m := range body.Matches {
		if m.Project == "finance" && m.Name == "total_revenue" {
			metricFound = true
		}
		if m.Project == "finance" && m.Kind == "ontology_concept" && m.Name == "Revenue" {
			ontologyFound = len(m.MappedExpressions) == 0
		}
	}
	if !metricFound || !ontologyFound {
		t.Fatalf("expected metric and ontology concept: %s", rec.Body.String())
	}
}

func TestLookupEndpoints(t *testing.T) {
	h := newHandler(t)
	for _, path := range []string{
		"/v1/projects/finance/models/sales",
		"/v1/projects/finance/models/sales/metrics/total_revenue",
		"/v1/projects/finance/models/sales/metrics/total_revenue/dimensions",
		"/v1/projects/finance/models/sales/dimensions/region",
		"/v1/projects/finance/models/sales/relationships",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", path, rec.Code, rec.Body.String())
		}
	}
}

func TestMetricDimensionsEndpointReportsUnreachableDimension(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/projects/finance/models/sales/metrics/total_revenue/dimensions", nil)
	rec := httptest.NewRecorder()
	newHandler(t).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body service.MetricDimensionCompatibilityResult
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Dimensions) != 1 || body.Dimensions[0].Qualified != "customer.region" || body.Dimensions[0].Status != service.CompatibilityUnreachable {
		t.Fatalf("compatibility response = %#v", body)
	}
}

func TestSearchRequiresQuery(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/projects/finance/semantics/search", nil)
	rec := httptest.NewRecorder()
	newHandler(t).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestUnknownProjectIsIsolated(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/projects/growth/semantics/search?q=revenue", nil)
	rec := httptest.NewRecorder()
	newHandler(t).ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
