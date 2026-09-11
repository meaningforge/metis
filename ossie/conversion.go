package ossie

import (
	"encoding/json"
	"fmt"
)

const MetricExtensionConversion MetricExtensionKind = "conversion"

const (
	ConversionCalculationConversions    = "conversions"
	ConversionCalculationConversionRate = "conversion_rate"
)

type ConversionPropertyPair struct {
	BaseProperty       string `json:"base_property"`
	ConversionProperty string `json:"conversion_property"`
}

type ConversionWindow struct {
	Count int    `json:"count"`
	Unit  string `json:"unit"`
}

type ConversionMetricSpec struct {
	Kind               MetricExtensionKind      `json:"kind"`
	BaseMetric         string                   `json:"base_metric"`
	ConversionMetric   string                   `json:"conversion_metric"`
	Entity             ConversionPropertyPair   `json:"entity"`
	Calculation        string                   `json:"calculation"`
	Window             *ConversionWindow        `json:"window,omitempty"`
	ConstantProperties []ConversionPropertyPair `json:"constant_properties,omitempty"`
}

func ConversionSpec(metric *Metric) (ConversionMetricSpec, bool, error) {
	data, ok, err := metricExtensionData(metric, MetricExtensionConversion)
	if err != nil || !ok {
		return ConversionMetricSpec{}, ok, err
	}
	var spec ConversionMetricSpec
	if err := json.Unmarshal([]byte(data), &spec); err != nil {
		return ConversionMetricSpec{}, false, fmt.Errorf("decode conversion metric extension: %w", err)
	}
	if err := ValidateConversionSpec(spec); err != nil {
		return ConversionMetricSpec{}, false, err
	}
	return spec, true, nil
}

func ValidateConversionSpec(spec ConversionMetricSpec) error {
	if spec.Kind != MetricExtensionConversion {
		return fmt.Errorf("conversion extension kind must be %q", MetricExtensionConversion)
	}
	if spec.BaseMetric == "" {
		return fmt.Errorf("conversion base_metric is required")
	}
	if spec.ConversionMetric == "" {
		return fmt.Errorf("conversion conversion_metric is required")
	}
	if spec.BaseMetric == spec.ConversionMetric {
		return fmt.Errorf("conversion base_metric and conversion_metric must differ")
	}
	if err := validateConversionPropertyPair("entity", spec.Entity); err != nil {
		return err
	}
	switch spec.Calculation {
	case ConversionCalculationConversions, ConversionCalculationConversionRate:
	default:
		return fmt.Errorf("unsupported conversion calculation %q", spec.Calculation)
	}
	if spec.Window != nil {
		if spec.Window.Count <= 0 {
			return fmt.Errorf("conversion window count must be positive")
		}
		if !IsBuiltInTimeOffsetUnit(spec.Window.Unit) {
			return fmt.Errorf("unsupported conversion window unit %q", spec.Window.Unit)
		}
	}
	seen := map[string]struct{}{}
	for i, property := range spec.ConstantProperties {
		if err := validateConversionPropertyPair(fmt.Sprintf("constant_properties[%d]", i), property); err != nil {
			return err
		}
		key := property.BaseProperty + "\x1f" + property.ConversionProperty
		if _, ok := seen[key]; ok {
			return fmt.Errorf("duplicate conversion constant property pair %q -> %q", property.BaseProperty, property.ConversionProperty)
		}
		if property == spec.Entity {
			return fmt.Errorf("conversion constant property duplicates entity pair %q -> %q", property.BaseProperty, property.ConversionProperty)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func ValidateConversionModel(model *SemanticModel, owner string, spec ConversionMetricSpec) error {
	if model == nil {
		return fmt.Errorf("conversion semantic model is required")
	}
	if spec.BaseMetric == owner || spec.ConversionMetric == owner {
		return fmt.Errorf("conversion metric cannot reference itself")
	}
	base := findMetric(model, spec.BaseMetric)
	if base == nil {
		return fmt.Errorf("conversion base metric %q not found", spec.BaseMetric)
	}
	conversion := findMetric(model, spec.ConversionMetric)
	if conversion == nil {
		return fmt.Errorf("conversion metric input %q not found", spec.ConversionMetric)
	}
	if err := requireConversionTimeBinding(model, spec.BaseMetric, base); err != nil {
		return err
	}
	if err := requireConversionTimeBinding(model, spec.ConversionMetric, conversion); err != nil {
		return err
	}
	pairs := append([]ConversionPropertyPair{spec.Entity}, spec.ConstantProperties...)
	for _, pair := range pairs {
		if !semanticModelHasField(model, pair.BaseProperty) {
			return fmt.Errorf("conversion base property %q not found", pair.BaseProperty)
		}
		if !semanticModelHasField(model, pair.ConversionProperty) {
			return fmt.Errorf("conversion conversion property %q not found", pair.ConversionProperty)
		}
	}
	return nil
}

func validateConversionPropertyPair(label string, pair ConversionPropertyPair) error {
	if pair.BaseProperty == "" || pair.ConversionProperty == "" {
		return fmt.Errorf("conversion %s base_property and conversion_property are required", label)
	}
	return nil
}

func findMetric(model *SemanticModel, name string) *Metric {
	for i := range model.Metrics {
		if model.Metrics[i].Name == name {
			return &model.Metrics[i]
		}
	}
	return nil
}

func requireConversionTimeBinding(model *SemanticModel, metricName string, metric *Metric) error {
	binding, ok, err := MetricTimeBinding(metric)
	if err != nil {
		return fmt.Errorf("conversion input metric %q has invalid time binding: %w", metricName, err)
	}
	if !ok {
		return fmt.Errorf("conversion input metric %q requires an explicit time_binding", metricName)
	}
	if err := validateMetricTimeDimension(model, metricName, binding.TimeDimension, "conversion"); err != nil {
		return err
	}
	return nil
}

func semanticModelHasField(model *SemanticModel, name string) bool {
	for _, dataset := range model.Datasets {
		for _, field := range dataset.Fields {
			if field.Name == name {
				return true
			}
		}
	}
	return false
}
