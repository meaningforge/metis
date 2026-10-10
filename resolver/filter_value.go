package resolver

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/serrors"
)

const maxFilterDecimalPrecision = 38

// NormalizeFilterValue parses one public string operand using the resolved
// semantic datatype. The returned value is suitable for typed SQL parameters.
func NormalizeFilterValue(filter query.Filter, datatype ossie.DataType) (query.Filter, error) {
	if !filterOperatorSupportsDatatype(filter.Operator, datatype) {
		return query.Filter{}, invalidFilterValue(filter, datatype, "filter operator is incompatible with the semantic datatype")
	}
	if filter.Operator == query.FilterIsNull || filter.Operator == query.FilterIsNotNull {
		return filter, nil
	}
	value, err := normalizeFilterOperand(filter.Value, datatype, filter.Operator)
	if err != nil {
		return query.Filter{}, invalidFilterValue(filter, datatype, "filter operand is incompatible with the semantic datatype")
	}
	filter.Value = value
	return filter, nil
}

func invalidFilterValue(filter query.Filter, datatype ossie.DataType, message string) error {
	return &serrors.Error{
		Code:    serrors.ErrInvalidFilterValue,
		Message: message,
		Details: map[string]any{"field": filter.Field, "datatype": datatype, "operator": filter.Operator},
	}
}

func filterOperatorSupportsDatatype(operator query.FilterOperator, datatype ossie.DataType) bool {
	switch datatype {
	case ossie.DataTypeBoolean, "", ossie.DataTypeOpaque:
		switch operator {
		case query.FilterEQ, query.FilterNEQ, query.FilterIN, query.FilterNotIn, query.FilterIsNull, query.FilterIsNotNull:
			return true
		default:
			return false
		}
	case ossie.DataTypeString, ossie.DataTypeInteger, ossie.DataTypeDecimal, ossie.DataTypeFloat,
		ossie.DataTypeDate, ossie.DataTypeTime, ossie.DataTypeDateTime, ossie.DataTypeDateTimeTz:
		return true
	default:
		return false
	}
}

func normalizeFilterOperand(value any, datatype ossie.DataType, operator query.FilterOperator) (any, error) {
	reflected := reflect.ValueOf(value)
	if reflected.IsValid() && (reflected.Kind() == reflect.Slice || reflected.Kind() == reflect.Array) {
		out := make([]any, reflected.Len())
		changed := false
		for i := 0; i < reflected.Len(); i++ {
			item := reflected.Index(i).Interface()
			if item == nil && (operator == query.FilterIN || operator == query.FilterNotIn) {
				continue
			}
			normalized, err := normalizeFilterScalar(item, datatype)
			if err != nil {
				return nil, err
			}
			out[i] = normalized
			changed = changed || !reflect.DeepEqual(item, normalized)
		}
		if !changed {
			return value, nil
		}
		return out, nil
	}
	return normalizeFilterScalar(value, datatype)
}

func normalizeFilterScalar(value any, datatype ossie.DataType) (any, error) {
	if value == nil {
		return nil, fmt.Errorf("non-null operand required")
	}
	text, ok := value.(string)
	if !ok {
		return nil, fmt.Errorf("string literal required")
	}
	switch datatype {
	case ossie.DataTypeString:
		return text, nil
	case ossie.DataTypeDate, ossie.DataTypeTime, ossie.DataTypeDateTime, ossie.DataTypeDateTimeTz:
		if !validTemporalFilterLiteral(datatype, text) {
			return nil, fmt.Errorf("valid temporal operand required")
		}
		return text, nil
	case ossie.DataTypeBoolean:
		switch text {
		case "true":
			return true, nil
		case "false":
			return false, nil
		default:
			return nil, fmt.Errorf("boolean operand required")
		}
	case ossie.DataTypeInteger:
		return normalizeIntegerOperand(text)
	case ossie.DataTypeDecimal:
		return normalizeDecimalOperand(text)
	case ossie.DataTypeFloat:
		return normalizeFloatOperand(text)
	case "", ossie.DataTypeOpaque:
		return text, nil
	default:
		return nil, fmt.Errorf("unsupported semantic datatype")
	}
}

