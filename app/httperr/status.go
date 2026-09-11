// Package httperr owns the HTTP projection of the shared Metis error contract.
//
// It is a package rather than a function inside app/rest because more than one
// HTTP entry point produces Metis errors: the REST handlers, and the bearer
// middleware in app/auth that rejects a request before any handler runs. Those
// two packages do not and should not import each other -- a transport-generic
// auth package depending on REST handlers would be backwards -- so the
// projection lives where both can reach it and neither owns it.
//
// Duplicating status mappings would give one error meaning two definitions
// that can drift. A completeness test here cannot see a status hardcoded in another
// package, so the drift would be silent.

package httperr

import (
	"net/http"

	"github.com/meaningforge/metis/serrors"
)

// statusByCode is this transport's projection of the shared error contract.
//
// It lives here, not in serrors, because a status code is HTTP's vocabulary
// and nothing in the semantic core should have an opinion about it. The table
// HTTP needs and the remediation an Agent needs answer different questions about
// the same code, and neither is derivable from the other: METRIC_NOT_FOUND and
// RELATIONSHIP_NOT_FOUND are both 404 with different remediations, while
// CHANGE_TARGET spans 404, 409, and 422.
//
// Every status is written out rather than inferred from a code's name or from
// its caller action. Names are identifiers, not a type hierarchy, and a code
// must never acquire protocol behavior from its spelling.
var statusByCode = map[serrors.ErrorCode]int{
	// The request named something that does not resolve.
	serrors.ErrProjectNotFound:   http.StatusNotFound,
	serrors.ErrModelNotFound:     http.StatusNotFound,
	serrors.ErrMetricNotFound:    http.StatusNotFound,
	serrors.ErrDimensionNotFound: http.StatusNotFound,
	serrors.ErrFieldNotFound:     http.StatusNotFound,
	// The model does not connect two datasets, or has no execution config or
	// engine registered. Nothing named by the request is missing, but the
	// resource the request needs does not exist either.
	serrors.ErrRelationshipNotFound:    http.StatusNotFound,
	serrors.ErrExecutionConfigNotFound: http.StatusNotFound,
	serrors.ErrEngineNotFound:          http.StatusNotFound,

	// Malformed input: the request, the model, or the execution manifest.
	serrors.ErrInvalidQuery:                     http.StatusBadRequest,
	serrors.ErrInvalidFilterValue:               http.StatusBadRequest,
	serrors.ErrInvalidSort:                      http.StatusBadRequest,
	serrors.ErrProjectRequired:                  http.StatusBadRequest,
	serrors.ErrTargetRequired:                   http.StatusBadRequest,
	serrors.ErrInvalidModel:                     http.StatusBadRequest,
	serrors.ErrInvalidRelationship:              http.StatusBadRequest,
	serrors.ErrInvalidExecutionConfig:           http.StatusBadRequest,
	serrors.ErrInvalidMetricExtension:           http.StatusBadRequest,
	serrors.ErrInvalidConversionMetric:          http.StatusBadRequest,
	serrors.ErrInvalidMetricDefinitionFilter:    http.StatusBadRequest,
	serrors.ErrIncompleteCustomCalendar:         http.StatusBadRequest,
	serrors.ErrInvalidMetricRollup:              http.StatusBadRequest,
	serrors.ErrIncompatibleQueryGrain:           http.StatusBadRequest,
	serrors.ErrMetricDependencyCycle:            http.StatusBadRequest,
	serrors.ErrExpressionParseFailed:            http.StatusBadRequest,
	serrors.ErrSemanticAnalysisFailed:           http.StatusBadRequest,
	serrors.ErrInconsistentExpressionReferences: http.StatusBadRequest,
	serrors.ErrUnresolvedSemanticReference:      http.StatusBadRequest,
	serrors.ErrUnresolvedMetricSource:           http.StatusBadRequest,
	serrors.ErrMetricSourceUnreachable:          http.StatusBadRequest,

	// Metis will not guess between candidates it can both see.
	serrors.ErrAmbiguousField:               http.StatusConflict,
	serrors.ErrAmbiguousExpressionReference: http.StatusConflict,
	serrors.ErrAmbiguousMetricSource:        http.StatusConflict,
	serrors.ErrAmbiguousRelationshipPath:    http.StatusConflict,
	serrors.ErrAmbiguousTarget:              http.StatusConflict,

	// Understood and well-formed, but the selected target cannot express it.
	// 400 would say the caller sent something malformed, which is not what
	// happened and is not actionable.
	serrors.ErrUnsupportedExpression:         http.StatusUnprocessableEntity,
	serrors.ErrUnsupportedSemanticExtension:  http.StatusUnprocessableEntity,
	serrors.ErrUnsupportedDialect:            http.StatusUnprocessableEntity,
	serrors.ErrUnsupportedTimeFilter:         http.StatusUnprocessableEntity,
	serrors.ErrUnsupportedQueryShape:         http.StatusUnprocessableEntity,
	serrors.ErrUnsupportedRelationshipFanout: http.StatusUnprocessableEntity,

	serrors.ErrUnauthenticated:               http.StatusUnauthorized,
	serrors.ErrProjectAccessDenied:           http.StatusForbidden,
	serrors.ErrDataAccessDenied:              http.StatusForbidden,
	serrors.ErrDataAccessPolicyUnavailable:   http.StatusUnprocessableEntity,
	serrors.ErrOntologyResolutionUnavailable: http.StatusUnprocessableEntity,
	serrors.ErrInvalidDataAccessPolicy:       http.StatusUnprocessableEntity,
	serrors.ErrQueryExecutionUnavailable:     http.StatusUnprocessableEntity,
	serrors.ErrQueryExecutionBusy:            http.StatusTooManyRequests,
	serrors.ErrQueryExecutionLimit:           http.StatusTooManyRequests,
	serrors.ErrQueryExecutionTimeout:         http.StatusRequestTimeout,
	serrors.ErrQueryExecutionCancelled:       http.StatusRequestTimeout,
	serrors.ErrQueryExecutionFailed:          http.StatusUnprocessableEntity,
	serrors.ErrQueryResultSchemaMismatch:     http.StatusUnprocessableEntity,
	serrors.ErrUnsupportedMetricAttribution:  http.StatusUnprocessableEntity,
	serrors.ErrInconsistentAttributionResult: http.StatusInternalServerError,
	serrors.ErrUnsupportedMetricComparison:   http.StatusUnprocessableEntity,
	serrors.ErrInconsistentComparisonResult:  http.StatusInternalServerError,
	serrors.ErrSemanticReleaseNotFound:       http.StatusNotFound,
	serrors.ErrInvalidSemanticRelease:        http.StatusUnprocessableEntity,
	serrors.ErrSemanticGenerationConflict:    http.StatusConflict,
	serrors.ErrSemanticPaginationRestart:     http.StatusConflict,
	serrors.ErrSemanticActivationUnavailable: http.StatusUnprocessableEntity,
	serrors.ErrManagedGitConflict:            http.StatusConflict,
	serrors.ErrManagedGitUnavailable:         http.StatusUnprocessableEntity,
	serrors.ErrManagedGitNotFound:            http.StatusNotFound,
	serrors.ErrManagedGitRejected:            http.StatusUnprocessableEntity,

	serrors.ErrInternalInvariant: http.StatusInternalServerError,
	serrors.ErrInternal:          http.StatusInternalServerError,
}

// StatusOf projects a stable Metis error code onto an HTTP status.
//
// Unregistered codes fail closed to 500. A code nobody mapped is one nobody
// decided was safe to expose as a client error, and inferring one from its name
// is exactly the substring matching this table exists to prevent.
func StatusOf(code serrors.ErrorCode) int {
	if status, ok := statusByCode[code]; ok {
		return status
	}
	return http.StatusInternalServerError
}

// StatusFor recovers the code from a possibly wrapped error and projects it.
// Recovering the code is not runtime-context dependence: nothing but the code
// decides the status, exactly as nothing but the code decides the remediation.
func StatusFor(err error) int {
	return StatusOf(serrors.ErrorCode(serrors.PayloadFrom(err).Code))
}
