package resolver

import "github.com/meaningforge/metis/query"

func validateQueryIntentShape(q query.SemanticQuery) error {
	switch q.Intent {
	case "":
		return nil
	case query.QueryIntentDistinctValues:
		if len(q.Metrics) != 0 {
			return invalidQuery("distinct_values intent does not accept metrics", map[string]any{"metrics": len(q.Metrics)})
		}
		if len(q.Dimensions) != 1 {
			return invalidQuery("distinct_values intent requires exactly one dimension", map[string]any{"dimensions": len(q.Dimensions)})
		}
		return nil
	default:
		return invalidQuery("unsupported query intent", map[string]any{"intent": q.Intent})
	}
}

func validateResolvedQueryIntent(q query.SemanticQuery, resolved *SemanticQuerySpec) error {
	if q.Intent != query.QueryIntentDistinctValues || resolved == nil {
		return nil
	}
	for _, filter := range resolved.Filters {
		if filter.Kind == FilterTargetMetric {
			return invalidQuery("distinct_values intent does not accept metric filters", map[string]any{"field": filter.Filter.Field})
		}
	}
	selected := resolved.Dimensions[0].Name
	for _, order := range resolved.OrderBy {
		if order.Kind != OrderTargetDimension || order.Name != selected {
			return invalidQuery("distinct_values order_by must target the requested dimension", map[string]any{"field": order.Name, "dimension": selected})
		}
	}
	return nil
}
