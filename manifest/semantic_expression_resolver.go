package manifest

import (
	"sort"

	"github.com/meaningforge/metis/expression"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/serrors"
)

// manifestExpressionResolver binds a complete expression against one immutable
// ModelIndex. It owns SemanticManifest-specific ambiguity rules; the expression
// package only consumes the resulting canonical symbols.
type manifestExpressionResolver struct {
	model         *ModelIndex
	currentMetric string
}

func newSemanticManifestExpressionResolver(model *ModelIndex, currentMetric string) expression.ReferenceResolver {
	return &manifestExpressionResolver{model: model, currentMetric: currentMetric}
}

func (r *manifestExpressionResolver) ResolveReferences(refs []expression.Reference) (map[expression.Reference]expression.BoundSymbol, error) {
	bindings := make(map[expression.Reference]expression.BoundSymbol, len(refs))
	bareFields := map[string]struct{}{}

	for _, ref := range refs {
		if ref.Name == "" {
			continue
		}
		if ref.Qualifier == "" {
			if metric, ok := r.model.Metrics[ref.Name]; ok && ref.Name != r.currentMetric {
				bindings[ref] = expression.BoundSymbol{
					Kind:        expression.BoundMetric,
					Name:        ref.Name,
					Type:        semanticType(metric.Datatype),
					Aggregation: expression.AggregationAggregate,
				}
				continue
			}
			bareFields[ref.Name] = struct{}{}
			continue
		}

		if _, ok := r.model.Datasets[ref.Qualifier]; !ok {
			return nil, &serrors.Error{Code: serrors.ErrUnresolvedSemanticReference, Message: "expression references unknown semantic dataset", Details: map[string]any{"dataset": ref.Qualifier, "field": ref.Name}}
		}
		field, ok := r.model.Fields[ref.Qualifier+"."+ref.Name]
		if !ok {
			return nil, &serrors.Error{Code: serrors.ErrUnresolvedSemanticReference, Message: "expression references unknown semantic field", Details: map[string]any{"dataset": ref.Qualifier, "field": ref.Name}}
		}
		bindings[ref] = boundColumn(ref.Qualifier, field.Field)
	}

	if len(bareFields) == 0 {
		return bindings, nil
	}
	candidates := make([]string, 0)
	for dataset := range r.model.Datasets {
		matches := true
		for field := range bareFields {
			if _, ok := r.model.Fields[dataset+"."+field]; !ok {
				matches = false
				break
			}
		}
		if matches {
			candidates = append(candidates, dataset)
		}
	}
	sort.Strings(candidates)
	switch len(candidates) {
	case 0:
		// No dataset holds every bare name, but that has two causes with two
		// different fixes. A name that exists in no dataset at all is a
		// reference the model never defines; names that each exist but never
		// together are individually defined and cannot be bound as one row.
		// Reporting both as one condition told the author to look in the wrong
		// place half the time.
		if undefined := namesInNoDataset(r.model, bareFields); len(undefined) > 0 {
			return nil, &serrors.Error{
				Code:    serrors.ErrUnresolvedSemanticReference,
				Message: "expression references names the model does not define",
				Details: map[string]any{"names": undefined},
			}
		}
		return nil, &serrors.Error{Code: serrors.ErrInconsistentExpressionReferences, Message: "expression references fields that cannot be bound to one semantic dataset", Details: map[string]any{"fields": sortedStringSet(bareFields)}}
	case 1:
		for fieldName := range bareFields {
			ref := expression.Reference{Name: fieldName}
			bindings[ref] = boundColumn(candidates[0], r.model.Fields[candidates[0]+"."+fieldName].Field)
		}
		return bindings, nil
	default:
		return nil, &serrors.Error{Code: serrors.ErrAmbiguousExpressionReference, Message: "unqualified expression references are ambiguous", Details: map[string]any{"fields": sortedStringSet(bareFields), "datasets": candidates}}
	}
}

func boundColumn(dataset string, field *ossie.Field) expression.BoundSymbol {
	return expression.BoundSymbol{
		Kind:        expression.BoundColumn,
		Qualifier:   dataset,
		Name:        field.Name,
		Type:        semanticType(field.Datatype),
		Aggregation: expression.AggregationScalar,
	}
}

func semanticType(datatype ossie.DataType) expression.SemanticType {
	switch datatype {
	case ossie.DataTypeString:
		return expression.TypeString
	case ossie.DataTypeInteger:
		return expression.TypeInteger
	case ossie.DataTypeDecimal, ossie.DataTypeFloat:
		return expression.TypeDecimal
	case ossie.DataTypeBoolean:
		return expression.TypeBoolean
	case ossie.DataTypeDate:
		return expression.TypeDate
	case ossie.DataTypeTime:
		return expression.TypeTime
	case ossie.DataTypeDateTime, ossie.DataTypeDateTimeTz:
		return expression.TypeTimestamp
	case ossie.DataTypeOpaque:
		return expression.TypeVariant
	default:
		return expression.TypeUnknown
	}
}

// namesInNoDataset returns the referenced names that no dataset in the model
// defines, sorted. An empty result means every name exists somewhere and the
// failure is about binding them together, not about defining them.
func namesInNoDataset(model *ModelIndex, names map[string]struct{}) []string {
	undefined := make([]string, 0, len(names))
	for name := range names {
		found := false
		for dataset := range model.Datasets {
			if _, ok := model.Fields[dataset+"."+name]; ok {
				found = true
				break
			}
		}
		if !found {
			undefined = append(undefined, name)
		}
	}
	sort.Strings(undefined)
	return undefined
}
