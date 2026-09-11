package ossie

import (
	"encoding/json"
	"fmt"
	"strings"
)

const MetricExtensionQualityClaims MetricExtensionKind = "quality_claims"

type QualityAggregation string

const (
	QualityAggregationScalar    QualityAggregation = "scalar"
	QualityAggregationAggregate QualityAggregation = "aggregate"
	QualityAggregationMixed     QualityAggregation = "mixed"
)

// MetricQualityClaimsSpec is deterministic authored evidence used only by
// model-quality diagnostics. It never changes metric evaluation semantics.
type MetricQualityClaimsSpec struct {
	Kind                MetricExtensionKind `json:"kind"`
	ExpectedType        DataType            `json:"expected_type,omitempty"`
	ExpectedAggregation QualityAggregation  `json:"expected_aggregation,omitempty"`
}

func MetricQualityClaims(metric *Metric) (MetricQualityClaimsSpec, bool, error) {
	data, ok, err := metricExtensionData(metric, MetricExtensionQualityClaims)
	if err != nil || !ok {
		return MetricQualityClaimsSpec{}, ok, err
	}
	var spec MetricQualityClaimsSpec
	if err := json.Unmarshal([]byte(data), &spec); err != nil {
		return MetricQualityClaimsSpec{}, false, fmt.Errorf("decode metric quality-claims extension: %w", err)
	}
	if err := ValidateMetricQualityClaims(spec); err != nil {
		return MetricQualityClaimsSpec{}, false, err
	}
	spec.ExpectedAggregation = QualityAggregation(strings.ToLower(strings.TrimSpace(string(spec.ExpectedAggregation))))
	return spec, true, nil
}

func ValidateMetricQualityClaims(spec MetricQualityClaimsSpec) error {
	if spec.Kind != MetricExtensionQualityClaims {
		return fmt.Errorf("metric quality-claims extension kind must be %q", MetricExtensionQualityClaims)
	}
	if spec.ExpectedType == "" && strings.TrimSpace(string(spec.ExpectedAggregation)) == "" {
		return fmt.Errorf("metric quality-claims extension requires expected_type or expected_aggregation")
	}
	if spec.ExpectedType != "" && !validDataType(spec.ExpectedType) {
		return fmt.Errorf("metric quality-claims expected_type is unsupported")
	}
	switch QualityAggregation(strings.ToLower(strings.TrimSpace(string(spec.ExpectedAggregation)))) {
	case "", QualityAggregationScalar, QualityAggregationAggregate, QualityAggregationMixed:
	default:
		return fmt.Errorf("metric quality-claims expected_aggregation must be scalar, aggregate, or mixed")
	}
	return nil
}
