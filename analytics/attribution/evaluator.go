package attribution

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
	"sort"
	"strings"

	"github.com/meaningforge/metis/compiler/artifact"
	"github.com/meaningforge/metis/ossie"
)

const ratioScale = 18

var ratioTolerance = new(big.Rat).SetFrac(big.NewInt(1), big.NewInt(100000000))

type UnsupportedEvidenceError struct{ Message string }

func (e *UnsupportedEvidenceError) Error() string { return e.Message }

// ValidateEvidenceSchema rejects unsupported public numeric/member types before
// a caller starts physical execution. Structural mismatches remain evaluator
// inconsistencies rather than capability errors.
func ValidateEvidenceSchema(strategy AttributionStrategy, dimension, column string, schema artifact.OutputSchema) error {
	e := DimensionEvidence{Dimension: dimension, Column: column, Schema: schema}
	switch strategy {
	case AttributionStrategyAdditive:
		_, err := validateSchema(schema, []string{evidenceColumn(e), "baseline_value", "current_value", "delta", "total_delta", "contribution_pct"}, 1, 2, 3, 4, 5)
		return err
	case AttributionStrategyRatio:
		_, err := validateSchema(schema, []string{evidenceColumn(e), "baseline_present", "current_present", "baseline_numerator", "baseline_denominator", "current_numerator", "current_denominator", "baseline_rate", "current_rate", "baseline_weight", "current_weight", "baseline_defined", "current_defined", "segment_defined", "rate_effect", "mix_effect", "entry_effect", "exit_effect", "segment_effect", "baseline_ratio", "current_ratio", "ratio_delta", "decomposed_delta", "reconciliation_residual", "attribution_defined"}, 3, 4, 5, 6, 7, 8, 9, 10, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23)
		return err
	default:
		return &UnsupportedEvidenceError{Message: fmt.Sprintf("unsupported attribution strategy %q", strategy)}
	}
}

type Descriptor struct {
	AnalysisID    string
	Metric        string
	TimeDimension string
	Baseline      AttributionPeriod
	Current       AttributionPeriod
	Strategy      AttributionStrategy
}

type DimensionEvidence struct {
	Dimension string
	// Column is the resolved internal output-column identity. Dimension remains
	// the canonical public ref. When empty, Dimension is also the column name.
	Column string
	Schema artifact.OutputSchema
	Rows   [][]any
}

func Evaluate(descriptor Descriptor, evidence []DimensionEvidence) (*AttributeMetricResult, error) {
	if descriptor.AnalysisID == "" || descriptor.Metric == "" || descriptor.TimeDimension == "" {
		return nil, fmt.Errorf("attribution descriptor is incomplete")
	}
	if descriptor.Strategy != AttributionStrategyAdditive && descriptor.Strategy != AttributionStrategyRatio {
		return nil, fmt.Errorf("unsupported attribution strategy %q", descriptor.Strategy)
	}
	out := &AttributeMetricResult{AnalysisID: descriptor.AnalysisID, Metric: descriptor.Metric, TimeDimension: descriptor.TimeDimension, Baseline: descriptor.Baseline, Current: descriptor.Current, Strategy: descriptor.Strategy}
	seen := make(map[string]struct{}, len(evidence))
	for _, item := range evidence {
		if item.Dimension == "" {
			return nil, fmt.Errorf("attribution evidence has empty dimension")
		}
		if _, ok := seen[item.Dimension]; ok {
			return nil, fmt.Errorf("duplicate attribution dimension %q", item.Dimension)
		}
		seen[item.Dimension] = struct{}{}
		var result DimensionAttributionResult
		var err error
		if descriptor.Strategy == AttributionStrategyAdditive {
			result, err = evaluateAdditive(item)
		} else {
			result, err = evaluateRatio(item)
		}
		if err != nil {
			return nil, fmt.Errorf("evaluate attribution dimension %q: %w", item.Dimension, err)
		}
		out.Dimensions = append(out.Dimensions, result)
	}
	sort.Slice(out.Dimensions, func(i, j int) bool { return out.Dimensions[i].Dimension < out.Dimensions[j].Dimension })
	return out, nil
}

