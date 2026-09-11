package observability_test

import (
	"context"
	"testing"
	"time"

	"github.com/meaningforge/metis/app/observability"
	"github.com/meaningforge/metis/execution/runner"
	"github.com/meaningforge/metis/serrors"
	"github.com/prometheus/client_golang/prometheus"
)

func TestPrometheusSinkProjectsCompileHTTPAndMCPObservations(t *testing.T) {
	registry := prometheus.NewRegistry()
	sink, err := observability.NewPrometheusSink(registry)
	if err != nil {
		t.Fatal(err)
	}
	recorder := observability.NewRecorder(sink)
	recorder.Record(context.Background(), observability.CompileObservation{
		Dialect: observability.DialectDoris, Result: observability.ResultError,
		Code: serrors.ErrInvalidQuery, Duration: 2 * time.Second,
	})
	recorder.Record(context.Background(), observability.HTTPObservation{
		Route: observability.RouteCompile, Method: observability.MethodPOST,
		StatusClass: observability.Status4xx, Duration: time.Second,
	})
	recorder.Record(context.Background(), observability.MCPObservation{
		Method: observability.MCPMethodCompile, Result: observability.ResultSuccess, Duration: time.Second,
	})
	recorder.Record(context.Background(), observability.PhaseObservation{
		Phase: observability.PhasePlanning, Dialect: observability.DialectDoris, Duration: time.Second,
	})
	recorder.Record(context.Background(), observability.OptimizerObservation{Outcome: observability.OptimizerRewritten})
	recorder.Record(context.Background(), observability.ExecutionObservation{
		Backend: observability.BackendDoris, Result: observability.ResultError,
		Code: runner.ExecutionResultContract, Duration: time.Second,
	})
	recorder.Record(context.Background(), observability.AuthorizationObservation{
		Action: observability.AuthorizationExecute, Effect: observability.AuthorizationDeny, Reason: observability.AuthorizationScopeMissing,
	})
	recorder.Record(context.Background(), observability.SemanticActivationObservation{
		Result: observability.ResultError, Failure: observability.ActivationFailureGenerationConflict, Duration: time.Second,
	})

	for name, want := range map[string]float64{
		"metis_compile_requests_total":                1,
		"metis_compile_errors_total":                  1,
		"metis_http_requests_total":                   1,
		"metis_mcp_requests_total":                    1,
		"metis_optimizer_runs_total":                  1,
		"metis_execution_requests_total":              1,
		"metis_execution_errors_total":                1,
		"metis_project_authorization_decisions_total": 1,
		"metis_semantic_activation_requests_total":    1,
	} {
		if got := metricFamilySum(t, registry, name); got != want {
			t.Fatalf("%s sum = %v, want %v", name, got, want)
		}
	}
	for name, want := range map[string]uint64{
		"metis_compile_duration_seconds":             1,
		"metis_http_request_duration_seconds":        1,
		"metis_mcp_request_duration_seconds":         1,
		"metis_compile_phase_duration_seconds":       1,
		"metis_execution_duration_seconds":           1,
		"metis_semantic_activation_duration_seconds": 1,
	} {
		if got := histogramCount(t, registry, name); got != want {
			t.Fatalf("%s count = %d, want %d", name, got, want)
		}
	}
}

