package query

import (
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
}

func (f *Filter) UnmarshalJSON(data []byte) error {
	type filterJSON struct {
		Field    string          `json:"field"`
		Operator FilterOperator  `json:"operator"`
		Value    json.RawMessage `json:"value"`
	}
	var raw filterJSON
	if err := json.Unmarshal(data, &raw); err != nil {
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
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	if isFilterScalar(value) {
		return value, nil
	}
	items, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("filter value must be a scalar or flat scalar array")
	}
	for _, item := range items {
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
	Filters    []Filter       `json:"filters,omitempty"`
	OrderBy    []OrderBy      `json:"order_by,omitempty"`
	Limit      *int           `json:"limit,omitempty"`
}

// UnmarshalJSON keeps physical output selection outside semantic identity.
// CompileRequest.dialect is the only compile-only physical-output selector.
func (q *SemanticQuery) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
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