func evaluateAdditive(e DimensionEvidence) (DimensionAttributionResult, error) {
	want := []string{evidenceColumn(e), "baseline_value", "current_value", "delta", "total_delta", "contribution_pct"}
	memberType, err := validateSchema(e.Schema, want, 1, 2, 3, 4, 5)
	if err != nil {
		return DimensionAttributionResult{}, err
	}
	result := &AdditiveAttributionResult{Segments: make([]AdditiveAttributionSegment, 0, len(e.Rows))}
	baselineTotal, currentTotal, deltaTotal := new(big.Rat), new(big.Rat), new(big.Rat)
	var repeatedTotal *big.Rat
	seen := map[string]struct{}{}
	contributions := make([]*big.Rat, 0, len(e.Rows))
	for _, row := range e.Rows {
		if len(row) != len(want) {
			return DimensionAttributionResult{}, fmt.Errorf("row width %d does not match schema width %d", len(row), len(want))
		}
		member, key, err := memberValue(row[0], memberType)
		if err != nil {
			return DimensionAttributionResult{}, err
		}
		if _, ok := seen[key]; ok {
			return DimensionAttributionResult{}, fmt.Errorf("duplicate normalized dimension member")
		}
		seen[key] = struct{}{}
		baseline, err := requiredNumber(row[1])
		if err != nil {
			return DimensionAttributionResult{}, fmt.Errorf("baseline_value: %w", err)
		}
		current, err := requiredNumber(row[2])
		if err != nil {
			return DimensionAttributionResult{}, fmt.Errorf("current_value: %w", err)
		}
		delta, err := requiredNumber(row[3])
		if err != nil {
			return DimensionAttributionResult{}, fmt.Errorf("delta: %w", err)
		}
		if delta.Cmp(new(big.Rat).Sub(current, baseline)) != 0 {
			return DimensionAttributionResult{}, fmt.Errorf("segment delta is inconsistent")
		}
		total, err := requiredNumber(row[4])
		if err != nil {
			return DimensionAttributionResult{}, fmt.Errorf("total_delta: %w", err)
		}
		if repeatedTotal == nil {
			repeatedTotal = new(big.Rat).Set(total)
		} else if repeatedTotal.Cmp(total) != 0 {
			return DimensionAttributionResult{}, fmt.Errorf("repeated total_delta is inconsistent")
		}
		baselineTotal.Add(baselineTotal, baseline)
		currentTotal.Add(currentTotal, current)
		deltaTotal.Add(deltaTotal, delta)
		contribution, parseErr := optionalNumber(row[5])
		if parseErr != nil {
			return DimensionAttributionResult{}, fmt.Errorf("contribution_pct: %w", parseErr)
		}
		contributions = append(contributions, contribution)
		segment := AdditiveAttributionSegment{Value: member, BaselineValue: decimal(baseline), CurrentValue: decimal(current), Delta: decimal(delta)}
		result.Segments = append(result.Segments, segment)
	}
	if repeatedTotal != nil && repeatedTotal.Cmp(deltaTotal) != 0 {
		return DimensionAttributionResult{}, fmt.Errorf("additive evidence does not reconcile")
	}
	if new(big.Rat).Sub(currentTotal, baselineTotal).Cmp(deltaTotal) != 0 {
		return DimensionAttributionResult{}, fmt.Errorf("additive totals do not reconcile")
	}
	for i := range result.Segments {
		if deltaTotal.Sign() != 0 {
			value, _ := requiredNumber(string(result.Segments[i].Delta))
			pct := new(big.Rat).Mul(new(big.Rat).Quo(value, deltaTotal), big.NewRat(100, 1))
			if !ratClose(contributions[i], pct) {
				return DimensionAttributionResult{}, fmt.Errorf("segment contribution_pct is inconsistent")
			}
			formatted := decimal(pct)
			result.Segments[i].ContributionPct = &formatted
		} else if contributions[i] != nil {
			return DimensionAttributionResult{}, fmt.Errorf("contribution_pct must be null when total_delta is zero")
		}
	}
	sortSegments(result.Segments, func(segment AdditiveAttributionSegment) json.RawMessage { return segment.Value }, memberType)
	result.Summary = AdditiveAttributionSummary{BaselineTotal: decimal(baselineTotal), CurrentTotal: decimal(currentTotal), TotalDelta: decimal(deltaTotal), ContributionDefined: deltaTotal.Sign() != 0, Reconciled: true}
	return DimensionAttributionResult{Dimension: e.Dimension, MemberType: memberType, Additive: result}, nil
}

