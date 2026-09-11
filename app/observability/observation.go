package observability

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/meaningforge/metis/execution/runner"
	"github.com/meaningforge/metis/serrors"
)

// ErrorCode returns the stable bounded code for an escaping error. Unknown
// errors fail closed to INTERNAL_ERROR.
func ErrorCode(err error) serrors.ErrorCode {
	if err == nil {
		return ""
	}
	var semanticErr *serrors.Error
	if errors.As(err, &semanticErr) && registeredErrorCode(semanticErr.Code) {
		return semanticErr.Code
	}
	return serrors.ErrInternal
}

// Kind is the closed identity of one runtime observation variant. It describes
// a stable semantic-runtime operation, never a Go package or function.
type Kind string

const (
	KindCompile       Kind = "compile"
	KindPhase         Kind = "compile_phase"
	KindOptimizer     Kind = "optimizer"
	KindHTTP          Kind = "http"
	KindMCP           Kind = "mcp"
	KindModelLoad     Kind = "model_load"
	KindAttribution   Kind = "attribution"
	KindComparison    Kind = "comparison"
	KindExecution     Kind = "execution"
	KindAuthorization Kind = "project_authorization"
	KindActivation    Kind = "semantic_activation"
)

type Result string

const (
	ResultSuccess Result = "success"
	ResultError   Result = "error"
)

type AuthorizationAction string

const (
	AuthorizationDiscover AuthorizationAction = "discover"
	AuthorizationCompile  AuthorizationAction = "compile"
	AuthorizationExecute  AuthorizationAction = "execute"
	AuthorizationAuthor   AuthorizationAction = "author"
	AuthorizationPublish  AuthorizationAction = "publish"
	AuthorizationActivate AuthorizationAction = "activate"
	AuthorizationAdmin    AuthorizationAction = "admin"
)

type AuthorizationEffect string

const (
	AuthorizationAllow AuthorizationEffect = "allow"
	AuthorizationDeny  AuthorizationEffect = "deny"
)

type AuthorizationReason string

const (
	AuthorizationAllAccess         AuthorizationReason = "all_access"
	AuthorizationScopeGranted      AuthorizationReason = "scope_granted"
	AuthorizationScopeMissing      AuthorizationReason = "scope_missing"
	AuthorizationPrincipalMissing  AuthorizationReason = "principal_missing"
	AuthorizationPolicyDenied      AuthorizationReason = "policy_denied"
	AuthorizationPolicyUnavailable AuthorizationReason = "policy_unavailable"
	AuthorizationInvalidAction     AuthorizationReason = "invalid_action"
	AuthorizationInvalidProject    AuthorizationReason = "invalid_project"
	AuthorizationInvalidDecision   AuthorizationReason = "invalid_decision"
)

// AuthorizationObservation intentionally excludes principal and project
// identities so every Prometheus label remains closed and low-cardinality.
type AuthorizationObservation struct {
	Action AuthorizationAction
	Effect AuthorizationEffect
	Reason AuthorizationReason
}

// ActivationFailure is the closed, redacted reason vocabulary for semantic
// generation activation. It never contains release, project, source, or error
// text.
type ActivationFailure string

const (
	ActivationFailureNone               ActivationFailure = "none"
	ActivationFailureGenerationConflict ActivationFailure = "generation_conflict"
	ActivationFailureReleaseNotFound    ActivationFailure = "release_not_found"
	ActivationFailureReleaseInvalid     ActivationFailure = "release_invalid"
	ActivationFailureGenerationInvalid  ActivationFailure = "generation_invalid"
	ActivationFailureCancelled          ActivationFailure = "cancelled"
	ActivationFailureUnavailable        ActivationFailure = "unavailable"
	ActivationFailureInternal           ActivationFailure = "internal"
)

type SemanticActivationObservation struct {
	Result   Result
	Failure  ActivationFailure
	Duration time.Duration
}

func (SemanticActivationObservation) observationKind() Kind { return KindActivation }
func (o SemanticActivationObservation) valid() bool {
	if o.Duration < 0 {
		return false
	}
	if o.Result == ResultSuccess {
		return o.Failure == ActivationFailureNone
	}
	return o.Result == ResultError && validActivationFailure(o.Failure) && o.Failure != ActivationFailureNone
}

