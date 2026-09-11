// Package attribution evaluates complete, normalized metric-attribution
// evidence bundles. It owns no semantic resolution, SQL, or execution state.
package attribution

import "encoding/json"

type DecimalString string
type AttributionMemberValue = json.RawMessage
type AttributionMemberType string

const (
	AttributionMemberString     AttributionMemberType = "string"
	AttributionMemberInteger    AttributionMemberType = "integer"
	AttributionMemberDecimal    AttributionMemberType = "decimal"
	AttributionMemberFloat      AttributionMemberType = "float"
	AttributionMemberBoolean    AttributionMemberType = "boolean"
	AttributionMemberDate       AttributionMemberType = "date"
	AttributionMemberTime       AttributionMemberType = "time"
	AttributionMemberDateTime   AttributionMemberType = "datetime"
	AttributionMemberDateTimeTZ AttributionMemberType = "datetime_tz"
)

type AttributionStrategy string

const (
	AttributionStrategyAdditive AttributionStrategy = "additive_contribution"
	AttributionStrategyRatio    AttributionStrategy = "ratio_mix_rate"
)

type AttributionPeriod struct {
	Start string `json:"start" jsonschema:"Inclusive RFC3339 timestamp with an explicit offset."`
	End   string `json:"end" jsonschema:"Exclusive RFC3339 timestamp with an explicit offset; must be after start."`
}

type AttributeMetricResult struct {
	AnalysisID    string                       `json:"analysis_id"`
	Metric        string                       `json:"metric"`
	TimeDimension string                       `json:"time_dimension"`
	Baseline      AttributionPeriod            `json:"baseline"`
	Current       AttributionPeriod            `json:"current"`
	Strategy      AttributionStrategy          `json:"strategy"`
	Dimensions    []DimensionAttributionResult `json:"dimensions"`
}

type DimensionAttributionResult struct {
	Dimension  string                     `json:"dimension"`
	MemberType AttributionMemberType      `json:"member_type"`
	Additive   *AdditiveAttributionResult `json:"additive,omitempty"`
	Ratio      *RatioAttributionResult    `json:"ratio,omitempty"`
}

type AdditiveAttributionResult struct {
	Summary  AdditiveAttributionSummary   `json:"summary"`
	Segments []AdditiveAttributionSegment `json:"segments"`
}

type AdditiveAttributionSummary struct {
	BaselineTotal       DecimalString `json:"baseline_total"`
	CurrentTotal        DecimalString `json:"current_total"`
	TotalDelta          DecimalString `json:"total_delta"`
	ContributionDefined bool          `json:"contribution_defined"`
	Reconciled          bool          `json:"reconciled"`
}

type AdditiveAttributionSegment struct {
	Value           AttributionMemberValue `json:"value"`
	BaselineValue   DecimalString          `json:"baseline_value"`
	CurrentValue    DecimalString          `json:"current_value"`
	Delta           DecimalString          `json:"delta"`
	ContributionPct *DecimalString         `json:"contribution_pct"`
}

type RatioAttributionResult struct {
	Summary  RatioAttributionSummary   `json:"summary"`
	Segments []RatioAttributionSegment `json:"segments"`
}

type RatioAttributionSummary struct {
	BaselineNumerator      *DecimalString `json:"baseline_numerator"`
	BaselineDenominator    *DecimalString `json:"baseline_denominator"`
	CurrentNumerator       *DecimalString `json:"current_numerator"`
	CurrentDenominator     *DecimalString `json:"current_denominator"`
	BaselineRatio          *DecimalString `json:"baseline_ratio"`
	CurrentRatio           *DecimalString `json:"current_ratio"`
	RatioDelta             *DecimalString `json:"ratio_delta"`
	DecomposedDelta        *DecimalString `json:"decomposed_delta"`
	ReconciliationResidual *DecimalString `json:"reconciliation_residual"`
	AttributionDefined     bool           `json:"attribution_defined"`
	Reconciled             bool           `json:"reconciled"`
}

type RatioAttributionSegment struct {
	Value               AttributionMemberValue `json:"value"`
	BaselinePresent     bool                   `json:"baseline_present"`
	CurrentPresent      bool                   `json:"current_present"`
	BaselineNumerator   DecimalString          `json:"baseline_numerator"`
	BaselineDenominator DecimalString          `json:"baseline_denominator"`
	CurrentNumerator    DecimalString          `json:"current_numerator"`
	CurrentDenominator  DecimalString          `json:"current_denominator"`
	BaselineRate        *DecimalString         `json:"baseline_rate"`
	CurrentRate         *DecimalString         `json:"current_rate"`
	BaselineWeight      *DecimalString         `json:"baseline_weight"`
	CurrentWeight       *DecimalString         `json:"current_weight"`
	BaselineDefined     bool                   `json:"baseline_defined"`
	CurrentDefined      bool                   `json:"current_defined"`
	SegmentDefined      bool                   `json:"segment_defined"`
	RateEffect          *DecimalString         `json:"rate_effect"`
	MixEffect           *DecimalString         `json:"mix_effect"`
	EntryEffect         *DecimalString         `json:"entry_effect"`
	ExitEffect          *DecimalString         `json:"exit_effect"`
	TotalEffect         *DecimalString         `json:"total_effect"`
}