func evaluateRatio(e DimensionEvidence) (DimensionAttributionResult, error) {
	want := []string{evidenceColumn(e), "baseline_present", "current_present", "baseline_numerator", "baseline_denominator", "current_numerator", "current_denominator", "baseline_rate", "current_rate", "baseline_weight", "current_weight", "baseline_defined", "current_defined", "segment_defined", "rate_effect", "mix_effect", "entry_effect", "exit_effect", "segment_effect", "baseline_ratio", "current_ratio", "ratio_delta", "decomposed_delta", "reconciliation_residual", "attribution_defined"}
	memberType, err := validateSchema(e.Schema, want, 3, 4, 5, 6, 7, 8, 9, 10, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23)
	if err != nil {
		return DimensionAttributionResult{}, err
	}
	result := &RatioAttributionResult{Segments: make([]RatioAttributionSegment, 0, len(e.Rows))}
	bnTotal, bdTotal, cnTotal, cdTotal, effects := new(big.Rat), new(big.Rat), new(big.Rat), new(big.Rat), new(big.Rat)
	seen := map[string]struct{}{}
	var summary []any
	for _, row := range e.Rows {
		if len(row) != len(want) {
			return DimensionAttributionResult{}, fmt.Errorf("row width %d does not match schema width %d", len(row), len(want))
		}
		member, key, err := memberValue(row[0], memberType)
		if err != nil {
			return DimensionAttributionResult{}, err
		}
		if _, ok := seen[key]; ok {
			return DimensionAttributionResult{}, fmt.Errorf("duplicate normalized dimension member")
		}
		seen[key] = struct{}{}
		bp, err := requiredBool(row[1])
		if err != nil {
			return DimensionAttributionResult{}, err
		}
		cp, err := requiredBool(row[2])
		if err != nil {
			return DimensionAttributionResult{}, err
		}
		nums := make([]*big.Rat, 4)
		for i := range nums {
			nums[i], err = requiredNumber(row[3+i])
			if err != nil {
				return DimensionAttributionResult{}, fmt.Errorf("%s: %w", want[3+i], err)
			}
		}
		bnTotal.Add(bnTotal, nums[0])
		bdTotal.Add(bdTotal, nums[1])
		cnTotal.Add(cnTotal, nums[2])
		cdTotal.Add(cdTotal, nums[3])
		optional := make([]*DecimalString, 12)
		for i, column := range []int{7, 8, 9, 10, 14, 15, 16, 17, 18, 19, 20, 21} {
			value, parseErr := optionalNumber(row[column])
			if parseErr != nil {
				return DimensionAttributionResult{}, fmt.Errorf("%s: %w", want[column], parseErr)
			}
			optional[i] = decimalPointer(value)
		}
		bDefined, err := requiredBool(row[11])
		if err != nil {
			return DimensionAttributionResult{}, err
		}
		cDefined, err := requiredBool(row[12])
		if err != nil {
			return DimensionAttributionResult{}, err
		}
		sDefined, err := requiredBool(row[13])
		if err != nil {
			return DimensionAttributionResult{}, err
		}
		if effect, parseErr := optionalNumber(row[18]); parseErr != nil {
			return DimensionAttributionResult{}, parseErr
		} else if effect != nil {
			effects.Add(effects, effect)
		}
		currentSummary := append([]any(nil), row[19:25]...)
		if summary == nil {
			summary = currentSummary
		} else if !sameValues(summary, currentSummary) {
			return DimensionAttributionResult{}, fmt.Errorf("repeated ratio summary is inconsistent")
		}
		result.Segments = append(result.Segments, RatioAttributionSegment{Value: member, BaselinePresent: bp, CurrentPresent: cp, BaselineNumerator: decimal(nums[0]), BaselineDenominator: decimal(nums[1]), CurrentNumerator: decimal(nums[2]), CurrentDenominator: decimal(nums[3]), BaselineRate: optional[0], CurrentRate: optional[1], BaselineWeight: optional[2], CurrentWeight: optional[3], BaselineDefined: bDefined, CurrentDefined: cDefined, SegmentDefined: sDefined, RateEffect: optional[4], MixEffect: optional[5], EntryEffect: optional[6], ExitEffect: optional[7], TotalEffect: optional[8]})
	}
	for _, segment := range result.Segments {
		if err := verifyRatioSegment(segment, bdTotal, cdTotal); err != nil {
			return DimensionAttributionResult{}, err
		}
	}
	result.Summary = RatioAttributionSummary{Reconciled: true}
	if len(e.Rows) != 0 {
		result.Summary.BaselineNumerator = decimalPointer(bnTotal)
		result.Summary.BaselineDenominator = decimalPointer(bdTotal)
		result.Summary.CurrentNumerator = decimalPointer(cnTotal)
		result.Summary.CurrentDenominator = decimalPointer(cdTotal)
		values := make([]*big.Rat, 5)
		for i := 0; i < 5; i++ {
			values[i], err = optionalNumber(summary[i])
			if err != nil {
				return DimensionAttributionResult{}, err
			}
		}
		defined, boolErr := requiredBool(summary[5])
		if boolErr != nil {
			return DimensionAttributionResult{}, boolErr
		}
		result.Summary.AttributionDefined = defined
		result.Summary.BaselineRatio = decimalPointer(values[0])
		result.Summary.CurrentRatio = decimalPointer(values[1])
		result.Summary.RatioDelta = decimalPointer(values[2])
		result.Summary.DecomposedDelta = decimalPointer(values[3])
		result.Summary.ReconciliationResidual = decimalPointer(values[4])
		wantBaseline := guardedRatio(bnTotal, bdTotal)
		wantCurrent := guardedRatio(cnTotal, cdTotal)
		var wantDelta *big.Rat
		if wantBaseline != nil && wantCurrent != nil {
			wantDelta = new(big.Rat).Sub(wantCurrent, wantBaseline)
		}
		allSegmentsDefined := true
		for _, segment := range result.Segments {
			allSegmentsDefined = allSegmentsDefined && segment.SegmentDefined
		}
		wantDefined := bdTotal.Sign() != 0 && cdTotal.Sign() != 0 && allSegmentsDefined
		if defined != wantDefined || !optionalClose(values[0], wantBaseline) || !optionalClose(values[1], wantCurrent) || !optionalClose(values[2], wantDelta) {
			return DimensionAttributionResult{}, fmt.Errorf("ratio summary is inconsistent with segment totals")
		}
		if defined {
			if anyNil(values) || abs(values[4]).Cmp(ratioTolerance) > 0 || abs(new(big.Rat).Sub(values[2], values[3])).Cmp(ratioTolerance) > 0 || abs(new(big.Rat).Sub(effects, values[3])).Cmp(ratioTolerance) > 0 {
				return DimensionAttributionResult{}, fmt.Errorf("ratio evidence does not reconcile")
			}
		} else if values[3] != nil || values[4] != nil {
			return DimensionAttributionResult{}, fmt.Errorf("undefined attribution has non-null decomposition evidence")
		}
	}
	sortSegments(result.Segments, func(segment RatioAttributionSegment) json.RawMessage { return segment.Value }, memberType)
	return DimensionAttributionResult{Dimension: e.Dimension, MemberType: memberType, Ratio: result}, nil
}

