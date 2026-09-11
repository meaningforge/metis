package scenarios

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/meaningforge/metis/cmd/s2sbench/bench/fixtures"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/query"
)

// ResultValueKind is the engine-neutral value family used by real-engine
// conformance. It is intentionally coarser than a physical database type:
// Float64 and Decimal(38, 9), for example, both prove a semantic number.
type ResultValueKind string

const (
	ResultString   ResultValueKind = "string"
	ResultInteger  ResultValueKind = "integer"
	ResultNumber   ResultValueKind = "number"
	ResultBoolean  ResultValueKind = "boolean"
	ResultDate     ResultValueKind = "date"
	ResultTime     ResultValueKind = "time"
	ResultDateTime ResultValueKind = "datetime"
	ResultOpaque   ResultValueKind = "opaque"
)

// ResultColumn is the stable logical output contract for one scenario column.
// Backends independently map their physical result metadata into ValueKind.
type ResultColumn struct {
	Name      string
	ValueKind ResultValueKind
}

// ResultValue stores one normalized typed scalar. Canonical is comparable
// across backend representations; Null is independent of physical NULL syntax.
type ResultValue struct {
	ValueKind ResultValueKind
	Canonical string
	Null      bool
}

// ResultRow is one typed logical row in a canonical or backend result set.
type ResultRow []ResultValue

// ResultSet is the normalized output returned by a real-engine adapter.
type ResultSet struct {
	Columns []ResultColumn
	Rows    []ResultRow
}

// ResultComparison declares whether physical row sequence is significant.
type ResultComparison string

const (
	ResultUnordered ResultComparison = "unordered"
	ResultOrdered   ResultComparison = "ordered"
)

// ResultExpectation is the canonical semantic result for a shared scenario.
// Ordered comparisons preserve row sequence; unordered comparisons retain
// duplicate rows while ignoring backend-dependent presentation order.
type ResultExpectation struct {
	ResultSet
	Comparison ResultComparison
}

// ResultLiteral keeps scenario declarations compact. resultFixtureScenario
// compiles every literal into a typed ResultValue using the fixture's Ossie
// output contract before it enters the canonical registry.
type ResultLiteral []string

// NullValue is the declaration-only sentinel for an expected SQL NULL.
const NullValue = "<null>"

// NumberComparisonTolerance is the engine-neutral absolute tolerance used
// only after production Runner normalization maps Decimal and Float into the
// coarse real-engine conformance number family.
const NumberComparisonTolerance = "0.000000001"

var numberComparisonTolerance = func() *big.Rat {
	value, ok := new(big.Rat).SetString(NumberComparisonTolerance)
	if !ok {
		panic("invalid conformance number tolerance")
	}
	return value
}()

// NumbersWithinTolerance compares two canonical numeric strings without
// passing through binary floating point.
func NumbersWithinTolerance(left, right string) bool {
	leftNumber, leftOK := new(big.Rat).SetString(strings.TrimSpace(left))
	rightNumber, rightOK := new(big.Rat).SetString(strings.TrimSpace(right))
	if !leftOK || !rightOK {
		return false
	}
	difference := new(big.Rat).Sub(leftNumber, rightNumber)
	difference.Abs(difference)
	return difference.Cmp(numberComparisonTolerance) <= 0
}

// ParseResultValue normalizes one non-NULL backend or expectation scalar.
func ParseResultValue(kind ResultValueKind, raw string) (ResultValue, error) {
	value := ResultValue{ValueKind: kind}
	switch kind {
	case ResultString, ResultOpaque:
		value.Canonical = raw
	case ResultInteger:
		integer, ok := new(big.Int).SetString(strings.TrimSpace(raw), 10)
		if !ok {
			return ResultValue{}, fmt.Errorf("invalid integer %q", raw)
		}
		value.Canonical = integer.String()
	case ResultNumber:
		number, ok := new(big.Rat).SetString(strings.TrimSpace(raw))
		if !ok {
			return ResultValue{}, fmt.Errorf("invalid number %q", raw)
		}
		value.Canonical = number.RatString()
	case ResultBoolean:
		switch strings.ToLower(strings.TrimSpace(raw)) {
		case "1":
			value.Canonical = strconv.FormatBool(true)
		case "0":
			value.Canonical = strconv.FormatBool(false)
		default:
			parsed, err := strconv.ParseBool(strings.TrimSpace(raw))
			if err != nil {
				return ResultValue{}, fmt.Errorf("invalid boolean %q", raw)
			}
			value.Canonical = strconv.FormatBool(parsed)
		}
	case ResultDate:
		parsed, err := time.Parse("2006-01-02", strings.TrimSpace(raw))
		if err != nil {
			return ResultValue{}, fmt.Errorf("invalid date %q", raw)
		}
		value.Canonical = parsed.Format("2006-01-02")
	case ResultTime:
		parsed, err := parseTime(raw)
		if err != nil {
			return ResultValue{}, err
		}
		value.Canonical = parsed
	case ResultDateTime:
		parsed, err := parseDateTime(raw)
		if err != nil {
			return ResultValue{}, err
		}
		value.Canonical = parsed
	default:
		return ResultValue{}, fmt.Errorf("unknown result value kind %q", kind)
	}
	return value, nil
}

