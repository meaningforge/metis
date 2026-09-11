package serrors_test

import (
	"os"
	"testing"

	"github.com/meaningforge/metis/serrors"
)

func TestLegacyPkgFacadeDoesNotReappear(t *testing.T) {
	if _, err := os.Stat("../pkg/serrors"); err == nil {
		t.Fatal("pkg/serrors exists; callers must import the root serrors owner directly")
	} else if !os.IsNotExist(err) {
		t.Fatalf("inspect pkg/serrors: %v", err)
	}
}

// The two directions of misclassification cost different things, and both are
// expensive. Reporting a caller-fixable failure as internal strips the caller of
// the message and details they need and inflates server error rates with
// non-faults. Reporting a Metis defect as a client error hides it from
// error-rate monitoring and tells the caller to fix something they cannot.
func TestInternalHelperProducesAnInternalError(t *testing.T) {
	err := serrors.Internal("join source dataset is not present in semantic model", map[string]any{"dataset": "orders"})

	if err.Code != serrors.ErrInternalInvariant {
		t.Fatalf("code = %q, want %q", err.Code, serrors.ErrInternalInvariant)
	}
	if action := serrors.CallerActionOf(err.Code); action != serrors.CallerActionReportDefect {
		t.Fatalf("caller action = %q, want %q", action, serrors.CallerActionReportDefect)
	}
	payload := serrors.PayloadFrom(err)
	if payload.CallerAction != serrors.CallerActionReportDefect {
		t.Fatalf("payload caller_action = %q", payload.CallerAction)
	}
	if payload.Details["dataset"] != "orders" {
		t.Fatalf("details lost: %#v", payload.Details)
	}
}

// The codes that replaced the old planning and compilation buckets stay
// client-actionable. A caller reaches them with a bad grain, an invalid filter
// value, or a model that declares an inconsistent extension, and in every one
// of those cases the message and details are what lets them fix it.
func TestPlanningAndCompilationCodesRemainClientActionable(t *testing.T) {
	for code, want := range map[serrors.ErrorCode]serrors.CallerAction{
		serrors.ErrIncompatibleQueryGrain: serrors.CallerActionChangeRequest,
		serrors.ErrInvalidFilterValue:     serrors.CallerActionChangeRequest,
		serrors.ErrInvalidSort:            serrors.CallerActionChangeRequest,

		serrors.ErrInvalidMetricExtension:        serrors.CallerActionChangeModel,
		serrors.ErrInvalidConversionMetric:       serrors.CallerActionChangeModel,
		serrors.ErrInvalidMetricDefinitionFilter: serrors.CallerActionChangeModel,
		serrors.ErrIncompleteCustomCalendar:      serrors.CallerActionChangeModel,
		serrors.ErrMetricSourceUnreachable:       serrors.CallerActionChangeModel,
		serrors.ErrInvalidRelationship:           serrors.CallerActionChangeModel,
	} {
		if got := serrors.CallerActionOf(code); got != want {
			t.Errorf("code %q action = %q, want %q", code, got, want)
		}
		if got := serrors.CallerActionOf(code); got == serrors.CallerActionReportDefect {
			t.Errorf("code %q became a Metis defect; it is reachable from a query or a model", code)
		}
	}
}

func TestInternalInvariantIsDistinctFromGenericInternalError(t *testing.T) {
	if serrors.ErrInternalInvariant == serrors.ErrInternal {
		t.Fatal("invariant violations and generic internal errors share a code")
	}
	if serrors.CallerActionOf(serrors.ErrInternalInvariant) != serrors.CallerActionOf(serrors.ErrInternal) {
		t.Fatal("both internal codes must recommend the same remediation")
	}
}
