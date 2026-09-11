package semanticplan

// PlanShapeSummary is an internal structural observation of a validated
// SemanticPlan. It deliberately describes typed plan work rather than estimated
// database runtime cost. Callers must not treat it as semantic identity or an
// Agent-facing contract.
type PlanShapeSummary struct {
	Joins                    int
	Predicates               int
	Projections              int
	Groups                   int
	Sorts                    int
	EvaluationNodes          int
	SourceEvaluationNodes    int
	DerivedEvaluationNodes   int
	AdvancedEvaluationNodes  int
	SourceGroups             int
	SourceGroupMetrics       int
	EvaluationJoins          int
	PreAggregationPredicates int
	PostEvaluationPredicates int
	DenseCalendarDomains     int
	CustomCalendarDomains    int
}

// SummarizeSemanticPlan returns a deterministic, non-mutating structural
// summary directly from the canonical typed-node DAG.
func SummarizeSemanticPlan(plan *SemanticPlan) PlanShapeSummary {
	if plan == nil {
		return PlanShapeSummary{}
	}

	summary := PlanShapeSummary{
		Joins:       len(plan.Joins),
		Predicates:  len(plan.Predicates),
		Projections: len(plan.Projections),
		Groups:      len(plan.Groups),
		Sorts:       len(plan.Sorts),
	}
	if plan.DenseCalendar != nil {
		summary.DenseCalendarDomains++
	}
	if plan.CustomDenseCalendar != nil {
		summary.CustomCalendarDomains++
	}
	summary.CustomCalendarDomains += CustomCalendarDomainCount(plan.Nodes)

	summary.EvaluationNodes = len(plan.Nodes)
	summary.PostEvaluationPredicates = len(plan.Output.Predicates)
	sourceGroups := map[string]map[string]struct{}{}
	for _, node := range plan.Nodes {
		base := node.NodeBase()
		if source, ok := NodeSourceState(node); ok {
			summary.EvaluationJoins += len(source.Joins)
		}
		for _, predicate := range base.Predicates {
			if predicate.Scope == SemanticPredicatePreAggregation {
				summary.PreAggregationPredicates++
			}
		}
		if node.Kind() == SemanticPlanNodeSourceAggregate {
			summary.SourceEvaluationNodes++
			metricState, ok := NodeMetricState(node)
			if ok && metricState.ShareGroup != "" {
				metrics := metricState.Metrics
				if len(metrics) == 0 {
					metrics = []string{base.ID}
				}
				set := sourceGroups[metricState.ShareGroup]
				if set == nil {
					set = map[string]struct{}{}
					sourceGroups[metricState.ShareGroup] = set
				}
				for _, metric := range metrics {
					set[metric] = struct{}{}
				}
			}
		} else {
			summary.DerivedEvaluationNodes++
		}
		if isAdvancedEvaluationKind(node.Kind()) {
			summary.AdvancedEvaluationNodes++
		}
	}
	summary.SourceGroups = len(sourceGroups)
	for _, metrics := range sourceGroups {
		summary.SourceGroupMetrics += len(metrics)
	}
	return summary
}

func isAdvancedEvaluationKind(kind SemanticPlanNodeKind) bool {
	switch kind {
	case SemanticPlanNodeCumulativeWindow,
		SemanticPlanNodeTimeOffset,
		SemanticPlanNodeOffsetToGrain,
		SemanticPlanNodeConversion,
		SemanticPlanNodeSemiAdditiveLast,
		SemanticPlanNodeSemiAdditiveFirst:
		return true
	default:
		return false
	}
}
