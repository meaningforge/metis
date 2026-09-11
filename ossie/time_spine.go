package ossie

import (
	"encoding/json"
	"fmt"
)

const ModelExtensionTimeSpine = "time_spine"

type TimeSpineSpec struct {
	Kind          string   `json:"kind"`
	Dataset       string   `json:"dataset"`
	TimeDimension string   `json:"time_dimension"`
	Grains        []string `json:"grains"`
}

func TimeSpine(model *SemanticModel) (TimeSpineSpec, bool, error) {
	if model == nil {
		return TimeSpineSpec{}, false, nil
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
			return TimeSpineSpec{}, false, fmt.Errorf("decode METIS model extension: %w", err)
		}
		if envelope.Kind != ModelExtensionTimeSpine {
			continue
		}
		if found != "" {
			return TimeSpineSpec{}, false, fmt.Errorf("duplicate METIS model extension kind %q", ModelExtensionTimeSpine)
		}
		found = extension.Data
	}
	if found == "" {
		return TimeSpineSpec{}, false, nil
	}
	var spec TimeSpineSpec
	if err := json.Unmarshal([]byte(found), &spec); err != nil {
		return TimeSpineSpec{}, false, fmt.Errorf("decode time-spine extension: %w", err)
	}
	if err := ValidateTimeSpineSpec(spec); err != nil {
		return TimeSpineSpec{}, false, err
	}
	return spec, true, nil
}

func ValidateTimeSpineSpec(spec TimeSpineSpec) error {
	if spec.Kind != ModelExtensionTimeSpine {
		return fmt.Errorf("time-spine extension kind must be %q", ModelExtensionTimeSpine)
	}
	if spec.Dataset == "" {
		return fmt.Errorf("time-spine dataset is required")
	}
	if spec.TimeDimension == "" {
		return fmt.Errorf("time-spine time_dimension is required")
	}
	if len(spec.Grains) == 0 {
		return fmt.Errorf("time-spine grains are required")
	}
	seen := map[string]struct{}{}
	for _, grain := range spec.Grains {
		switch grain {
		case "day", "week", "month", "quarter", "year":
		default:
			return fmt.Errorf("unsupported time-spine grain %q", grain)
		}
		if _, ok := seen[grain]; ok {
			return fmt.Errorf("duplicate time-spine grain %q", grain)
		}
		seen[grain] = struct{}{}
	}
	return nil
}

func ValidateTimeSpineModel(model *SemanticModel, spec TimeSpineSpec) error {
	if model == nil {
		return fmt.Errorf("semantic model is required")
	}
	var dataset *Dataset
	for i := range model.Datasets {
		if model.Datasets[i].Name == spec.Dataset {
			dataset = &model.Datasets[i]
			break
		}
	}
	if dataset == nil {
		return fmt.Errorf("time-spine dataset %q not found", spec.Dataset)
	}
	for i := range dataset.Fields {
		field := &dataset.Fields[i]
		if field.Name != spec.TimeDimension {
			continue
		}
		if field.Dimension == nil {
			return fmt.Errorf("time-spine field %q must be a time dimension", spec.TimeDimension)
		}
		isTime := false
		if field.Dimension.IsTime != nil {
			isTime = *field.Dimension.IsTime
		} else {
			isTime = isTemporalDataType(field.Datatype)
		}
		if !isTime {
			return fmt.Errorf("time-spine field %q must be a time dimension", spec.TimeDimension)
		}
		return nil
	}
	return fmt.Errorf("time-spine time dimension %q not found in dataset %q", spec.TimeDimension, spec.Dataset)
}
