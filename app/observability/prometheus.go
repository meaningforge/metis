package observability

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"
)

// PrometheusSink projects typed observations onto the bounded metric surface.
// It owns no semantic state and accepts only observations validated by Recorder.
type PrometheusSink struct {
	compileRequests        *prometheus.CounterVec
	compileDuration        *prometheus.HistogramVec
	compileErrors          *prometheus.CounterVec
	phaseDuration          *prometheus.HistogramVec
	optimizerRuns          *prometheus.CounterVec
	httpRequests           *prometheus.CounterVec
	httpDuration           *prometheus.HistogramVec
	mcpRequests            *prometheus.CounterVec
	mcpDuration            *prometheus.HistogramVec
	attributionRequests    *prometheus.CounterVec
	attributionDuration    prometheus.Histogram
	attributionErrors      *prometheus.CounterVec
	comparisonRequests     *prometheus.CounterVec
	comparisonDuration     prometheus.Histogram
	comparisonErrors       *prometheus.CounterVec
	executionRequests      *prometheus.CounterVec
	executionDuration      *prometheus.HistogramVec
	executionErrors        *prometheus.CounterVec
	authorizationDecisions *prometheus.CounterVec
	activationRequests     *prometheus.CounterVec
	activationDuration     prometheus.Histogram
}

func NewPrometheusSink(registerer prometheus.Registerer) (*PrometheusSink, error) {
	sink := &PrometheusSink{
		compileRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "metis_compile_requests_total",
			Help: "Total semantic compile operations by selected dialect and result.",
		}, []string{"dialect", "result"}),
		compileDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "metis_compile_duration_seconds",
			Help: "Semantic compile operation duration in seconds by selected dialect.",
		}, []string{"dialect"}),
		compileErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "metis_compile_errors_total",
			Help: "Total failed semantic compile operations by selected dialect and stable error code.",
		}, []string{"dialect", "code"}),
		phaseDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "metis_compile_phase_duration_seconds",
			Help: "Compile phase duration in seconds by phase and selected dialect.",
		}, []string{"phase", "dialect"}),
		optimizerRuns: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "metis_optimizer_runs_total",
			Help: "Total semantic optimizer runs by bounded rewrite outcome.",
		}, []string{"outcome"}),
		httpRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "metis_http_requests_total",
			Help: "Total Agent-facing REST requests by registered route, method, and status class.",
		}, []string{"route", "method", "status_class"}),
		httpDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "metis_http_request_duration_seconds",
			Help: "Agent-facing REST request duration in seconds by registered route and method.",
		}, []string{"route", "method"}),
		mcpRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "metis_mcp_requests_total",
			Help: "Total MCP tool calls by registered method and result.",
		}, []string{"method", "result"}),
		mcpDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "metis_mcp_request_duration_seconds",
			Help: "MCP tool call duration in seconds by registered method.",
		}, []string{"method"}),
		attributionRequests:    prometheus.NewCounterVec(prometheus.CounterOpts{Name: "metis_attribution_requests_total", Help: "Total metric attribution operations by result."}, []string{"result"}),
		attributionDuration:    prometheus.NewHistogram(prometheus.HistogramOpts{Name: "metis_attribution_duration_seconds", Help: "Complete metric attribution operation duration in seconds."}),
		attributionErrors:      prometheus.NewCounterVec(prometheus.CounterOpts{Name: "metis_attribution_errors_total", Help: "Failed metric attribution operations by stable error code."}, []string{"code"}),
		comparisonRequests:     prometheus.NewCounterVec(prometheus.CounterOpts{Name: "metis_comparison_requests_total", Help: "Total metric comparison operations by result."}, []string{"result"}),
		comparisonDuration:     prometheus.NewHistogram(prometheus.HistogramOpts{Name: "metis_comparison_duration_seconds", Help: "Complete metric comparison operation duration in seconds."}),
		comparisonErrors:       prometheus.NewCounterVec(prometheus.CounterOpts{Name: "metis_comparison_errors_total", Help: "Failed metric comparison operations by stable error code."}, []string{"code"}),
		executionRequests:      prometheus.NewCounterVec(prometheus.CounterOpts{Name: "metis_execution_requests_total", Help: "Total physical execution attempts by bounded Backend family and result."}, []string{"backend", "result"}),
		executionDuration:      prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "metis_execution_duration_seconds", Help: "Physical execution duration in seconds by bounded Backend family."}, []string{"backend"}),
		executionErrors:        prometheus.NewCounterVec(prometheus.CounterOpts{Name: "metis_execution_errors_total", Help: "Failed physical executions by bounded Backend family and internal execution code."}, []string{"backend", "code"}),
		authorizationDecisions: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "metis_project_authorization_decisions_total", Help: "Project authorization decisions by closed action, effect, and reason."}, []string{"action", "effect", "reason"}),
		activationRequests:     prometheus.NewCounterVec(prometheus.CounterOpts{Name: "metis_semantic_activation_requests_total", Help: "Semantic generation activation attempts by result and redacted failure reason."}, []string{"result", "failure"}),
		activationDuration:     prometheus.NewHistogram(prometheus.HistogramOpts{Name: "metis_semantic_activation_duration_seconds", Help: "Complete semantic generation activation duration in seconds."}),
	}
	if registerer == nil {
		return sink, nil
	}
	for _, collector := range []prometheus.Collector{
		sink.compileRequests, sink.compileDuration, sink.compileErrors, sink.phaseDuration, sink.optimizerRuns,
		sink.httpRequests, sink.httpDuration, sink.mcpRequests, sink.mcpDuration,
		sink.attributionRequests, sink.attributionDuration, sink.attributionErrors,
		sink.comparisonRequests, sink.comparisonDuration, sink.comparisonErrors,
		sink.executionRequests, sink.executionDuration, sink.executionErrors,
		sink.authorizationDecisions,
		sink.activationRequests, sink.activationDuration,
	} {
		if err := registerer.Register(collector); err != nil {
			return nil, err
		}
	}
	return sink, nil
}

