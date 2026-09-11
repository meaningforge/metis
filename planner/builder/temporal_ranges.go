package builder

import (
	"fmt"
	"time"

	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/planner/temporal"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/serrors"
)

// ApplyTemporalReadRanges derives source-read and final-output predicate
// ownership from typed time-offset, offset-to-grain, and cumulative nodes.
func ApplyTemporalReadRanges(plan *semanticplan.SemanticPlan, groups []semanticplan.GroupBy, predicates []semanticplan.Predicate) error {
	if err := applyTimeOffsetReadRanges(plan, groups, predicates); err != nil {
		return err
	}
	if err := applyOffsetToGrainReadRanges(plan, groups, predicates); err != nil {
		return err
	}
	if err := applyRollingCumulativeReadRanges(plan, groups, predicates); err != nil {
		return err
	}
	return applyGrainToDateReadRanges(plan, groups, predicates)
}

// ApplyTimeOffsetReadRanges enriches one plan with time-offset read ranges.
func ApplyTimeOffsetReadRanges(plan *semanticplan.SemanticPlan, groups []semanticplan.GroupBy, predicates []semanticplan.Predicate) error {
	return applyTimeOffsetReadRanges(plan, groups, predicates)
}

// ApplyOffsetToGrainReadRanges enriches one plan with boundary read ranges.
func ApplyOffsetToGrainReadRanges(plan *semanticplan.SemanticPlan, groups []semanticplan.GroupBy, predicates []semanticplan.Predicate) error {
	return applyOffsetToGrainReadRanges(plan, groups, predicates)
}

// ApplyRollingCumulativeReadRanges enriches one plan with rolling lookbacks.
func ApplyRollingCumulativeReadRanges(plan *semanticplan.SemanticPlan, groups []semanticplan.GroupBy, predicates []semanticplan.Predicate) error {
	return applyRollingCumulativeReadRanges(plan, groups, predicates)
}

// ApplyGrainToDateReadRanges enriches one plan with reset-period read ranges.
func ApplyGrainToDateReadRanges(plan *semanticplan.SemanticPlan, groups []semanticplan.GroupBy, predicates []semanticplan.Predicate) error {
	return applyGrainToDateReadRanges(plan, groups, predicates)
}

// ApplyDenseCalendarRanges records the resolved output and source predicates
// used by a dense-calendar domain without changing their ownership.
func ApplyDenseCalendarRanges(calendar *semanticplan.DenseCalendarPlan, plan *semanticplan.SemanticPlan, output []semanticplan.Predicate) {
	if calendar == nil || plan == nil || calendar.QueryTimeDimension == "" {
		return
	}
	calendar.OutputPredicates = matchingTimePredicates(output, calendar.QueryTimeDimension)
	for _, node := range plan.Nodes {
		if _, ok := node.(semanticplan.SourceAggregateNode); !ok {
			continue
		}
		read := matchingTimePredicates(nodePredicates(node), calendar.QueryTimeDimension)
		if len(read) != 0 {
			calendar.ReadPredicates = read
			return
		}
	}
}

type timeOffsetLookback struct {
	count int
	unit  query.TimeGrain
}

func applyTimeOffsetReadRanges(plan *semanticplan.SemanticPlan, groups []semanticplan.GroupBy, predicates []semanticplan.Predicate) error {
	if plan == nil {
		return nil
	}
	lookbacks := map[string][]timeOffsetLookback{}
	for _, node := range plan.Nodes {
		timeOffset, ok := node.(semanticplan.TimeOffsetNode)
		if ok {
			lookbacks[timeOffset.Spec.TimeDimension] = append(lookbacks[timeOffset.Spec.TimeDimension], timeOffsetLookback{count: timeOffset.Spec.Offset.Count, unit: query.TimeGrain(timeOffset.Spec.Offset.Unit)})
		}
	}
	for _, predicate := range predicates {
		if predicate.Field == nil || len(lookbacks[predicate.Field.Name]) == 0 {
			continue
		}
		outputName := matchingTimeGroupName(groups, predicate, predicate.Field.Name)
		if outputName == "" {
			return &serrors.Error{Code: serrors.ErrIncompatibleQueryGrain, Message: "time-offset filter requires its time dimension in query grain", Details: map[string]any{"time_dimension": predicate.Field.Name}}
		}
		appendPostPredicate(plan, semanticplan.PostEvaluationPredicate{Name: outputName, Filter: predicate.Filter})
		custom := false
		for _, lookback := range lookbacks[predicate.Field.Name] {
			custom = custom || !ossie.IsBuiltInTimeOffsetUnit(string(lookback.unit))
		}
		if custom {
			if err := validateTimeRelativeRangeOperator(predicate); err != nil {
				return err
			}
			removeSourcePredicate(plan, predicate, nil)
			continue
		}
		expanded, err := expandHistoricalPredicateForLookbacks(predicate, lookbacks[predicate.Field.Name])
		if err != nil {
			return err
		}
		replaceSourcePredicate(plan, predicate, expanded, nil)
	}
	return nil
}

