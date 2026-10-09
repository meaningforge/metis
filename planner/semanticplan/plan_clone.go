package semanticplan

import "github.com/meaningforge/metis/query"

// ClonePlan returns an independently owned semantic-plan snapshot suitable for
// planning rewrites and deterministic observation. Optimization trace is
// deliberately omitted: it describes how a plan was reached, not the plan's
// semantic structure.
func ClonePlan(input *SemanticPlan) *SemanticPlan {
	if input == nil {
		return nil
	}

	plan := *input
	plan.Joins = append([]Join(nil), input.Joins...)
	plan.Projections = append([]Projection(nil), input.Projections...)
	for i := range plan.Projections {
		plan.Projections[i].Datasets = append([]string(nil), input.Projections[i].Datasets...)
	}
	plan.Predicates = clonePredicates(input.Predicates)
	plan.BooleanPredicates = cloneBooleanPredicates(input.BooleanPredicates)
	plan.Groups = append([]GroupBy(nil), input.Groups...)
	plan.Sorts = append([]Sort(nil), input.Sorts...)
	plan.OptimizationTrace = nil
	plan.Requested = append([]string(nil), input.Requested...)
	if nodes, err := CloneNodes(input.Nodes); err == nil {
		plan.Nodes = nodes
	} else {
		// Invalid typed nodes are rejected by validation. Keep cloning total so
		// callers can preserve the invalid input for its eventual diagnostic.
		plan.Nodes = append([]SemanticPlanNode(nil), input.Nodes...)
	}
	plan.Output = CloneOutputContract(input.Output)
	plan.DenseCalendar = CloneDenseCalendarPlan(input.DenseCalendar)
	plan.CustomDenseCalendar = CloneCustomDenseCalendarPlan(input.CustomDenseCalendar)
	plan.SharedGrain = CloneSharedGrainResolution(input.SharedGrain)
	if input.Limit != nil {
		limit := *input.Limit
		plan.Limit = &limit
	}
	return &plan
}

func cloneBooleanPredicates(predicates []BooleanPredicate) []BooleanPredicate {
	if predicates == nil {
		return nil
	}
	out := make([]BooleanPredicate, len(predicates))
	for i, predicate := range predicates {
		out[i].Kind = predicate.Kind
		if predicate.Leaf != nil {
			leaf := clonePredicate(*predicate.Leaf)
			out[i].Leaf = &leaf
		}
		out[i].Children = cloneBooleanPredicates(predicate.Children)
	}
	return out
}

func clonePredicates(predicates []Predicate) []Predicate {
	if predicates == nil {
		return nil
	}
	out := make([]Predicate, len(predicates))
	for i := range predicates {
		out[i] = clonePredicate(predicates[i])
	}
	return out
}

func clonePredicate(predicate Predicate) Predicate {
	out := predicate
	out.Filter = cloneQueryFilter(predicate.Filter)
	return out
}

func cloneQueryFilter(filter query.Filter) query.Filter {
	out := filter
	if filter.Children != nil {
		out.Children = make([]query.Filter, len(filter.Children))
		for i := range filter.Children {
			out.Children[i] = cloneQueryFilter(filter.Children[i])
		}
	}
	return out
}

// CloneDenseCalendarPlan returns an independently owned dense-calendar plan.
func CloneDenseCalendarPlan(in *DenseCalendarPlan) *DenseCalendarPlan {
	if in == nil {
		return nil
	}
	out := *in
	if in.TimeField != nil {
		field := *in.TimeField
		out.TimeField = &field
	}
	out.OutputPredicates = clonePredicates(in.OutputPredicates)
	out.ReadPredicates = clonePredicates(in.ReadPredicates)
	return &out
}

// CloneCustomDenseCalendarPlan returns an independently owned custom dense-calendar plan.
func CloneCustomDenseCalendarPlan(in *CustomDenseCalendarPlan) *CustomDenseCalendarPlan {
	if in == nil {
		return nil
	}
	out := *in
	if in.BucketField != nil {
		field := *in.BucketField
		out.BucketField = &field
	}
	if in.OrdinalField != nil {
		field := *in.OrdinalField
		out.OrdinalField = &field
	}
	return &out
}

// CloneSharedGrainResolution returns an independently owned shared-grain resolution.
func CloneSharedGrainResolution(in *SharedGrainResolution) *SharedGrainResolution {
	if in == nil {
		return nil
	}
	out := *in
	out.Grain = cloneGroups(in.Grain)
	out.Metrics = make([]MetricSharedGrainEvidence, 0, len(in.Metrics))
	for _, evidence := range in.Metrics {
		out.Metrics = append(out.Metrics, CloneMetricSharedGrainEvidence(evidence))
	}
	return &out
}
