package conversion

import "github.com/meaningforge/metis/planner/semanticplan"

type denseNodeSpec struct {
	Name          string
	OutputGrain   []semanticplan.GroupBy
	Metrics       []string
	FillMetrics   map[string]struct{}
	HasCumulative bool
	HasTimeOffset bool
}

func denseTimeRelativeSemanticNodes(nodes []semanticplan.SemanticPlanNode) (map[string]denseNodeSpec, error) {
	nodesByID := semanticplan.NodesByID(nodes)
	indexes := semanticNodeIndexes(nodes)
	out := map[string]denseNodeSpec{}
	for _, node := range nodes {
		baseMetric, cumulative, offset := "", false, false
		switch typed := node.(type) {
		case semanticplan.CumulativeWindowNode:
			if len(typed.Base.Inputs) != 1 {
				continue
			}
			baseMetric, cumulative = typed.Spec.BaseMetric, true
		case semanticplan.TimeOffsetNode:
			if len(typed.Base.Inputs) != 1 {
				continue
			}
			baseMetric, offset = typed.Spec.BaseMetric, true
		default:
			continue
		}
		base, ok := nodesByID[node.NodeBase().Inputs[0].NodeID]
		if !ok || base.Kind() != semanticplan.SemanticPlanNodeSourceAggregate {
			continue
		}
		source, ok := semanticNodeDenseSourceGroup(nodes, base, indexes)
		if !ok {
			continue
		}
		spec := out[source.Name]
		if spec.FillMetrics == nil {
			spec = denseNodeSpec{Name: source.Name, OutputGrain: cloneGroups(base.NodeBase().OutputGrain), Metrics: append([]string(nil), source.Metrics...), FillMetrics: map[string]struct{}{}}
		}
		spec.FillMetrics[baseMetric] = struct{}{}
		spec.HasCumulative = spec.HasCumulative || cumulative
		spec.HasTimeOffset = spec.HasTimeOffset || offset
		out[source.Name] = spec
	}
	return out, nil
}

func offsetToGrainDenseSemanticNodes(nodes []semanticplan.SemanticPlanNode, customOnly bool) (map[string]denseNodeSpec, error) {
	nodesByID := semanticplan.NodesByID(nodes)
	indexes := semanticNodeIndexes(nodes)
	out := map[string]denseNodeSpec{}
	for _, node := range nodes {
		offset, ok := node.(semanticplan.OffsetToGrainNode)
		if !ok || offset.OffsetPlan == nil || len(offset.Base.Inputs) != 1 || customOnly && !offset.OffsetPlan.CustomCalendar {
			continue
		}
		base, ok := nodesByID[offset.Base.Inputs[0].NodeID]
		if !ok || base.Kind() != semanticplan.SemanticPlanNodeSourceAggregate {
			continue
		}
		source, ok := semanticNodeDenseSourceGroup(nodes, base, indexes)
		if !ok {
			continue
		}
		spec := out[source.Name]
		if spec.FillMetrics == nil {
			spec = denseNodeSpec{Name: source.Name, OutputGrain: cloneGroups(base.NodeBase().OutputGrain), Metrics: append([]string(nil), source.Metrics...), FillMetrics: map[string]struct{}{}}
		}
		spec.FillMetrics[offset.Spec.BaseMetric] = struct{}{}
		out[source.Name] = spec
	}
	return out, nil
}

func semanticNodeDenseSourceGroup(nodes []semanticplan.SemanticPlanNode, base semanticplan.SemanticPlanNode, indexes map[string]int) (semanticSourceGroup, bool) {
	state, ok := semanticplan.NodeMetricState(base)
	if !ok {
		return semanticSourceGroup{}, false
	}
	if state.ShareGroup != "" {
		for _, source := range semanticNodeSourceGroups(nodes) {
			if source.Name == state.ShareGroup {
				return source, true
			}
		}
		return semanticSourceGroup{}, false
	}
	baseState := base.NodeBase()
	index, ok := indexes[baseState.ID]
	if !ok {
		return semanticSourceGroup{}, false
	}
	metrics := state.Metrics
	if len(metrics) == 0 {
		metrics = []string{baseState.ID}
	}
	return semanticSourceGroup{Name: metricCTEName(index, baseState.ID), Metrics: append([]string(nil), metrics...)}, true
}

func semanticNodeIndexes(nodes []semanticplan.SemanticPlanNode) map[string]int {
	out := make(map[string]int, len(nodes))
	for i, node := range nodes {
		out[node.NodeBase().ID] = i
	}
	return out
}

func mergeDenseNodeSpecs(dst map[string]denseNodeSpec, src map[string]denseNodeSpec) map[string]denseNodeSpec {
	if dst == nil {
		dst = map[string]denseNodeSpec{}
	}
	for name, incoming := range src {
		current, ok := dst[name]
		if !ok {
			dst[name] = incoming
			continue
		}
		if current.FillMetrics == nil {
			current.FillMetrics = map[string]struct{}{}
		}
		for metric := range incoming.FillMetrics {
			current.FillMetrics[metric] = struct{}{}
		}
		current.HasCumulative = current.HasCumulative || incoming.HasCumulative
		current.HasTimeOffset = current.HasTimeOffset || incoming.HasTimeOffset
		dst[name] = current
	}
	return dst
}

func splitDenseGroups(groups []semanticplan.GroupBy, calendar *semanticplan.DenseCalendarPlan) (*semanticplan.GroupBy, []semanticplan.GroupBy) {
	var timeGroup *semanticplan.GroupBy
	other := make([]semanticplan.GroupBy, 0, len(groups))
	for i := range groups {
		group := groups[i]
		matches := group.Field != nil && group.Field.Name == calendar.QueryTimeDimension || unqualifiedName(group.Name) == calendar.QueryTimeDimension
		if timeGroup == nil && matches && group.Grain != nil && *group.Grain == calendar.Grain {
			copy := group
			timeGroup = &copy
			continue
		}
		other = append(other, group)
	}
	return timeGroup, other
}

func splitCustomDenseGroups(groups []semanticplan.GroupBy, calendar *semanticplan.CustomDenseCalendarPlan) (*semanticplan.GroupBy, []semanticplan.GroupBy) {
	var timeGroup *semanticplan.GroupBy
	other := make([]semanticplan.GroupBy, 0, len(groups))
	for i := range groups {
		group := groups[i]
		matches := group.CustomCalendar != nil && group.CustomCalendar.Grain == calendar.Grain && group.CustomCalendar.Dataset == calendar.Dataset.Name && (group.Name == calendar.QueryTimeDimension || unqualifiedName(group.Name) == calendar.QueryTimeDimension)
		if timeGroup == nil && matches {
			copy := group
			timeGroup = &copy
			continue
		}
		other = append(other, group)
	}
	return timeGroup, other
}