func (AuthorizationObservation) observationKind() Kind { return KindAuthorization }
func (o AuthorizationObservation) valid() bool {
	return validAuthorizationAction(o.Action) && validAuthorizationEffect(o.Effect) && validAuthorizationReason(o.Reason)
}

type Backend string

const (
	BackendUnresolved Backend = "unresolved"
	BackendDuckDB     Backend = "duckdb"
	BackendDoris      Backend = "doris"
	BackendClickHouse Backend = "clickhouse"
)

// Dialect is the bounded physical target label used by compile observations.
// Unresolved covers failures that occur before a target is selected and any
// unsupported configured value; arbitrary configuration never becomes a label.
type Dialect string

const (
	DialectUnresolved Dialect = "unresolved"
	DialectDuckDB     Dialect = "DUCKDB"
	DialectDoris      Dialect = "DORIS"
	DialectClickHouse Dialect = "CLICKHOUSE"
)

type Phase string

const (
	PhaseAnalysis     Phase = "analysis"
	PhasePlanning     Phase = "planning"
	PhaseOptimization Phase = "optimization"
	PhaseRendering    Phase = "rendering"
)

type OptimizerOutcome string

const (
	OptimizerRewritten OptimizerOutcome = "rewritten"
	OptimizerNoop      OptimizerOutcome = "noop"
)

type StatusClass string

const (
	Status2xx StatusClass = "2xx"
	Status4xx StatusClass = "4xx"
	Status5xx StatusClass = "5xx"
)

type HTTPRoute string

const (
	RouteCompile                HTTPRoute = "/v1/compile-sql"
	RouteQueryMetrics           HTTPRoute = "/v1/query-metrics"
	RouteDimensionValues        HTTPRoute = "/v1/dimension-values"
	RouteAttributeMetric        HTTPRoute = "/v1/attribute-metric"
	RouteCompareMetrics         HTTPRoute = "/v1/compare-metrics"
	RouteExplain                HTTPRoute = "/v1/explain"
	RouteValidate               HTTPRoute = "/v1/validate"
	RouteSearchSemantics        HTTPRoute = "/v1/projects/:project/semantics/search"
	RouteGetModel               HTTPRoute = "/v1/projects/:project/models/:model"
	RouteGetMetric              HTTPRoute = "/v1/projects/:project/models/:model/metrics/:metric"
	RouteGetMetricDimensions    HTTPRoute = "/v1/projects/:project/models/:model/metrics/:metric/dimensions"
	RouteGetDimension           HTTPRoute = "/v1/projects/:project/models/:model/dimensions/:dimension"
	RouteGetRelationships       HTTPRoute = "/v1/projects/:project/models/:model/relationships"
	RouteGetSemanticContext     HTTPRoute = "/v1/projects/:project/models/:model/semantic-context"
	RouteAgentSemanticSearch    HTTPRoute = "/v1/projects/:project/semantic/search"
	RouteSearchOntologyConcepts HTTPRoute = "/v1/projects/:project/ontology/search"
	RouteResolveOntologyConcept HTTPRoute = "/v1/projects/:project/ontology/resolve"
)

type HTTPMethod string

const (
	MethodGET  HTTPMethod = "GET"
	MethodPOST HTTPMethod = "POST"
)

type MCPMethod string

const (
	MCPMethodListProjects           MCPMethod = "list_projects"
	MCPMethodListModels             MCPMethod = "list_models"
	MCPMethodSearchOntologyConcepts MCPMethod = "search_ontology_concepts"
	MCPMethodResolveOntologyConcept MCPMethod = "resolve_ontology_concept"
	MCPMethodGetModel               MCPMethod = "get_model"
	MCPMethodListMetrics            MCPMethod = "list_metrics"
	MCPMethodGetMetric              MCPMethod = "get_metric"
	MCPMethodGetDimensions          MCPMethod = "get_dimensions"
	MCPMethodGetDimension           MCPMethod = "get_dimension"
	MCPMethodGetDimensionValues     MCPMethod = "get_dimension_values"
	MCPMethodGetRelationships       MCPMethod = "get_relationships"
	MCPMethodCompile                MCPMethod = "compile_sql"
	MCPMethodQueryMetrics           MCPMethod = "query_metrics"
	MCPMethodAttributeMetric        MCPMethod = "attribute_metric"
	MCPMethodCompareMetrics         MCPMethod = "compare_metrics"
)