func evidenceColumn(e DimensionEvidence) string {
	if e.Column != "" {
		return e.Column
	}
	return e.Dimension
}

func validateSchema(schema artifact.OutputSchema, names []string, numeric ...int) (AttributionMemberType, error) {
	if len(schema.Columns) != len(names) {
		return "", fmt.Errorf("schema width %d, want %d", len(schema.Columns), len(names))
	}
	for i, name := range names {
		if schema.Columns[i].Name != name {
			return "", fmt.Errorf("schema column %d is %q, want %q", i, schema.Columns[i].Name, name)
		}
		wantKind := artifact.OutputMetric
		if i == 0 || isBooleanEvidenceColumn(name) {
			wantKind = artifact.OutputDimension
		}
		if schema.Columns[i].Kind != wantKind {
			return "", fmt.Errorf("schema column %q has kind %q", name, schema.Columns[i].Kind)
		}
	}
	for _, i := range numeric {
		if dt := schema.Columns[i].Datatype; dt != ossie.DataTypeInteger && dt != ossie.DataTypeDecimal {
			return "", &UnsupportedEvidenceError{Message: fmt.Sprintf("numeric column %q has unsupported datatype %q", names[i], dt)}
		}
	}
	memberType, ok := memberTypeOf(schema.Columns[0].Datatype)
	if !ok {
		return "", &UnsupportedEvidenceError{Message: fmt.Sprintf("dimension member datatype %q is unsupported", schema.Columns[0].Datatype)}
	}
	return memberType, nil
}

