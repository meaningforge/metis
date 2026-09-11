package httperr

import (
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/meaningforge/metis/serrors"
)

// The projection must be total. A code with no status silently becomes a 500,
// which reports a caller's mistake as a server fault and pollutes error rates
// with failures that are not faults.
func TestEveryErrorCodeHasARegisteredHTTPStatus(t *testing.T) {
	for _, code := range serrors.Codes() {
		if _, ok := statusByCode[code]; !ok {
			t.Errorf("code %q has no registered HTTP status; add it to statusByCode", code)
		}
	}
	if len(statusByCode) != len(serrors.Codes()) {
		t.Errorf("status table has %d codes, the contract declares %d", len(statusByCode), len(serrors.Codes()))
	}
	for code := range statusByCode {
		if serrors.CallerActionOf(code) == serrors.CallerActionReportDefect && code != serrors.ErrInternal && code != serrors.ErrInternalInvariant && code != serrors.ErrInconsistentAttributionResult && code != serrors.ErrInconsistentComparisonResult {
			t.Errorf("status table contains %q, which is not a declared error code", code)
		}
	}
}

// A code nobody mapped must not acquire a client-facing status from its name.
// This is the substring matching the explicit table exists to prevent.
func TestUnregisteredCodeFailsClosedToServerError(t *testing.T) {
	for _, code := range []serrors.ErrorCode{"FUTURE_NOT_FOUND", "INVALID_FUTURE", "AMBIGUOUS_FUTURE", ""} {
		if got := StatusOf(code); got != http.StatusInternalServerError {
			t.Errorf("StatusOf(%q) = %d, want 500", code, got)
		}
	}
}

// Only a Metis defect may be a 5xx. Any other code reaching 500 tells an
// operator that Metis is broken when the caller or the model is what needs to
// change, and hides real defects among the noise.
func TestOnlyReportDefectCodesMapToServerErrors(t *testing.T) {
	for code, status := range statusByCode {
		serverError := status >= 500
		defect := serrors.CallerActionOf(code) == serrors.CallerActionReportDefect
		if serverError != defect {
			t.Errorf("code %q maps to %d but its action is %q; only REPORT_DEFECT may be 5xx", code, status, serrors.CallerActionOf(code))
		}
	}
}

// The two projections must stay independent in both directions, because that
// independence is the whole reason they are separate tables. If either could be
// computed from the other, one of them is redundant.
func TestStatusAndCallerActionAreIndependentProjections(t *testing.T) {
	actionsPerStatus := map[int]map[serrors.CallerAction]struct{}{}
	statusesPerAction := map[serrors.CallerAction]map[int]struct{}{}
	for code, status := range statusByCode {
		action := serrors.CallerActionOf(code)
		if actionsPerStatus[status] == nil {
			actionsPerStatus[status] = map[serrors.CallerAction]struct{}{}
		}
		actionsPerStatus[status][action] = struct{}{}
		if statusesPerAction[action] == nil {
			statusesPerAction[action] = map[int]struct{}{}
		}
		statusesPerAction[action][status] = struct{}{}
	}

	sharedStatus := false
	for _, actions := range actionsPerStatus {
		if len(actions) > 1 {
			sharedStatus = true
		}
	}
	if !sharedStatus {
		t.Error("no HTTP status covers more than one remediation; caller_action would add nothing over status")
	}

	spreadAction := false
	for _, statuses := range statusesPerAction {
		if len(statuses) > 1 {
			spreadAction = true
		}
	}
	if !spreadAction {
		t.Error("no remediation spans more than one HTTP status; the status table would be derivable from the action")
	}
}

// The error-code pair, asserted so a future retagging cannot
// quietly collapse it: same status, opposite advice about whether the caller can
// do anything at all.
func TestSameStatusCanCarryOppositeRemediations(t *testing.T) {
	if statusByCode[serrors.ErrMetricNotFound] != statusByCode[serrors.ErrRelationshipNotFound] {
		t.Fatal("expected METRIC_NOT_FOUND and RELATIONSHIP_NOT_FOUND to share a status")
	}
	if serrors.CallerActionOf(serrors.ErrMetricNotFound) == serrors.CallerActionOf(serrors.ErrRelationshipNotFound) {
		t.Fatal("METRIC_NOT_FOUND and RELATIONSHIP_NOT_FOUND must not share a remediation")
	}
}

// This package is the only place an HTTP error status may be chosen.
//
// The completeness test above proves the table covers every code; it cannot see
// a status literal written somewhere else. That gap is how the auth middleware
// came to define UNAUTHENTICATED -> 401 a second time, and how three compile
// handlers came to hardcode 400 beside a payload built by hand, all while every
// test stayed green.
//
// The invariant is deliberately blunt: no non-test file outside this package may
// name a 4xx or 5xx status at all. A narrower rule -- "only flag a status on the
// same line as an error code" -- was tried first and missed the auth case, whose
// line named serrors.PayloadFrom rather than a code. Success statuses stay
// allowed; they are not a projection of anything.
func TestNoOtherPackageNamesAnHTTPErrorStatus(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	// http.StatusText reports "" for a non-status integer, which makes the set
	// of real error statuses derivable rather than hand-listed.
	errorStatusNames := map[string]struct{}{}
	for code := 400; code < 600; code++ {
		if name := http.StatusText(code); name != "" {
			errorStatusNames["http.Status"+strings.NewReplacer(" ", "", "-", "").Replace(name)] = struct{}{}
		}
	}
	statusRef := regexp.MustCompile(`http\.Status[A-Za-z]+`)

	var offenders []string
	walkErr := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "vendor", "testdata", "httperr":
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(source), "\n") {
			for _, ref := range statusRef.FindAllString(line, -1) {
				if _, isError := errorStatusNames[ref]; !isError {
					continue
				}
				rel, _ := filepath.Rel(root, path)
				offenders = append(offenders, fmt.Sprintf("%s:%d: %s", rel, i+1, strings.TrimSpace(line)))
			}
		}
		return nil
	})
	if walkErr != nil {
		t.Fatal(walkErr)
	}
	for _, offender := range offenders {
		t.Errorf("HTTP error status named outside app/httperr: %s\n\treturn a serrors.Error and let httperr.StatusOf project it", offender)
	}
}
