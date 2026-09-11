package policy

import (
	"sort"

	"github.com/meaningforge/metis/expression"
	"github.com/meaningforge/metis/resolver"
)

// FieldClosure expands canonical field seeds through selected field expressions.
// It does not discover workload seeds: metric, relationship, calendar and closed
// workflow obligations must be collected by their respective semantic owners.
// All returned fields are canonical and deterministically ordered. Cycles,
// missing expressions and unprovable source ownership fail closed.
func FieldClosure(q *resolver.SemanticQuerySpec, seeds []FieldRef) ([]FieldRef, error) {
	if q == nil || q.Model == nil || q.Model.Model == nil || len(seeds) > MaxFields {
		return nil, ErrInvalid
	}
	state := map[FieldRef]uint8{}
	var visit func(FieldRef) error
	visit = func(f FieldRef) error {
		if f.Dataset.Model != q.Model.Model.Name || !hasField(q, f) {
			return ErrInvalid
		}
		if state[f] == 1 {
			return ErrInvalid
		}
		if state[f] == 2 {
			return nil
		}
		if len(state) >= MaxFields {
			return ErrInvalid
		}
		state[f] = 1
		selected, ok := q.FieldExpression(f.Dataset.Dataset, f.Field)
		if !ok || !selected.IsResolved() {
			return ErrInvalid
		}
		parsed, err := expression.Parse(selected.Source, expression.ProfileForDialect(selected.SourceDialect))
		if err != nil {
			return ErrInvalid
		}
		// CollectReferences is deliberately a general discovery projection and
		// shortens path references. Security closure first rejects path access,
		// whose ownership cannot be proven by that projection.
		guard := &fieldOwnershipVisitor{}
		expression.Walk(parsed, guard)
		if guard.invalid {
			return ErrInvalid
		}
		for _, ref := range expression.CollectReferences(parsed) {
			dataset := f.Dataset.Dataset
			if ref.Qualifier != "" {
				dataset = ref.Qualifier
			}
			if q.Model.Datasets[dataset] == nil {
				return ErrInvalid
			}
			dependency := FieldRef{Dataset: DatasetRef{Model: f.Dataset.Model, Dataset: dataset}, Field: ref.Name}
			if dependency == f {
				continue
			} // the field's own physical column
			if hasField(q, dependency) {
				if err := visit(dependency); err != nil {
					return err
				}
			} else if dataset != f.Dataset.Dataset {
				// An unregistered column on a different source is not bounded
				// canonical dependency evidence.
				return ErrInvalid
			}
		}
		state[f] = 2
		return nil
	}
	for _, seed := range seeds {
		if err := visit(seed); err != nil {
			return nil, err
		}
	}
	out := make([]FieldRef, 0, len(state))
	for f := range state {
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Dataset.Model != out[j].Dataset.Model {
			return out[i].Dataset.Model < out[j].Dataset.Model
		}
		if out[i].Dataset.Dataset != out[j].Dataset.Dataset {
			return out[i].Dataset.Dataset < out[j].Dataset.Dataset
		}
		return out[i].Field < out[j].Field
	})
	return out, nil
}

type fieldOwnershipVisitor struct{ invalid bool }

func (v *fieldOwnershipVisitor) Enter(expr expression.Expr) bool {
	switch e := expr.(type) {
	case *expression.IdentifierExpr:
		if len(e.Parts) == 0 || len(e.Parts) > 2 {
			v.invalid = true
		}
	case *expression.PathAccessExpr, *expression.IndexExpr:
		v.invalid = true
	}
	return !v.invalid
}
func (*fieldOwnershipVisitor) Leave(expression.Expr) {}
