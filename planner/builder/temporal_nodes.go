package builder

import (
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/serrors"
)

// ValidateTimeOffsetQueryGrain verifies that a resolved query exposes the
// explicit built-in or custom grain a time-offset node needs.
func ValidateTimeOffsetQueryGrain(metricName string, spec ossie.TimeOffsetMetricSpec, groups []semanticplan.GroupBy) error {
	var matched *semanticplan.GroupBy
	for i := range groups {
		group := &groups[i]
		if group.CustomCalendar != nil && group.CustomCalendar.BaseTimeField != nil && group.CustomCalendar.BaseTimeField.Name == spec.TimeDimension {
			matched = group
			break
		}
		if group.Field == nil {
			continue
		}
		if group.Field.Name == spec.TimeDimension || unqualifiedName(group.Name) == spec.TimeDimension {
			matched = group
			break
		}
	}
	if matched == nil {
		return &serrors.Error{Code: serrors.ErrIncompatibleQueryGrain, Message: "time-offset metric requires its time dimension at an explicit query grain", Details: map[string]any{"metric": metricName, "time_dimension": spec.TimeDimension}}
	}
	if !ossie.IsBuiltInTimeOffsetUnit(spec.Offset.Unit) {
		if matched.CustomCalendar == nil || string(matched.CustomCalendar.Grain) != spec.Offset.Unit {
			return &serrors.Error{Code: serrors.ErrIncompatibleQueryGrain, Message: "custom time-offset unit must match the resolved custom query grain", Details: map[string]any{"metric": metricName, "time_dimension": spec.TimeDimension, "offset_unit": spec.Offset.Unit}}
		}
		return nil
	}
	if matched.Grain == nil {
		return &serrors.Error{Code: serrors.ErrIncompatibleQueryGrain, Message: "time-offset metric requires its time dimension at an explicit query grain", Details: map[string]any{"metric": metricName, "time_dimension": spec.TimeDimension}}
	}
	want := query.TimeGrain(spec.Offset.Unit)
	if *matched.Grain != want {
		return &serrors.Error{Code: serrors.ErrIncompatibleQueryGrain, Message: "time-offset unit must match query time grain in v1", Details: map[string]any{"metric": metricName, "time_dimension": spec.TimeDimension, "offset_unit": spec.Offset.Unit, "query_grain": *matched.Grain}}
	}
	return nil
}

// PlanOffsetToGrain converts an offset-to-grain metric specification and
// resolved query grain into node-owned typed boundary evidence.
func PlanOffsetToGrain(metricName string, spec ossie.OffsetToGrainMetricSpec, groups []semanticplan.GroupBy) (semanticplan.OffsetToGrainPlan, error) {
	for i := range groups {
		group := &groups[i]
		if group.CustomCalendar != nil && group.CustomCalendar.BaseTimeField != nil && group.CustomCalendar.BaseTimeField.Name == spec.TimeDimension {
			return planCustomOffsetToGrain(metricName, spec, group)
		}
		if group.Field == nil || (group.Field.Name != spec.TimeDimension && unqualifiedName(group.Name) != spec.TimeDimension) {
			continue
		}
		if group.Grain == nil {
			return semanticplan.OffsetToGrainPlan{}, offsetToGrainPlanningError(metricName, spec, "offset-to-grain requires its time dimension at an explicit query grain")
		}
		boundary := query.TimeGrain(spec.Grain)
		if !builtInGrainAtOrBelow(*group.Grain, boundary) {
			return semanticplan.OffsetToGrainPlan{}, offsetToGrainPlanningError(metricName, spec, "offset-to-grain query grain must be equal to or finer than boundary grain")
		}
		return semanticplan.OffsetToGrainPlan{TimeDimension: spec.TimeDimension, QueryGrain: *group.Grain, BoundaryGrain: boundary}, nil
	}
	return semanticplan.OffsetToGrainPlan{}, offsetToGrainPlanningError(metricName, spec, "offset-to-grain requires its time dimension in the query grain")
}

func planCustomOffsetToGrain(metricName string, spec ossie.OffsetToGrainMetricSpec, group *semanticplan.GroupBy) (semanticplan.OffsetToGrainPlan, error) {
	calendar := group.CustomCalendar
	queryGrain := string(calendar.Grain)
	if queryGrain != spec.Grain && !ossie.CustomCalendarGrainIsDescendant(calendar.Spec, queryGrain, spec.Grain) {
		return semanticplan.OffsetToGrainPlan{}, offsetToGrainPlanningError(metricName, spec, "custom offset-to-grain query grain must be equal to or descend from boundary grain")
	}
	boundary, ok := calendar.Levels[spec.Grain]
	if !ok || boundary.BucketField == nil || boundary.BucketExpression == "" || calendar.OrdinalField == nil || calendar.OrdinalExpression == "" || calendar.Dataset == "" || calendar.DatasetSource == "" {
		return semanticplan.OffsetToGrainPlan{}, offsetToGrainPlanningError(metricName, spec, "custom offset-to-grain boundary mapping is incomplete")
	}
	return semanticplan.OffsetToGrainPlan{
		TimeDimension:      spec.TimeDimension,
		QueryGrain:         calendar.Grain,
		BoundaryGrain:      query.TimeGrain(spec.Grain),
		CustomCalendar:     true,
		Dataset:            semanticplan.DatasetRef{Name: calendar.Dataset, Source: calendar.DatasetSource},
		BoundaryBucket:     boundary.BucketField,
		BoundaryExpression: boundary.BucketExpression,
		QueryOrdinal:       calendar.OrdinalField,
		OrdinalExpression:  calendar.OrdinalExpression,
	}, nil
}

func builtInGrainAtOrBelow(queryGrain, boundary query.TimeGrain) bool {
	rank := func(grain query.TimeGrain) (int, bool) {
		switch grain {
		case query.TimeGrainHour:
			return 0, true
		case query.TimeGrainDay:
			return 1, true
		case query.TimeGrainWeek:
			return 2, true
		case query.TimeGrainMonth:
			return 3, true
		case query.TimeGrainQuarter:
			return 4, true
		case query.TimeGrainYear:
			return 5, true
		default:
			return 0, false
		}
	}
	queryRank, queryOK := rank(queryGrain)
	boundaryRank, boundaryOK := rank(boundary)
	return queryOK && boundaryOK && queryRank <= boundaryRank
}

func offsetToGrainPlanningError(metricName string, spec ossie.OffsetToGrainMetricSpec, message string) error {
	return &serrors.Error{Code: serrors.ErrUnsupportedTimeFilter, Message: message, Details: map[string]any{
		"metric": metricName, "time_dimension": spec.TimeDimension, "boundary_grain": spec.Grain,
	}}
}

func unqualifiedName(name string) string {
	for i := len(name) - 1; i >= 0; i-- {
		if name[i] == '.' {
			return name[i+1:]
		}
	}
	return name
}
