package rest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/meaningforge/metis/app/observability"
	service "github.com/meaningforge/metis/app/service/semantic"
	"github.com/meaningforge/metis/serrors"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestObservedRESTVocabularyMatchesRegisteredRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	v1 := router.Group("/v1")
	RegisterDiscoveryRoutes(v1, service.NewDiscoveryService(nil))
	RegisterCompileRoutes(v1, nil)
	want := map[string]observability.HTTPMethod{
		string(observability.RouteCompile):                observability.MethodPOST,
		string(observability.RouteExplain):                observability.MethodPOST,
		string(observability.RouteValidate):               observability.MethodPOST,
		string(observability.RouteSearchSemantics):        observability.MethodGET,
		string(observability.RouteGetModel):               observability.MethodGET,
		string(observability.RouteGetMetric):              observability.MethodGET,
		string(observability.RouteGetMetricDimensions):    observability.MethodGET,
		string(observability.RouteGetDimension):           observability.MethodGET,
		string(observability.RouteGetRelationships):       observability.MethodGET,
		string(observability.RouteGetSemanticContext):     observability.MethodPOST,
		string(observability.RouteAgentSemanticSearch):    observability.MethodPOST,
		string(observability.RouteSearchOntologyConcepts): observability.MethodPOST,
		string(observability.RouteResolveOntologyConcept): observability.MethodPOST,
	}
	if len(router.Routes()) != len(want) {
		t.Fatalf("registered routes = %d, observation vocabulary = %d", len(router.Routes()), len(want))
	}
	for _, route := range router.Routes() {
		if method, ok := want[route.Path]; !ok || string(method) != route.Method {
			t.Fatalf("registered route %s %s is missing from observation vocabulary", route.Method, route.Path)
		}
	}
}

func TestRESTObservationUsesRouteTemplateAndPropagatesExceptionTrace(t *testing.T) {
	gin.SetMode(gin.TestMode)
	memory := observability.NewMemorySink(10)
	exporter := tracetest.NewInMemoryExporter()
	provider := trace.NewTracerProvider(trace.WithSyncer(exporter))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	tracing := observability.NewTracing(provider)

	router := gin.New()
	router.Use(TraceContextMiddleware(tracing))
	v1 := router.Group("/v1")
	v1.Use(ObservationMiddleware(observability.NewRecorder(memory), tracing))
	v1.GET("/projects/:project/models/:model", func(c *gin.Context) {
		writeJSON(c, nil, &serrors.Error{Code: serrors.ErrModelNotFound, Message: "private model missing"})
	})
	req := httptest.NewRequest(http.MethodGet, "/v1/projects/customer-secret/models/private-model", nil)
	req.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	observations := memory.Observations()
	if len(observations) != 1 {
		t.Fatalf("HTTP observations = %#v", observations)
	}
	httpObservation := observations[0].(observability.HTTPObservation)
	if httpObservation.Route != observability.RouteGetModel || httpObservation.StatusClass != observability.Status4xx {
		t.Fatalf("HTTP observation = %#v", httpObservation)
	}
	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("HTTP spans = %#v", spans)
	}
	if spans[0].Parent.TraceID().String() != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("HTTP remote parent = %s", spans[0].Parent.TraceID())
	}
	if spans[0].Status.Code != codes.Error || len(spans[0].Events) != 1 || spans[0].Events[0].Name != "exception" {
		t.Fatalf("HTTP exception trace = %#v", spans[0])
	}
}

func TestRESTObservationRecordsPanicsAsInternal5xxAndNaturallyPropagatesToRecovery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	memory := observability.NewMemorySink(10)
	exporter := tracetest.NewInMemoryExporter()
	provider := trace.NewTracerProvider(trace.WithSyncer(exporter))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	tracing := observability.NewTracing(provider)

	router := gin.New()
	router.Use(gin.CustomRecovery(func(c *gin.Context, _ any) { c.Status(http.StatusInternalServerError) }))
	router.Use(TraceContextMiddleware(tracing))
	v1 := router.Group("/v1")
	v1.Use(ObservationMiddleware(observability.NewRecorder(memory), tracing))
	v1.POST("/compile-sql", func(*gin.Context) { panic("compiler defect") })
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/v1/compile-sql", nil))

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("panic status = %d", recorder.Code)
	}
	observations := memory.Observations()
	if len(observations) != 1 || observations[0].(observability.HTTPObservation).StatusClass != observability.Status5xx {
		t.Fatalf("panic observations = %#v", observations)
	}
	spans := exporter.GetSpans()
	if len(spans) != 1 || spans[0].Status.Code != codes.Error || len(spans[0].Events) != 1 {
		t.Fatalf("panic span = %#v", spans)
	}
}