func applyOffsetToGrainReadRanges(plan *semanticplan.SemanticPlan, groups []semanticplan.GroupBy, predicates []semanticplan.Predicate) error {
	if plan == nil {
		return nil
	}
	type boundaryReads struct {
		unit                          query.TimeGrain
		builtInSources, customSources map[string]struct{}
	}
	byID := semanticplan.NodesByID(plan.Nodes)
	boundaries := map[string]boundaryReads{}
	for _, node := range plan.Nodes {
		offset, ok := node.(semanticplan.OffsetToGrainNode)
		if !ok || offset.OffsetPlan == nil {
			continue
		}
		current := boundaries[offset.Spec.TimeDimension]
		sources := map[string]struct{}{}
		collectSourceNodeIDs(offset.Base.ID, byID, sources, map[string]bool{})
		if offset.OffsetPlan.CustomCalendar {
			if current.customSources == nil {
				current.customSources = map[string]struct{}{}
			}
			for source := range sources {
				current.customSources[source] = struct{}{}
			}
		} else {
			if current.builtInSources == nil {
				current.builtInSources = map[string]struct{}{}
			}
			if current.unit == "" || temporal.IsCoarserGrain(offset.OffsetPlan.BoundaryGrain, current.unit) {
				current.unit = offset.OffsetPlan.BoundaryGrain
			}
			for source := range sources {
				current.builtInSources[source] = struct{}{}
			}
		}
		boundaries[offset.Spec.TimeDimension] = current
	}
	for _, predicate := range predicates {
		if predicate.Field == nil {
			continue
		}
		boundary, ok := boundaries[predicate.Field.Name]
		if !ok {
			continue
		}
		outputName := matchingTimeGroupName(groups, predicate, predicate.Field.Name)
		if outputName == "" {
			return &serrors.Error{Code: serrors.ErrIncompatibleQueryGrain, Message: "offset-to-grain filter requires its time dimension in query grain", Details: map[string]any{"time_dimension": predicate.Field.Name}}
		}
		if err := validateTimeRelativeRangeOperator(predicate); err != nil {
			return err
		}
		appendPostPredicate(plan, semanticplan.PostEvaluationPredicate{Name: outputName, Filter: predicate.Filter})
		if len(boundary.customSources) != 0 {
			removeSourcePredicate(plan, predicate, boundary.customSources)
		}
		if len(boundary.builtInSources) == 0 {
			continue
		}
		expanded, err := expandOffsetToGrainPredicate(predicate, boundary.unit)
		if err != nil {
			return err
		}
		for source := range boundary.builtInSources {
			mergeHistoricalReadPredicate(plan, source, expanded)
		}
	}
	return nil
}

func applyRollingCumulativeReadRanges(plan *semanticplan.SemanticPlan, groups []semanticplan.GroupBy, predicates []semanticplan.Predicate) error {
	if plan == nil {
		return nil
	}
	type lookback struct {
		count   int
		unit    query.TimeGrain
		sources map[string]struct{}
	}
	byID, lookbacks := semanticplan.NodesByID(plan.Nodes), map[string]lookback{}
	for _, node := range plan.Nodes {
		cumulative, ok := node.(semanticplan.CumulativeWindowNode)
		if !ok || cumulative.Spec.Window.Type != "rolling" {
			continue
		}
		candidate := lookback{count: -(cumulative.Spec.Window.Count - 1), unit: query.TimeGrain(cumulative.Spec.Window.Unit), sources: map[string]struct{}{}}
		collectSourceNodeIDs(cumulative.Base.ID, byID, candidate.sources, map[string]bool{})
		current, ok := lookbacks[cumulative.Spec.TimeDimension]
		if !ok {
			lookbacks[cumulative.Spec.TimeDimension] = candidate
			continue
		}
		if candidate.count < current.count {
			current.count, current.unit = candidate.count, candidate.unit
		}
		for source := range candidate.sources {
			current.sources[source] = struct{}{}
		}
		lookbacks[cumulative.Spec.TimeDimension] = current
	}
	for _, predicate := range predicates {
		if predicate.Field == nil {
			continue
		}
		lookback, ok := lookbacks[predicate.Field.Name]
		if !ok {
			continue
		}
		if matchingTimeGroupName(groups, predicate, predicate.Field.Name) == "" {
			return &serrors.Error{Code: serrors.ErrIncompatibleQueryGrain, Message: "rolling cumulative filter requires its time dimension in query grain", Details: map[string]any{"time_dimension": predicate.Field.Name}}
		}
		expanded, err := expandHistoricalPredicate(predicate, lookback.count, lookback.unit)
		if err != nil {
			return err
		}
		for source := range lookback.sources {
			appendSourcePredicate(plan, source, expanded)
		}
	}
	return nil
}

