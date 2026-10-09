package query

import (
	"bytes"
	"encoding/json"
	"fmt"
)

type TimeGrain string

const (
	TimeGrainYear    TimeGrain = "year"
	TimeGrainQuarter TimeGrain = "quarter"
	TimeGrainMonth   TimeGrain = "month"
	TimeGrainWeek    TimeGrain = "week"
	TimeGrainDay     TimeGrain = "day"
	TimeGrainHour    TimeGrain = "hour"
)

type MetricRef struct {
	Name string `json:"name"`
}
type DimensionRef struct {
	Name  string     `json:"name"`
	Grain *TimeGrain `json:"grain,omitempty"`
}

type FilterOperator string

const (
	FilterEQ        FilterOperator = "eq"
	FilterNEQ       FilterOperator = "neq"
	FilterGT        FilterOperator = "gt"
	FilterGTE       FilterOperator = "gte"
	FilterLT        FilterOperator = "lt"
	FilterLTE       FilterOperator = "lte"
	FilterIN        FilterOperator = "in"
	FilterNotIn     FilterOperator = "not_in"
	FilterBetween   FilterOperator = "between"
	FilterIsNull    FilterOperator = "is_null"
	FilterIsNotNull FilterOperator = "is_not_null"
)

type Filter struct {
	Field    string         `json:"field" jsonschema:"Canonical metric or dimension ref returned by semantic discovery."`
	Operator FilterOperator `json:"operator" jsonschema:"One of eq, neq, gt, gte, lt, lte, in, not_in, between, is_null, or is_not_null. between is inclusive and requires a two-item value array; in and not_in require an array; null operators omit value."`
	Value    any            `json:"value,omitempty" jsonschema:"JSON scalar or flat scalar array appropriate for operator. Dates and timestamps use ISO-8601 strings."`
	Kind     PredicateKind  `json:"-"`
	Children []Filter       `json:"-"`
}

type PredicateKind string

const (
	PredicateAnd PredicateKind = "and"
	PredicateOr  PredicateKind = "or"
	PredicateNot PredicateKind = "not"
)

// Predicate is the single public filters contract. Its top-level slice is an
// implicit AND for Go construction; JSON always uses one tagged node.
type Predicate []Filter

func Logical(kind PredicateKind, children ...Filter) Filter {
	return Filter{Kind: kind, Children: append([]Filter(nil), children...)}
}

func Leaf(field string, operator FilterOperator, value any) Filter {
	return Filter{Field: field, Operator: operator, Value: value}
}

func (f *Filter) UnmarshalJSON(data []byte) error {
	type filterJSON struct {
		Field    string          `json:"field"`
		Operator FilterOperator  `json:"operator"`
		Value    json.RawMessage `json:"value"`
	}
	var raw filterJSON
	if err := strictUnmarshal(data, &raw); err != nil {
		return err
	}
	value, err := decodeFilterValue(raw.Value)
	if err != nil {
		return err
	}
	f.Field = raw.Field
	f.Operator = raw.Operator
	f.Value = value
	return nil
}

func decodeFilterValue(raw json.RawMessage) (any, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if n, ok := value.(json.Number); ok {
		return ParseFilterNumber(string(n))
	}
	if value == nil {
		return nil, nil
	}
	if isFilterScalar(value) {
		return value, nil
	}
	items, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("filter value must be a scalar or flat scalar array")
	}
	for i, item := range items {
		if n, ok := item.(json.Number); ok {
			checked, err := ParseFilterNumber(string(n))
			if err != nil {
				return nil, err
			}
			items[i] = checked
			continue
		}
		if !isFilterScalar(item) && item != nil {
			return nil, fmt.Errorf("filter array values must be scalar")
		}
	}
	return items, nil
}

func isFilterScalar(value any) bool {
	switch value.(type) {
	case string, float64, bool:
		return true
	default:
		return false
	}
}

type SortDirection string

const (
	SortAsc  SortDirection = "asc"
	SortDesc SortDirection = "desc"
)

type OrderBy struct {
	Field     string        `json:"field"`
	Direction SortDirection `json:"direction"`
}

type QueryIntent string

const QueryIntentDistinctValues QueryIntent = "distinct_values"

type SemanticQuery struct {
	Project    string         `json:"project"`
	Model      string         `json:"model"`
	Intent     QueryIntent    `json:"intent,omitempty"`
	Metrics    []MetricRef    `json:"metrics,omitempty"`
	Dimensions []DimensionRef `json:"dimensions,omitempty"`
	Filters    Predicate      `json:"filters,omitempty"`
	OrderBy    []OrderBy      `json:"order_by,omitempty"`
	Limit      *int           `json:"limit,omitempty"`
}

// UnmarshalJSON keeps physical output selection outside semantic identity.
// CompileRequest.dialect is the only compile-only physical-output selector.
func (q *SemanticQuery) UnmarshalJSON(data []byte) error {
	if err := rejectDuplicateJSONKeys(data); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	allowed := map[string]bool{"project": true, "model": true, "intent": true, "metrics": true, "dimensions": true, "filters": true, "order_by": true, "limit": true}
	for field := range fields {
		if !allowed[field] {
			return fmt.Errorf("semantic query contains unknown field %q", field)
		}
	}
	for _, field := range []string{"target", "execution_binding"} {
		value, ok := fields[field]
		if ok && len(value) != 0 && string(value) != "null" {
			return fmt.Errorf("semantic query %s is not supported; use dialect on the compile request", field)
		}
	}
	type semanticQueryJSON SemanticQuery
	var decoded semanticQueryJSON
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*q = SemanticQuery(decoded)
	return nil
}