func isBooleanEvidenceColumn(name string) bool {
	switch name {
	case "baseline_present", "current_present", "baseline_defined", "current_defined", "segment_defined", "attribution_defined":
		return true
	default:
		return false
	}
}

func memberTypeOf(dt ossie.DataType) (AttributionMemberType, bool) {
	switch dt {
	case ossie.DataTypeString:
		return AttributionMemberString, true
	case ossie.DataTypeInteger:
		return AttributionMemberInteger, true
	case ossie.DataTypeDecimal:
		return AttributionMemberDecimal, true
	case ossie.DataTypeFloat:
		return AttributionMemberFloat, true
	case ossie.DataTypeBoolean:
		return AttributionMemberBoolean, true
	case ossie.DataTypeDate:
		return AttributionMemberDate, true
	case ossie.DataTypeTime:
		return AttributionMemberTime, true
	case ossie.DataTypeDateTime:
		return AttributionMemberDateTime, true
	case ossie.DataTypeDateTimeTz:
		return AttributionMemberDateTimeTZ, true
	}
	return "", false
}

func requiredNumber(value any) (*big.Rat, error) {
	if value == nil {
		return nil, fmt.Errorf("numeric evidence is null")
	}
	text := ""
	switch v := value.(type) {
	case string:
		text = v
	case json.Number:
		text = v.String()
	case int:
		text = fmt.Sprint(v)
	case int8:
		text = fmt.Sprint(v)
	case int16:
		text = fmt.Sprint(v)
	case int32:
		text = fmt.Sprint(v)
	case int64:
		text = fmt.Sprint(v)
	case uint:
		text = fmt.Sprint(v)
	case uint8:
		text = fmt.Sprint(v)
	case uint16:
		text = fmt.Sprint(v)
	case uint32:
		text = fmt.Sprint(v)
	case uint64:
		text = fmt.Sprint(v)
	default:
		return nil, fmt.Errorf("unsupported numeric evidence type %T", value)
	}
	if strings.ContainsAny(text, "eE/") {
		return nil, fmt.Errorf("numeric evidence is not a finite base-10 decimal")
	}
	out, ok := new(big.Rat).SetString(text)
	if !ok {
		return nil, fmt.Errorf("invalid numeric evidence")
	}
	return out, nil
}
func optionalNumber(value any) (*big.Rat, error) {
	if value == nil {
		return nil, nil
	}
	return requiredNumber(value)
}
func requiredBool(value any) (bool, error) {
	result, ok := value.(bool)
	if !ok {
		return false, fmt.Errorf("boolean evidence has type %T", value)
	}
	return result, nil
}
func decimal(value *big.Rat) DecimalString {
	if value == nil {
		return "0"
	}
	if value.IsInt() {
		return DecimalString(value.Num().String())
	}
	text := value.FloatString(ratioScale)
	text = strings.TrimRight(strings.TrimRight(text, "0"), ".")
	if text == "-0" {
		text = "0"
	}
	return DecimalString(text)
}
func decimalPointer(value *big.Rat) *DecimalString {
	if value == nil {
		return nil
	}
	formatted := decimal(value)
	return &formatted
}
func abs(value *big.Rat) *big.Rat {
	if value == nil {
		return nil
	}
	return new(big.Rat).Abs(value)
}
func anyNil(values []*big.Rat) bool {
	for _, value := range values {
		if value == nil {
			return true
		}
	}
	return false
}