func applyGrainToDateReadRanges(plan *semanticplan.SemanticPlan, groups []semanticplan.GroupBy, predicates []semanticplan.Predicate) error {
	if plan == nil {
		return nil
	}
	type resetPlan struct {
		unit    query.TimeGrain
		sources map[string]struct{}
	}
	byID, resets := semanticplan.NodesByID(plan.Nodes), map[string]resetPlan{}
	for _, node := range plan.Nodes {
		cumulative, ok := node.(semanticplan.CumulativeWindowNode)
		if !ok || cumulative.Spec.Window.Type != "grain_to_date" || !IsBuiltInCumulativeUnit(cumulative.Spec.Window.Unit) {
			continue
		}
		candidate := resetPlan{unit: query.TimeGrain(cumulative.Spec.Window.Unit), sources: map[string]struct{}{}}
		collectSourceNodeIDs(cumulative.Base.ID, byID, candidate.sources, map[string]bool{})
		current, ok := resets[cumulative.Spec.TimeDimension]
		if !ok || temporal.IsCoarserGrain(candidate.unit, current.unit) {
			current.unit = candidate.unit
		}
		if current.sources == nil {
			current.sources = map[string]struct{}{}
		}
		for source := range candidate.sources {
			current.sources[source] = struct{}{}
		}
		resets[cumulative.Spec.TimeDimension] = current
	}
	for _, predicate := range predicates {
		if predicate.Field == nil {
			continue
		}
		reset, ok := resets[predicate.Field.Name]
		if !ok {
			continue
		}
		if matchingTimeGroupName(groups, predicate, predicate.Field.Name) == "" {
			return &serrors.Error{Code: serrors.ErrIncompatibleQueryGrain, Message: "grain-to-date filter requires its time dimension in query grain", Details: map[string]any{"time_dimension": predicate.Field.Name}}
		}
		expanded, err := expandGrainToDatePredicate(predicate, reset.unit)
		if err != nil {
			return err
		}
		for source := range reset.sources {
			mergeHistoricalReadPredicate(plan, source, expanded)
		}
	}
	return nil
}

func matchingTimeGroupName(groups []semanticplan.GroupBy, predicate semanticplan.Predicate, timeDimension string) string {
	for _, group := range groups {
		if group.CustomCalendar != nil && group.CustomCalendar.Dataset == predicate.Dataset && group.CustomCalendar.BaseTimeField != nil && group.CustomCalendar.BaseTimeField.Name == timeDimension {
			return group.Name
		}
		if group.Field != nil && group.Field.Name == timeDimension && group.Dataset == predicate.Dataset {
			return group.Name
		}
	}
	return ""
}

func appendPostPredicate(plan *semanticplan.SemanticPlan, predicate semanticplan.PostEvaluationPredicate) {
	for _, existing := range plan.Output.Predicates {
		if existing.Name == predicate.Name && existing.Filter.Field == predicate.Filter.Field && existing.Filter.Operator == predicate.Filter.Operator {
			return
		}
	}
	plan.Output.Predicates = append(plan.Output.Predicates, predicate)
}

func nodePredicates(node semanticplan.SemanticPlanNode) []semanticplan.Predicate {
	if node == nil {
		return nil
	}
	var out []semanticplan.Predicate
	for _, predicate := range node.NodeBase().Predicates {
		if predicate.Predicate != nil {
			out = append(out, *predicate.Predicate)
		}
	}
	return out
}