func validTemporalFilterLiteral(datatype ossie.DataType, text string) bool {
	var layouts []string
	switch datatype {
	case ossie.DataTypeDate:
		layouts = []string{"2006-01-02"}
	case ossie.DataTypeTime:
		layouts = []string{"15:04:05.999999999"}
	case ossie.DataTypeDateTime:
		layouts = []string{"2006-01-02T15:04:05.999999999", time.RFC3339Nano}
	case ossie.DataTypeDateTimeTz:
		layouts = []string{time.RFC3339Nano}
	default:
		return false
	}
	for _, layout := range layouts {
		if _, err := time.Parse(layout, text); err == nil {
			return true
		}
	}
	return false
}

func normalizeIntegerOperand(text string) (any, error) {
	canonical, integral, _, _, err := canonicalExactNumber(text)
	if err != nil || !integral {
		return nil, fmt.Errorf("integer operand required")
	}
	parsed, err := strconv.ParseInt(canonical, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("integer operand is outside the supported range")
	}
	return parsed, nil
}

func normalizeDecimalOperand(text string) (any, error) {
	canonical, _, precision, scale, err := canonicalExactNumber(text)
	if err != nil || precision > maxFilterDecimalPrecision || scale > maxFilterDecimalPrecision {
		return nil, fmt.Errorf("decimal operand is outside the supported range")
	}
	return json.Number(canonical), nil
}

func normalizeFloatOperand(text string) (any, error) {
	n, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
		return nil, fmt.Errorf("float operand is outside the supported range")
	}
	return n, nil
}

// canonicalExactNumber expands a bounded JSON exponent without converting
// through binary floating point. precision follows SQL DECIMAL semantics:
// integer digits plus scale, including leading fractional zeroes.
func canonicalExactNumber(text string) (canonical string, integral bool, precision, scale int, err error) {
	number, err := query.ParseFilterNumber(text)
	if err != nil {
		return "", false, 0, 0, err
	}
	s := number.String()
	negative := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	exponent := 0
	if index := strings.IndexAny(s, "eE"); index >= 0 {
		exponent, err = strconv.Atoi(s[index+1:])
		if err != nil {
			return "", false, 0, 0, fmt.Errorf("invalid exponent")
		}
		s = s[:index]
	}
	integerPart, fractionalPart := s, ""
	if index := strings.IndexByte(s, '.'); index >= 0 {
		integerPart, fractionalPart = s[:index], s[index+1:]
	}
	digits := integerPart + fractionalPart
	point := len(integerPart) + exponent
	if point < -256 || point > 256 {
		return "", false, 0, 0, fmt.Errorf("number is outside the supported range")
	}
	switch {
	case point <= 0:
		integerPart = "0"
		fractionalPart = strings.Repeat("0", -point) + digits
	case point >= len(digits):
		integerPart = digits + strings.Repeat("0", point-len(digits))
		fractionalPart = ""
	default:
		integerPart, fractionalPart = digits[:point], digits[point:]
	}
	integerPart = strings.TrimLeft(integerPart, "0")
	if integerPart == "" {
		integerPart = "0"
	}
	fractionalPart = strings.TrimRight(fractionalPart, "0")
	if integerPart == "0" && fractionalPart == "" {
		negative = false
	}
	canonical = integerPart
	if fractionalPart != "" {
		canonical += "." + fractionalPart
	}
	if negative {
		canonical = "-" + canonical
	}
	scale = len(fractionalPart)
	integerDigits := len(strings.TrimLeft(integerPart, "0"))
	precision = integerDigits + scale
	if precision == 0 {
		precision = 1
	}
	return canonical, fractionalPart == "", precision, scale, nil
}
