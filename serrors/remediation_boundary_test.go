package serrors_test

import (
	"testing"

	"github.com/meaningforge/metis/serrors"
)

// Each ErrorCode implies one remediation. A code that names a
// pipeline stage cannot satisfy that, because a stage collects whatever fails
// inside it. These names are therefore permanently unavailable rather than
// merely absent, so the bucket cannot quietly return under a new spelling.
func TestRetiredStageNamedCodesStayRetired(t *testing.T) {
	retired := map[string]string{
		"PLANNING_FAILED":               "name the condition; INTERNAL_INVARIANT_VIOLATION when it is Metis's own defect",
		"METRIC_PLANNING_FAILED":        "name the condition; INTERNAL_INVARIANT_VIOLATION when it is Metis's own defect",
		"COMPILATION_FAILED":            "name the condition; INTERNAL_INVARIANT_VIOLATION when it is Metis's own defect",
		"UNSUPPORTED_METRIC_EVALUATION": "attribute each site: internal invariant, INCOMPATIBLE_QUERY_GRAIN, or INVALID_FILTER_VALUE",
	}
	for _, code := range serrors.Codes() {
		if remedy, ok := retired[string(code)]; ok {
			t.Errorf("retired stage-named code %q is registered again: %s", code, remedy)
		}
	}
}

// A lookup code answers "the request named something that does not resolve". A
// model referencing an asset it does not define is a different condition with a
// different remediation, so it must not read as a caller lookup failure.
func TestModelReferenceIsNotALookupFailure(t *testing.T) {
	for _, code := range []serrors.ErrorCode{
		serrors.ErrProjectNotFound, serrors.ErrModelNotFound, serrors.ErrMetricNotFound,
		serrors.ErrDimensionNotFound, serrors.ErrFieldNotFound,
	} {
		if got := serrors.CallerActionOf(code); got != serrors.CallerActionChangeRequest {
			t.Errorf("lookup code %q action = %q, want CHANGE_REQUEST", code, got)
		}
	}
	if got := serrors.CallerActionOf(serrors.ErrUnresolvedSemanticReference); got != serrors.CallerActionChangeModel {
		t.Errorf("UNRESOLVED_SEMANTIC_REFERENCE action = %q, want CHANGE_MODEL", got)
	}
}

// The two metric-source conditions must stay separable: one is a property of the
// metric alone, the other of the model's join graph, and they are fixed by
// editing different parts of the model.
func TestMetricSourceConditionsAreDistinctCodes(t *testing.T) {
	if serrors.ErrUnresolvedMetricSource == serrors.ErrMetricSourceUnreachable {
		t.Fatal("metric-source conditions collapsed into one code")
	}
	for _, code := range []serrors.ErrorCode{serrors.ErrUnresolvedMetricSource, serrors.ErrMetricSourceUnreachable} {
		if got := serrors.CallerActionOf(code); got != serrors.CallerActionChangeModel {
			t.Errorf("%q action = %q, want CHANGE_MODEL", code, got)
		}
	}
}

// An execution-config lookup is remedied by changing the target or deployment,
// not by renaming the project, so it cannot share PROJECT_NOT_FOUND.
func TestExecutionConfigLookupIsNotAProjectLookup(t *testing.T) {
	if serrors.ErrExecutionConfigNotFound == serrors.ErrProjectNotFound {
		t.Fatal("execution-config lookup collapsed into PROJECT_NOT_FOUND")
	}
	if got := serrors.CallerActionOf(serrors.ErrExecutionConfigNotFound); got != serrors.CallerActionChangeTarget {
		t.Errorf("EXECUTION_CONFIG_NOT_FOUND action = %q, want CHANGE_TARGET", got)
	}
}