func collectSourceNodeIDs(name string, nodes map[string]semanticplan.SemanticPlanNode, out map[string]struct{}, visiting map[string]bool) {
	if visiting[name] {
		return
	}
	visiting[name] = true
	defer delete(visiting, name)
	node, ok := nodes[name]
	if !ok || node == nil {
		return
	}
	if _, ok := node.(semanticplan.SourceAggregateNode); ok {
		out[node.NodeBase().ID] = struct{}{}
		return
	}
	for _, input := range node.NodeBase().Inputs {
		collectSourceNodeIDs(input.NodeID, nodes, out, visiting)
	}
}

func removeSourcePredicate(plan *semanticplan.SemanticPlan, predicate semanticplan.Predicate, sources map[string]struct{}) {
	for i, node := range plan.Nodes {
		value, ok := node.(semanticplan.SourceAggregateNode)
		if !ok || (sources != nil && !containsSource(sources, value.Base.ID)) {
			continue
		}
		kept := value.Base.Predicates[:0]
		for _, candidate := range value.Base.Predicates {
			if candidate.Predicate == nil || !sameSourcePredicate(*candidate.Predicate, predicate) {
				kept = append(kept, candidate)
			}
		}
		value.Base.Predicates = kept
		plan.Nodes[i] = value
	}
}

func replaceSourcePredicate(plan *semanticplan.SemanticPlan, original, expanded semanticplan.Predicate, sources map[string]struct{}) {
	for i, node := range plan.Nodes {
		value, ok := node.(semanticplan.SourceAggregateNode)
		if !ok || (sources != nil && !containsSource(sources, value.Base.ID)) {
			continue
		}
		for j := range value.Base.Predicates {
			if value.Base.Predicates[j].Predicate != nil && sameSourcePredicate(*value.Base.Predicates[j].Predicate, original) {
				owned := expanded
				value.Base.Predicates[j].Predicate = &owned
			}
		}
		plan.Nodes[i] = value
	}
}

func mergeHistoricalReadPredicate(plan *semanticplan.SemanticPlan, source string, expanded semanticplan.Predicate) {
	for i, node := range plan.Nodes {
		value, ok := node.(semanticplan.SourceAggregateNode)
		if !ok || value.Base.ID != source {
			continue
		}
		for j := range value.Base.Predicates {
			candidate := &value.Base.Predicates[j]
			if candidate.Predicate == nil || expanded.Field == nil || candidate.Predicate.Field == nil || candidate.Predicate.Field.Name != expanded.Field.Name || candidate.Predicate.Dataset != expanded.Dataset {
				continue
			}
			newLower, oldLower := historicalLowerBound(expanded), historicalLowerBound(*candidate.Predicate)
			if oldLower == "" || (newLower != "" && newLower < oldLower) {
				owned := expanded
				candidate.Predicate = &owned
			}
			plan.Nodes[i] = value
			return
		}
		appendSourcePredicate(plan, source, expanded)
		return
	}
}

// MergeHistoricalReadPredicate keeps the earliest source-read lower bound for
// one typed source node.
func MergeHistoricalReadPredicate(plan *semanticplan.SemanticPlan, source string, expanded semanticplan.Predicate) {
	mergeHistoricalReadPredicate(plan, source, expanded)
}

func appendSourcePredicate(plan *semanticplan.SemanticPlan, source string, predicate semanticplan.Predicate) {
	for i, node := range plan.Nodes {
		value, ok := node.(semanticplan.SourceAggregateNode)
		if !ok || value.Base.ID != source {
			continue
		}
		owned := predicate
		value.Base.Predicates = append(value.Base.Predicates, semanticplan.SemanticPlanNodePredicate{Scope: semanticplan.SemanticPredicatePreAggregation, OwnerNodeID: value.Base.ID, Proof: semanticplan.SemanticPredicateProofSourceOwnership, Predicate: &owned})
		plan.Nodes[i] = value
		return
	}
}

func containsSource(sources map[string]struct{}, name string) bool { _, ok := sources[name]; return ok }
func sameSourcePredicate(a, b semanticplan.Predicate) bool {
	return a.Field != nil && b.Field != nil && a.Field.Name == b.Field.Name && a.Dataset == b.Dataset
}

