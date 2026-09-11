package ossie

import (
	"encoding/json"
	"fmt"
)

const ModelExtensionCustomCalendar = "custom_calendar"

type CustomCalendarSpec struct {
	Kind     string                `json:"kind"`
	Dataset  string                `json:"dataset"`
	BaseTime string                `json:"base_time_dimension"`
	Grains   []CustomCalendarGrain `json:"grains"`
}

type CustomCalendarGrain struct {
	Name             string `json:"name"`
	BucketDimension  string `json:"bucket_dimension"`
	OrdinalDimension string `json:"ordinal_dimension"`
	DenseMapping     bool   `json:"dense_mapping,omitempty"`
	ParentGrain      string `json:"parent_grain,omitempty"`
}

func CustomCalendar(model *SemanticModel) (CustomCalendarSpec, bool, error) {
	if model == nil {
		return CustomCalendarSpec{}, false, nil
	}
	var found string
	for _, extension := range model.CustomExtensions {
		if extension.VendorName != MetisExtensionVendor {
			continue
		}
		var envelope struct {
			Kind string `json:"kind"`
		}
		if err := json.Unmarshal([]byte(extension.Data), &envelope); err != nil {
			return CustomCalendarSpec{}, false, fmt.Errorf("decode METIS model extension: %w", err)
		}
		if envelope.Kind != ModelExtensionCustomCalendar {
			continue
		}
		if found != "" {
			return CustomCalendarSpec{}, false, fmt.Errorf("duplicate METIS model extension kind %q", ModelExtensionCustomCalendar)
		}
		found = extension.Data
	}
	if found == "" {
		return CustomCalendarSpec{}, false, nil
	}
	var spec CustomCalendarSpec
	if err := json.Unmarshal([]byte(found), &spec); err != nil {
		return CustomCalendarSpec{}, false, fmt.Errorf("decode custom-calendar extension: %w", err)
	}
	if err := ValidateCustomCalendarSpec(spec); err != nil {
		return CustomCalendarSpec{}, false, err
	}
	return spec, true, nil
}

func ValidateCustomCalendarSpec(spec CustomCalendarSpec) error {
	if spec.Kind != ModelExtensionCustomCalendar {
		return fmt.Errorf("custom-calendar extension kind must be %q", ModelExtensionCustomCalendar)
	}
	if spec.Dataset == "" {
		return fmt.Errorf("custom-calendar dataset is required")
	}
	if spec.BaseTime == "" {
		return fmt.Errorf("custom-calendar base_time_dimension is required")
	}
	if len(spec.Grains) == 0 {
		return fmt.Errorf("custom-calendar grains are required")
	}
	byName := make(map[string]CustomCalendarGrain, len(spec.Grains))
	for _, grain := range spec.Grains {
		if grain.Name == "" || grain.BucketDimension == "" || grain.OrdinalDimension == "" {
			return fmt.Errorf("custom-calendar grain name, bucket_dimension, and ordinal_dimension are required")
		}
		if isBuiltInCalendarGrain(grain.Name) {
			return fmt.Errorf("custom-calendar grain %q conflicts with a built-in grain", grain.Name)
		}
		if grain.BucketDimension == grain.OrdinalDimension {
			return fmt.Errorf("custom-calendar grain %q bucket and ordinal dimensions must differ", grain.Name)
		}
		if grain.ParentGrain == grain.Name {
			return fmt.Errorf("custom-calendar grain %q cannot parent itself", grain.Name)
		}
		if _, ok := byName[grain.Name]; ok {
			return fmt.Errorf("duplicate custom-calendar grain %q", grain.Name)
		}
		byName[grain.Name] = grain
	}
	for _, grain := range spec.Grains {
		if grain.ParentGrain == "" {
			continue
		}
		if _, ok := byName[grain.ParentGrain]; !ok {
			return fmt.Errorf("custom-calendar grain %q parent_grain %q not found", grain.Name, grain.ParentGrain)
		}
	}
	for _, grain := range spec.Grains {
		seen := map[string]struct{}{}
		current := grain
		for current.ParentGrain != "" {
			if _, ok := seen[current.Name]; ok {
				return fmt.Errorf("custom-calendar grain hierarchy contains a cycle at %q", current.Name)
			}
			seen[current.Name] = struct{}{}
			current = byName[current.ParentGrain]
		}
	}
	return nil
}