// Observation is a sealed operational event. Its concrete variants contain no
// semantic request, SQL, plan, fingerprint, principal, or raw error text.
type Observation interface {
	observationKind() Kind
	valid() bool
}

type CompileObservation struct {
	Dialect  Dialect
	Result   Result
	Code     serrors.ErrorCode
	Duration time.Duration
}

func (CompileObservation) observationKind() Kind { return KindCompile }
func (o CompileObservation) valid() bool {
	return validDialect(o.Dialect) && o.Duration >= 0 && validResultAndCode(o.Result, o.Code)
}

type PhaseObservation struct {
	Phase    Phase
	Dialect  Dialect
	Duration time.Duration
}

func (PhaseObservation) observationKind() Kind { return KindPhase }
func (o PhaseObservation) valid() bool {
	return validPhase(o.Phase) && validDialect(o.Dialect) && o.Duration >= 0
}

type OptimizerObservation struct {
	Outcome OptimizerOutcome
}

func (OptimizerObservation) observationKind() Kind { return KindOptimizer }
func (o OptimizerObservation) valid() bool {
	return o.Outcome == OptimizerRewritten || o.Outcome == OptimizerNoop
}

type HTTPObservation struct {
	Route       HTTPRoute
	Method      HTTPMethod
	StatusClass StatusClass
	Duration    time.Duration
}

func (HTTPObservation) observationKind() Kind { return KindHTTP }
func (o HTTPObservation) valid() bool {
	return validHTTPRouteMethod(o.Route, o.Method) && validStatusClass(o.StatusClass) && o.Duration >= 0
}

type MCPObservation struct {
	Method   MCPMethod
	Result   Result
	Duration time.Duration
}

type AttributionObservation struct {
	Result   Result
	Code     serrors.ErrorCode
	Duration time.Duration
	Queries  int
	Rows     int64
	Bytes    int64
}

type ComparisonObservation struct {
	Result   Result
	Code     serrors.ErrorCode
	Duration time.Duration
	Queries  int
	Rows     int64
	Bytes    int64
}

// ExecutionObservation is one physical execution attempt. Rows and Bytes are
// available to non-label sinks only; Backend, Result, and Code are closed.
type ExecutionObservation struct {
	Backend  Backend
	Result   Result
	Code     runner.ExecutionErrorCode
	Duration time.Duration
	Rows     int64
	Bytes    int64
}

func (ExecutionObservation) observationKind() Kind { return KindExecution }
func (o ExecutionObservation) valid() bool {
	if o.Duration < 0 || o.Rows < 0 || o.Bytes < 0 || !validBackend(o.Backend) {
		return false
	}
	if o.Result == ResultSuccess {
		return o.Code == ""
	}
	return o.Result == ResultError && validExecutionCode(o.Code)
}

// ObserveExecution adapts Runner's secret-free lifecycle event into the
// closed application observation contract. QueryID and DataSource identity
// are deliberately dropped.
func (r *Recorder) ObserveExecution(ctx context.Context, observation runner.ExecutionObservation) {
	result := ResultSuccess
	if observation.Code != "" {
		result = ResultError
	}
	r.Record(ctx, ExecutionObservation{
		Backend: NormalizeBackend(string(observation.DataSourceType)), Result: result,
		Code: observation.Code, Duration: observation.Duration, Rows: observation.Rows, Bytes: observation.Bytes,
	})
}

func (ComparisonObservation) observationKind() Kind { return KindComparison }
func (o ComparisonObservation) valid() bool {
	return o.Duration >= 0 && o.Queries >= 0 && o.Rows >= 0 && o.Bytes >= 0 && validResultAndCode(o.Result, o.Code)
}

func (AttributionObservation) observationKind() Kind { return KindAttribution }
func (o AttributionObservation) valid() bool {
	return o.Duration >= 0 && o.Queries >= 0 && o.Rows >= 0 && o.Bytes >= 0 && validResultAndCode(o.Result, o.Code)
}