func ratClose(left, right *big.Rat) bool {
	return left != nil && right != nil && abs(new(big.Rat).Sub(left, right)).Cmp(ratioTolerance) <= 0
}

func optionalClose(left, right *big.Rat) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return ratClose(left, right)
}

func guardedRatio(left, right *big.Rat) *big.Rat {
	if left == nil || right == nil || right.Sign() == 0 {
		return nil
	}
	return new(big.Rat).Quo(left, right)
}

func verifyRatioSegment(segment RatioAttributionSegment, baselineTotal, currentTotal *big.Rat) error {
	parse := func(value DecimalString) *big.Rat { parsed, _ := new(big.Rat).SetString(string(value)); return parsed }
	parseOptional := func(value *DecimalString) *big.Rat {
		if value == nil {
			return nil
		}
		return parse(*value)
	}
	bn, bd := parse(segment.BaselineNumerator), parse(segment.BaselineDenominator)
	cn, cd := parse(segment.CurrentNumerator), parse(segment.CurrentDenominator)
	if !segment.BaselinePresent && !segment.CurrentPresent {
		return fmt.Errorf("ratio segment is absent from both periods")
	}
	if (!segment.BaselinePresent && (bn.Sign() != 0 || bd.Sign() != 0)) || (!segment.CurrentPresent && (cn.Sign() != 0 || cd.Sign() != 0)) {
		return fmt.Errorf("absent ratio period does not carry additive identity values")
	}
	baselineRate, currentRate := guardedRatio(bn, bd), guardedRatio(cn, cd)
	if !segment.BaselinePresent {
		baselineRate = nil
	}
	if !segment.CurrentPresent {
		currentRate = nil
	}
	baselineWeight, currentWeight := guardedRatio(bd, baselineTotal), guardedRatio(cd, currentTotal)
	if segment.BaselineDefined != (segment.BaselinePresent && bd.Sign() != 0) || segment.CurrentDefined != (segment.CurrentPresent && cd.Sign() != 0) || segment.SegmentDefined != ((!segment.BaselinePresent || bd.Sign() != 0) && (!segment.CurrentPresent || cd.Sign() != 0)) {
		return fmt.Errorf("ratio defined-state evidence is inconsistent")
	}
	if !optionalClose(parseOptional(segment.BaselineRate), baselineRate) || !optionalClose(parseOptional(segment.CurrentRate), currentRate) || !optionalClose(parseOptional(segment.BaselineWeight), baselineWeight) || !optionalClose(parseOptional(segment.CurrentWeight), currentWeight) {
		return fmt.Errorf("ratio rate/weight evidence is inconsistent")
	}
	rate, mix, entry, exit := new(big.Rat), new(big.Rat), new(big.Rat), new(big.Rat)
	if segment.BaselinePresent && segment.CurrentPresent {
		if baselineRate == nil || currentRate == nil || baselineWeight == nil || currentWeight == nil {
			rate, mix = nil, nil
		} else {
			half := big.NewRat(1, 2)
			rate.Mul(half, new(big.Rat).Mul(new(big.Rat).Add(baselineWeight, currentWeight), new(big.Rat).Sub(currentRate, baselineRate)))
			mix.Mul(half, new(big.Rat).Mul(new(big.Rat).Add(baselineRate, currentRate), new(big.Rat).Sub(currentWeight, baselineWeight)))
		}
	} else if !segment.BaselinePresent && segment.CurrentPresent {
		if currentRate == nil || currentWeight == nil {
			entry = nil
		} else {
			entry.Mul(currentWeight, currentRate)
		}
	} else if segment.BaselinePresent && !segment.CurrentPresent {
		if baselineRate == nil || baselineWeight == nil {
			exit = nil
		} else {
			exit.Neg(new(big.Rat).Mul(baselineWeight, baselineRate))
		}
	}
	if !optionalClose(parseOptional(segment.RateEffect), rate) || !optionalClose(parseOptional(segment.MixEffect), mix) || !optionalClose(parseOptional(segment.EntryEffect), entry) || !optionalClose(parseOptional(segment.ExitEffect), exit) {
		return fmt.Errorf("ratio segment effect evidence is inconsistent")
	}
	var total *big.Rat
	if rate != nil && mix != nil && entry != nil && exit != nil {
		total = new(big.Rat).Add(new(big.Rat).Add(rate, mix), new(big.Rat).Add(entry, exit))
	}
	if !optionalClose(parseOptional(segment.TotalEffect), total) {
		return fmt.Errorf("ratio total effect evidence is inconsistent")
	}
	return nil
}

