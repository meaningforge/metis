package query

import (
	"encoding/json"
	"fmt"
	"regexp"
)

var filterNumberSyntax = regexp.MustCompile(`^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]{1,3})?$`)

// ParseFilterNumber validates a bounded decimal literal and preserves its
// original text. Public filter operands arrive as strings; datatype-aware
// normalization belongs to semantic resolution and must not round through
// float64 first. Errors intentionally omit the operand.
func ParseFilterNumber(text string) (json.Number, error) {
	if len(text) > 256 || !filterNumberSyntax.MatchString(text) {
		return "", fmt.Errorf("filter number requires bounded JSON decimal syntax")
	}
	number := json.Number(text)
	if _, err := json.Marshal(number); err != nil {
		return "", fmt.Errorf("filter number requires a finite decimal")
	}
	return number, nil
}
