package serrors

import "sort"

// CallerAction is the next machine-actionable remediation category Metis can
// recommend for a deterministic failure.
//
// It answers a question the error code alone does not: given that this failed,
// what should the recipient do next? An Agent that gets CHANGE_REQUEST should
// retry with a different query; one that gets CHANGE_MODEL should stop retrying
// and escalate, because the artifact at fault is not one it can edit and another
// attempt will fail identically.
//
// The five values do not sit on one axis. Three name an artifact to edit and two
// name an action with no artifact. That asymmetry is deliberate: making the
// taxonomy uniform would cost the property that matters, which is that every
// value names something the recipient can actually do next. AUTHENTICATE is not
// "who changes what" and is nonetheless an unambiguous remediation.
//
// CallerAction is a domain concept, not a transport one. HTTP status is a
// separate projection of the same code, owned by the REST adapter.
type CallerAction string

const (
	// The submitted query or request parameters must change.
	CallerActionChangeRequest CallerAction = "CHANGE_REQUEST"
	// The semantic model must change. The caller cannot act.
	CallerActionChangeModel CallerAction = "CHANGE_MODEL"
	// The engine, dialect, or execution binding must change.
	CallerActionChangeTarget CallerAction = "CHANGE_TARGET"
	// Credentials must be supplied or refreshed.
	CallerActionAuthenticate CallerAction = "AUTHENTICATE"
	// Nothing outside Metis can fix this.
	CallerActionReportDefect CallerAction = "REPORT_DEFECT"
)

