package ossie

import (
	"encoding/json"
	"fmt"
)

const MetricExtensionOffsetToGrain MetricExtensionKind = "offset_to_grain"

// OffsetToGrainMetricSpec evaluates BaseMetric at the start boundary of the
// containing Grain and aligns that value to the query's finer/equal time rows.
// It is deliberately distinct from time_offset, which shifts by N periods.
type OffsetToGrainMetricSpec struct {
	Kind          MetricExtensionKind `json:"kind"`
	BaseMetric    string              `json:"base_metric"`
	TimeDimension string              `json:"time_dimension"`
	Grain         string              `json:"grain"`
}

func OffsetToGrainSpec(metric *Metric) (OffsetToGrainMetricSpec, bool, error) {
	data, ok, err := metricExtensionData(metric, MetricExtensionOffsetToGrain)
	if err != nil || !ok {
		return OffsetToGrainMetricSpec{}, ok, err
	}
	var spec OffsetToGrainMetricSpec
	if err := json.Unmarshal([]byte(data), &spec); err != nil {
		return OffsetToGrainMetricSpec{}, false, fmt.Errorf("decode offset-to-grain metric extension: %w", err)
	}
	if err := ValidateOffsetToGrainSpec(spec); err != nil {
		return OffsetToGrainMetricSpec{}, false, err
	}
	return spec, true, nil
}

func ValidateOffsetToGrainSpec(spec OffsetToGrainMetricSpec) error {
	if spec.Kind != MetricExtensionOffsetToGrain {
		return fmt.Errorf("offset-to-grain extension kind must be %q", MetricExtensionOffsetToGrain)
	}
	if spec.BaseMetric == "" {
		return fmt.Errorf("offset-to-grain base_metric is required")
	}
	if spec.TimeDimension == "" {
		return fmt.Errorf("offset-to-grain time_dimension is required")
	}
	if spec.Grain == "" {
		return fmt.Errorf("offset-to-grain grain is required")
	}
	return nil
}

func ValidateOffsetToGrainCalendarModel(model *SemanticModel, spec OffsetToGrainMetricSpec) error {
	if isBuiltInCalendarGrain(spec.Grain) {
		return nil
	}
	calendar, ok, err := CustomCalendar(model)
	if err != nil {
		return fmt.Errorf("resolve custom-calendar offset-to-grain %q: %w", spec.Grain, err)
	}
	if !ok {
		return fmt.Errorf("unsupported offset-to-grain grain %q", spec.Grain)
	}
	if _, ok := CustomCalendarGrainByName(calendar, spec.Grain); !ok {
		return fmt.Errorf("unsupported offset-to-grain grain %q", spec.Grain)
	}
	if spec.TimeDimension != calendar.BaseTime {
		return fmt.Errorf("custom-calendar offset-to-grain %q requires time_dimension %q", spec.Grain, calendar.BaseTime)
	}
	return nil
}

func validateOffsetToGrainMetrics(model *SemanticModel, metrics map[string]struct{}) error {
	for i := range model.Metrics {
		metric := &model.Metrics[i]
		spec, ok, err := OffsetToGrainSpec(metric)
		if err != nil {
			return invalid("invalid METIS offset-to-grain metric extension", map[string]any{"model": model.Name, "metric": metric.Name, "cause": err.Error()})
		}
		if !ok {
			continue
		}
		if err := validateMetricReferenceAndTimeDimension(model, metrics, metric.Name, spec.BaseMetric, spec.TimeDimension, "offset-to-grain"); err != nil {
			return err
		}
		if err := ValidateOffsetToGrainCalendarModel(model, spec); err != nil {
			return invalid("invalid METIS offset-to-grain metric extension", map[string]any{"model": model.Name, "metric": metric.Name, "cause": err.Error()})
		}
	}
	return nil
}
