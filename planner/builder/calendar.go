package builder

import (
	"github.com/meaningforge/metis/expression"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/resolver"
	"github.com/meaningforge/metis/serrors"
)

// PlanResolvedDimension converts resolved dimension evidence into the logical
// projection and grouping evidence owned by a SemanticPlan.
func PlanResolvedDimension(d resolver.ResolvedDimension) (semanticplan.Projection, semanticplan.GroupBy, error) {
	if d.CustomCalendar == nil {
		projection := semanticplan.Projection{Name: d.Name, Kind: semanticplan.ProjectionDimension, Field: d.Field, Dataset: d.Dataset, Grain: d.Grain, Expression: d.Expression}
		group := semanticplan.GroupBy{Name: d.Name, Dataset: d.Dataset, Field: d.Field, Grain: d.Grain, Expression: d.Expression}
		return projection, group, nil
	}
	custom := d.CustomCalendar
	if d.Grain == nil || custom.Dataset == nil || custom.BaseTimeField == nil || custom.BucketField == nil || custom.OrdinalField == nil {
		return semanticplan.Projection{}, semanticplan.GroupBy{}, &serrors.Error{Code: serrors.ErrInternalInvariant, Message: "resolved custom calendar metadata is incomplete", Details: map[string]any{"dimension": d.Name}}
	}
	if string(*d.Grain) != custom.Grain.Name {
		return semanticplan.Projection{}, semanticplan.GroupBy{}, &serrors.Error{Code: serrors.ErrIncompatibleQueryGrain, Message: "resolved custom calendar grain does not match query grain", Details: map[string]any{"dimension": d.Name, "query_grain": *d.Grain, "calendar_grain": custom.Grain.Name}}
	}
	if custom.BucketSelectedExpression == "" || custom.OrdinalSelectedExpression == "" {
		return semanticplan.Projection{}, semanticplan.GroupBy{}, &serrors.Error{Code: serrors.ErrInternalInvariant, Message: "custom calendar physical expressions must be selected before planning", Details: map[string]any{"dimension": d.Name, "grain": *d.Grain}}
	}
	levels := make(map[string]semanticplan.CustomCalendarLevel, len(custom.Levels))
	for name, resolvedLevel := range custom.Levels {
		if resolvedLevel == nil || resolvedLevel.BucketField == nil || resolvedLevel.OrdinalField == nil || resolvedLevel.BucketSelectedExpression == "" || resolvedLevel.OrdinalSelectedExpression == "" {
			return semanticplan.Projection{}, semanticplan.GroupBy{}, &serrors.Error{Code: serrors.ErrIncompleteCustomCalendar, Message: "custom calendar hierarchy level is incomplete", Details: map[string]any{"dimension": d.Name, "grain": name}}
		}
		levels[name] = semanticplan.CustomCalendarLevel{
			Grain:             resolvedLevel.Grain,
			BucketField:       resolvedLevel.BucketField,
			BucketExpression:  resolvedLevel.BucketSelectedExpression,
			OrdinalField:      resolvedLevel.OrdinalField,
			OrdinalExpression: resolvedLevel.OrdinalSelectedExpression,
		}
	}
	calendar := &semanticplan.CustomCalendarGrouping{
		Spec:               custom.Spec,
		Grain:              *d.Grain,
		Dataset:            custom.Dataset.Name,
		DatasetSource:      custom.Dataset.Source,
		BaseTimeField:      custom.BaseTimeField,
		BaseTimeExpression: d.Expression.Source,
		BucketField:        custom.BucketField,
		BucketExpression:   custom.BucketSelectedExpression,
		OrdinalField:       custom.OrdinalField,
		OrdinalExpression:  custom.OrdinalSelectedExpression,
		Levels:             levels,
	}
	projection := semanticplan.Projection{
		Name:           d.Name,
		Kind:           semanticplan.ProjectionDimension,
		Field:          custom.BucketField,
		Dataset:        custom.Dataset.Name,
		Expression:     expression.NewResolvedExpression("custom_calendar", custom.BucketSelectedExpression),
		CustomCalendar: calendar,
	}
	group := semanticplan.GroupBy{
		Name:           d.Name,
		Dataset:        custom.Dataset.Name,
		Field:          custom.BucketField,
		Expression:     expression.NewResolvedExpression("custom_calendar", custom.BucketSelectedExpression),
		CustomCalendar: calendar,
	}
	return projection, group, nil
}

