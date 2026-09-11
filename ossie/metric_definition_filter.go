package ossie

import (
	"encoding/json"
	"fmt"

	"github.com/meaningforge/metis/query"
)

const MetricExtensionDefinitionFilter MetricExtensionKind = "definition_filter"

const (
	MetricDefinitionFilterStagePreAggregation  = "pre_aggregation"
	MetricDefinitionFilterStagePostAggregation = "post_aggregation"
)

type MetricDefinitionFilterSpec struct {
	Kind    MetricExtensionKind `json:"kind"`
	Stage   string              `json:"stage,omitempty"`
	Filters []query.Filter      `json:"filters"`
}

func MetricDefinitionFilter(metric *Metric) (MetricDefinitionFilterSpec, bool, error) {
	data, ok, err := metricExtensionData(metric, MetricExtensionDefinitionFilter)
	if err != nil || !ok {
		return MetricDefinitionFilterSpec{}, ok, err
	}
	var spec MetricDefinitionFilterSpec
	if err := json.Unmarshal([]byte(data), &spec); err != nil {
		return MetricDefinitionFilterSpec{}, false, fmt.Errorf("decode metric definition-filter extension: %w", err)
	}
	if err := ValidateMetricDefinitionFilterSpec(spec); err != nil {
		return MetricDefinitionFilterSpec{}, false, err
	}
	return spec, true, nil
}

func EffectiveMetricDefinitionFilterStage(spec MetricDefinitionFilterSpec) string {
	if spec.Stage == "" {
		return MetricDefinitionFilterStagePreAggregation
	}
	return spec.Stage
}

func ValidateMetricDefinitionFilterSpec(spec MetricDefinitionFilterSpec) error {
	if spec.Kind != MetricExtensionDefinitionFilter {
		return fmt.Errorf("metric definition-filter extension kind must be %q", MetricExtensionDefinitionFilter)
	}
	switch EffectiveMetricDefinitionFilterStage(spec) {
	case MetricDefinitionFilterStagePreAggregation, MetricDefinitionFilterStagePostAggregation:
	default:
		return fmt.Errorf("unsupported metric definition-filter stage %q", spec.Stage)
	}
	if len(spec.Filters) == 0 {
		return fmt.Errorf("metric definition-filter filters must be non-empty")
	}
	for i, filter := range spec.Filters {
		if filter.Field == "" {
			return fmt.Errorf("metric definition-filter filters[%d].field is required", i)
		}
		if !validMetricDefinitionFilterOperator(filter.Operator) {
			return fmt.Errorf("unsupported metric definition-filter operator %q", filter.Operator)
		}
		switch filter.Operator {
		case query.FilterIsNull, query.FilterIsNotNull:
			if filter.Value != nil {
				return fmt.Errorf("metric definition-filter operator %q must not declare value", filter.Operator)
			}
		default:
			if filter.Value == nil {
				return fmt.Errorf("metric definition-filter operator %q requires value", filter.Operator)
			}
		}
	}
	return nil
}

func ValidateMetricDefinitionFilterModel(model *SemanticModel, metricName string, spec MetricDefinitionFilterSpec) error {
	if model == nil {
		return fmt.Errorf("semantic model is required")
	}
	if EffectiveMetricDefinitionFilterStage(spec) == MetricDefinitionFilterStagePostAggregation {
		for _, filter := range spec.Filters {
			if filter.Field != metricName {
				return fmt.Errorf("post-aggregation metric definition-filter for metric %q may only target its owning metric; got %q", metricName, filter.Field)
			}
		}
		return nil
	}
	for _, filter := range spec.Filters {
		matches := 0
		for _, dataset := range model.Datasets {
			for _, field := range dataset.Fields {
				if field.Name == filter.Field {
					matches++
				}
			}
		}
		if matches == 0 {
			return fmt.Errorf("metric definition-filter field %q not found for metric %q", filter.Field, metricName)
		}
		if matches > 1 {
			return fmt.Errorf("metric definition-filter field %q is ambiguous for metric %q; use a unique field name", filter.Field, metricName)
		}
	}
	return nil
}

func validMetricDefinitionFilterOperator(operator query.FilterOperator) bool {
	switch operator {
	case query.FilterEQ, query.FilterNEQ, query.FilterGT, query.FilterGTE, query.FilterLT, query.FilterLTE,
		query.FilterIN, query.FilterNotIn, query.FilterBetween, query.FilterIsNull, query.FilterIsNotNull:
		return true
	default:
		return false
	}
}
