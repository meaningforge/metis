package ossie

import (
	"encoding/json"
	"fmt"
)

const RelationshipExtensionTemporal = "temporal"

const (
	TemporalCardinalityManyToOne = "many_to_one"
	TemporalCardinalityOneToOne  = "one_to_one"
)

// TemporalRelationshipSpec models a point-in-time relationship from a fact-side
// event timestamp to a versioned dimension row. V1 uses [valid_from, valid_to)
// semantics; a NULL valid_to means the version remains current indefinitely.
// Cardinality is an explicit semantic assertion: temporal joins must prove that
// one fact event resolves to at most one dimension version rather than relying
// on relationship direction or column-name heuristics.
type TemporalRelationshipSpec struct {
	Kind              string `json:"kind"`
	FromTimeDimension string `json:"from_time_dimension"`
	ToValidFrom       string `json:"to_valid_from"`
	ToValidTo         string `json:"to_valid_to"`
	Cardinality       string `json:"cardinality"`
}

func TemporalRelationship(rel *Relationship) (TemporalRelationshipSpec, bool, error) {
	if rel == nil {
		return TemporalRelationshipSpec{}, false, nil
	}
	var found string
	for _, extension := range rel.CustomExtensions {
		if extension.VendorName != MetisExtensionVendor {
			continue
		}
		var envelope struct {
			Kind string `json:"kind"`
		}
		if err := json.Unmarshal([]byte(extension.Data), &envelope); err != nil {
			return TemporalRelationshipSpec{}, false, fmt.Errorf("decode METIS relationship extension: %w", err)
		}
		if envelope.Kind != RelationshipExtensionTemporal {
			continue
		}
		if found != "" {
			return TemporalRelationshipSpec{}, false, fmt.Errorf("duplicate METIS relationship extension kind %q", RelationshipExtensionTemporal)
		}
		found = extension.Data
	}
	if found == "" {
		return TemporalRelationshipSpec{}, false, nil
	}
	var spec TemporalRelationshipSpec
	if err := json.Unmarshal([]byte(found), &spec); err != nil {
		return TemporalRelationshipSpec{}, false, fmt.Errorf("decode temporal relationship extension: %w", err)
	}
	if err := ValidateTemporalRelationshipSpec(spec); err != nil {
		return TemporalRelationshipSpec{}, false, err
	}
	return spec, true, nil
}

func ValidateTemporalRelationshipSpec(spec TemporalRelationshipSpec) error {
	if spec.Kind != RelationshipExtensionTemporal {
		return fmt.Errorf("temporal relationship extension kind must be %q", RelationshipExtensionTemporal)
	}
	if spec.FromTimeDimension == "" {
		return fmt.Errorf("temporal relationship from_time_dimension is required")
	}
	if spec.ToValidFrom == "" {
		return fmt.Errorf("temporal relationship to_valid_from is required")
	}
	if spec.ToValidTo == "" {
		return fmt.Errorf("temporal relationship to_valid_to is required")
	}
	if spec.ToValidFrom == spec.ToValidTo {
		return fmt.Errorf("temporal relationship validity fields must be distinct")
	}
	switch spec.Cardinality {
	case TemporalCardinalityManyToOne, TemporalCardinalityOneToOne:
	default:
		if spec.Cardinality == "" {
			return fmt.Errorf("temporal relationship cardinality is required")
		}
		return fmt.Errorf("temporal relationship cardinality must be %q or %q", TemporalCardinalityManyToOne, TemporalCardinalityOneToOne)
	}
	return nil
}

func ValidateTemporalRelationshipModel(model *SemanticModel, rel *Relationship, spec TemporalRelationshipSpec) error {
	if model == nil || rel == nil {
		return fmt.Errorf("semantic model and relationship are required")
	}
	from := datasetByName(model, rel.From)
	to := datasetByName(model, rel.To)
	if from == nil || to == nil {
		return fmt.Errorf("temporal relationship endpoints must exist")
	}
	if err := requireTemporalRelationshipField(from, spec.FromTimeDimension, true); err != nil {
		return fmt.Errorf("from time dimension: %w", err)
	}
	if err := requireTemporalRelationshipField(to, spec.ToValidFrom, false); err != nil {
		return fmt.Errorf("valid_from: %w", err)
	}
	if err := requireTemporalRelationshipField(to, spec.ToValidTo, false); err != nil {
		return fmt.Errorf("valid_to: %w", err)
	}
	if spec.Cardinality == TemporalCardinalityOneToOne && !relationshipColumnsAreUnique(from, rel.FromColumns) {
		return fmt.Errorf("one_to_one temporal relationship requires from_columns to match a primary or unique key on dataset %q", from.Name)
	}
	return nil
}

func relationshipColumnsAreUnique(dataset *Dataset, columns []string) bool {
	if dataset == nil || len(columns) == 0 {
		return false
	}
	if sameStringSet(dataset.PrimaryKey, columns) {
		return true
	}
	for _, key := range dataset.UniqueKeys {
		if sameStringSet(key, columns) {
			return true
		}
	}
	return false
}

func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	counts := make(map[string]int, len(a))
	for _, value := range a {
		counts[value]++
	}
	for _, value := range b {
		counts[value]--
		if counts[value] < 0 {
			return false
		}
	}
	for _, count := range counts {
		if count != 0 {
			return false
		}
	}
	return true
}

func datasetByName(model *SemanticModel, name string) *Dataset {
	for i := range model.Datasets {
		if model.Datasets[i].Name == name {
			return &model.Datasets[i]
		}
	}
	return nil
}

func requireTemporalRelationshipField(dataset *Dataset, name string, requireDimension bool) error {
	for i := range dataset.Fields {
		field := &dataset.Fields[i]
		if field.Name != name {
			continue
		}
		if !isTemporalDataType(field.Datatype) {
			return fmt.Errorf("field %q on dataset %q must have a temporal datatype", name, dataset.Name)
		}
		if requireDimension {
			if field.Dimension == nil {
				return fmt.Errorf("field %q on dataset %q must be a time dimension", name, dataset.Name)
			}
			isTime := field.Dimension.IsTime == nil || *field.Dimension.IsTime
			if !isTime {
				return fmt.Errorf("field %q on dataset %q must be a time dimension", name, dataset.Name)
			}
		}
		return nil
	}
	return fmt.Errorf("field %q not found on dataset %q", name, dataset.Name)
}