// EnrichCustomCalendars attaches custom-calendar evidence to typed metric
// nodes and derives the one supported dense-calendar domain for the query.
func EnrichCustomCalendars(nodes []semanticplan.SemanticPlanNode, groups []semanticplan.GroupBy) (*semanticplan.CustomDenseCalendarPlan, error) {
	if err := planCustomCalendarOffsets(nodes); err != nil {
		return nil, err
	}
	if err := planCustomCalendarCumulatives(nodes); err != nil {
		return nil, err
	}
	if err := planCustomCalendarGrainToDates(nodes, groups); err != nil {
		return nil, err
	}
	return planCustomDenseCalendar(nodes, groups)
}

func planCustomCalendarOffsets(nodes []semanticplan.SemanticPlanNode) error {
	for i := range nodes {
		timeOffset, ok := nodes[i].(semanticplan.TimeOffsetNode)
		if !ok || ossie.IsBuiltInTimeOffsetUnit(timeOffset.Spec.Offset.Unit) {
			continue
		}
		planned, err := planCustomCalendarOffset(timeOffset.Base.ID, timeOffset.Spec, timeOffset.Base.OutputGrain)
		if err != nil {
			return err
		}
		timeOffset.CustomCalendar = semanticplan.CloneCustomCalendarOffsetPlan(&planned)
		nodes[i] = timeOffset
	}
	return nil
}

func planCustomCalendarOffset(metricName string, spec ossie.TimeOffsetMetricSpec, groups []semanticplan.GroupBy) (semanticplan.CustomCalendarOffsetPlan, error) {
	for _, group := range groups {
		calendar := group.CustomCalendar
		if calendar == nil || string(calendar.Grain) != spec.Offset.Unit {
			continue
		}
		if calendar.BaseTimeField == nil || calendar.BaseTimeField.Name != spec.TimeDimension {
			continue
		}
		if calendar.Dataset == "" || calendar.DatasetSource == "" || calendar.BucketField == nil || calendar.OrdinalField == nil || calendar.BucketExpression == "" || calendar.OrdinalExpression == "" {
			return semanticplan.CustomCalendarOffsetPlan{}, &serrors.Error{Code: serrors.ErrIncompleteCustomCalendar, Message: "custom-calendar offset mapping is incomplete", Details: map[string]any{"metric": metricName, "unit": spec.Offset.Unit}}
		}
		return semanticplan.CustomCalendarOffsetPlan{
			Grain:             calendar.Grain,
			Count:             spec.Offset.Count,
			Dataset:           semanticplan.DatasetRef{Name: calendar.Dataset, Source: calendar.DatasetSource},
			BucketField:       calendar.BucketField,
			BucketExpression:  calendar.BucketExpression,
			OrdinalField:      calendar.OrdinalField,
			OrdinalExpression: calendar.OrdinalExpression,
		}, nil
	}
	return semanticplan.CustomCalendarOffsetPlan{}, &serrors.Error{
		Code:    serrors.ErrIncompleteCustomCalendar,
		Message: "custom-calendar time offset requires the matching custom query grain",
		Details: map[string]any{"metric": metricName, "time_dimension": spec.TimeDimension, "offset_unit": spec.Offset.Unit},
	}
}

func planCustomCalendarCumulatives(nodes []semanticplan.SemanticPlanNode) error {
	for i := range nodes {
		cumulative, ok := nodes[i].(semanticplan.CumulativeWindowNode)
		if !ok || cumulative.Spec.Window.Type != "rolling" || IsBuiltInCumulativeUnit(cumulative.Spec.Window.Unit) {
			continue
		}
		planned, err := planCustomCalendarCumulative(cumulative.Base.ID, cumulative.Spec, cumulative.Base.OutputGrain)
		if err != nil {
			return err
		}
		cumulative.CustomCalendarRolling = semanticplan.CloneCustomCalendarCumulativePlan(&planned)
		nodes[i] = cumulative
	}
	return nil
}