func TestPrometheusSinkCannotExposeInvalidDynamicLabels(t *testing.T) {
	registry := prometheus.NewRegistry()
	sink, err := observability.NewPrometheusSink(registry)
	if err != nil {
		t.Fatal(err)
	}
	recorder := observability.NewRecorder(sink)
	recorder.Record(context.Background(), observability.HTTPObservation{
		Route: "/v1/projects/customer-secret/models/private", Method: observability.MethodGET,
		StatusClass: observability.Status2xx, Duration: time.Second,
	})
	recorder.Record(context.Background(), observability.MCPObservation{
		Method: "customer_supplied_method", Result: observability.ResultSuccess, Duration: time.Second,
	})
	recorder.Record(context.Background(), observability.CompileObservation{
		Dialect: "customer_dialect", Result: observability.ResultSuccess, Duration: time.Second,
	})
	recorder.Record(context.Background(), observability.HTTPObservation{
		Route: observability.RouteCompile, Method: observability.MethodGET,
		StatusClass: observability.Status2xx, Duration: time.Second,
	})
	recorder.Record(context.Background(), observability.AuthorizationObservation{
		Action: "customer_action", Effect: observability.AuthorizationAllow, Reason: observability.AuthorizationScopeGranted,
	})

	for _, name := range []string{"metis_http_requests_total", "metis_mcp_requests_total", "metis_compile_requests_total", "metis_project_authorization_decisions_total"} {
		if got := metricFamilySum(t, registry, name); got != 0 {
			t.Fatalf("%s accepted an invalid dynamic label: %v", name, got)
		}
	}
}