// callerActionByCode is the authoritative code-to-action registration table.
//
// Every entry is derived from what the code's call sites actually report, never
// from the spelling of its name. A registration test asserts that this table's
// key set and the declared ErrorCode set are equal, so a new code cannot ship
// without an explicit decision about what its recipient should do.
//
// This table maps each error code to exactly one caller action. When call
// sites of a code would need different actions, the code is too broad and must
// be split rather than given a runtime-dependent action here.
var callerActionByCode = map[ErrorCode]CallerAction{
	// The request named it, malformed it, or asked for something its own shape
	// makes impossible. The caller holds the artifact that must change.
	ErrInvalidQuery:                  CallerActionChangeRequest,
	ErrInvalidFilterValue:            CallerActionChangeRequest,
	ErrInvalidSort:                   CallerActionChangeRequest,
	ErrProjectRequired:               CallerActionChangeRequest,
	ErrIncompatibleQueryGrain:        CallerActionChangeRequest,
	ErrUnsupportedTimeFilter:         CallerActionChangeRequest,
	ErrUnsupportedQueryShape:         CallerActionChangeRequest,
	ErrUnsupportedRelationshipFanout: CallerActionChangeRequest,
	ErrUnsupportedMetricAttribution:  CallerActionChangeRequest,
	ErrUnsupportedMetricComparison:   CallerActionChangeRequest,
	ErrProjectNotFound:               CallerActionChangeRequest,
	ErrProjectAccessDenied:           CallerActionChangeRequest,
	ErrDataAccessDenied:              CallerActionChangeRequest,
	ErrDataAccessPolicyUnavailable:   CallerActionChangeTarget,
	ErrInvalidDataAccessPolicy:       CallerActionChangeTarget,
	ErrModelNotFound:                 CallerActionChangeRequest,
	ErrMetricNotFound:                CallerActionChangeRequest,
	ErrDimensionNotFound:             CallerActionChangeRequest,
	ErrFieldNotFound:                 CallerActionChangeRequest,
	// Ambiguity the caller resolves by qualifying what they asked for.
	ErrAmbiguousField:               CallerActionChangeRequest,
	ErrAmbiguousExpressionReference: CallerActionChangeRequest,
	ErrAmbiguousMetricSource:        CallerActionChangeRequest,

	// The semantic model is malformed, incomplete, or internally inconsistent.
	// No query avoids these and no target choice changes them.
	ErrInvalidModel:                     CallerActionChangeModel,
	ErrInvalidRelationship:              CallerActionChangeModel,
	ErrInvalidMetricExtension:           CallerActionChangeModel,
	ErrInvalidConversionMetric:          CallerActionChangeModel,
	ErrInvalidMetricDefinitionFilter:    CallerActionChangeModel,
	ErrIncompleteCustomCalendar:         CallerActionChangeModel,
	ErrInvalidMetricRollup:              CallerActionChangeModel,
	ErrInconsistentExpressionReferences: CallerActionChangeModel,
	ErrMetricDependencyCycle:            CallerActionChangeModel,
	ErrExpressionParseFailed:            CallerActionChangeModel,
	ErrSemanticAnalysisFailed:           CallerActionChangeModel,
	ErrUnresolvedSemanticReference:      CallerActionChangeModel,
	ErrUnresolvedMetricSource:           CallerActionChangeModel,
	// The model does not connect two datasets the query needs together. The
	// caller could ask for less instead, but that abandons the request rather
	// than remedying the reported condition: the datasets are still unjoined.
	ErrMetricSourceUnreachable: CallerActionChangeModel,
	ErrRelationshipNotFound:    CallerActionChangeModel,
	// More than one join path exists; only the model author can remove one.
	ErrAmbiguousRelationshipPath: CallerActionChangeModel,
	// A semantic-critical extension the model declares is not supported. Metis
	// will not silently drop it, and no choice of engine changes that.
	ErrUnsupportedSemanticExtension: CallerActionChangeModel,
	// The model declares no expression usable for the selected target. Metis's
	// own suggestions on this code say to add one, so CHANGE_MODEL is what it
	// already recommends in prose.
	ErrUnsupportedExpression: CallerActionChangeModel,

	// The target, its binding, or its configuration is what must change.
	ErrTargetRequired:                CallerActionChangeTarget,
	ErrAmbiguousTarget:               CallerActionChangeTarget,
	ErrExecutionConfigNotFound:       CallerActionChangeTarget,
	ErrInvalidExecutionConfig:        CallerActionChangeTarget,
	ErrEngineNotFound:                CallerActionChangeTarget,
	ErrUnsupportedDialect:            CallerActionChangeTarget,
	ErrQueryExecutionUnavailable:     CallerActionChangeTarget,
	ErrQueryExecutionBusy:            CallerActionChangeRequest,
	ErrQueryExecutionLimit:           CallerActionChangeRequest,
	ErrQueryExecutionTimeout:         CallerActionChangeRequest,
	ErrQueryExecutionCancelled:       CallerActionChangeRequest,
	ErrQueryExecutionFailed:          CallerActionChangeTarget,
	ErrQueryResultSchemaMismatch:     CallerActionChangeTarget,
	ErrSemanticActivationUnavailable: CallerActionChangeTarget,
	ErrManagedGitConflict:            CallerActionChangeRequest,
	ErrManagedGitUnavailable:         CallerActionChangeTarget,
	ErrManagedGitNotFound:            CallerActionChangeRequest,
	ErrManagedGitRejected:            CallerActionChangeModel,
	ErrOntologyResolutionUnavailable: CallerActionChangeTarget,

	ErrSemanticReleaseNotFound:    CallerActionChangeRequest,
	ErrInvalidSemanticRelease:     CallerActionChangeRequest,
	ErrSemanticGenerationConflict: CallerActionChangeRequest,
	ErrSemanticPaginationRestart:  CallerActionChangeRequest,

	ErrUnauthenticated: CallerActionAuthenticate,

	ErrInternalInvariant:             CallerActionReportDefect,
	ErrInternal:                      CallerActionReportDefect,
	ErrInconsistentAttributionResult: CallerActionReportDefect,
	ErrInconsistentComparisonResult:  CallerActionReportDefect,
}

// CallerActionOf returns the remediation for a stable Metis error code.
//
// The signature is deliberate. Taking a code rather than an error keeps
// code -> caller_action a pure function, and therefore a contract a client can
// reason about before it ever sees a failure. A CallerActionFrom(err) that
// consulted runtime context would make the same code arrive with different
// actions on different calls, and the published contract would diverge from the
// moment it shipped.
//
// Unregistered codes fail closed to REPORT_DEFECT: a code nobody classified is
// one nobody decided the caller can act on, and telling them to change
// something they cannot is worse than telling them to report it.
func CallerActionOf(code ErrorCode) CallerAction {
	if action, ok := callerActionByCode[code]; ok {
		return action
	}
	return CallerActionReportDefect
}

// CallerActions returns every registered action in deterministic order.
func CallerActions() []CallerAction {
	seen := map[CallerAction]struct{}{}
	for _, action := range callerActionByCode {
		seen[action] = struct{}{}
	}
	out := make([]CallerAction, 0, len(seen))
	for action := range seen {
		out = append(out, action)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
