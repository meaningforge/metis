package comparison

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"sort"
	"strconv"
	"strings"

	"github.com/meaningforge/metis/compiler/artifact"
	"github.com/meaningforge/metis/ossie"
)

const percentScale = 18

type UnsupportedEvidenceError struct{ Message string }

func (e *UnsupportedEvidenceError) Error() string { return e.Message }

type Evidence struct {
	Schema artifact.OutputSchema
	Rows   [][]any
}

type normalizedMember struct {
	public Member
	key    string
}

type normalizedRow struct {
	members []normalizedMember
	metrics []*big.Rat
}

func ValidateEvidenceSchema(descriptor Descriptor, schema artifact.OutputSchema) error {
	_, _, err := validateSchema(descriptor, schema)
	return err
}

func Evaluate(descriptor Descriptor, baseline, current Evidence) (*Result, error) {
	if descriptor.AnalysisID == "" || descriptor.TimeDimension == "" || len(descriptor.Metrics) == 0 {
		return nil, fmt.Errorf("comparison descriptor is incomplete")
	}
	if !sameSchema(baseline.Schema, current.Schema) {
		return nil, fmt.Errorf("comparison evidence schemas diverged")
	}
	dimensionTypes, metricTypes, err := validateSchema(descriptor, baseline.Schema)
	if err != nil {
		return nil, err
	}
	left, err := normalizeRows(baseline.Rows, dimensionTypes, metricTypes)
	if err != nil {
		return nil, fmt.Errorf("baseline evidence: %w", err)
	}
	right, err := normalizeRows(current.Rows, dimensionTypes, metricTypes)
	if err != nil {
		return nil, fmt.Errorf("current evidence: %w", err)
	}
	keys := make([]string, 0, len(left)+len(right))
	seen := make(map[string]struct{}, len(left)+len(right))
	for key := range left {
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	for key := range right {
		if _, exists := seen[key]; !exists {
			keys = append(keys, key)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		leftRow := left[keys[i]]
		if leftRow == nil {
			leftRow = right[keys[i]]
		}
		rightRow := left[keys[j]]
		if rightRow == nil {
			rightRow = right[keys[j]]
		}
		return compareTuple(leftRow.members, rightRow.members) < 0
	})
	result := &Result{
		AnalysisID: descriptor.AnalysisID, TimeDimension: descriptor.TimeDimension,
		Baseline: descriptor.Baseline, Current: descriptor.Current,
		Metrics: make([]string, len(descriptor.Metrics)), Dimensions: make([]string, len(descriptor.Dimensions)),
		Rows: make([]Row, 0, len(keys)),
	}
	for i, ref := range descriptor.Metrics {
		result.Metrics[i] = ref.Public
	}
	for i, ref := range descriptor.Dimensions {
		result.Dimensions[i] = ref.Public
	}
	for _, key := range keys {
		baselineRow, baselinePresent := left[key]
		currentRow, currentPresent := right[key]
		source := baselineRow
		if source == nil {
			source = currentRow
		}
		row := Row{BaselinePresent: baselinePresent, CurrentPresent: currentPresent, Members: make([]Member, len(source.members)), Values: make([]MetricValue, len(descriptor.Metrics))}
		for i := range source.members {
			row.Members[i] = source.members[i].public
			row.Members[i].Dimension = descriptor.Dimensions[i].Public
		}
		for i, ref := range descriptor.Metrics {
			value := MetricValue{Metric: ref.Public}
			var baselineValue, currentValue *big.Rat
			if baselinePresent {
				baselineValue = baselineRow.metrics[i]
			}
			if currentPresent {
				currentValue = currentRow.metrics[i]
			}
			value.BaselineValue = exactDecimalPointer(baselineValue)
			value.CurrentValue = exactDecimalPointer(currentValue)
			if baselineValue != nil && currentValue != nil {
				value.ChangeDefined = true
				delta := new(big.Rat).Sub(currentValue, baselineValue)
				value.Delta = exactDecimalPointer(delta)
				if baselineValue.Sign() != 0 {
					value.PercentDefined = true
					percent := new(big.Rat).Mul(new(big.Rat).Quo(delta, baselineValue), big.NewRat(100, 1))
					value.PercentChange = roundedDecimalPointer(percent)
				}
			}
			row.Values[i] = value
		}
		result.Rows = append(result.Rows, row)
	}
	return result, nil
}

func validateSchema(descriptor Descriptor, schema artifact.OutputSchema) ([]MemberType, []ossie.DataType, error) {
	want := len(descriptor.Dimensions) + len(descriptor.Metrics)
	if len(schema.Columns) != want {
		return nil, nil, fmt.Errorf("comparison schema width %d, want %d", len(schema.Columns), want)
	}
	dimensionTypes := make([]MemberType, len(descriptor.Dimensions))
	metricTypes := make([]ossie.DataType, len(descriptor.Metrics))
	seenPublic := map[string]struct{}{}
	for i, ref := range descriptor.Dimensions {
		if ref.Public == "" || ref.Column == "" {
			return nil, nil, fmt.Errorf("comparison dimension ref %d is incomplete", i)
		}
		if _, exists := seenPublic[ref.Public]; exists {
			return nil, nil, fmt.Errorf("duplicate comparison output ref %q", ref.Public)
		}
		seenPublic[ref.Public] = struct{}{}
		column := schema.Columns[i]
		if column.Name != ref.Column || column.Kind != artifact.OutputDimension {
			return nil, nil, fmt.Errorf("comparison dimension column %d is inconsistent", i)
		}
		memberType, ok := memberTypeOf(column.Datatype)
		if !ok {
			return nil, nil, &UnsupportedEvidenceError{Message: fmt.Sprintf("unsupported comparison dimension datatype %q", column.Datatype)}
		}
		dimensionTypes[i] = memberType
	}
	for i, ref := range descriptor.Metrics {
		if ref.Public == "" || ref.Column == "" {
			return nil, nil, fmt.Errorf("comparison metric ref %d is incomplete", i)
		}
		if _, exists := seenPublic[ref.Public]; exists {
			return nil, nil, fmt.Errorf("duplicate comparison output ref %q", ref.Public)
		}
		seenPublic[ref.Public] = struct{}{}
		column := schema.Columns[len(descriptor.Dimensions)+i]
		if column.Name != ref.Column || column.Kind != artifact.OutputMetric {
			return nil, nil, fmt.Errorf("comparison metric column %d is inconsistent", i)
		}
		if column.Datatype != ossie.DataTypeInteger && column.Datatype != ossie.DataTypeDecimal {
			return nil, nil, &UnsupportedEvidenceError{Message: fmt.Sprintf("unsupported comparison metric datatype %q", column.Datatype)}
		}
		metricTypes[i] = column.Datatype
	}
	return dimensionTypes, metricTypes, nil
}

func normalizeRows(rows [][]any, dimensionTypes []MemberType, metricTypes []ossie.DataType) (map[string]*normalizedRow, error) {
	if len(dimensionTypes) == 0 && len(rows) > 1 {
		return nil, fmt.Errorf("scalar comparison returned %d rows", len(rows))
	}
	out := make(map[string]*normalizedRow, len(rows))
	for rowIndex, values := range rows {
		if len(values) != len(dimensionTypes)+len(metricTypes) {
			return nil, fmt.Errorf("row %d width %d does not match schema width %d", rowIndex, len(values), len(dimensionTypes)+len(metricTypes))
		}
		row := &normalizedRow{members: make([]normalizedMember, len(dimensionTypes)), metrics: make([]*big.Rat, len(metricTypes))}
		keys := make([]string, len(dimensionTypes))
		for i, memberType := range dimensionTypes {
			member, key, err := normalizeMember(values[i], memberType)
			if err != nil {
				return nil, fmt.Errorf("row %d dimension %d: %w", rowIndex, i, err)
			}
			row.members[i] = normalizedMember{public: Member{MemberType: memberType, Value: member}, key: key}
			keys[i] = fmt.Sprintf("%d:%s", len(key), key)
		}
		for i, datatype := range metricTypes {
			value := values[len(dimensionTypes)+i]
			if value == nil {
				continue
			}
			number, err := normalizedMetric(value, datatype)
			if err != nil {
				return nil, fmt.Errorf("row %d metric %d: %w", rowIndex, i, err)
			}
			row.metrics[i] = number
		}
		key := strings.Join(keys, "|")
		if _, exists := out[key]; exists {
			return nil, fmt.Errorf("duplicate normalized dimension tuple")
		}
		out[key] = row
	}
	return out, nil
}

func memberTypeOf(datatype ossie.DataType) (MemberType, bool) {
	switch datatype {
	case ossie.DataTypeString:
		return MemberString, true
	case ossie.DataTypeInteger:
		return MemberInteger, true
	case ossie.DataTypeDecimal:
		return MemberDecimal, true
	case ossie.DataTypeFloat:
		return MemberFloat, true
	case ossie.DataTypeBoolean:
		return MemberBoolean, true
	case ossie.DataTypeDate:
		return MemberDate, true
	case ossie.DataTypeTime:
		return MemberTime, true
	case ossie.DataTypeDateTime:
		return MemberDateTime, true
	case ossie.DataTypeDateTimeTz:
		return MemberDateTimeTZ, true
	default:
		return "", false
	}
}

func normalizeMember(value any, memberType MemberType) (json.RawMessage, string, error) {
	if value == nil {
		return json.RawMessage("null"), "null", nil
	}
	key := ""
	switch memberType {
	case MemberString, MemberDate, MemberTime, MemberDateTime, MemberDateTimeTZ:
		text, ok := value.(string)
		if !ok {
			return nil, "", fmt.Errorf("string-like member has type %T", value)
		}
		key = "string:" + text
	case MemberInteger:
		number, err := normalizedNumber(value, false)
		if err != nil || !number.IsInt() {
			return nil, "", fmt.Errorf("integer member is invalid")
		}
		if _, ok := value.(json.Number); !ok {
			return nil, "", fmt.Errorf("integer member has type %T", value)
		}
		key = "number:" + number.RatString()
	case MemberDecimal:
		number, err := normalizedNumber(value, true)
		if err != nil {
			return nil, "", err
		}
		key = "number:" + number.RatString()
	case MemberFloat:
		number, err := normalizedNumber(value, false)
		if err != nil {
			return nil, "", err
		}
		if _, ok := value.(json.Number); !ok {
			return nil, "", fmt.Errorf("float member has type %T", value)
		}
		key = "number:" + number.RatString()
	case MemberBoolean:
		boolean, ok := value.(bool)
		if !ok {
			return nil, "", fmt.Errorf("boolean member has type %T", value)
		}
		key = fmt.Sprintf("boolean:%t", boolean)
	default:
		return nil, "", fmt.Errorf("unsupported member type %q", memberType)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, "", err
	}
	if len(encoded) == 0 || encoded[0] == '{' || encoded[0] == '[' {
		return nil, "", fmt.Errorf("dimension member must be a JSON scalar")
	}
	return encoded, key, nil
}

func normalizedMetric(value any, datatype ossie.DataType) (*big.Rat, error) {
	switch datatype {
	case ossie.DataTypeInteger:
		number, err := normalizedNumber(value, false)
		if err != nil || !number.IsInt() {
			return nil, fmt.Errorf("integer metric is invalid")
		}
		if _, ok := value.(json.Number); !ok {
			return nil, fmt.Errorf("integer metric has type %T", value)
		}
		return number, nil
	case ossie.DataTypeDecimal:
		return normalizedNumber(value, true)
	default:
		return nil, fmt.Errorf("unsupported metric datatype %q", datatype)
	}
}

func normalizedNumber(value any, requireString bool) (*big.Rat, error) {
	var text string
	switch typed := value.(type) {
	case string:
		text = typed
	case json.Number:
		if requireString {
			return nil, fmt.Errorf("decimal evidence must be a string")
		}
		text = typed.String()
	default:
		return nil, fmt.Errorf("numeric evidence has type %T", value)
	}
	if text == "" || text != strings.TrimSpace(text) || strings.Contains(text, "/") {
		return nil, fmt.Errorf("numeric evidence is not a decimal token")
	}
	if requireString {
		number, ok := new(big.Rat).SetString(text)
		if !ok {
			return nil, fmt.Errorf("invalid numeric evidence")
		}
		return number, nil
	}
	if !json.Valid([]byte(text)) {
		return nil, fmt.Errorf("invalid numeric evidence")
	}
	parsed, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsInf(parsed, 0) || math.IsNaN(parsed) {
		return nil, fmt.Errorf("invalid numeric evidence")
	}
	number, ok := exactJSONNumber(text)
	if !ok {
		return nil, fmt.Errorf("invalid numeric evidence")
	}
	return number, nil
}

// exactJSONNumber converts a syntactically valid, finite JSON number into an
// exact rational. strconv.ParseFloat above bounds exponent size and rejects
// non-finite evidence; this conversion avoids binary-float identity drift.
func exactJSONNumber(text string) (*big.Rat, bool) {
	negative := strings.HasPrefix(text, "-")
	if negative {
		text = text[1:]
	}
	mantissa, exponentText, hasExponent := strings.Cut(text, "e")
	if !hasExponent {
		mantissa, exponentText, hasExponent = strings.Cut(text, "E")
	}
	exponent := 0
	if hasExponent {
		parsed, err := strconv.Atoi(exponentText)
		if err != nil {
			return nil, false
		}
		exponent = parsed
	}
	integer, fraction, hasPoint := strings.Cut(mantissa, ".")
	if !hasPoint {
		fraction = ""
	}
	digits := integer + fraction
	numerator, ok := new(big.Int).SetString(digits, 10)
	if !ok {
		return nil, false
	}
	if negative {
		numerator.Neg(numerator)
	}
	scale := len(fraction) - exponent
	if scale <= 0 {
		numerator.Mul(numerator, pow10(-scale))
		return new(big.Rat).SetInt(numerator), true
	}
	return new(big.Rat).SetFrac(numerator, pow10(scale)), true
}

func pow10(exponent int) *big.Int {
	return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(exponent)), nil)
}

