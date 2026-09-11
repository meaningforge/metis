// Package serrors owns Metis's stable semantic error codes, structured error
// values, caller-remediation categories, and transport-neutral payloads.
package serrors

import (
	"errors"
	"fmt"
	"sort"
)

type ErrorCode string

// Error codes name a condition, never a pipeline stage. An operator reading
// METRIC_SOURCE_UNREACHABLE knows what is wrong; one reading PLANNING_FAILED
// only knows where the code happened to be. Codes follow four shapes:
//
//	<SUBJECT>_NOT_FOUND      a named asset does not exist
//	<SUBJECT>_REQUIRED       a required input was absent
//	<ADJECTIVE>_<SUBJECT>    INVALID_, UNSUPPORTED_, AMBIGUOUS_, INCOMPATIBLE_,
//	                         INCOMPLETE_ describe the state that is wrong
//	INTERNAL_*               a Metis defect, never the caller's to fix
//
// A code ending in _FAILED is only acceptable when the verb is the condition
// itself, as in EXPRESSION_PARSE_FAILED. When a failure cannot be named
// precisely, it uses INTERNAL_ERROR rather than a vague new code: a bucket that
// means several unrelated things tells an operator nothing and cannot be
// narrowed later without a compatibility break.
const (
	// Request and model shape.
	ErrInvalidModel        ErrorCode = "INVALID_MODEL"
	ErrInvalidQuery        ErrorCode = "INVALID_QUERY"
	ErrInvalidFilterValue  ErrorCode = "INVALID_FILTER_VALUE"
	ErrInvalidSort         ErrorCode = "INVALID_SORT"
	ErrInvalidRelationship ErrorCode = "INVALID_RELATIONSHIP"
	ErrProjectRequired     ErrorCode = "PROJECT_REQUIRED"

	// Asset lookup. These report that something *the request named* does not
	// resolve. A model that references a missing asset is a different condition
	// with a different remedy - see UNRESOLVED_SEMANTIC_REFERENCE.
	ErrProjectNotFound               ErrorCode = "PROJECT_NOT_FOUND"
	ErrProjectAccessDenied           ErrorCode = "PROJECT_ACCESS_DENIED"
	ErrDataAccessDenied              ErrorCode = "DATA_ACCESS_DENIED"
	ErrDataAccessPolicyUnavailable   ErrorCode = "DATA_ACCESS_POLICY_UNAVAILABLE"
	ErrInvalidDataAccessPolicy       ErrorCode = "INVALID_DATA_ACCESS_POLICY"
	ErrOntologyResolutionUnavailable ErrorCode = "ONTOLOGY_RESOLUTION_UNAVAILABLE"
	ErrModelNotFound                 ErrorCode = "MODEL_NOT_FOUND"
	ErrMetricNotFound                ErrorCode = "METRIC_NOT_FOUND"
	ErrDimensionNotFound             ErrorCode = "DIMENSION_NOT_FOUND"
	ErrFieldNotFound                 ErrorCode = "FIELD_NOT_FOUND"
	ErrRelationshipNotFound          ErrorCode = "RELATIONSHIP_NOT_FOUND"
	ErrEngineNotFound                ErrorCode = "ENGINE_NOT_FOUND"

	// A semantic model refers to something it does not define: a metric
	// dependency, an expression's dataset or field, or a definition-filter
	// field. The request is well-formed; the model is internally incomplete,
	// and only its author can act.
	ErrUnresolvedSemanticReference ErrorCode = "UNRESOLVED_SEMANTIC_REFERENCE"

	// Ambiguity. Metis never guesses; it names the competing candidates.
	ErrAmbiguousField               ErrorCode = "AMBIGUOUS_FIELD"
	ErrAmbiguousExpressionReference ErrorCode = "AMBIGUOUS_EXPRESSION_REFERENCE"
	ErrAmbiguousMetricSource        ErrorCode = "AMBIGUOUS_METRIC_SOURCE"
	ErrAmbiguousRelationshipPath    ErrorCode = "AMBIGUOUS_RELATIONSHIP_PATH"
	ErrAmbiguousTarget              ErrorCode = "AMBIGUOUS_TARGET"

	// Expression analysis.
	ErrExpressionParseFailed            ErrorCode = "EXPRESSION_PARSE_FAILED"
	ErrSemanticAnalysisFailed           ErrorCode = "SEMANTIC_ANALYSIS_FAILED"
	ErrInconsistentExpressionReferences ErrorCode = "INCONSISTENT_EXPRESSION_REFERENCES"
	ErrMetricDependencyCycle            ErrorCode = "METRIC_DEPENDENCY_CYCLE"

	// Metric semantics declared by the model.
	ErrInvalidMetricExtension        ErrorCode = "INVALID_METRIC_EXTENSION"
	ErrInvalidConversionMetric       ErrorCode = "INVALID_CONVERSION_METRIC"
	ErrInvalidMetricDefinitionFilter ErrorCode = "INVALID_METRIC_DEFINITION_FILTER"
	ErrIncompleteCustomCalendar      ErrorCode = "INCOMPLETE_CUSTOM_CALENDAR"
	// A metric rolls up a base metric whose aggregation cannot be merged --
	// holistic, algebraic without retained partial state, or not derivable.
	ErrInvalidMetricRollup ErrorCode = "INVALID_METRIC_ROLLUP"

	// Query and metric semantics that cannot be satisfied together.
	ErrIncompatibleQueryGrain ErrorCode = "INCOMPATIBLE_QUERY_GRAIN"
	// A metric's own root cannot be resolved to exactly one source dataset.
	// This is a property of the metric alone; no query can cause or avoid it.
	ErrUnresolvedMetricSource ErrorCode = "UNRESOLVED_METRIC_SOURCE"
	// The metric's root is known, but the model has no relationship path from
	// it to a dataset the query needs. Distinct from the above: the metric is
	// well-defined, the model is missing a join.
	ErrMetricSourceUnreachable ErrorCode = "METRIC_SOURCE_UNREACHABLE"

	// Capability limits of the selected target or of Metis itself.
	ErrUnsupportedExpression         ErrorCode = "UNSUPPORTED_EXPRESSION"
	ErrUnsupportedSemanticExtension  ErrorCode = "UNSUPPORTED_SEMANTIC_EXTENSION"
	ErrUnsupportedDialect            ErrorCode = "UNSUPPORTED_DIALECT"
	ErrUnsupportedTimeFilter         ErrorCode = "UNSUPPORTED_TIME_FILTER"
	ErrUnsupportedQueryShape         ErrorCode = "UNSUPPORTED_QUERY_SHAPE"
	ErrUnsupportedRelationshipFanout ErrorCode = "UNSUPPORTED_RELATIONSHIP_FANOUT"
	ErrUnsupportedMetricAttribution  ErrorCode = "UNSUPPORTED_METRIC_ATTRIBUTION"
	ErrInconsistentAttributionResult ErrorCode = "INCONSISTENT_ATTRIBUTION_RESULT"
	ErrUnsupportedMetricComparison   ErrorCode = "UNSUPPORTED_METRIC_COMPARISON"
	ErrInconsistentComparisonResult  ErrorCode = "INCONSISTENT_COMPARISON_RESULT"

	// Execution placement.
	ErrExecutionConfigNotFound   ErrorCode = "EXECUTION_CONFIG_NOT_FOUND"
	ErrInvalidExecutionConfig    ErrorCode = "INVALID_EXECUTION_CONFIG"
	ErrTargetRequired            ErrorCode = "TARGET_REQUIRED"
	ErrQueryExecutionUnavailable ErrorCode = "QUERY_EXECUTION_UNAVAILABLE"
	ErrQueryExecutionBusy        ErrorCode = "QUERY_EXECUTION_BUSY"
	ErrQueryExecutionLimit       ErrorCode = "QUERY_EXECUTION_LIMIT_EXCEEDED"
	ErrQueryExecutionTimeout     ErrorCode = "QUERY_EXECUTION_TIMEOUT"
	ErrQueryExecutionCancelled   ErrorCode = "QUERY_EXECUTION_CANCELLED"
	ErrQueryExecutionFailed      ErrorCode = "QUERY_EXECUTION_FAILED"
	ErrQueryResultSchemaMismatch ErrorCode = "QUERY_RESULT_SCHEMA_MISMATCH"

	// Semantic release activation.
	ErrSemanticReleaseNotFound       ErrorCode = "SEMANTIC_RELEASE_NOT_FOUND"
	ErrInvalidSemanticRelease        ErrorCode = "INVALID_SEMANTIC_RELEASE"
	ErrSemanticGenerationConflict    ErrorCode = "SEMANTIC_GENERATION_CONFLICT"
	ErrSemanticActivationUnavailable ErrorCode = "SEMANTIC_ACTIVATION_UNAVAILABLE"
	ErrManagedGitConflict            ErrorCode = "MANAGED_GIT_CONFLICT"
	ErrManagedGitUnavailable         ErrorCode = "MANAGED_GIT_UNAVAILABLE"
	ErrManagedGitNotFound            ErrorCode = "MANAGED_GIT_NOT_FOUND"
	ErrManagedGitRejected            ErrorCode = "MANAGED_GIT_REJECTED"
	ErrSemanticPaginationRestart     ErrorCode = "SEMANTIC_PAGINATION_RESTART_REQUIRED"

	// Authentication.
	ErrUnauthenticated ErrorCode = "UNAUTHENTICATED"

	// Metis defects. See Internal for the rule that selects these.
	ErrInternalInvariant ErrorCode = "INTERNAL_INVARIANT_VIOLATION"
	ErrInternal          ErrorCode = "INTERNAL_ERROR"
)

