package policy

import (
	"time"

	"github.com/meaningforge/metis/expression"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/resolver"
)

// BoundPredicate is application-owned binding evidence, not policy-adapter
// output. Column is the single physical column proven by semantic binding.
// Consumers must lower it as a typed relation-local column with parameters.
type BoundPredicate struct {
	Field    FieldRef
	Column   string
	Datatype ossie.DataType
	Operator query.FilterOperator
	Values   []any
}

type BoundSource struct {
	Dataset    DatasetRef
	Predicates []BoundPredicate
}

// Bind validates a snapshot against its exact workload and resolved model
// expressions. The caller owns one immutable semantic generation and passes
// the exact Resolver results for the selected Renderer. This function performs
// no expression selection, Renderer lookup, policy evaluation, or SQL rewriting.
// Complete dependency collection and subsequent scan enforcement are separate
// mandatory obligations; successful binding alone never authorizes execution.
func Bind(snapshot Snapshot, req Request, resolved map[string]*resolver.SemanticQuerySpec) ([]BoundSource, error) {
	if !snapshot.Matches(req) {
		return nil, ErrInvalid
	}
	for _, source := range req.Sources {
		q := resolved[source.Dataset.Model]
		if q == nil || q.Project != req.ProjectID || q.Model == nil || q.Model.Model == nil || q.Model.Model.Name != source.Dataset.Model || q.Model.Datasets[source.Dataset.Dataset] == nil {
			return nil, ErrInvalid
		}
		for _, f := range source.RequiredFields {
			if !hasField(q, f) {
				return nil, ErrInvalid
			}
		}
	}
	if snapshot.Effect() == Unrestricted {
		return nil, nil
	}
	if snapshot.Effect() != Constrained {
		return nil, ErrInvalid
	}
	// Refuse an incomplete canonical workload instead of allowing a computed
	// field to hide a denied dependency from the adapter. Cross-source expansion
	// must also have been present in the original, single policy request.
	required := make(map[FieldRef]bool)
	for _, source := range req.Sources {
		for _, field := range source.RequiredFields {
			required[field] = true
		}
	}
	for _, source := range req.Sources {
		closure, err := FieldClosure(resolved[source.Dataset.Model], source.RequiredFields)
		if err != nil {
			return nil, ErrInvalid
		}
		for _, field := range closure {
			if !required[field] {
				return nil, ErrInvalid
			}
		}
	}
	constraints := snapshot.Constraints()
	bound := make([]BoundSource, 0, len(constraints))
	for _, source := range constraints {
		q := resolved[source.Dataset.Model]
		for _, f := range source.DeniedFields {
			if !hasField(q, f) {
				return nil, ErrInvalid
			}
		}
		out := BoundSource{Dataset: source.Dataset}
		for _, predicate := range source.RowPredicates {
			if !hasField(q, predicate.Field) {
				return nil, ErrInvalid
			}
			field := q.Model.Fields[predicate.Field.Dataset.Dataset+"."+predicate.Field.Field].Field
			selected, ok := q.FieldExpression(source.Dataset.Dataset, field.Name)
			if !ok || !selected.IsResolved() {
				return nil, ErrInvalid
			}
			parsed, err := expression.Parse(selected.Source, expression.ProfileForDialect(selected.SourceDialect))
			if err != nil {
				return nil, ErrInvalid
			}
			identifier, ok := parsed.(*expression.IdentifierExpr)
			if !ok || len(identifier.Parts) == 0 || len(identifier.Parts) > 2 {
				return nil, ErrInvalid
			}
			if len(identifier.Parts) == 2 && identifier.Parts[0] != source.Dataset.Dataset {
				return nil, ErrInvalid
			}
			column := identifier.Parts[len(identifier.Parts)-1]
			if column == "" {
				return nil, ErrInvalid
			}
			// A differently named semantic field is an indirect dependency, not
			// proof of a raw column. Never follow it or infer its physical identity.
			if column != field.Name && q.Model.Fields[source.Dataset.Dataset+"."+column] != nil {
				return nil, ErrInvalid
			}
			if !compatiblePredicate(field.Datatype, predicate) {
				return nil, ErrInvalid
			}
			out.Predicates = append(out.Predicates, BoundPredicate{Field: predicate.Field, Column: column, Datatype: field.Datatype, Operator: predicate.Operator, Values: cloneSlice(predicate.Values)})
		}
		bound = append(bound, out)
	}
	return bound, nil
}

func hasField(q *resolver.SemanticQuerySpec, f FieldRef) bool {
	if q == nil || q.Model == nil {
		return false
	}
	h := q.Model.Fields[f.Dataset.Dataset+"."+f.Field]
	return h != nil && h.Field != nil && h.Dataset == f.Dataset.Dataset && h.Field.Name == f.Field
}

func compatiblePredicate(datatype ossie.DataType, p Predicate) bool {
	// Even null checks require known scalar types. Unknown/opaque types are not
	// a permissive escape hatch from the typed policy contract.
	switch datatype {
	case ossie.DataTypeString, ossie.DataTypeInteger, ossie.DataTypeFloat, ossie.DataTypeDecimal,
		ossie.DataTypeBoolean, ossie.DataTypeDate, ossie.DataTypeTime, ossie.DataTypeDateTime, ossie.DataTypeDateTimeTz:
	default:
		return false
	}
	if datatype == ossie.DataTypeBoolean {
		switch p.Operator {
		case query.FilterEQ, query.FilterNEQ, query.FilterIN, query.FilterNotIn, query.FilterIsNull, query.FilterIsNotNull:
		default:
			return false
		}
	}
	for _, value := range p.Values {
		switch datatype {
		case ossie.DataTypeString:
			if _, ok := value.(string); !ok {
				return false
			}
		case ossie.DataTypeBoolean:
			if _, ok := value.(bool); !ok {
				return false
			}
		case ossie.DataTypeInteger:
			if _, ok := value.(int64); !ok {
				return false
			}
		case ossie.DataTypeDecimal, ossie.DataTypeFloat:
			switch value.(type) {
			case int64, float64:
			default:
				return false
			}
		default:
			text, ok := value.(string)
			if !ok || !validTemporalLiteral(datatype, text) {
				return false
			}
		}
	}
	return true
}

func validTemporalLiteral(datatype ossie.DataType, text string) bool {
	layout := ""
	switch datatype {
	case ossie.DataTypeDate:
		layout = "2006-01-02"
	case ossie.DataTypeTime:
		layout = "15:04:05.999999999"
	case ossie.DataTypeDateTime:
		layout = "2006-01-02T15:04:05.999999999"
	case ossie.DataTypeDateTimeTz:
		layout = time.RFC3339Nano
	default:
		return false
	}
	_, err := time.Parse(layout, text)
	return err == nil
}
