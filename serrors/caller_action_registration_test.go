package serrors

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"testing"
)

// Every declared code must answer "what should the recipient do next?" without
// consulting anything but itself. This is the acceptance condition for the
// A code that cannot determine a caller action statically is still too broad and
// must be split rather than registered with a guess.
//
// This test is internal on purpose. CallerActionOf fails closed to
// REPORT_DEFECT, so an external test asserting "the action is valid" would pass
// for a code nobody registered. Completeness can only be checked against the
// table's key set.
func TestEveryDeclaredCodeIsRegisteredForACallerAction(t *testing.T) {
	declared := declaredErrorCodes(t)
	if len(declared) == 0 {
		t.Fatal("no ErrorCode constants parsed from error.go")
	}
	for _, code := range declared {
		if _, ok := callerActionByCode[code]; !ok {
			t.Errorf("declared code %q has no registered CallerAction", code)
		}
	}
	declaredSet := make(map[ErrorCode]struct{}, len(declared))
	for _, code := range declared {
		declaredSet[code] = struct{}{}
	}
	for code := range callerActionByCode {
		if _, ok := declaredSet[code]; !ok {
			t.Errorf("code %q has a registered action but is not a declared ErrorCode", code)
		}
	}
	if len(callerActionByCode) != len(declared) {
		t.Errorf("registered %d actions for %d declared codes", len(callerActionByCode), len(declared))
	}
}

func declaredErrorCodes(t *testing.T) []ErrorCode {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "error.go", nil, 0)
	if err != nil {
		t.Fatalf("parse error.go: %v", err)
	}
	var out []ErrorCode
	ast.Inspect(file, func(n ast.Node) bool {
		spec, ok := n.(*ast.ValueSpec)
		if !ok {
			return true
		}
		if ident, ok := spec.Type.(*ast.Ident); !ok || ident.Name != "ErrorCode" {
			return true
		}
		for _, value := range spec.Values {
			lit, ok := value.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				continue
			}
			unquoted, err := strconv.Unquote(lit.Value)
			if err != nil {
				t.Fatalf("unquote %s: %v", lit.Value, err)
			}
			out = append(out, ErrorCode(unquoted))
		}
		return true
	})
	return out
}