func validateTimeRelativeRangeOperator(predicate semanticplan.Predicate) error {
	switch predicate.Filter.Operator {
	case query.FilterBetween, query.FilterGTE, query.FilterGT, query.FilterLTE, query.FilterLT:
		return nil
	case query.FilterEQ, query.FilterNEQ, query.FilterIN, query.FilterNotIn, query.FilterIsNull, query.FilterIsNotNull:
		return &serrors.Error{Code: serrors.ErrUnsupportedTimeFilter, Message: "time-relative v1 supports range filters on its time dimension", Details: map[string]any{"field": predicate.Filter.Field, "operator": predicate.Filter.Operator}}
	default:
		return &serrors.Error{Code: serrors.ErrUnsupportedTimeFilter, Message: "unsupported time-relative time filter", Details: map[string]any{"field": predicate.Filter.Field, "operator": predicate.Filter.Operator}}
	}
}

func expandHistoricalPredicateForLookbacks(predicate semanticplan.Predicate, lookbacks []timeOffsetLookback) (semanticplan.Predicate, error) {
	if err := validateTimeRelativeRangeOperator(predicate); err != nil {
		return semanticplan.Predicate{}, err
	}
	best := predicate
	var earliest time.Time
	haveEarliest := false
	for _, lookback := range lookbacks {
		candidate, err := expandHistoricalPredicate(predicate, lookback.count, lookback.unit)
		if err != nil {
			return semanticplan.Predicate{}, err
		}
		lower := historicalLowerBound(candidate)
		if lower == "" {
			continue
		}
		parsed, _, err := temporal.Parse(lower)
		if err != nil {
			return semanticplan.Predicate{}, timeOffsetFilterError(predicate, err)
		}
		if !haveEarliest || parsed.Before(earliest) {
			best, earliest, haveEarliest = candidate, parsed, true
		}
	}
	return best, nil
}

func expandHistoricalPredicate(predicate semanticplan.Predicate, count int, unit query.TimeGrain) (semanticplan.Predicate, error) {
	if err := validateTimeRelativeRangeOperator(predicate); err != nil {
		return semanticplan.Predicate{}, err
	}
	expanded := predicate
	switch predicate.Filter.Operator {
	case query.FilterBetween:
		values, err := twoDateStrings(predicate.Filter.Value)
		if err != nil {
			return semanticplan.Predicate{}, timeOffsetFilterError(predicate, err)
		}
		start, err := temporal.Shift(values[0], count, unit)
		if err != nil {
			return semanticplan.Predicate{}, timeOffsetFilterError(predicate, err)
		}
		expanded.Filter.Value = []string{start, values[1]}
	case query.FilterGTE, query.FilterGT:
		value, ok := predicate.Filter.Value.(string)
		if !ok {
			return semanticplan.Predicate{}, timeOffsetFilterError(predicate, fmt.Errorf("lower-bound value must be a temporal string"))
		}
		shifted, err := temporal.Shift(value, count, unit)
		if err != nil {
			return semanticplan.Predicate{}, timeOffsetFilterError(predicate, err)
		}
		expanded.Filter.Value = shifted
	}
	return expanded, nil
}

func expandOffsetToGrainPredicate(predicate semanticplan.Predicate, unit query.TimeGrain) (semanticplan.Predicate, error) {
	expanded := predicate
	switch predicate.Filter.Operator {
	case query.FilterBetween:
		values, err := twoDateStrings(predicate.Filter.Value)
		if err != nil {
			return semanticplan.Predicate{}, offsetToGrainFilterError(predicate, err)
		}
		start, err := offsetToGrainBoundaryStart(values[0], unit)
		if err != nil {
			return semanticplan.Predicate{}, offsetToGrainFilterError(predicate, err)
		}
		expanded.Filter.Value = []string{start, values[1]}
	case query.FilterGTE, query.FilterGT:
		value, ok := predicate.Filter.Value.(string)
		if !ok {
			return semanticplan.Predicate{}, offsetToGrainFilterError(predicate, fmt.Errorf("lower-bound value must be a temporal string"))
		}
		start, err := offsetToGrainBoundaryStart(value, unit)
		if err != nil {
			return semanticplan.Predicate{}, offsetToGrainFilterError(predicate, err)
		}
		expanded.Filter.Operator, expanded.Filter.Value = query.FilterGTE, start
	}
	return expanded, nil
}