func TestPrometheusCustomMetricCardinalityBudget(t *testing.T) {
	registry := prometheus.NewRegistry()
	sink, err := observability.NewPrometheusSink(registry)
	if err != nil {
		t.Fatal(err)
	}
	recorder := observability.NewRecorder(sink)
	ctx := context.Background()
	// Keep the error-code vocabulary bounded. Review this cardinality budget
	// for every new code; identity, source, field, scope, repository, and
	// credential values must never become metric label dimensions.
	const registeredErrorCodeBudget = 68
	if got := len(serrors.Codes()); got != registeredErrorCodeBudget {
		t.Fatalf("registered stable error codes = %d, budget = %d; review the metric cardinality contract", got, registeredErrorCodeBudget)
	}
	dialects := []observability.Dialect{
		observability.DialectDuckDB,
		observability.DialectDoris,
		observability.DialectClickHouse,
		observability.DialectUnresolved,
	}
	for _, dialect := range dialects {
		recorder.Record(ctx, observability.CompileObservation{
			Dialect: dialect, Result: observability.ResultSuccess, Duration: time.Second,
		})
		for _, code := range serrors.Codes() {
			recorder.Record(ctx, observability.CompileObservation{
				Dialect: dialect, Result: observability.ResultError, Code: code, Duration: time.Second,
			})
		}
		for _, phase := range []observability.Phase{
			observability.PhaseAnalysis,
			observability.PhasePlanning,
			observability.PhaseOptimization,
			observability.PhaseRendering,
		} {
			recorder.Record(ctx, observability.PhaseObservation{
				Phase: phase, Dialect: dialect, Duration: time.Second,
			})
		}
	}

	routes := []struct {
		route  observability.HTTPRoute
		method observability.HTTPMethod
	}{
		{observability.RouteCompile, observability.MethodPOST},
		{observability.RouteQueryMetrics, observability.MethodPOST},
		{observability.RouteDimensionValues, observability.MethodPOST},
		{observability.RouteAttributeMetric, observability.MethodPOST},
		{observability.RouteExplain, observability.MethodPOST},
		{observability.RouteValidate, observability.MethodPOST},
		{observability.RouteSearchSemantics, observability.MethodGET},
		{observability.RouteGetModel, observability.MethodGET},
		{observability.RouteGetMetric, observability.MethodGET},
		{observability.RouteGetMetricDimensions, observability.MethodGET},
		{observability.RouteGetDimension, observability.MethodGET},
		{observability.RouteGetRelationships, observability.MethodGET},
		{observability.RouteGetSemanticContext, observability.MethodPOST},
		{observability.RouteAgentSemanticSearch, observability.MethodPOST},
		{observability.RouteSearchOntologyConcepts, observability.MethodPOST},
		{observability.RouteResolveOntologyConcept, observability.MethodPOST},
		{observability.RouteCompareMetrics, observability.MethodPOST},
	}
	for _, route := range routes {
		for _, status := range []observability.StatusClass{
			observability.Status2xx, observability.Status4xx, observability.Status5xx,
		} {
			recorder.Record(ctx, observability.HTTPObservation{
				Route: route.route, Method: route.method, StatusClass: status, Duration: time.Second,
			})
		}
	}

	methods := []observability.MCPMethod{
		observability.MCPMethodListProjects,
		observability.MCPMethodSearchOntologyConcepts,
		observability.MCPMethodResolveOntologyConcept,
		observability.MCPMethodListModels,
		observability.MCPMethodGetModel,
		observability.MCPMethodListMetrics,
		observability.MCPMethodGetMetric,
		observability.MCPMethodGetDimensions,
		observability.MCPMethodGetDimension,
		observability.MCPMethodGetDimensionValues,
		observability.MCPMethodGetRelationships,
		observability.MCPMethodCompile,
		observability.MCPMethodQueryMetrics,
		observability.MCPMethodAttributeMetric,
		observability.MCPMethodCompareMetrics,
	}
	for _, method := range methods {
		for _, result := range []observability.Result{observability.ResultSuccess, observability.ResultError} {
			recorder.Record(ctx, observability.MCPObservation{
				Method: method, Result: result, Duration: time.Second,
			})
		}
	}
	recorder.Record(ctx, observability.AttributionObservation{Result: observability.ResultSuccess, Duration: time.Second})
	for _, code := range serrors.Codes() {
		recorder.Record(ctx, observability.AttributionObservation{Result: observability.ResultError, Code: code, Duration: time.Second})
	}
	recorder.Record(ctx, observability.ComparisonObservation{Result: observability.ResultSuccess, Duration: time.Second})
	for _, code := range serrors.Codes() {
		recorder.Record(ctx, observability.ComparisonObservation{Result: observability.ResultError, Code: code, Duration: time.Second})
	}
	for _, outcome := range []observability.OptimizerOutcome{
		observability.OptimizerRewritten, observability.OptimizerNoop,
	} {
		recorder.Record(ctx, observability.OptimizerObservation{Outcome: outcome})
	}
	backends := []observability.Backend{
		observability.BackendDuckDB,
		observability.BackendDoris,
		observability.BackendClickHouse,
		observability.BackendUnresolved,
	}
	executionCodes := []runner.ExecutionErrorCode{
		runner.ExecutionInvalidInput, runner.ExecutionConfig, runner.ExecutionSecret,
		runner.ExecutionOpen, runner.ExecutionDriver, runner.ExecutionResultContract,
		runner.ExecutionLimit, runner.ExecutionCapacity, runner.ExecutionTimeout,
		runner.ExecutionCancelled, runner.ExecutionCleanup, runner.ExecutionClosed,
	}
	for _, backend := range backends {
		recorder.Record(ctx, observability.ExecutionObservation{Backend: backend, Result: observability.ResultSuccess, Duration: time.Second})
		for _, code := range executionCodes {
			recorder.Record(ctx, observability.ExecutionObservation{Backend: backend, Result: observability.ResultError, Code: code, Duration: time.Second})
		}
	}
	authorizationActions := []observability.AuthorizationAction{
		observability.AuthorizationDiscover, observability.AuthorizationCompile, observability.AuthorizationExecute,
		observability.AuthorizationAuthor, observability.AuthorizationPublish, observability.AuthorizationActivate, observability.AuthorizationAdmin,
	}
	authorizationEffects := []observability.AuthorizationEffect{observability.AuthorizationAllow, observability.AuthorizationDeny}
	authorizationReasons := []observability.AuthorizationReason{
		observability.AuthorizationAllAccess, observability.AuthorizationScopeGranted, observability.AuthorizationScopeMissing,
		observability.AuthorizationPrincipalMissing, observability.AuthorizationPolicyDenied, observability.AuthorizationPolicyUnavailable,
		observability.AuthorizationInvalidAction, observability.AuthorizationInvalidProject, observability.AuthorizationInvalidDecision,
	}
	for _, action := range authorizationActions {
		for _, effect := range authorizationEffects {
			for _, reason := range authorizationReasons {
				recorder.Record(ctx, observability.AuthorizationObservation{Action: action, Effect: effect, Reason: reason})
			}
		}
	}
	activationFailures := []observability.ActivationFailure{
		observability.ActivationFailureGenerationConflict, observability.ActivationFailureReleaseNotFound,
		observability.ActivationFailureReleaseInvalid, observability.ActivationFailureGenerationInvalid,
		observability.ActivationFailureCancelled, observability.ActivationFailureUnavailable, observability.ActivationFailureInternal,
	}
	recorder.Record(ctx, observability.SemanticActivationObservation{Result: observability.ResultSuccess, Failure: observability.ActivationFailureNone, Duration: time.Second})
	for _, failure := range activationFailures {
		recorder.Record(ctx, observability.SemanticActivationObservation{Result: observability.ResultError, Failure: failure, Duration: time.Second})
	}

	wantLabelSets := map[string]int{
		"metis_compile_requests_total":                len(dialects) * 2,
		"metis_compile_errors_total":                  len(dialects) * len(serrors.Codes()),
		"metis_compile_duration_seconds":              len(dialects),
		"metis_compile_phase_duration_seconds":        len(dialects) * 4,
		"metis_optimizer_runs_total":                  2,
		"metis_http_requests_total":                   len(routes) * 3,
		"metis_http_request_duration_seconds":         len(routes),
		"metis_mcp_requests_total":                    len(methods) * 2,
		"metis_mcp_request_duration_seconds":          len(methods),
		"metis_attribution_requests_total":            2,
		"metis_attribution_duration_seconds":          1,
		"metis_attribution_errors_total":              len(serrors.Codes()),
		"metis_comparison_requests_total":             2,
		"metis_comparison_duration_seconds":           1,
		"metis_comparison_errors_total":               len(serrors.Codes()),
		"metis_execution_requests_total":              len(backends) * 2,
		"metis_execution_duration_seconds":            len(backends),
		"metis_execution_errors_total":                len(backends) * len(executionCodes),
		"metis_project_authorization_decisions_total": len(authorizationActions) * len(authorizationEffects) * len(authorizationReasons),
		"metis_semantic_activation_requests_total":    len(activationFailures) + 1,
		"metis_semantic_activation_duration_seconds":  1,
	}
	families, err := registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	if len(families) != len(wantLabelSets) {
		t.Fatalf("custom metric families = %d, want %d", len(families), len(wantLabelSets))
	}
	for _, family := range families {
		want, ok := wantLabelSets[family.GetName()]
		if !ok {
			t.Fatalf("unexpected custom metric family %q", family.GetName())
		}
		if got := len(family.Metric); got != want {
			t.Errorf("%s label sets = %d, want %d", family.GetName(), got, want)
		}
		if family.GetType().String() == "HISTOGRAM" {
			for _, metric := range family.Metric {
				if got, want := len(metric.GetHistogram().Bucket), len(prometheus.DefBuckets); got != want {
					t.Errorf("%s finite buckets = %d, want Prometheus defaults = %d", family.GetName(), got, want)
				}
			}
		}
	}
}

func metricFamilySum(t *testing.T, gatherer prometheus.Gatherer, name string) float64 {
	t.Helper()
	families, err := gatherer.Gather()
	if err != nil {
		t.Fatal(err)
	}
	var total float64
	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		for _, metric := range family.Metric {
			total += metric.GetCounter().GetValue()
		}
	}
	return total
}

func histogramCount(t *testing.T, gatherer prometheus.Gatherer, name string) uint64 {
	t.Helper()
	families, err := gatherer.Gather()
	if err != nil {
		t.Fatal(err)
	}
	var total uint64
	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		for _, metric := range family.Metric {
			total += metric.GetHistogram().GetSampleCount()
		}
	}
	return total
}