func memberValue(value any, memberType AttributionMemberType) (json.RawMessage, string, error) {
	if value == nil {
		return json.RawMessage("null"), "null", nil
	}
	key := ""
	switch memberType {
	case AttributionMemberString, AttributionMemberDate, AttributionMemberTime, AttributionMemberDateTime, AttributionMemberDateTimeTZ:
		if _, ok := value.(string); !ok {
			return nil, "", fmt.Errorf("string-like dimension member has type %T", value)
		}
		key = "string:" + value.(string)
	case AttributionMemberInteger:
		number, err := requiredNumber(value)
		if err != nil || !number.IsInt() {
			return nil, "", fmt.Errorf("integer dimension member is invalid")
		}
		key = "number:" + number.RatString()
	case AttributionMemberDecimal:
		number, err := requiredNumber(value)
		if err != nil {
			return nil, "", err
		}
		if _, ok := value.(string); !ok {
			return nil, "", fmt.Errorf("decimal dimension member has type %T", value)
		}
		key = "number:" + number.RatString()
	case AttributionMemberFloat:
		if _, ok := value.(json.Number); !ok {
			return nil, "", fmt.Errorf("float dimension member has type %T", value)
		}
		number, err := requiredNumber(value)
		if err != nil {
			return nil, "", err
		}
		key = "number:" + number.RatString()
	case AttributionMemberBoolean:
		if _, ok := value.(bool); !ok {
			return nil, "", fmt.Errorf("boolean dimension member has type %T", value)
		}
		key = fmt.Sprintf("boolean:%t", value.(bool))
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

func sameValues(left, right []any) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		a, _ := json.Marshal(left[i])
		b, _ := json.Marshal(right[i])
		if !bytes.Equal(a, b) {
			return false
		}
	}
	return true
}

func sortSegments[T any](segments []T, value func(T) json.RawMessage, memberType AttributionMemberType) {
	sort.SliceStable(segments, func(i, j int) bool { return compareMembers(value(segments[i]), value(segments[j]), memberType) < 0 })
}

func compareMembers(left, right json.RawMessage, memberType AttributionMemberType) int {
	if bytes.Equal(left, []byte("null")) {
		if bytes.Equal(right, []byte("null")) {
			return 0
		}
		return 1
	}
	if bytes.Equal(right, []byte("null")) {
		return -1
	}
	if memberType == AttributionMemberInteger || memberType == AttributionMemberDecimal || memberType == AttributionMemberFloat {
		var a, b any
		_ = json.Unmarshal(left, &a)
		_ = json.Unmarshal(right, &b)
		ar, _ := requiredNumber(a)
		br, _ := requiredNumber(b)
		if cmp := ar.Cmp(br); cmp != 0 {
			return cmp
		}
	}
	return bytes.Compare(left, right)
}