func (MCPObservation) observationKind() Kind { return KindMCP }
func (o MCPObservation) valid() bool {
	return validMCPMethod(o.Method) && o.Duration >= 0 && (o.Result == ResultSuccess || o.Result == ResultError)
}

type ModelLoadObservation struct {
	Result   Result
	Duration time.Duration
}

func (ModelLoadObservation) observationKind() Kind { return KindModelLoad }
func (o ModelLoadObservation) valid() bool {
	return o.Duration >= 0 && (o.Result == ResultSuccess || o.Result == ResultError)
}

// Sink receives already-validated observations. Implementations must not
// mutate compilation state or retain request-scoped semantic values.
type Sink interface {
	Observe(context.Context, Observation)
}

// Recorder validates observations and isolates compilation from sink failures.
// A sink panic is contained so observability can never change query semantics.
type Recorder struct {
	sinks []Sink
}

func NewRecorder(sinks ...Sink) *Recorder {
	filtered := make([]Sink, 0, len(sinks))
	for _, sink := range sinks {
		if sink != nil {
			filtered = append(filtered, sink)
		}
	}
	return &Recorder{sinks: filtered}
}

func (r *Recorder) Record(ctx context.Context, observation Observation) {
	if r == nil || observation == nil {
		return
	}
	normalized, ok := normalizeObservation(observation)
	if !ok || !normalized.valid() {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	for _, sink := range r.sinks {
		observeSafely(ctx, sink, normalized)
	}
}

func observeSafely(ctx context.Context, sink Sink, observation Observation) {
	defer func() { _ = recover() }()
	sink.Observe(ctx, observation)
}

// MemorySink is a bounded observation sink for deterministic tests.
type MemorySink struct {
	mu           sync.Mutex
	capacity     int
	observations []Observation
}

func NewMemorySink(capacity int) *MemorySink {
	if capacity < 1 {
		capacity = 1
	}
	return &MemorySink{capacity: capacity}
}

func (s *MemorySink) Observe(_ context.Context, observation Observation) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.observations) == s.capacity {
		copy(s.observations, s.observations[1:])
		s.observations[len(s.observations)-1] = observation
		return
	}
	s.observations = append(s.observations, observation)
}

func (s *MemorySink) Observations() []Observation {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Observation(nil), s.observations...)
}

func validResultAndCode(result Result, code serrors.ErrorCode) bool {
	switch result {
	case ResultSuccess:
		return code == ""
	case ResultError:
		for _, registered := range serrors.Codes() {
			if code == registered {
				return true
			}
		}
	}
	return false
}

func validPhase(phase Phase) bool {
	return phase == PhaseAnalysis || phase == PhasePlanning || phase == PhaseOptimization || phase == PhaseRendering
}

func NormalizeDialect(value string) Dialect {
	switch Dialect(strings.ToUpper(strings.TrimSpace(value))) {
	case DialectDuckDB:
		return DialectDuckDB
	case DialectDoris:
		return DialectDoris
	case DialectClickHouse:
		return DialectClickHouse
	default:
		return DialectUnresolved
	}
}

func validDialect(dialect Dialect) bool {
	return NormalizeDialect(string(dialect)) == dialect
}

func NormalizeBackend(value string) Backend {
	switch Backend(strings.ToLower(strings.TrimSpace(value))) {
	case BackendDuckDB:
		return BackendDuckDB
	case BackendDoris:
		return BackendDoris
	case BackendClickHouse:
		return BackendClickHouse
	default:
		return BackendUnresolved
	}
}

func validBackend(value Backend) bool { return NormalizeBackend(string(value)) == value }

func validExecutionCode(code runner.ExecutionErrorCode) bool {
	switch code {
	case runner.ExecutionInvalidInput, runner.ExecutionConfig, runner.ExecutionSecret,
		runner.ExecutionOpen, runner.ExecutionDriver, runner.ExecutionResultContract,
		runner.ExecutionLimit, runner.ExecutionCapacity, runner.ExecutionTimeout,
		runner.ExecutionCancelled, runner.ExecutionCleanup, runner.ExecutionClosed:
		return true
	default:
		return false
	}
}

func validAuthorizationAction(action AuthorizationAction) bool {
	switch action {
	case AuthorizationDiscover, AuthorizationCompile, AuthorizationExecute, AuthorizationAuthor,
		AuthorizationPublish, AuthorizationActivate, AuthorizationAdmin:
		return true
	default:
		return false
	}
}

