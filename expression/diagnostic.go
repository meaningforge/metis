package expression

import (
	"fmt"
	"strings"
)

type ParseErrorCode string

const (
	ParseUnexpectedToken ParseErrorCode = "UNEXPECTED_TOKEN"
	ParseExpectedToken   ParseErrorCode = "EXPECTED_TOKEN"
	ParseInvalidSyntax   ParseErrorCode = "INVALID_SYNTAX"
	ParseInvalidLambda   ParseErrorCode = "INVALID_LAMBDA"
	ParseInvalidType     ParseErrorCode = "INVALID_TYPE"
	ParseUnexpectedEOF   ParseErrorCode = "UNEXPECTED_EOF"
)

// ParseError is the stable syntax-diagnostic contract exposed by the native
// expression parser. SemanticManifest-level semantic errors intentionally remain
// separate from parser syntax errors.
type ParseError struct {
	Code     ParseErrorCode
	Message  string
	Span     SourceSpan
	Expected []string
	Found    string
}

func (e *ParseError) Error() string {
	if e == nil {
		return "<nil>"
	}
	msg := e.Message
	if msg == "" {
		msg = string(e.Code)
	}
	if len(e.Expected) > 0 {
		msg += "; expected " + strings.Join(e.Expected, " or ")
	}
	if e.Found != "" {
		msg += fmt.Sprintf(", found %q", e.Found)
	}
	if e.Span.Valid() {
		msg += fmt.Sprintf(" at [%d,%d)", e.Span.Start, e.Span.End)
	}
	return msg
}

func IsParseError(err error) bool {
	_, ok := err.(*ParseError)
	return ok
}
