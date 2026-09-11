package expression

import (
	"errors"
	"strings"
	"testing"
)

func TestParseErrorErrorFormatsStableDiagnosticFields(t *testing.T) {
	err := (&ParseError{
		Code:     ParseExpectedToken,
		Message:  "expected closing parenthesis",
		Span:     SourceSpan{Start: 4, End: 5},
		Expected: []string{")", ","},
		Found:    "]",
	}).Error()

	for _, want := range []string{
		"expected closing parenthesis",
		"expected ) or ,",
		`found "]"`,
		"at [4,5)",
	} {
		if !strings.Contains(err, want) {
			t.Fatalf("Error() = %q, want containing %q", err, want)
		}
	}
}

func TestParseErrorErrorFallsBackToCode(t *testing.T) {
	if got := (&ParseError{Code: ParseInvalidSyntax}).Error(); !strings.Contains(got, string(ParseInvalidSyntax)) {
		t.Fatalf("Error() = %q, want code %q", got, ParseInvalidSyntax)
	}
}

func TestParseErrorErrorHandlesNilReceiver(t *testing.T) {
	var err *ParseError
	if got := err.Error(); got != "<nil>" {
		t.Fatalf("Error() = %q, want <nil>", got)
	}
}

func TestIsParseErrorOnlyMatchesParserDiagnostics(t *testing.T) {
	if !IsParseError(&ParseError{Code: ParseUnexpectedToken}) {
		t.Fatal("IsParseError(ParseError) = false, want true")
	}
	if IsParseError(errors.New("syntax failed")) {
		t.Fatal("IsParseError(generic error) = true, want false")
	}
}