func ValidateCustomCalendarModel(model *SemanticModel, spec CustomCalendarSpec) error {
	if model == nil {
		return fmt.Errorf("semantic model is required")
	}
	dataset := datasetByName(model, spec.Dataset)
	if dataset == nil {
		return fmt.Errorf("custom-calendar dataset %q not found", spec.Dataset)
	}
	base := fieldByName(dataset, spec.BaseTime)
	if base == nil || base.Dimension == nil || !isTemporalDataType(base.Datatype) {
		return fmt.Errorf("custom-calendar base_time_dimension %q must be a temporal dimension", spec.BaseTime)
	}
	for _, grain := range spec.Grains {
		bucket := fieldByName(dataset, grain.BucketDimension)
		if bucket == nil || bucket.Dimension == nil || !isTemporalDataType(bucket.Datatype) {
			return fmt.Errorf("custom-calendar grain %q bucket_dimension %q must be a temporal dimension", grain.Name, grain.BucketDimension)
		}
		ordinal := fieldByName(dataset, grain.OrdinalDimension)
		if ordinal == nil || ordinal.Dimension == nil || ordinal.Datatype != DataTypeInteger {
			return fmt.Errorf("custom-calendar grain %q ordinal_dimension %q must be an integer dimension", grain.Name, grain.OrdinalDimension)
		}
	}
	return nil
}

func ValidateTimeOffsetCalendarModel(model *SemanticModel, spec TimeOffsetMetricSpec) error {
	if IsBuiltInTimeOffsetUnit(spec.Offset.Unit) {
		return nil
	}
	calendar, ok, err := CustomCalendar(model)
	if err != nil {
		return fmt.Errorf("resolve custom-calendar offset unit %q: %w", spec.Offset.Unit, err)
	}
	if !ok {
		return fmt.Errorf("unsupported time-offset unit %q", spec.Offset.Unit)
	}
	grain, ok := CustomCalendarGrainByName(calendar, spec.Offset.Unit)
	if !ok {
		return fmt.Errorf("unsupported time-offset unit %q", spec.Offset.Unit)
	}
	if spec.TimeDimension != calendar.BaseTime {
		return fmt.Errorf("custom-calendar time-offset unit %q requires time_dimension %q", spec.Offset.Unit, calendar.BaseTime)
	}
	if !grain.DenseMapping {
		return fmt.Errorf("custom-calendar time-offset unit %q requires dense_mapping=true", spec.Offset.Unit)
	}
	return nil
}

func CustomCalendarGrainByName(spec CustomCalendarSpec, name string) (CustomCalendarGrain, bool) {
	for _, grain := range spec.Grains {
		if grain.Name == name {
			return grain, true
		}
	}
	return CustomCalendarGrain{}, false
}

func CustomCalendarGrainIsDescendant(spec CustomCalendarSpec, child, ancestor string) bool {
	if child == "" || ancestor == "" {
		return false
	}
	current, ok := CustomCalendarGrainByName(spec, child)
	if !ok {
		return false
	}
	if current.Name == ancestor {
		return true
	}
	for current.ParentGrain != "" {
		if current.ParentGrain == ancestor {
			return true
		}
		current, ok = CustomCalendarGrainByName(spec, current.ParentGrain)
		if !ok {
			return false
		}
	}
	return false
}

func fieldByName(dataset *Dataset, name string) *Field {
	if dataset == nil {
		return nil
	}
	for i := range dataset.Fields {
		if dataset.Fields[i].Name == name {
			return &dataset.Fields[i]
		}
	}
	return nil
}

func isBuiltInCalendarGrain(name string) bool {
	switch name {
	case "hour", "day", "week", "month", "quarter", "year":
		return true
	default:
		return false
	}
}