func (s *PrometheusSink) Observe(_ context.Context, observation Observation) {
	if s == nil {
		return
	}
	switch value := observation.(type) {
	case CompileObservation:
		dialect := string(value.Dialect)
		result := string(value.Result)
		s.compileRequests.WithLabelValues(dialect, result).Inc()
		s.compileDuration.WithLabelValues(dialect).Observe(value.Duration.Seconds())
		if value.Result == ResultError {
			s.compileErrors.WithLabelValues(dialect, string(value.Code)).Inc()
		}
	case HTTPObservation:
		route, method := string(value.Route), string(value.Method)
		s.httpRequests.WithLabelValues(route, method, string(value.StatusClass)).Inc()
		s.httpDuration.WithLabelValues(route, method).Observe(value.Duration.Seconds())
	case PhaseObservation:
		s.phaseDuration.WithLabelValues(string(value.Phase), string(value.Dialect)).Observe(value.Duration.Seconds())
	case OptimizerObservation:
		s.optimizerRuns.WithLabelValues(string(value.Outcome)).Inc()
	case MCPObservation:
		method := string(value.Method)
		s.mcpRequests.WithLabelValues(method, string(value.Result)).Inc()
		s.mcpDuration.WithLabelValues(method).Observe(value.Duration.Seconds())
	case AttributionObservation:
		s.attributionRequests.WithLabelValues(string(value.Result)).Inc()
		s.attributionDuration.Observe(value.Duration.Seconds())
		if value.Result == ResultError {
			s.attributionErrors.WithLabelValues(string(value.Code)).Inc()
		}
	case ComparisonObservation:
		s.comparisonRequests.WithLabelValues(string(value.Result)).Inc()
		s.comparisonDuration.Observe(value.Duration.Seconds())
		if value.Result == ResultError {
			s.comparisonErrors.WithLabelValues(string(value.Code)).Inc()
		}
	case ExecutionObservation:
		backend := string(value.Backend)
		s.executionRequests.WithLabelValues(backend, string(value.Result)).Inc()
		s.executionDuration.WithLabelValues(backend).Observe(value.Duration.Seconds())
		if value.Result == ResultError {
			s.executionErrors.WithLabelValues(backend, string(value.Code)).Inc()
		}
	case AuthorizationObservation:
		s.authorizationDecisions.WithLabelValues(string(value.Action), string(value.Effect), string(value.Reason)).Inc()
	case SemanticActivationObservation:
		s.activationRequests.WithLabelValues(string(value.Result), string(value.Failure)).Inc()
		s.activationDuration.Observe(value.Duration.Seconds())
	}
}
