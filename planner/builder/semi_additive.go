package builder

import (
	"strings"

	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/resolver"
	"github.com/meaningforge/metis/serrors"
)

const (
	semiAdditiveRawOrderPrefix    = "__metis_ordered_"
	semiAdditivePrivateBasePrefix = "__metis_semi_input_"
)

func semiAdditivePrivateBaseName(consumer, base string) string {
	return semiAdditivePrivateBasePrefix + consumer + "_" + base
}

func queriedSemiAdditiveTimeWindow(groups []semanticplan.GroupBy, dimension string) bool {
	for _, group := range groups {
		if group.CustomCalendar != nil && group.CustomCalendar.BaseTimeField != nil {
			base := group.CustomCalendar.BaseTimeField.Name
			if base == dimension || group.Name == dimension || unqualifiedName(group.Name) == dimension {
				return true
			}
		}
		if group.Grain == nil || group.Field == nil {
			continue
		}
		if group.Field.Name == dimension || group.Name == dimension || unqualifiedName(group.Name) == dimension {
			return true
		}
	}
	return false
}

func semiAdditiveRawOrderName(dimension string) string {
	name := strings.TrimSpace(dimension)
	name = strings.ReplaceAll(name, ".", "_")
	return semiAdditiveRawOrderPrefix + name
}

func hiddenTimeGroup(q *resolver.SemanticQuerySpec, name string) (semanticplan.GroupBy, error) {
	return hiddenSemanticGroup(q, name, "time dimension")
}

func hiddenWindowGrouping(q *resolver.SemanticQuerySpec, name string) (semanticplan.GroupBy, error) {
	group, err := hiddenSemanticGroup(q, name, "window grouping")
	if err != nil {
		return semanticplan.GroupBy{}, err
	}
	if group.Field == nil || group.Field.Dimension == nil {
		return semanticplan.GroupBy{}, semiAdditivePlanningError(name, "semi-additive window grouping must resolve to a semantic dimension")
	}
	if isTimeDimensionField(group.Field) {
		return semanticplan.GroupBy{}, semiAdditivePlanningError(name, "semi-additive window grouping must resolve to a non-time semantic dimension")
	}
	return group, nil
}

func isTimeDimensionField(field *ossie.Field) bool {
	if field == nil || field.Dimension == nil {
		return false
	}
	if field.Dimension.IsTime != nil {
		return *field.Dimension.IsTime
	}
	switch field.Datatype {
	case ossie.DataTypeDate, ossie.DataTypeTime, ossie.DataTypeDateTime, ossie.DataTypeDateTimeTz:
		return true
	default:
		return false
	}
}

func hiddenSemanticGroup(q *resolver.SemanticQuerySpec, name, role string) (semanticplan.GroupBy, error) {
	var found *semanticplan.GroupBy
	for _, dataset := range q.Model.Datasets {
		if dataset == nil {
			continue
		}
		for i := range dataset.Fields {
			field := &dataset.Fields[i]
			if field.Name != name {
				continue
			}
			resolvedExpression, ok := q.FieldExpression(dataset.Name, field.Name)
			if !ok {
				return semanticplan.GroupBy{}, semiAdditivePlanningError(name, "semi-additive "+role+" has no renderable expression")
			}
			candidate := semanticplan.GroupBy{Name: field.Name, Dataset: dataset.Name, Field: field, Expression: resolvedExpression}
			if found != nil {
				return semanticplan.GroupBy{}, semiAdditivePlanningError(name, "semi-additive "+role+" is ambiguous")
			}
			found = &candidate
		}
	}
	if found == nil {
		return semanticplan.GroupBy{}, semiAdditivePlanningError(name, "semi-additive "+role+" is not present in the semantic model")
	}
	return *found, nil
}

func containsGroup(groups []semanticplan.GroupBy, name string) bool {
	for _, group := range groups {
		if group.Name == name || unqualifiedName(group.Name) == name {
			return true
		}
	}
	return false
}

func semiAdditivePlanningError(metric, message string) error {
	return &serrors.Error{Code: serrors.ErrInvalidMetricExtension, Message: message, Details: map[string]any{"metric": metric}}
}

// semiAdditiveEvaluationKind maps a declared semi-additive policy to its
// canonical node kind.
func semiAdditiveEvaluationKind(metric string, aggregation string) (semanticplan.SemanticPlanNodeKind, error) {
	switch aggregation {
	case "last":
		return semanticplan.SemanticPlanNodeSemiAdditiveLast, nil
	case "first":
		return semanticplan.SemanticPlanNodeSemiAdditiveFirst, nil
	default:
		return "", semiAdditivePlanningError(metric, "unsupported semi-additive aggregation policy")
	}
}

// requireSemiAdditiveEvaluationKind fails closed when a semi-additive node
// reaches source enrichment with a kind its typed payload does not
// support.
func requireSemiAdditiveEvaluationKind(node semanticplan.SemiAdditiveNode, aggregation string) error {
	expected, err := semiAdditiveEvaluationKind(node.Base.ID, aggregation)
	if err != nil {
		return err
	}
	if node.Kind() != expected {
		return &serrors.Error{
			Code:    serrors.ErrInternalInvariant,
			Message: "semi-additive evaluation kind was not settled before source planning",
			Details: map[string]any{"metric": node.Base.ID, "kind": string(node.Kind()), "aggregation": aggregation, "expected_kind": string(expected)},
		}
	}
	return nil
}