// Internal reports a condition that no query and no semantic model can produce:
// a nil guard, a pipeline-ordering violation, a structure Metis itself built
// turning out inconsistent, or a fail-closed invariant check catching Metis's
// own defect.
//
// The test for choosing this code is a single question: could any combination
// of user-supplied query and user-authored model reach this line? If one could,
// the failure is the caller's to act on and must keep a client-actionable code
// instead. Reporting a caller-fixable problem as internal strips them of the
// message and details they need, and pollutes server error rates with failures
// that are not faults.
//
// The converse costs just as much in the other direction: reporting a Metis
// defect as a client error hides it from error-rate monitoring and tells the
// caller to fix something they cannot.
func Internal(message string, details map[string]any) *Error {
	return &Error{Code: ErrInternalInvariant, Message: message, Details: details}
}

type Error struct {
	Code        ErrorCode      `json:"code"`
	Message     string         `json:"message"`
	Details     map[string]any `json:"details,omitempty"`
	Suggestions []string       `json:"suggestions,omitempty"`
}

// Payload is the transport-neutral serialized form of a Metis error. REST and
// MCP adapters use this same shape so Agents can depend on code and details
// without parsing transport-specific messages.
type Payload struct {
	Code         string         `json:"code"`
	CallerAction CallerAction   `json:"caller_action"`
	Message      string         `json:"message"`
	Details      map[string]any `json:"details,omitempty"`
	Suggestions  []string       `json:"suggestions,omitempty"`
}

// Codes returns every registered stable error code in deterministic order.
// It is the inventory a machine-readable interface description is generated
// from, and the input to the registration completeness test.
func Codes() []ErrorCode {
	out := make([]ErrorCode, 0, len(callerActionByCode))
	for code := range callerActionByCode {
		out = append(out, code)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// PayloadFrom preserves the stable Metis error contract through wrapped
// errors. Unknown failures retain the existing INTERNAL_ERROR behavior.
func PayloadFrom(err error) Payload {
	var metisErr *Error
	if errors.As(err, &metisErr) {
		return Payload{
			Code:         string(metisErr.Code),
			CallerAction: CallerActionOf(metisErr.Code),
			Message:      metisErr.Message,
			Details:      metisErr.Details,
			Suggestions:  metisErr.Suggestions,
		}
	}
	if err == nil {
		return Payload{}
	}
	return Payload{Code: string(ErrInternal), CallerAction: CallerActionReportDefect, Message: err.Error()}
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}
