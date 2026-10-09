package query

import (
	"encoding/json"
	"fmt"
	"math/big"
	"regexp"
	"strconv"
)

var filterNumberSyntax = regexp.MustCompile(`^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]{1,3})?$`)

// ParseFilterNumber preserves the current float64 contract without silently
// changing a JSON literal's decimal value. Decimal round trips (such as 0.1)
// remain supported; integral values must additionally be exact binary integers.
// Bounds are checked before rational parsing to limit allocation. Errors never
// include the original operand.
func ParseFilterNumber(text string) (float64, error) {
	if len(text) > 256 || !filterNumberSyntax.MatchString(text) {
		return 0, fmt.Errorf("filter number requires bounded JSON decimal syntax")
	}
	want, ok := new(big.Rat).SetString(text)
	if !ok {
		return 0, fmt.Errorf("filter number requires a finite decimal")
	}
	n, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return 0, fmt.Errorf("filter number is outside the supported range")
	}
	if want.IsInt() && want.Cmp(new(big.Rat).SetFloat64(n)) != 0 {
		return 0, fmt.Errorf("integer filter loses precision in the query API")
	}
	b, err := json.Marshal(n)
	if err != nil {
		return 0, fmt.Errorf("filter number must be finite")
	}
	got, ok := new(big.Rat).SetString(string(b))
	if !ok || want.Cmp(got) != 0 {
		return 0, fmt.Errorf("filter number loses precision in the query API")
	}
	return n, nil
}