func planCustomCalendarCumulative(metricName string, spec ossie.CumulativeMetricSpec, groups []semanticplan.GroupBy) (semanticplan.CustomCalendarCumulativePlan, error) {
	for _, group := range groups {
		calendar := group.CustomCalendar
		if calendar == nil || string(calendar.Grain) != spec.Window.Unit {
			continue
		}
		if calendar.BaseTimeField == nil || calendar.BaseTimeField.Name != spec.TimeDimension {
			continue
		}
		if calendar.Dataset == "" || calendar.DatasetSource == "" || calendar.BucketField == nil || calendar.OrdinalField == nil || calendar.BucketExpression == "" || calendar.OrdinalExpression == "" {
			return semanticplan.CustomCalendarCumulativePlan{}, &serrors.Error{Code: serrors.ErrIncompleteCustomCalendar, Message: "custom-calendar cumulative mapping is incomplete", Details: map[string]any{"metric": metricName, "unit": spec.Window.Unit}}
		}
		return semanticplan.CustomCalendarCumulativePlan{
			Grain:             calendar.Grain,
			Count:             spec.Window.Count,
			Dataset:           semanticplan.DatasetRef{Name: calendar.Dataset, Source: calendar.DatasetSource},
			BucketField:       calendar.BucketField,
			BucketExpression:  calendar.BucketExpression,
			OrdinalField:      calendar.OrdinalField,
			OrdinalExpression: calendar.OrdinalExpression,
		}, nil
	}
	return semanticplan.CustomCalendarCumulativePlan{}, &serrors.Error{
		Code:    serrors.ErrIncompleteCustomCalendar,
		Message: "custom-calendar rolling cumulative requires the matching custom query grain",
		Details: map[string]any{"metric": metricName, "time_dimension": spec.TimeDimension, "window_unit": spec.Window.Unit},
	}
}

// IsBuiltInCumulativeUnit reports whether a cumulative window is expressed in
// the fixed set of built-in time units rather than a custom-calendar grain.
func IsBuiltInCumulativeUnit(unit string) bool {
	switch unit {
	case "hour", "day", "week", "month", "quarter", "year":
		return true
	default:
		return false
	}
}

func planCustomCalendarGrainToDates(nodes []semanticplan.SemanticPlanNode, groups []semanticplan.GroupBy) error {
	for i := range nodes {
		cumulative, ok := nodes[i].(semanticplan.CumulativeWindowNode)
		if !ok || cumulative.Spec.Window.Type != "grain_to_date" || IsBuiltInCumulativeUnit(cumulative.Spec.Window.Unit) {
			continue
		}
		planned, err := planCustomCalendarGrainToDate(cumulative.Base.ID, cumulative.Spec, groups)
		if err != nil {
			return err
		}
		cumulative.CustomCalendarGrainToDate = semanticplan.CloneCustomCalendarGrainToDatePlan(&planned)
		nodes[i] = cumulative
	}
	return nil
}

func planCustomCalendarGrainToDate(metricName string, spec ossie.CumulativeMetricSpec, groups []semanticplan.GroupBy) (semanticplan.CustomCalendarGrainToDatePlan, error) {
	for _, group := range groups {
		calendar := group.CustomCalendar
		if calendar == nil || calendar.BaseTimeField == nil || calendar.BaseTimeField.Name != spec.TimeDimension {
			continue
		}
		queryGrain := string(calendar.Grain)
		resetGrain := spec.Window.Unit
		if !ossie.CustomCalendarGrainIsDescendant(calendar.Spec, queryGrain, resetGrain) {
			continue
		}
		reset, ok := calendar.Levels[resetGrain]
		if !ok || reset.BucketField == nil || reset.BucketExpression == "" || calendar.OrdinalField == nil || calendar.OrdinalExpression == "" || calendar.Dataset == "" || calendar.DatasetSource == "" {
			return semanticplan.CustomCalendarGrainToDatePlan{}, &serrors.Error{Code: serrors.ErrIncompleteCustomCalendar, Message: "custom-calendar grain-to-date reset mapping is incomplete", Details: map[string]any{"metric": metricName, "query_grain": queryGrain, "reset_grain": resetGrain}}
		}
		return semanticplan.CustomCalendarGrainToDatePlan{
			QueryGrain:             calendar.Grain,
			ResetGrain:             query.TimeGrain(resetGrain),
			Dataset:                semanticplan.DatasetRef{Name: calendar.Dataset, Source: calendar.DatasetSource},
			ResetBucketField:       reset.BucketField,
			ResetBucketExpression:  reset.BucketExpression,
			QueryOrdinalField:      calendar.OrdinalField,
			QueryOrdinalExpression: calendar.OrdinalExpression,
		}, nil
	}
	return semanticplan.CustomCalendarGrainToDatePlan{}, &serrors.Error{
		Code:    serrors.ErrIncompatibleQueryGrain,
		Message: "custom-calendar grain-to-date requires query grain at or below reset boundary",
		Details: map[string]any{"metric": metricName, "time_dimension": spec.TimeDimension, "reset_grain": spec.Window.Unit},
	}
}