func expandGrainToDatePredicate(predicate semanticplan.Predicate, unit query.TimeGrain) (semanticplan.Predicate, error) {
	expanded := predicate
	switch predicate.Filter.Operator {
	case query.FilterBetween:
		values, err := twoDateStrings(predicate.Filter.Value)
		if err != nil {
			return semanticplan.Predicate{}, grainToDateFilterError(predicate, err)
		}
		start, err := temporal.PeriodStart(values[0], unit)
		if err != nil {
			return semanticplan.Predicate{}, grainToDateFilterError(predicate, err)
		}
		expanded.Filter.Value = []string{start, values[1]}
	case query.FilterGTE, query.FilterGT:
		value, ok := predicate.Filter.Value.(string)
		if !ok {
			return semanticplan.Predicate{}, grainToDateFilterError(predicate, fmt.Errorf("lower-bound value must be a temporal string"))
		}
		start, err := temporal.PeriodStart(value, unit)
		if err != nil {
			return semanticplan.Predicate{}, grainToDateFilterError(predicate, err)
		}
		expanded.Filter.Operator, expanded.Filter.Value = query.FilterGTE, start
	case query.FilterLTE, query.FilterLT:
	case query.FilterEQ, query.FilterNEQ, query.FilterIN, query.FilterNotIn, query.FilterIsNull, query.FilterIsNotNull:
		return semanticplan.Predicate{}, &serrors.Error{Code: serrors.ErrUnsupportedTimeFilter, Message: "grain-to-date v1 supports range filters on its time dimension", Details: map[string]any{"field": predicate.Filter.Field, "operator": predicate.Filter.Operator}}
	default:
		return semanticplan.Predicate{}, &serrors.Error{Code: serrors.ErrUnsupportedTimeFilter, Message: "unsupported grain-to-date time filter", Details: map[string]any{"field": predicate.Filter.Field, "operator": predicate.Filter.Operator}}
	}
	return expanded, nil
}

func historicalLowerBound(predicate semanticplan.Predicate) string {
	switch predicate.Filter.Operator {
	case query.FilterBetween:
		values, err := twoDateStrings(predicate.Filter.Value)
		if err == nil {
			return values[0]
		}
	case query.FilterGTE, query.FilterGT:
		value, _ := predicate.Filter.Value.(string)
		return value
	}
	return ""
}
func twoDateStrings(value any) ([2]string, error) {
	var out [2]string
	switch values := value.(type) {
	case []string:
		if len(values) != 2 {
			return out, fmt.Errorf("between requires exactly two values")
		}
		out[0], out[1] = values[0], values[1]
	case []any:
		if len(values) != 2 {
			return out, fmt.Errorf("between requires exactly two values")
		}
		first, ok1 := values[0].(string)
		second, ok2 := values[1].(string)
		if !ok1 || !ok2 {
			return out, fmt.Errorf("between values must be temporal strings")
		}
		out[0], out[1] = first, second
	default:
		return out, fmt.Errorf("between values must be a two-element temporal list")
	}
	return out, nil
}
func offsetToGrainBoundaryStart(value string, unit query.TimeGrain) (string, error) {
	if unit != query.TimeGrainHour {
		return temporal.PeriodStart(value, unit)
	}
	return temporal.HourStart(value)
}
func timeOffsetFilterError(predicate semanticplan.Predicate, cause error) error {
	return &serrors.Error{Code: serrors.ErrUnsupportedTimeFilter, Message: "invalid time-relative time filter", Details: map[string]any{"field": predicate.Filter.Field, "operator": predicate.Filter.Operator, "cause": cause.Error()}}
}
func offsetToGrainFilterError(predicate semanticplan.Predicate, cause error) error {
	return &serrors.Error{Code: serrors.ErrUnsupportedTimeFilter, Message: "invalid offset-to-grain time filter", Details: map[string]any{"field": predicate.Filter.Field, "operator": predicate.Filter.Operator, "cause": cause.Error()}}
}
func grainToDateFilterError(predicate semanticplan.Predicate, cause error) error {
	return &serrors.Error{Code: serrors.ErrUnsupportedTimeFilter, Message: "invalid grain-to-date time filter", Details: map[string]any{"field": predicate.Filter.Field, "operator": predicate.Filter.Operator, "cause": cause.Error()}}
}
func matchingTimePredicates(predicates []semanticplan.Predicate, timeDimension string) []semanticplan.Predicate {
	var out []semanticplan.Predicate
	for _, predicate := range predicates {
		if predicate.Field != nil && (predicate.Field.Name == timeDimension || unqualifiedName(predicate.Filter.Field) == timeDimension) {
			out = append(out, predicate)
		}
	}
	return out
}