func compareTuple(left, right []normalizedMember) int {
	for i := range left {
		if cmp := compareMember(left[i], right[i]); cmp != 0 {
			return cmp
		}
	}
	return 0
}

func compareMember(left, right normalizedMember) int {
	leftNull := bytes.Equal(left.public.Value, []byte("null"))
	rightNull := bytes.Equal(right.public.Value, []byte("null"))
	if leftNull || rightNull {
		if leftNull && rightNull {
			return 0
		}
		if leftNull {
			return 1
		}
		return -1
	}
	if left.public.MemberType == MemberInteger || left.public.MemberType == MemberDecimal || left.public.MemberType == MemberFloat {
		leftNumber, _ := new(big.Rat).SetString(strings.TrimPrefix(left.key, "number:"))
		rightNumber, _ := new(big.Rat).SetString(strings.TrimPrefix(right.key, "number:"))
		if cmp := leftNumber.Cmp(rightNumber); cmp != 0 {
			return cmp
		}
	}
	return bytes.Compare(left.public.Value, right.public.Value)
}

func exactDecimalPointer(value *big.Rat) *DecimalString {
	if value == nil {
		return nil
	}
	formatted := exactDecimal(value)
	return &formatted
}

func roundedDecimalPointer(value *big.Rat) *DecimalString {
	if value == nil {
		return nil
	}
	formatted := decimalAtScale(value, percentScale)
	return &formatted
}

