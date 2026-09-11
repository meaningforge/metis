package ossie

import (
	"encoding/json"
	"fmt"
)

const MetricExtensionFill MetricExtensionKind = "fill"

type MetricFillPolicy string

const (
	MetricFillNone MetricFillPolicy = "none"
	MetricFillZero MetricFillPolicy = "zero"
)

type MetricFillSpec struct {
	Kind   MetricExtensionKind `json:"kind"`
	Policy MetricFillPolicy    `json:"policy"`
}

func FillSpec(metric *Metric) (MetricFillSpec, bool, error) {
	data, ok, err := metricExtensionData(metric, MetricExtensionFill)
	if err != nil || !ok {
		return MetricFillSpec{}, ok, err
	}
	var spec MetricFillSpec
	if err := json.Unmarshal([]byte(data), &spec); err != nil {
		return MetricFillSpec{}, false, fmt.Errorf("decode metric fill extension: %w", err)
	}
	if err := ValidateMetricFillSpec(metric, spec); err != nil {
		return MetricFillSpec{}, false, err
	}
	return spec, true, nil
}

func ValidateMetricFillSpec(metric *Metric, spec MetricFillSpec) error {
	if spec.Kind != MetricExtensionFill {
		return fmt.Errorf("metric fill extension kind must be %q", MetricExtensionFill)
	}
	switch spec.Policy {
	case MetricFillNone:
		return nil
	case MetricFillZero:
		if metric == nil {
			return fmt.Errorf("metric fill zero requires a metric")
		}
		if !MetricDatatypeSupportsZero(metric.Datatype) {
			return fmt.Errorf("metric fill zero requires a numeric datatype, got %q", metric.Datatype)
		}
		return nil
	default:
		return fmt.Errorf("unsupported metric fill policy %q", spec.Policy)
	}
}

func MetricDatatypeSupportsZero(datatype DataType) bool {
	switch datatype {
	case DataTypeInteger, DataTypeDecimal, DataTypeFloat:
		return true
	default:
		return false
	}
}
