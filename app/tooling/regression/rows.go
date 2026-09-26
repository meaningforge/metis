package regression

import (
	"encoding/json"
	"fmt"
	"math/big"
	"regexp"
	"strings"
	"time"
)

const maxResultRows = 1000
const maxResultBytes = 1 << 20

type Scalar struct {
	Type  string `json:"type" yaml:"type"`
	Value any    `json:"value,omitempty" yaml:"value"`
}

type Tolerance struct {
	Abs string `json:"abs,omitempty" yaml:"abs"`
	Rel string `json:"rel,omitempty" yaml:"rel"`
}

type ExpectedRows struct {
	Mode       string               `json:"mode" yaml:"mode"`
	Values     [][]Scalar           `json:"values" yaml:"values"`
	KeyColumns []string             `json:"key_columns,omitempty" yaml:"key_columns"`
	Tolerances map[string]Tolerance `json:"tolerances,omitempty" yaml:"tolerances"`
}

var decimalText = regexp.MustCompile(`^[+-]?[0-9]+(?:\.[0-9]+)?(?:[eE][+-]?[0-9]{1,3})?$`)
var integerText = regexp.MustCompile(`^[+-]?[0-9]+$`)

func exactNumber(text string) (*big.Rat, error) {
	if len(text) > 256 || !decimalText.MatchString(text) {
		return nil, fmt.Errorf("invalid bounded decimal")
	}
	n, ok := new(big.Rat).SetString(text)
	if !ok {
		return nil, fmt.Errorf("invalid decimal")
	}
	return n, nil
}

func scalarKind(datatype string) string {
	switch datatype {
	case "String":
		return "string"
	case "Integer":
		return "integer"
	case "Decimal":
		return "decimal"
	case "Float":
		return "float"
	case "Boolean":
		return "bool"
	case "Date":
		return "date"
	case "Time":
		return "time"
	case "DateTime":
		return "datetime"
	case "DateTimeTz":
		return "datetime_tz"
	default:
		return "unsupported"
	}
}

func canonicalScalar(s Scalar, datatype string) (string, error) {
	if s.Type == "null" {
		if s.Value != nil {
			return "", fmt.Errorf("null must have no value")
		}
		return "null", nil
	}
	if s.Type != scalarKind(datatype) {
		return "", fmt.Errorf("scalar tag does not match declared datatype")
	}
	if s.Type == "bool" {
		value, ok := s.Value.(bool)
		if !ok {
			return "", fmt.Errorf("bool requires a boolean")
		}
		return fmt.Sprintf("bool:%t", value), nil
	}
	text, ok := s.Value.(string)
	if !ok {
		return "", fmt.Errorf("%s requires an explicit string value", s.Type)
	}
	switch s.Type {
	case "integer", "decimal", "float":
		if s.Type == "integer" && !integerText.MatchString(text) {
			return "", fmt.Errorf("integer requires exact integer text")
		}
		n, err := exactNumber(text)
		if err != nil {
			return "", err
		}
		return s.Type + ":" + n.RatString(), nil
	case "date", "time", "datetime", "datetime_tz":
		layouts := map[string]string{"date": "2006-01-02", "time": "15:04:05.999999999", "datetime": "2006-01-02T15:04:05.999999999", "datetime_tz": time.RFC3339Nano}
		instant, err := time.Parse(layouts[s.Type], text)
		if err != nil {
			return "", fmt.Errorf("invalid %s literal", s.Type)
		}
		if s.Type == "datetime_tz" {
			instant = instant.UTC()
		}
		return s.Type + ":" + instant.Format(layouts[s.Type]), nil
	case "string":
		return "string:" + text, nil
	default:
		return "", fmt.Errorf("unsupported scalar")
	}
}

func normalizedScalar(value any, datatype string) (Scalar, error) {
	if value == nil {
		return Scalar{Type: "null"}, nil
	}
	if number, ok := value.(json.Number); ok {
		value = string(number)
	}
	s := Scalar{Type: scalarKind(datatype), Value: value}
	_, err := canonicalScalar(s, datatype)
	return s, err
}

