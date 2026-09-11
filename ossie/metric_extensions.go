package ossie

import (
	"encoding/json"
	"fmt"
)

const MetisExtensionVendor Vendor = "METIS"

type MetricExtensionKind string

const (
	MetricExtensionCumulative   MetricExtensionKind = "cumulative"
	MetricExtensionTimeOffset   MetricExtensionKind = "time_offset"
	MetricExtensionSemiAdditive MetricExtensionKind = "semi_additive"
	MetricExtensionTimeBinding  MetricExtensionKind = "time_binding"
)

const (
	SemiAdditiveRollupSum = "sum"
	SemiAdditiveRollupMin = "min"
	SemiAdditiveRollupMax = "max"
)

type CumulativeMetricSpec struct {
	Kind          MetricExtensionKind `json:"kind"`
	BaseMetric    string              `json:"base_metric"`
	TimeDimension string              `json:"time_dimension"`
	Window        CumulativeWindow    `json:"window"`
}

type CumulativeWindow struct {
	Type  string `json:"type"`
	Count int    `json:"count,omitempty"`
	Unit  string `json:"unit,omitempty"`
}

type TimeOffsetMetricSpec struct {
	Kind          MetricExtensionKind `json:"kind"`
	BaseMetric    string              `json:"base_metric"`
	TimeDimension string              `json:"time_dimension"`
	Offset        TimeOffset          `json:"offset"`
}
type TimeOffset struct {
	Count int    `json:"count"`
	Unit  string `json:"unit"`
}

type SemiAdditiveMetricSpec struct {
	Kind                 MetricExtensionKind `json:"kind"`
	BaseMetric           string              `json:"base_metric"`
	NonAdditiveDimension string              `json:"non_additive_dimension"`
	Aggregation          string              `json:"aggregation"`
	TieBreakDimension    string              `json:"tie_break_dimension,omitempty"`
	NullPolicy           string              `json:"null_policy,omitempty"`
	WindowGroupings      []string            `json:"window_groupings,omitempty"`
	RollupAggregation    string              `json:"rollup_aggregation,omitempty"`
}

type MetricTimeBindingSpec struct {
	Kind          MetricExtensionKind `json:"kind"`
	TimeDimension string              `json:"time_dimension"`
}

func metricExtensionData(metric *Metric, want MetricExtensionKind) (string, bool, error) {
	if metric == nil {
		return "", false, nil
	}
	found := ""
	for _, extension := range metric.CustomExtensions {
		if extension.VendorName != MetisExtensionVendor {
			continue
		}
		var envelope struct {
			Kind MetricExtensionKind `json:"kind"`
		}
		if err := json.Unmarshal([]byte(extension.Data), &envelope); err != nil {
			return "", false, fmt.Errorf("decode METIS metric extension: %w", err)
		}
		if envelope.Kind != want {
			continue
		}
		if found != "" {
			return "", false, fmt.Errorf("duplicate METIS metric extension kind %q", want)
		}
		found = extension.Data
	}
	return found, found != "", nil
}

func CumulativeSpec(metric *Metric) (CumulativeMetricSpec, bool, error) {
	data, ok, err := metricExtensionData(metric, MetricExtensionCumulative)
	if err != nil || !ok {
		return CumulativeMetricSpec{}, ok, err
	}
	var spec CumulativeMetricSpec
	if err := json.Unmarshal([]byte(data), &spec); err != nil {
		return CumulativeMetricSpec{}, false, fmt.Errorf("decode cumulative metric extension: %w", err)
	}
	if err := ValidateCumulativeSpec(spec); err != nil {
		return CumulativeMetricSpec{}, false, err
	}
	return spec, true, nil
}
func TimeOffsetSpec(metric *Metric) (TimeOffsetMetricSpec, bool, error) {
	data, ok, err := metricExtensionData(metric, MetricExtensionTimeOffset)
	if err != nil || !ok {
		return TimeOffsetMetricSpec{}, ok, err
	}
	var spec TimeOffsetMetricSpec
	if err := json.Unmarshal([]byte(data), &spec); err != nil {
		return TimeOffsetMetricSpec{}, false, fmt.Errorf("decode time-offset metric extension: %w", err)
	}
	if err := ValidateTimeOffsetSpec(spec); err != nil {
		return TimeOffsetMetricSpec{}, false, err
	}
	return spec, true, nil
}
func SemiAdditiveSpec(metric *Metric) (SemiAdditiveMetricSpec, bool, error) {
	data, ok, err := metricExtensionData(metric, MetricExtensionSemiAdditive)
	if err != nil || !ok {
		return SemiAdditiveMetricSpec{}, ok, err
	}
	var spec SemiAdditiveMetricSpec
	if err := json.Unmarshal([]byte(data), &spec); err != nil {
		return SemiAdditiveMetricSpec{}, false, fmt.Errorf("decode semi-additive metric extension: %w", err)
	}
	if err := ValidateSemiAdditiveSpec(spec); err != nil {
		return SemiAdditiveMetricSpec{}, false, err
	}
	return spec, true, nil
}
func MetricTimeBinding(metric *Metric) (MetricTimeBindingSpec, bool, error) {
	data, ok, err := metricExtensionData(metric, MetricExtensionTimeBinding)
	if err != nil || !ok {
		return MetricTimeBindingSpec{}, ok, err
	}
	var spec MetricTimeBindingSpec
	if err := json.Unmarshal([]byte(data), &spec); err != nil {
		return MetricTimeBindingSpec{}, false, fmt.Errorf("decode metric time-binding extension: %w", err)
	}
	if err := ValidateMetricTimeBinding(spec); err != nil {
		return MetricTimeBindingSpec{}, false, err
	}
	return spec, true, nil
}

