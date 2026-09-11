package serrors_test

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"strings"
	"testing"

	"github.com/meaningforge/metis/serrors"
)

// An unregistered code must not inherit a caller-actionable remediation from its
// spelling. Telling a caller to change something they cannot is worse than
// telling them to report it.
func TestUnregisteredCodeFailsClosedToReportDefect(t *testing.T) {
	for _, code := range []serrors.ErrorCode{"FUTURE_NOT_FOUND", "INVALID_FUTURE_THING", "", "UNSUPPORTED_FUTURE"} {
		if got := serrors.CallerActionOf(code); got != serrors.CallerActionReportDefect {
			t.Errorf("CallerActionOf(%q) = %q, want REPORT_DEFECT", code, got)
		}
	}
}

// REPORT_DEFECT is reserved for internal failures plus the explicit public
// attribution reconciliation invariant.
func TestReportDefectNamesOnlyDefectCodes(t *testing.T) {
	for _, code := range serrors.Codes() {
		internal := strings.HasPrefix(string(code), "INTERNAL_") || code == serrors.ErrInconsistentAttributionResult || code == serrors.ErrInconsistentComparisonResult
		defect := serrors.CallerActionOf(code) == serrors.CallerActionReportDefect
		if internal != defect {
			t.Errorf("code %q: internal = %v but action REPORT_DEFECT = %v", code, internal, defect)
		}
	}
}

// The action must be derivable from the code alone. A CallerActionFrom(err) --
// or any exported function taking an error and returning a CallerAction --
// reintroduces runtime context into a published classification, which is the
// problem splitting the codes was done to remove.
func TestNoExportedFunctionDerivesACallerActionFromAnError(t *testing.T) {
	for _, filename := range []string{"caller_action.go", "error.go"} {
		file, err := parser.ParseFile(token.NewFileSet(), filename, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", filename, err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || !fn.Name.IsExported() || fn.Type.Results == nil {
				continue
			}
			if !returnsCallerAction(fn) {
				continue
			}
			for _, param := range fn.Type.Params.List {
				if ident, ok := param.Type.(*ast.Ident); ok && ident.Name == "error" {
					t.Errorf("%s: %s takes an error and returns a CallerAction; derive it from the code instead", filename, fn.Name.Name)
				}
			}
		}
	}
}

func returnsCallerAction(fn *ast.FuncDecl) bool {
	for _, result := range fn.Type.Results.List {
		if ident, ok := result.Type.(*ast.Ident); ok && ident.Name == "CallerAction" {
			return true
		}
	}
	return false
}

// Wrapping must not change the remediation: the code survives errors.As, and
// nothing but the code decides the action.
func TestWrappedErrorsKeepTheirCallerAction(t *testing.T) {
	base := &serrors.Error{Code: serrors.ErrUnresolvedSemanticReference, Message: "boom"}
	wrapped := fmt.Errorf("compile: %w", fmt.Errorf("plan: %w", base))

	payload := serrors.PayloadFrom(wrapped)

	if payload.CallerAction != serrors.CallerActionChangeModel {
		t.Fatalf("caller_action = %q, want CHANGE_MODEL", payload.CallerAction)
	}
	if payload.Code != string(serrors.ErrUnresolvedSemanticReference) {
		t.Fatalf("code = %q", payload.Code)
	}
	if got := serrors.PayloadFrom(errors.New("unstructured")).CallerAction; got != serrors.CallerActionReportDefect {
		t.Fatalf("unknown Go error caller_action = %q, want REPORT_DEFECT", got)
	}
}

// The two lookup conditions #380 separated must land on different remediations.
// If they did not, that split bought nothing.
func TestTheLookupSplitProducesDifferentRemediations(t *testing.T) {
	if serrors.CallerActionOf(serrors.ErrMetricNotFound) != serrors.CallerActionChangeRequest {
		t.Error("METRIC_NOT_FOUND must tell the caller to change the request")
	}
	if serrors.CallerActionOf(serrors.ErrUnresolvedSemanticReference) != serrors.CallerActionChangeModel {
		t.Error("UNRESOLVED_SEMANTIC_REFERENCE must tell the recipient to change the model")
	}
}

// caller_action is a domain value. Nothing in serrors may know about HTTP.
func TestCallerActionCarriesNoTransportVocabulary(t *testing.T) {
	for _, action := range serrors.CallerActions() {
		if _, err := fmt.Sscanf(string(action), "%d", new(int)); err == nil {
			t.Errorf("action %q looks like a status code", action)
		}
		for _, status := range []int{http.StatusBadRequest, http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity, http.StatusInternalServerError} {
			if strings.Contains(string(action), fmt.Sprint(status)) {
				t.Errorf("action %q names an HTTP status", action)
			}
		}
	}
}
