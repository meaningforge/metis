package ossie

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

const DataSourceExtensionKind = "data_source"

// DataSourcePlacement is the typed METIS projection that binds one complete
// SemanticModel to a deployment DataSource name. Keeping placement at model
// scope prevents a single compiled query from silently spanning warehouses.
type DataSourcePlacement struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

// SemanticModelDataSource extracts one typed METIS data_source extension.
// Extensions from other vendors and other METIS extension kinds remain opaque.
func SemanticModelDataSource(model *SemanticModel) (DataSourcePlacement, bool, error) {
	if model == nil {
		return DataSourcePlacement{}, false, nil
	}
	var data string
	for _, extension := range model.CustomExtensions {
		if extension.VendorName != MetisExtensionVendor {
			continue
		}
		var envelope struct {
			Kind string `json:"kind"`
		}
		if err := json.Unmarshal([]byte(extension.Data), &envelope); err != nil {
			return DataSourcePlacement{}, false, fmt.Errorf("decode METIS semantic-model extension: %w", err)
		}
		if envelope.Kind != DataSourceExtensionKind {
			continue
		}
		if data != "" {
			return DataSourcePlacement{}, false, fmt.Errorf("duplicate METIS data_source extension")
		}
		data = extension.Data
	}
	if data == "" {
		return DataSourcePlacement{}, false, nil
	}
	decoder := json.NewDecoder(bytes.NewBufferString(data))
	decoder.DisallowUnknownFields()
	var placement DataSourcePlacement
	if err := decoder.Decode(&placement); err != nil {
		return DataSourcePlacement{}, false, fmt.Errorf("decode METIS data_source extension: %w", err)
	}
	if placement.Kind != DataSourceExtensionKind {
		return DataSourcePlacement{}, false, fmt.Errorf("data source placement kind must be %q", DataSourceExtensionKind)
	}
	if placement.Name == "" || placement.Name != strings.TrimSpace(placement.Name) || len(placement.Name) > 128 {
		return DataSourcePlacement{}, false, fmt.Errorf("data source placement name must be non-empty, trimmed, and at most 128 characters")
	}
	return placement, true, nil
}