func ValidateCumulativeSpec(spec CumulativeMetricSpec) error {
	if spec.Kind != MetricExtensionCumulative {
		return fmt.Errorf("cumulative extension kind must be %q", MetricExtensionCumulative)
	}
	if spec.BaseMetric == "" {
		return fmt.Errorf("cumulative base_metric is required")
	}
	if spec.TimeDimension == "" {
		return fmt.Errorf("cumulative time_dimension is required")
	}
	switch spec.Window.Type {
	case "unbounded":
		if spec.Window.Count != 0 || spec.Window.Unit != "" {
			return fmt.Errorf("unbounded cumulative window must not declare count or unit")
		}
	case "rolling":
		if spec.Window.Count <= 0 {
			return fmt.Errorf("rolling cumulative window count must be positive")
		}
		if spec.Window.Unit == "" {
			return fmt.Errorf("rolling cumulative window unit is required")
		}
	case "grain_to_date":
		if spec.Window.Count != 0 {
			return fmt.Errorf("grain-to-date cumulative window must not declare count")
		}
		if spec.Window.Unit == "" {
			return fmt.Errorf("grain-to-date cumulative window unit is required")
		}
		if isBuiltInCalendarGrain(spec.Window.Unit) && !validGrainToDateUnit(spec.Window.Unit) {
			return fmt.Errorf("unsupported grain-to-date cumulative window unit %q", spec.Window.Unit)
		}
	default:
		return fmt.Errorf("unsupported cumulative window type %q", spec.Window.Type)
	}
	return nil
}

func validCumulativeWindowUnit(unit string) bool {
	switch unit {
	case "year", "quarter", "month", "week", "day", "hour":
		return true
	default:
		return false
	}
}

func validGrainToDateUnit(unit string) bool {
	switch unit {
	case "year", "quarter", "month", "week", "day":
		return true
	default:
		return false
	}
}

func ValidateTimeOffsetSpec(spec TimeOffsetMetricSpec) error {
	if spec.Kind != MetricExtensionTimeOffset {
		return fmt.Errorf("time-offset extension kind must be %q", MetricExtensionTimeOffset)
	}
	if spec.BaseMetric == "" {
		return fmt.Errorf("time-offset base_metric is required")
	}
	if spec.TimeDimension == "" {
		return fmt.Errorf("time-offset time_dimension is required")
	}
	if spec.Offset.Count >= 0 {
		return fmt.Errorf("time-offset count must be negative")
	}
	if spec.Offset.Unit == "" {
		return fmt.Errorf("time-offset unit is required")
	}
	return nil
}

func IsBuiltInTimeOffsetUnit(unit string) bool {
	switch unit {
	case "hour", "day", "week", "month", "quarter", "year":
		return true
	default:
		return false
	}
}

func ValidateSemiAdditiveSpec(spec SemiAdditiveMetricSpec) error {
	if spec.Kind != MetricExtensionSemiAdditive {
		return fmt.Errorf("semi-additive extension kind must be %q", MetricExtensionSemiAdditive)
	}
	if spec.BaseMetric == "" {
		return fmt.Errorf("semi-additive base_metric is required")
	}
	if spec.NonAdditiveDimension == "" {
		return fmt.Errorf("semi-additive non_additive_dimension is required")
	}
	if spec.TieBreakDimension == spec.NonAdditiveDimension && spec.TieBreakDimension != "" {
		return fmt.Errorf("semi-additive tie_break_dimension must differ from non_additive_dimension")
	}
	if spec.NullPolicy != "" && spec.NullPolicy != "skip" {
		return fmt.Errorf("unsupported semi-additive null_policy %q", spec.NullPolicy)
	}
	seenGroupings := map[string]struct{}{}
	for _, grouping := range spec.WindowGroupings {
		if grouping == "" {
			return fmt.Errorf("semi-additive window_groupings must not contain empty names")
		}
		if grouping == spec.NonAdditiveDimension {
			return fmt.Errorf("semi-additive window grouping must differ from non_additive_dimension")
		}
		if _, ok := seenGroupings[grouping]; ok {
			return fmt.Errorf("duplicate semi-additive window grouping %q", grouping)
		}
		seenGroupings[grouping] = struct{}{}
	}
	if len(spec.WindowGroupings) == 0 && spec.RollupAggregation != "" {
		return fmt.Errorf("semi-additive rollup_aggregation requires window_groupings")
	}
	if len(spec.WindowGroupings) > 0 {
		switch EffectiveSemiAdditiveRollupAggregation(spec) {
		case SemiAdditiveRollupSum, SemiAdditiveRollupMin, SemiAdditiveRollupMax:
		default:
			return fmt.Errorf("unsupported semi-additive rollup_aggregation %q", spec.RollupAggregation)
		}
	}
	switch spec.Aggregation {
	case "last", "first":
		return nil
	default:
		return fmt.Errorf("unsupported semi-additive aggregation %q", spec.Aggregation)
	}
}

func EffectiveSemiAdditiveRollupAggregation(spec SemiAdditiveMetricSpec) string {
	if spec.RollupAggregation == "" {
		return SemiAdditiveRollupSum
	}
	return spec.RollupAggregation
}

func ValidateMetricTimeBinding(spec MetricTimeBindingSpec) error {
	if spec.Kind != MetricExtensionTimeBinding {
		return fmt.Errorf("time-binding extension kind must be %q", MetricExtensionTimeBinding)
	}
	if spec.TimeDimension == "" {
		return fmt.Errorf("time-binding time_dimension is required")
	}
	return nil
}