func validateRuntimeCase(c Case) error {
	if c.Expect.SQLRenderResult != nil {
		return fmt.Errorf("runtime cases cannot override or snapshot SQL")
	}
	if c.Expect.Outcome != "success" {
		if runtimeFailureCode(c.Expect.Code) {
			return fmt.Errorf("runtime failures cannot be expected semantic errors")
		}
		return nil
	}
	if c.Expect.RowCount == nil || *c.Expect.RowCount < 0 || *c.Expect.RowCount > maxResultRows || c.Expect.Rows == nil {
		return fmt.Errorf("runtime success requires row_count from 0 to %d and complete rows", maxResultRows)
	}
	rows := c.Expect.Rows
	if rows.Mode != "ordered" && rows.Mode != "unordered" {
		return fmt.Errorf("rows mode must be ordered or unordered")
	}
	if int64(len(rows.Values)) != *c.Expect.RowCount {
		return fmt.Errorf("row_count must match expected rows")
	}
	columns := c.Expect.OutputSchema.Columns
	names := map[string]int{}
	for i, col := range columns {
		if _, exists := names[col.Name]; exists || scalarKind(col.Datatype) == "unsupported" {
			return fmt.Errorf("runtime columns require unique names and supported scalar datatypes")
		}
		names[col.Name] = i
	}
	keys := map[string]bool{}
	for _, name := range rows.KeyColumns {
		if _, ok := names[name]; !ok || keys[name] {
			return fmt.Errorf("key_columns must name unique output columns")
		}
		keys[name] = true
	}
	for name, tolerance := range rows.Tolerances {
		index, ok := names[name]
		if !ok || keys[name] {
			return fmt.Errorf("tolerance must name a non-key output column")
		}
		kind := scalarKind(columns[index].Datatype)
		if kind != "integer" && kind != "decimal" && kind != "float" {
			return fmt.Errorf("tolerance requires a numeric column")
		}
		if tolerance.Abs == "" && tolerance.Rel == "" {
			return fmt.Errorf("empty tolerance")
		}
		for _, text := range []string{tolerance.Abs, tolerance.Rel} {
			if text == "" {
				continue
			}
			n, err := exactNumber(text)
			if err != nil || n.Sign() < 0 {
				return fmt.Errorf("tolerances must be finite nonnegative decimal strings")
			}
		}
	}
	needsKeys := (rows.Mode == "unordered" && len(rows.Tolerances) > 0) || (rows.Mode == "ordered" && len(rows.Values) > 1)
	if needsKeys && len(rows.KeyColumns) == 0 {
		return fmt.Errorf("approximate unordered or multi-row ordered results require exact unique key_columns")
	}
	if rows.Mode == "ordered" && len(rows.Values) > 1 {
		ordered := map[string]bool{}
		for _, order := range c.Request.Query.OrderBy {
			ordered[order.Field] = true
		}
		for _, key := range rows.KeyColumns {
			if !ordered[key] {
				return fmt.Errorf("ordered results must explicitly order_by every key column")
			}
		}
	}
	for _, row := range rows.Values {
		if len(row) != len(columns) {
			return fmt.Errorf("expected row width must match schema")
		}
		for i, cell := range row {
			if _, err := canonicalScalar(cell, columns[i].Datatype); err != nil {
				return err
			}
		}
	}
	if len(rows.KeyColumns) > 0 {
		if _, err := rowIndex(rows.Values, columns, rows.KeyColumns); err != nil {
			return err
		}
	}
	encoded, err := json.Marshal(rows.Values)
	if err != nil || len(encoded) > maxResultBytes {
		return fmt.Errorf("expected rows exceed byte limit")
	}
	return nil
}

