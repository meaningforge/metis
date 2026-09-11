package conversion

import (
	"fmt"
	"strings"

	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/serrors"
)

const (
	conversionBasePopulationCTE = "__metis_conversion_base_population"
	conversionCandidatesCTE     = "__metis_conversion_candidates"
	conversionAssignedCTE       = "__metis_conversion_assigned"
	conversionBaseValueColumn   = "__metis_base_value"
	conversionEventValueColumn  = "__metis_conversion_value"
	conversionAssignedColumn    = "__metis_assigned_value"
	conversionRankColumn        = "__metis_conversion_rank"
	metricFillAlignedInputsCTE  = "__metis_fill_aligned_inputs"
	customDenseOrdinalColumn    = "__metis_dense_ordinal"
	customCalendarPeriodsCTE    = "__metis_calendar_periods"
	customCalendarSource        = "__metis_source_period"
	customCalendarTarget        = "__metis_target_period"
	customCalendarBucket        = "__metis_bucket"
	customCalendarOrdinal       = "__metis_ordinal"
)

func semanticPlanNodeInputIDs(inputs []semanticplan.SemanticPlanNodeInput) []string {
	out := make([]string, 0, len(inputs))
	for _, input := range inputs {
		out = append(out, input.NodeID)
	}
	return out
}

type semanticSourceGroup struct {
	Name    string
	Metrics []string
}

func semanticNodeSourceGroups(nodes []semanticplan.SemanticPlanNode) []semanticSourceGroup {
	indexes := make(map[string]int)
	var out []semanticSourceGroup
	for _, node := range nodes {
		if node.Kind() != semanticplan.SemanticPlanNodeSourceAggregate {
			continue
		}
		state, ok := semanticplan.NodeMetricState(node)
		if !ok || state.ShareGroup == "" {
			continue
		}
		index, ok := indexes[state.ShareGroup]
		if !ok {
			indexes[state.ShareGroup] = len(out)
			out = append(out, semanticSourceGroup{Name: state.ShareGroup})
			index = len(out) - 1
		}
		metrics := state.Metrics
		if len(metrics) == 0 {
			metrics = []string{node.NodeBase().ID}
		}
		for _, metric := range metrics {
			if !containsString(out[index].Metrics, metric) {
				out[index].Metrics = append(out[index].Metrics, metric)
			}
		}
	}
	return out
}

func sourceGroup(groups []semanticSourceGroup, name string) *semanticSourceGroup {
	for i := range groups {
		if groups[i].Name == name {
			return &groups[i]
		}
	}
	return nil
}

func cumulativeMergeOperator(node semanticplan.SemanticPlanNode, base string, nodesByID map[string]semanticplan.SemanticPlanNode) (string, error) {
	baseNode, ok := nodesByID[base]
	if !ok {
		return "", metricLoweringError("cumulative base metric has no evaluation node", base)
	}
	source, ok := baseNode.(semanticplan.SourceAggregateNode)
	if !ok {
		return "", requireMergeableRollup(semanticplan.RollupContract{Reason: "base metric is not a source aggregation, so it retains no partial state to merge"}, node.NodeBase().ID, base)
	}
	contract := source.Rollup
	if err := requireMergeableRollup(contract, node.NodeBase().ID, base); err != nil {
		return "", err
	}
	return contract.Merge, nil
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func uniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func evaluationVisibleMetrics(plan *semanticplan.SemanticPlan) []string {
	seen := map[string]struct{}{}
	var out []string
	appendMetric := func(name string) {
		if name == "" {
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	for _, projection := range plan.Projections {
		if projection.Kind == semanticplan.ProjectionMetric {
			appendMetric(projection.Name)
		}
	}
	for _, sort := range plan.Sorts {
		if sort.Kind == semanticplan.SortMetric {
			appendMetric(sort.Name)
		}
	}
	return out
}

func semanticNodeOutputMetrics(plan *semanticplan.SemanticPlan, nodes []semanticplan.SemanticPlanNode) []string {
	out := evaluationVisibleMetrics(plan)
	seen := make(map[string]struct{}, len(out))
	for _, metric := range out {
		seen[metric] = struct{}{}
	}
	nodeIDs := make(map[string]struct{}, len(nodes))
	for _, node := range nodes {
		nodeIDs[node.NodeBase().ID] = struct{}{}
	}
	for _, predicate := range plan.Output.Predicates {
		if _, ok := nodeIDs[predicate.Name]; !ok {
			continue
		}
		if _, ok := seen[predicate.Name]; ok {
			continue
		}
		seen[predicate.Name] = struct{}{}
		out = append(out, predicate.Name)
	}
	return out
}

func metricCTEName(index int, metric string) string {
	var normalized strings.Builder
	for _, r := range metric {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' {
			normalized.WriteRune(r)
		} else {
			normalized.WriteByte('_')
		}
	}
	return fmt.Sprintf("metric_%03d_%s", index+1, normalized.String())
}

func metricLoweringError(message, metric string) error {
	return serrors.Internal(message, map[string]any{"metric": metric})
}

func queryGrainLoweringError(message, metric string) error {
	return &serrors.Error{Code: serrors.ErrIncompatibleQueryGrain, Message: message, Details: map[string]any{"metric": metric}}
}

func metricUsesZeroFill(metric *ossie.Metric) (bool, error) {
	spec, ok, err := ossie.FillSpec(metric)
	if err != nil || !ok {
		return false, err
	}
	return spec.Policy == ossie.MetricFillZero, nil
}

func metricFillPlanningError(metric string, cause error) error {
	return &serrors.Error{Code: serrors.ErrInvalidMetricExtension, Message: "invalid metric fill policy", Details: map[string]any{"metric": metric, "cause": cause.Error()}}
}

func postEvaluationPredicates(plan *semanticplan.SemanticPlan) []semanticplan.PostEvaluationPredicate {
	if plan == nil || len(plan.Nodes) == 0 {
		return nil
	}
	return append([]semanticplan.PostEvaluationPredicate(nil), plan.Output.Predicates...)
}

func semanticMetricRelations(plan *semanticplan.SemanticPlan) map[string]string {
	if plan == nil || len(plan.Nodes) == 0 {
		return nil
	}
	relations := make(map[string]string, len(plan.Nodes))
	for i, node := range plan.Nodes {
		base := node.NodeBase()
		relation := metricCTEName(i, base.ID)
		metricState, hasMetrics := semanticplan.NodeMetricState(node)
		if node.Kind() == semanticplan.SemanticPlanNodeSourceAggregate && hasMetrics && metricState.ShareGroup != "" {
			relation = metricState.ShareGroup
		}
		relations[base.ID] = relation
		if !hasMetrics {
			continue
		}
		for _, metric := range metricState.Metrics {
			if metric != "" {
				relations[metric] = relation
			}
		}
	}
	return relations
}
