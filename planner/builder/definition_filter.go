package builder

import (
	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/serrors"
)

// ResolveDefinitionFilterField resolves one definition-filter field against the
// already-selected model and fails closed for missing or ambiguous fields.
func ResolveDefinitionFilterField(model *manifest.ModelIndex, name string) (*manifest.FieldHandle, error) {
	if model == nil {
		return nil, &serrors.Error{Code: serrors.ErrInternalInvariant, Message: "semantic model index is required"}
	}
	var match *manifest.FieldHandle
	for _, dataset := range model.Datasets {
		if dataset == nil {
			continue
		}
		for i := range dataset.Fields {
			field := &dataset.Fields[i]
			if field.Name != name {
				continue
			}
			candidate := &manifest.FieldHandle{Dataset: dataset.Name, Field: field}
			if match != nil {
				return nil, &serrors.Error{Code: serrors.ErrAmbiguousField, Message: "metric definition-filter field is ambiguous", Details: map[string]any{"field": name}}
			}
			match = candidate
		}
	}
	if match == nil {
		return nil, &serrors.Error{Code: serrors.ErrUnresolvedSemanticReference, Message: "metric definition-filter field is not defined by the model", Details: map[string]any{"field": name}}
	}
	return match, nil
}

// DefinitionFilterPlanningError wraps a construction failure in the stable
// metric definition-filter error contract.
func DefinitionFilterPlanningError(metric, message string, cause error) error {
	details := map[string]any{"metric": metric}
	if cause != nil {
		details["cause"] = cause.Error()
	}
	return &serrors.Error{Code: serrors.ErrInvalidMetricDefinitionFilter, Message: message, Details: details}
}