func rowKey(row []Scalar, columns []ExpectedColumn, selected []string) (string, error) {
	parts := make([]string, 0, len(columns))
	for i, col := range columns {
		if len(selected) > 0 {
			found := false
			for _, name := range selected {
				if name == col.Name {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		value, err := canonicalScalar(row[i], col.Datatype)
		if err != nil {
			return "", err
		}
		parts = append(parts, value)
	}
	encoded, err := json.Marshal(parts)
	return string(encoded), err
}

func rowIndex(rows [][]Scalar, columns []ExpectedColumn, keys []string) (map[string]int, error) {
	index := map[string]int{}
	for i, row := range rows {
		key, err := rowKey(row, columns, keys)
		if err != nil {
			return nil, err
		}
		if _, exists := index[key]; exists {
			return nil, fmt.Errorf("duplicate result key")
		}
		index[key] = i
	}
	return index, nil
}

func cellMatches(want, got Scalar, datatype string, tolerance Tolerance) bool {
	w, we := canonicalScalar(want, datatype)
	g, ge := canonicalScalar(got, datatype)
	if we != nil || ge != nil {
		return false
	}
	if w == g {
		return true
	}
	if want.Type != got.Type || want.Type == "null" || (tolerance.Abs == "" && tolerance.Rel == "") {
		return false
	}
	wn, err := exactNumber(want.Value.(string))
	if err != nil {
		return false
	}
	gn, err := exactNumber(got.Value.(string))
	if err != nil {
		return false
	}
	abs, rel := new(big.Rat), new(big.Rat)
	if tolerance.Abs != "" {
		abs, _ = exactNumber(tolerance.Abs)
	}
	if tolerance.Rel != "" {
		rel, _ = exactNumber(tolerance.Rel)
	}
	rel.Mul(rel, new(big.Rat).Abs(wn))
	if rel.Cmp(abs) > 0 {
		abs = rel
	}
	difference := new(big.Rat).Abs(new(big.Rat).Sub(gn, wn))
	return difference.Cmp(abs) <= 0
}

func compareRows(expected ExpectedRows, actual [][]Scalar, columns []ExpectedColumn) []string {
	if len(expected.Values) != len(actual) {
		return []string{"rows.length"}
	}
	var actualIndex map[string]int
	if len(expected.KeyColumns) > 0 {
		var err error
		actualIndex, err = rowIndex(actual, columns, expected.KeyColumns)
		if err != nil {
			return []string{"rows.duplicate_keys"}
		}
	}
	if expected.Mode == "unordered" && len(expected.Tolerances) == 0 {
		counts := map[string]int{}
		for _, row := range expected.Values {
			key, _ := rowKey(row, columns, nil)
			counts[key]++
		}
		for _, row := range actual {
			key, err := rowKey(row, columns, nil)
			if err != nil {
				return []string{"rows.invalid_value"}
			}
			counts[key]--
		}
		for _, count := range counts {
			if count != 0 {
				return []string{"rows.multiset"}
			}
		}
		return nil
	}
	differences := []string{}
	for i, want := range expected.Values {
		position := i
		if expected.Mode == "unordered" {
			key, _ := rowKey(want, columns, expected.KeyColumns)
			var found bool
			position, found = actualIndex[key]
			if !found {
				return []string{"rows.key_set"}
			}
		}
		for j, cell := range want {
			if !cellMatches(cell, actual[position][j], columns[j].Datatype, expected.Tolerances[columns[j].Name]) {
				differences = append(differences, fmt.Sprintf("rows[%d][%d]", i, j))
				if len(differences) == maxReportedDifferences {
					return differences
				}
			}
		}
	}
	return differences
}

// Runtime outages must never be accepted as expected semantic errors.
func runtimeFailureCode(code string) bool {
	return strings.HasPrefix(code, "QUERY_EXECUTION_") || strings.Contains(code, "EXECUTION_CONFIG") || code == "QUERY_RESULT_SCHEMA_MISMATCH" || strings.Contains(code, "ACCESS_DENIED") || strings.Contains(code, "POLICY")
}
