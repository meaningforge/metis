package expression

import (
	"strings"
	"testing"
)

func TestSemanticErrorErrorFormatsStableFields(t *testing.T) {
	err := (&SemanticError{
		Code:    ErrTypeMismatch,
		Message: "AND expects BOOLEAN operands",
		Span:    SourceSpan{Start: 3, End: 9},
	}).Error()

	for _, want := range []string{
		string(ErrTypeMismatch),
		"AND expects BOOLEAN operands",
		"at [3,9)",
	} {
		if !strings.Contains(err, want) {
			t.Fatalf("Error() = %q, want containing %q", err, want)
		}
	}
}

func TestSemanticErrorErrorHandlesNilReceiver(t *testing.T) {
	var err *SemanticError
	if got := err.Error(); got != "" {
		t.Fatalf("Error() = %q, want empty string", got)
	}
}
