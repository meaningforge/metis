package serrors_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"strconv"
	"testing"

	"github.com/meaningforge/metis/serrors"
)

// declaredErrorCodes reads the ErrorCode constants straight from the source of
// truth. Go cannot enumerate constants at runtime, so a code declared but never
// registered would otherwise be invisible: CallerActionOf falls back to
// REPORT_DEFECT and the REST projection falls back to 500, so the omission
// first shows up as a client-facing server error on a failure that is not a
// server fault.
func declaredErrorCodes(t *testing.T) map[serrors.ErrorCode]string {
	t.Helper()
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, "error.go", nil, 0)
	if err != nil {
		t.Fatalf("parse error.go: %v", err)
	}

	declared := map[serrors.ErrorCode]string{}
	for _, decl := range file.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.CONST {
			continue
		}
		for _, spec := range genDecl.Specs {
			valueSpec, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			ident, ok := valueSpec.Type.(*ast.Ident)
			if !ok || ident.Name != "ErrorCode" {
				continue
			}
			for i, name := range valueSpec.Names {
				if i >= len(valueSpec.Values) {
					continue
				}
				literal, ok := valueSpec.Values[i].(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					t.Fatalf("%s is not a string literal error code", name.Name)
				}
				value, err := strconv.Unquote(literal.Value)
				if err != nil {
					t.Fatalf("unquote %s: %v", name.Name, err)
				}
				declared[serrors.ErrorCode(value)] = name.Name
			}
		}
	}
	if len(declared) == 0 {
		t.Fatal("no ErrorCode constants found; the parser no longer matches the source")
	}
	return declared
}

func TestEveryDeclaredErrorCodeIsRegistered(t *testing.T) {
	declared := declaredErrorCodes(t)
	registered := map[serrors.ErrorCode]struct{}{}
	for _, code := range serrors.Codes() {
		registered[code] = struct{}{}
	}

	for code, identifier := range declared {
		if _, ok := registered[code]; !ok {
			t.Errorf("%s (%q) is not registered; add it to callerActionByCode", identifier, code)
		}
	}
}

func TestNoRegisteredCodeIsUndeclared(t *testing.T) {
	declared := declaredErrorCodes(t)
	for _, code := range serrors.Codes() {
		if _, ok := declared[code]; !ok {
			t.Errorf("registered code %q has no ErrorCode constant", code)
		}
	}
}

func TestEveryRegisteredCodeResolvesToARealCallerAction(t *testing.T) {
	valid := map[serrors.CallerAction]struct{}{
		serrors.CallerActionChangeRequest: {},
		serrors.CallerActionChangeModel:   {},
		serrors.CallerActionChangeTarget:  {},
		serrors.CallerActionAuthenticate:  {},
		serrors.CallerActionReportDefect:  {},
	}
	for _, code := range serrors.Codes() {
		action := serrors.CallerActionOf(code)
		if _, ok := valid[action]; !ok {
			t.Errorf("code %q resolves to unknown caller action %q", code, action)
		}
		if action == serrors.CallerActionReportDefect &&
			code != serrors.ErrInternal && code != serrors.ErrInternalInvariant && code != serrors.ErrInconsistentAttributionResult && code != serrors.ErrInconsistentComparisonResult {
			t.Errorf("code %q tells the recipient to report a defect without being an approved defect code", code)
		}
	}
}

func TestCodesIsDeterministicallyOrdered(t *testing.T) {
	first := serrors.Codes()
	second := serrors.Codes()
	if len(first) != len(second) {
		t.Fatalf("inventory length changed between calls: %d then %d", len(first), len(second))
	}
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("inventory order changed at %d: %q then %q", i, first[i], second[i])
		}
		if i > 0 && first[i-1] >= first[i] {
			t.Fatalf("inventory is not sorted at %d: %q then %q", i, first[i-1], first[i])
		}
	}
}

func TestUnregisteredCodeFailsClosedAsInternal(t *testing.T) {
	if action := serrors.CallerActionOf(serrors.ErrorCode("NEWLY_INVENTED_CODE")); action != serrors.CallerActionReportDefect {
		t.Fatalf("unregistered code action = %q, want %q", action, serrors.CallerActionReportDefect)
	}
}

var screamingSnake = regexp.MustCompile(`^[A-Z][A-Z0-9]*(_[A-Z0-9]+)*$`)

// Codes and actions share one casing convention so a single payload never mixes
// two. Go identifiers stay MixedCaps; the convention here governs wire values,
// which is the same split gRPC and the Google API design guide use.
func TestCodesAndCallerActionsUseOneCasingConvention(t *testing.T) {
	for _, code := range serrors.Codes() {
		if !screamingSnake.MatchString(string(code)) {
			t.Errorf("code %q is not SCREAMING_SNAKE_CASE", code)
		}
	}
	for _, action := range serrors.CallerActions() {
		if !screamingSnake.MatchString(string(action)) {
			t.Errorf("caller action %q is not SCREAMING_SNAKE_CASE", action)
		}
	}
}

// An action is what to do; a code is what happened. Sharing a string would make
// {"code":"X","caller_action":"X"} read as a serialization bug rather than two
// facts. UNAUTHENTICATED was the one collision the class vocabulary carried, and
// the action vocabulary avoids it: the action is AUTHENTICATE, an instruction,
// while the code stays UNAUTHENTICATED, a state.
func TestCallerActionStringsDoNotCollideWithCodeStrings(t *testing.T) {
	codes := map[string]struct{}{}
	for _, code := range serrors.Codes() {
		codes[string(code)] = struct{}{}
	}
	for _, action := range serrors.CallerActions() {
		if _, collides := codes[string(action)]; collides {
			t.Errorf("caller action %q is also an error code; give the action a distinct name", action)
		}
	}
}
