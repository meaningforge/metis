package semanticplan

import (
	"fmt"
	"math"

	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/query"
)

// RelationPredicate is a bound, source-local constraint. The canonical Field
// and physical Column are distinct; no expression text or policy lookup is
// permitted downstream. Placement is always the relation input before joins.
type RelationPredicate struct {
	Field    string
	Column   string
	Datatype ossie.DataType
	Operator query.FilterOperator
	Values   []any
}

// RelationPolicy is immutable, so plan clones may share its identity safely.
// It carries bound constraints, never Principal, entitlement lookup or an
// executable policy. Scope is opaque operation-wide equality evidence.
type RelationPolicy struct {
	scope      string
	dataset    string
	predicates []RelationPredicate
}

func NewRelationPolicy(scope, dataset string, predicates []RelationPredicate) (*RelationPolicy, error) {
	if scope == "" || dataset == "" {
		return nil, fmt.Errorf("relation policy identity is required")
	}
	for _, predicate := range predicates {
		if predicate.Field == "" || predicate.Column == "" || predicate.Datatype == "" {
			return nil, fmt.Errorf("relation policy binding is incomplete")
		}
		switch predicate.Operator {
		case query.FilterIsNull, query.FilterIsNotNull:
			if len(predicate.Values) != 0 {
				return nil, fmt.Errorf("invalid relation policy value arity")
			}
		case query.FilterIN, query.FilterNotIn:
			if len(predicate.Values) == 0 {
				return nil, fmt.Errorf("invalid relation policy value arity")
			}
		case query.FilterBetween:
			if len(predicate.Values) != 2 {
				return nil, fmt.Errorf("invalid relation policy value arity")
			}
		case query.FilterEQ, query.FilterNEQ, query.FilterGT, query.FilterGTE, query.FilterLT, query.FilterLTE:
			if len(predicate.Values) != 1 {
				return nil, fmt.Errorf("invalid relation policy value arity")
			}
		default:
			return nil, fmt.Errorf("unsupported relation policy operator")
		}
		for _, value := range predicate.Values {
			switch v := value.(type) {
			case string, bool, int64:
			case float64:
				if math.IsNaN(v) || math.IsInf(v, 0) {
					return nil, fmt.Errorf("invalid relation policy scalar")
				}
			default:
				return nil, fmt.Errorf("invalid relation policy scalar")
			}
		}
	}
	return &RelationPolicy{scope: scope, dataset: dataset, predicates: cloneRelationPredicates(predicates)}, nil
}

func (p *RelationPolicy) Predicates() []RelationPredicate {
	if p == nil {
		return nil
	}
	return cloneRelationPredicates(p.predicates)
}

func cloneRelationPredicates(in []RelationPredicate) []RelationPredicate {
	out := append([]RelationPredicate(nil), in...)
	for i := range out {
		out[i].Values = append([]any(nil), in[i].Values...)
	}
	return out
}

// WithRelationPolicies installs constraints on all owned source inputs before
// optimization. Missing coverage is terminal. Construction-only query-shape
// fields are not a second policy authority.
func WithRelationPolicies(plan *SemanticPlan, scope string, policies map[string]*RelationPolicy) (*SemanticPlan, error) {
	if plan == nil || scope == "" || plan.PolicyScope != "" {
		return nil, fmt.Errorf("invalid relation policy installation")
	}
	for dataset, policy := range policies {
		if policy == nil || policy.scope != scope || policy.dataset != dataset {
			return nil, fmt.Errorf("relation policy scope mismatch")
		}
	}
	out := ClonePlan(plan)
	out.PolicyScope = scope
	err := visitOwnedRelationInputs(out, true, func(dataset *DatasetRef) error {
		if dataset.Name == "" {
			return nil
		}
		policy := policies[dataset.Name]
		if policy == nil {
			return fmt.Errorf("relation policy coverage is incomplete")
		}
		dataset.Policy = policy
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func validateRelationPolicies(plan *SemanticPlan) error {
	return visitOwnedRelationInputs(plan, false, func(dataset *DatasetRef) error {
		if dataset.Name == "" {
			return nil
		}
		if plan.PolicyScope == "" {
			if dataset.Policy != nil {
				return fmt.Errorf("relation policy has no operation scope")
			}
			return nil
		}
		p := dataset.Policy
		if p == nil || p.scope != plan.PolicyScope || p.dataset != dataset.Name {
			return fmt.Errorf("relation policy scope or coverage mismatch")
		}
		return nil
	})
}

func visitOwnedRelationInputs(plan *SemanticPlan, write bool, visit func(*DatasetRef) error) error {
	if plan.DenseCalendar != nil {
		if err := visit(&plan.DenseCalendar.Dataset); err != nil {
			return err
		}
	}
	if plan.CustomDenseCalendar != nil {
		if err := visit(&plan.CustomDenseCalendar.Dataset); err != nil {
			return err
		}
	}
	for i, node := range plan.Nodes {
		if source, ok := NodeSourceState(node); ok {
			if err := visit(&source.Root); err != nil {
				return err
			}
			for j := range source.Joins {
				join := &source.Joins[j]
				dataset := DatasetRef{Name: join.ToDataset, Source: join.ToSource, Policy: join.Policy}
				if err := visit(&dataset); err != nil {
					return err
				}
				if write {
					join.Policy = dataset.Policy
				}
			}
			if write {
				updated, err := WithNodeSourceState(node, source)
				if err != nil {
					return err
				}
				node = updated
			}
		}
		switch n := node.(type) {
		case CumulativeWindowNode:
			if n.CustomCalendarRolling != nil {
				if err := visit(&n.CustomCalendarRolling.Dataset); err != nil {
					return err
				}
			}
			if n.CustomCalendarGrainToDate != nil {
				if err := visit(&n.CustomCalendarGrainToDate.Dataset); err != nil {
					return err
				}
			}
		case TimeOffsetNode:
			if n.CustomCalendar != nil {
				if err := visit(&n.CustomCalendar.Dataset); err != nil {
					return err
				}
			}
		case OffsetToGrainNode:
			if n.OffsetPlan != nil && n.OffsetPlan.CustomCalendar {
				if err := visit(&n.OffsetPlan.Dataset); err != nil {
					return err
				}
			}
		case ConversionNode:
			if n.PhysicalInputs != nil {
				if err := visit(&n.PhysicalInputs.BaseDataset); err != nil {
					return err
				}
				if err := visit(&n.PhysicalInputs.ConversionDataset); err != nil {
					return err
				}
			}
		}
		if write {
			plan.Nodes[i] = node
		}
	}
	return nil
}

func projectRelationPolicy(p *projector, policy *RelationPolicy) {
	if policy == nil {
		return
	} // preserve every unrestricted fingerprint
	p.node("relation_policy", func() {
		p.text("scope", policy.scope)
		p.text("dataset", policy.dataset)
		p.text("placement", "relation_input")
		p.sequence("predicates", len(policy.predicates), func(i int) {
			predicate := policy.predicates[i]
			p.text("field", predicate.Field)
			p.text("column", predicate.Column)
			p.text("datatype", string(predicate.Datatype))
			p.text("operator", string(predicate.Operator))
			p.sequence("values", len(predicate.Values), func(j int) {
				p.text("type", fmt.Sprintf("%T", predicate.Values[j]))
				p.opaque("value", predicate.Values[j])
			})
		})
	})
}