func planCustomDenseCalendar(nodes []semanticplan.SemanticPlanNode, groups []semanticplan.GroupBy) (*semanticplan.CustomDenseCalendarPlan, error) {
	if semanticplan.CustomCalendarDomainCount(nodes) == 0 && !hasCustomOffsetToGrainNodes(nodes) {
		return nil, nil
	}
	var selected *semanticplan.CustomDenseCalendarPlan
	add := func(metric string, grain query.TimeGrain, dataset semanticplan.DatasetRef, bucketExpression, ordinalExpression string) error {
		var matched *semanticplan.GroupBy
		for i := range groups {
			group := &groups[i]
			if group.CustomCalendar == nil || group.CustomCalendar.Grain != grain || group.CustomCalendar.Dataset != dataset.Name {
				continue
			}
			matched = group
			break
		}
		if matched == nil || matched.CustomCalendar == nil || matched.CustomCalendar.BucketField == nil || matched.CustomCalendar.OrdinalField == nil {
			return &serrors.Error{Code: serrors.ErrIncompatibleQueryGrain, Message: "custom dense calendar requires the resolved custom query grain", Details: map[string]any{"metric": metric, "grain": grain}}
		}
		if bucketExpression == "" {
			bucketExpression = matched.CustomCalendar.BucketExpression
		}
		if ordinalExpression == "" {
			ordinalExpression = matched.CustomCalendar.OrdinalExpression
		}
		candidate := &semanticplan.CustomDenseCalendarPlan{
			Dataset: dataset, QueryTimeDimension: matched.Name, Grain: grain,
			BucketField: matched.CustomCalendar.BucketField, BucketExpression: bucketExpression,
			OrdinalField: matched.CustomCalendar.OrdinalField, OrdinalExpression: ordinalExpression,
		}
		if selected == nil {
			selected = candidate
			return nil
		}
		if selected.Dataset != candidate.Dataset || selected.Grain != candidate.Grain || selected.QueryTimeDimension != candidate.QueryTimeDimension {
			return &serrors.Error{Code: serrors.ErrUnsupportedQueryShape, Message: "v1 supports one custom dense-calendar domain per query", Details: map[string]any{"metric": metric, "grain": grain}}
		}
		return nil
	}
	for _, node := range nodes {
		if offset, ok := node.(semanticplan.TimeOffsetNode); ok && offset.CustomCalendar != nil {
			calendar := offset.CustomCalendar
			if err := add(offset.Base.ID, calendar.Grain, calendar.Dataset, calendar.BucketExpression, calendar.OrdinalExpression); err != nil {
				return nil, err
			}
		}
	}
	for _, node := range nodes {
		if cumulative, ok := node.(semanticplan.CumulativeWindowNode); ok && cumulative.CustomCalendarRolling != nil {
			calendar := cumulative.CustomCalendarRolling
			if err := add(cumulative.Base.ID, calendar.Grain, calendar.Dataset, calendar.BucketExpression, calendar.OrdinalExpression); err != nil {
				return nil, err
			}
		}
	}
	for _, node := range nodes {
		offset, ok := node.(semanticplan.OffsetToGrainNode)
		if !ok || offset.OffsetPlan == nil || !offset.OffsetPlan.CustomCalendar {
			continue
		}
		boundary := offset.OffsetPlan
		if err := add(offset.Base.ID, boundary.QueryGrain, boundary.Dataset, "", boundary.OrdinalExpression); err != nil {
			return nil, err
		}
	}
	return selected, nil
}

func hasCustomOffsetToGrainNodes(nodes []semanticplan.SemanticPlanNode) bool {
	for _, node := range nodes {
		offset, ok := node.(semanticplan.OffsetToGrainNode)
		if ok && offset.OffsetPlan != nil && offset.OffsetPlan.CustomCalendar {
			return true
		}
	}
	return false
}
