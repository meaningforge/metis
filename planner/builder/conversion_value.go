package builder

import (
	"strings"

	"github.com/meaningforge/metis/expression"
	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/resolver"
	"github.com/meaningforge/metis/serrors"
)

func planConversionEventValue(q *resolver.SemanticQuerySpec, metric resolver.ResolvedMetric, eventRoot string) (semanticplan.ConversionEventValuePlan, error) {
	var model *manifest.ModelIndex
	if q != nil {
		model = q.Model
	}
	if model == nil || metric.Metric == nil || strings.TrimSpace(eventRoot) == "" {
		return semanticplan.ConversionEventValuePlan{}, conversionValuePlanningError(metric.Name, "conversion event value requires model, metric, and event root", nil)
	}
	parsed, err := expression.Parse(metric.Expression.Source, expression.ProfileForDialect(metric.Expression.SourceDialect))
	if err != nil {
		return semanticplan.ConversionEventValuePlan{}, conversionValuePlanningError(metric.Name, "conversion event metric expression is not parseable", map[string]any{"cause": err.Error()})
	}
	call, ok := parsed.(*expression.FunctionCallExpr)
	if !ok {
		return semanticplan.ConversionEventValuePlan{}, conversionValuePlanningError(metric.Name, "conversion event metric must be a direct additive aggregate", map[string]any{"supported": []string{"SUM(local_event_field)", "COUNT(*)"}})
	}

	switch strings.ToUpper(strings.TrimSpace(call.Name)) {
	case "COUNT":
		if len(call.Args) != 1 {
			return semanticplan.ConversionEventValuePlan{}, conversionValuePlanningError(metric.Name, "conversion COUNT input must be COUNT(*)", nil)
		}
		if _, ok := call.Args[0].(*expression.WildcardExpr); !ok {
			return semanticplan.ConversionEventValuePlan{}, conversionValuePlanningError(metric.Name, "conversion COUNT input must be COUNT(*)", nil)
		}
		return semanticplan.ConversionEventValuePlan{Metric: metric.Name, Dataset: eventRoot, Kind: semanticplan.ConversionEventValueCountRows}, nil
	case "SUM":
		if len(call.Args) != 1 {
			return semanticplan.ConversionEventValuePlan{}, conversionValuePlanningError(metric.Name, "conversion SUM input requires exactly one local event field", nil)
		}
		identifier, ok := call.Args[0].(*expression.IdentifierExpr)
		if !ok {
			return semanticplan.ConversionEventValuePlan{}, conversionValuePlanningError(metric.Name, "conversion SUM input must reference one local event field directly", nil)
		}
		ref, err := conversionEventValueFieldRef(metric.Name, eventRoot, identifier.Parts)
		if err != nil {
			return semanticplan.ConversionEventValuePlan{}, err
		}
		handle := model.Fields[ref.Dataset+"."+ref.Name]
		if handle == nil || handle.Field == nil {
			return semanticplan.ConversionEventValuePlan{}, conversionValuePlanningError(metric.Name, "conversion event value field is unavailable", map[string]any{"dataset": ref.Dataset, "field": ref.Name})
		}
		selected, ok := q.FieldExpression(ref.Dataset, ref.Name)
		if !ok {
			return semanticplan.ConversionEventValuePlan{}, &serrors.Error{Code: serrors.ErrUnsupportedExpression, Message: "conversion event value field has no compatible Renderer expression", Details: map[string]any{"metric": metric.Name, "dataset": ref.Dataset, "field": ref.Name}}
		}
		field := semanticplan.ConversionPhysicalFieldRef{Dataset: ref.Dataset, Name: ref.Name, Expression: selected.Source}
		return semanticplan.ConversionEventValuePlan{Metric: metric.Name, Dataset: eventRoot, Kind: semanticplan.ConversionEventValueSumField, Field: &field}, nil
	default:
		return semanticplan.ConversionEventValuePlan{}, conversionValuePlanningError(metric.Name, "conversion event metric aggregate is not composable before candidate matching", map[string]any{"aggregate": call.Name, "supported": []string{"SUM", "COUNT(*)"}})
	}
}

func conversionEventValueFieldRef(metricName, eventRoot string, parts []string) (semanticplan.ConversionFieldRef, error) {
	switch len(parts) {
	case 1:
		return semanticplan.ConversionFieldRef{Dataset: eventRoot, Name: parts[0]}, nil
	case 2:
		if parts[0] != eventRoot {
			return semanticplan.ConversionFieldRef{}, conversionValuePlanningError(metricName, "conversion event value field must belong to its event root", map[string]any{"event_root": eventRoot, "field_dataset": parts[0], "field": parts[1]})
		}
		return semanticplan.ConversionFieldRef{Dataset: parts[0], Name: parts[1]}, nil
	default:
		return semanticplan.ConversionFieldRef{}, conversionValuePlanningError(metricName, "conversion event value field must be a local semantic field reference", map[string]any{"reference": strings.Join(parts, ".")})
	}
}

func conversionValuePlanningError(metric, message string, details map[string]any) error {
	if details == nil {
		details = map[string]any{}
	}
	if metric != "" {
		details["metric"] = metric
	}
	return &serrors.Error{Code: serrors.ErrInvalidConversionMetric, Message: message, Details: details}
}