// NullResultValue constructs a typed NULL for the supplied logical column.
func NullResultValue(kind ResultValueKind) ResultValue {
	return ResultValue{ValueKind: kind, Null: true}
}

func buildResultExpectation(fixture fixtures.ID, q query.SemanticQuery, literals []ResultLiteral) *ResultExpectation {
	columns, err := resultColumns(fixture, q)
	if err != nil {
		panic(err)
	}
	rows := make([]ResultRow, len(literals))
	for rowIndex, literal := range literals {
		if len(literal) != len(columns) {
			panic(fmt.Sprintf("fixture %q result row %d has %d values, want %d", fixture, rowIndex, len(literal), len(columns)))
		}
		row := make(ResultRow, len(literal))
		for columnIndex, raw := range literal {
			kind := columns[columnIndex].ValueKind
			if raw == NullValue {
				row[columnIndex] = NullResultValue(kind)
				continue
			}
			value, err := ParseResultValue(kind, raw)
			if err != nil {
				panic(fmt.Sprintf("fixture %q result row %d column %q: %v", fixture, rowIndex, columns[columnIndex].Name, err))
			}
			row[columnIndex] = value
		}
		rows[rowIndex] = row
	}
	comparison := ResultUnordered
	if len(q.OrderBy) != 0 {
		comparison = ResultOrdered
	}
	return &ResultExpectation{
		ResultSet:  ResultSet{Columns: columns, Rows: rows},
		Comparison: comparison,
	}
}

func resultColumns(fixture fixtures.ID, q query.SemanticQuery) ([]ResultColumn, error) {
	definition, ok := fixtures.Lookup(fixture)
	if !ok {
		return nil, fmt.Errorf("unknown canonical fixture %q", fixture)
	}
	document, err := ossie.NewLoader().Load(definition.Document)
	if err != nil {
		return nil, fmt.Errorf("load canonical fixture %q: %w", fixture, err)
	}
	var model *ossie.SemanticModel
	for i := range document.SemanticModel {
		if document.SemanticModel[i].Name == definition.Model {
			model = &document.SemanticModel[i]
			break
		}
	}
	if model == nil {
		return nil, fmt.Errorf("fixture %q does not define model %q", fixture, definition.Model)
	}

	columns := make([]ResultColumn, 0, len(q.Dimensions)+len(q.Metrics))
	for _, dimension := range q.Dimensions {
		field, err := findResultField(*model, dimension.Name)
		if err != nil {
			return nil, fmt.Errorf("fixture %q dimension %q: %w", fixture, dimension.Name, err)
		}
		columns = append(columns, ResultColumn{Name: dimension.Name, ValueKind: resultKind(field.Datatype)})
	}
	for _, metricRef := range q.Metrics {
		metric, ok := findResultMetric(*model, metricRef.Name)
		if !ok {
			return nil, fmt.Errorf("fixture %q does not define metric %q", fixture, metricRef.Name)
		}
		columns = append(columns, ResultColumn{Name: metricRef.Name, ValueKind: resultKind(metric.Datatype)})
	}
	return columns, nil
}

func findResultField(model ossie.SemanticModel, name string) (ossie.Field, error) {
	parts := strings.Split(name, ".")
	if len(parts) == 2 {
		for _, dataset := range model.Datasets {
			if dataset.Name != parts[0] {
				continue
			}
			for _, field := range dataset.Fields {
				if field.Name == parts[1] {
					return field, nil
				}
			}
		}
		return ossie.Field{}, fmt.Errorf("unknown qualified field")
	}
	var found ossie.Field
	foundAny := false
	for _, dataset := range model.Datasets {
		for i := range dataset.Fields {
			if dataset.Fields[i].Name != name {
				continue
			}
			if foundAny {
				return ossie.Field{}, fmt.Errorf("ambiguous field")
			}
			found = dataset.Fields[i]
			foundAny = true
		}
	}
	if !foundAny {
		return ossie.Field{}, fmt.Errorf("unknown field")
	}
	return found, nil
}

func findResultMetric(model ossie.SemanticModel, name string) (ossie.Metric, bool) {
	for _, metric := range model.Metrics {
		if metric.Name == name {
			return metric, true
		}
	}
	return ossie.Metric{}, false
}

func resultKind(datatype ossie.DataType) ResultValueKind {
	switch datatype {
	case ossie.DataTypeString:
		return ResultString
	case ossie.DataTypeInteger:
		return ResultInteger
	case ossie.DataTypeDecimal, ossie.DataTypeFloat:
		return ResultNumber
	case ossie.DataTypeBoolean:
		return ResultBoolean
	case ossie.DataTypeDate:
		return ResultDate
	case ossie.DataTypeTime:
		return ResultTime
	case ossie.DataTypeDateTime, ossie.DataTypeDateTimeTz:
		return ResultDateTime
	default:
		return ResultOpaque
	}
}

func parseTime(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	for _, layout := range []string{"15:04:05.999999999", "15:04:05"} {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return parsed.Format("15:04:05.999999999"), nil
		}
	}
	return "", fmt.Errorf("invalid time %q", raw)
}

func parseDateTime(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05.999999999Z07:00", "2006-01-02 15:04:05.999999999", "2006-01-02 15:04:05"} {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return parsed.Format(time.RFC3339Nano), nil
		}
	}
	return "", fmt.Errorf("invalid datetime %q", raw)
}