func exactDecimal(value *big.Rat) DecimalString {
	if value.IsInt() {
		return DecimalString(value.Num().String())
	}
	denominator := new(big.Int).Set(value.Denom())
	twos, fives := 0, 0
	two, five, remainder := big.NewInt(2), big.NewInt(5), new(big.Int)
	for remainder.Mod(denominator, two).Sign() == 0 {
		denominator.Quo(denominator, two)
		twos++
	}
	for remainder.Mod(denominator, five).Sign() == 0 {
		denominator.Quo(denominator, five)
		fives++
	}
	if denominator.Cmp(big.NewInt(1)) != 0 {
		return decimalAtScale(value, percentScale)
	}
	scale := twos
	if fives > scale {
		scale = fives
	}
	return decimalAtScale(value, scale)
}

func decimalAtScale(value *big.Rat, scale int) DecimalString {
	text := strings.TrimRight(strings.TrimRight(value.FloatString(scale), "0"), ".")
	if text == "-0" {
		text = "0"
	}
	return DecimalString(text)
}

func sameSchema(left, right artifact.OutputSchema) bool {
	if len(left.Columns) != len(right.Columns) {
		return false
	}
	for i := range left.Columns {
		if left.Columns[i].Name != right.Columns[i].Name || left.Columns[i].Kind != right.Columns[i].Kind || left.Columns[i].Datatype != right.Columns[i].Datatype {
			return false
		}
	}
	return true
}
