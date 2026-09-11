// Package comparison evaluates complete normalized two-period metric evidence.
// It owns no semantic resolution, SQL, database, or execution state.
package comparison

import "encoding/json"

type DecimalString string
type MemberValue = json.RawMessage
type MemberType string

const (
	MemberString     MemberType = "string"
	MemberInteger    MemberType = "integer"
	MemberDecimal    MemberType = "decimal"
	MemberFloat      MemberType = "float"
	MemberBoolean    MemberType = "boolean"
	MemberDate       MemberType = "date"
	MemberTime       MemberType = "time"
	MemberDateTime   MemberType = "datetime"
	MemberDateTimeTZ MemberType = "datetime_tz"
)

type Period struct {
	Start string `json:"start" jsonschema:"Inclusive RFC3339 timestamp with an explicit offset."`
	End   string `json:"end" jsonschema:"Exclusive RFC3339 timestamp with an explicit offset; must be after start."`
}

type Member struct {
	Dimension  string      `json:"dimension"`
	MemberType MemberType  `json:"member_type"`
	Value      MemberValue `json:"value"`
}

type MetricValue struct {
	Metric         string         `json:"metric"`
	BaselineValue  *DecimalString `json:"baseline_value"`
	CurrentValue   *DecimalString `json:"current_value"`
	Delta          *DecimalString `json:"delta"`
	PercentChange  *DecimalString `json:"percent_change"`
	ChangeDefined  bool           `json:"change_defined"`
	PercentDefined bool           `json:"percent_defined"`
}

type Row struct {
	Members         []Member      `json:"members"`
	BaselinePresent bool          `json:"baseline_present"`
	CurrentPresent  bool          `json:"current_present"`
	Values          []MetricValue `json:"values"`
}

type Result struct {
	AnalysisID    string   `json:"analysis_id"`
	TimeDimension string   `json:"time_dimension"`
	Baseline      Period   `json:"baseline"`
	Current       Period   `json:"current"`
	Metrics       []string `json:"metrics"`
	Dimensions    []string `json:"dimensions"`
	Rows          []Row    `json:"rows"`
}

type OutputRef struct {
	Public string
	Column string
}

type Descriptor struct {
	AnalysisID    string
	TimeDimension string
	Baseline      Period
	Current       Period
	Metrics       []OutputRef
	Dimensions    []OutputRef
}