func validAuthorizationEffect(effect AuthorizationEffect) bool {
	return effect == AuthorizationAllow || effect == AuthorizationDeny
}

func validAuthorizationReason(reason AuthorizationReason) bool {
	switch reason {
	case AuthorizationAllAccess, AuthorizationScopeGranted, AuthorizationScopeMissing,
		AuthorizationPrincipalMissing, AuthorizationPolicyDenied, AuthorizationPolicyUnavailable,
		AuthorizationInvalidAction, AuthorizationInvalidProject, AuthorizationInvalidDecision:
		return true
	default:
		return false
	}
}

func validActivationFailure(failure ActivationFailure) bool {
	switch failure {
	case ActivationFailureNone, ActivationFailureGenerationConflict, ActivationFailureReleaseNotFound,
		ActivationFailureReleaseInvalid, ActivationFailureGenerationInvalid, ActivationFailureCancelled,
		ActivationFailureUnavailable, ActivationFailureInternal:
		return true
	default:
		return false
	}
}

func validHTTPRouteMethod(route HTTPRoute, method HTTPMethod) bool {
	switch route {
	case RouteCompile, RouteQueryMetrics, RouteDimensionValues, RouteAttributeMetric, RouteCompareMetrics, RouteExplain, RouteValidate, RouteGetSemanticContext, RouteAgentSemanticSearch, RouteSearchOntologyConcepts, RouteResolveOntologyConcept:
		return method == MethodPOST
	case RouteSearchSemantics, RouteGetModel, RouteGetMetric, RouteGetMetricDimensions,
		RouteGetDimension, RouteGetRelationships:
		return method == MethodGET
	default:
		return false
	}
}

func validMCPMethod(method MCPMethod) bool {
	switch method {
	case MCPMethodSearchOntologyConcepts, MCPMethodResolveOntologyConcept, MCPMethodListProjects, MCPMethodListModels, MCPMethodGetModel, MCPMethodListMetrics, MCPMethodGetMetric,
		MCPMethodGetDimensions, MCPMethodGetDimension, MCPMethodGetDimensionValues, MCPMethodGetRelationships, MCPMethodCompile, MCPMethodQueryMetrics, MCPMethodAttributeMetric, MCPMethodCompareMetrics:
		return true
	default:
		return false
	}
}

func validStatusClass(status StatusClass) bool {
	return status == Status2xx || status == Status4xx || status == Status5xx
}

func StatusClassFor(status int) StatusClass {
	switch status / 100 {
	case 2:
		return Status2xx
	case 4:
		return Status4xx
	case 5:
		return Status5xx
	default:
		return ""
	}
}

func normalizeObservation(observation Observation) (Observation, bool) {
	switch value := observation.(type) {
	case CompileObservation:
		return value, true
	case *CompileObservation:
		if value != nil {
			return *value, true
		}
	case PhaseObservation:
		return value, true
	case *PhaseObservation:
		if value != nil {
			return *value, true
		}
	case OptimizerObservation:
		return value, true
	case *OptimizerObservation:
		if value != nil {
			return *value, true
		}
	case HTTPObservation:
		return value, true
	case *HTTPObservation:
		if value != nil {
			return *value, true
		}
	case MCPObservation:
		return value, true
	case *MCPObservation:
		if value != nil {
			return *value, true
		}
	case ModelLoadObservation:
		return value, true
	case *ModelLoadObservation:
		if value != nil {
			return *value, true
		}
	case AttributionObservation:
		return value, true
	case *AttributionObservation:
		if value != nil {
			return *value, true
		}
	case ComparisonObservation:
		return value, true
	case *ComparisonObservation:
		if value != nil {
			return *value, true
		}
	case ExecutionObservation:
		return value, true
	case *ExecutionObservation:
		if value != nil {
			return *value, true
		}
	case AuthorizationObservation:
		return value, true
	case *AuthorizationObservation:
		if value != nil {
			return *value, true
		}
	case SemanticActivationObservation:
		return value, true
	case *SemanticActivationObservation:
		if value != nil {
			return *value, true
		}
	}
	return nil, false
}
