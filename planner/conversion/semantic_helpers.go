package conversion

import (
	"strings"

	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/serrors"
)

func nodePlacedPredicates(node semanticplan.SemanticPlanNode) []semanticplan.Predicate {
	out := make([]semanticplan.Predicate, 0, len(node.NodeBase().Predicates))
	for _, predicate := range node.NodeBase().Predicates {
		if predicate.Predicate != nil {
			out = append(out, *predicate.Predicate)
		}
	}
	return out
}
func semanticPlanNodePreAggregationPredicates(node semanticplan.SemanticPlanNode) []semanticplan.Predicate {
	return nodePlacedPredicates(node)
}
func cloneGroups(groups []semanticplan.GroupBy) []semanticplan.GroupBy {
	return append([]semanticplan.GroupBy(nil), groups...)
}
func conversionDependenciesMatch(dependencies []string, spec ossie.ConversionMetricSpec) bool {
	if len(dependencies) != 2 {
		return false
	}
	seen := map[string]int{}
	for _, dependency := range dependencies {
		seen[dependency]++
	}
	return seen[spec.BaseMetric] == 1 && seen[spec.ConversionMetric] == 1
}
func semiAdditivePlanningError(metric, message string) error {
	return &serrors.Error{Code: serrors.ErrInvalidMetricExtension, Message: message, Details: map[string]any{"metric": metric}}
}
func requireMergeableRollup(contract semanticplan.RollupContract, metric, base string) error {
	if contract.Mergeable {
		return nil
	}
	details := map[string]any{"metric": metric, "base_metric": base, "cause": contract.Reason}
	if contract.Function != "" {
		details["base_aggregation"] = contract.Function
	}
	if contract.Algebra != "" {
		details["rollup_algebra"] = string(contract.Algebra)
	}
	return &serrors.Error{Code: serrors.ErrInvalidMetricRollup, Message: "metric rolls up a base metric whose aggregation cannot be merged", Details: details}
}
func containsGroup(groups []semanticplan.GroupBy, name string) bool {
	for _, group := range groups {
		if group.Name == name {
			return true
		}
	}
	return false
}
func queriedSemiAdditiveTimeWindow(groups []semanticplan.GroupBy, dimension string) bool {
	for _, group := range groups {
		if group.CustomCalendar != nil && group.CustomCalendar.BaseTimeField != nil {
			base := group.CustomCalendar.BaseTimeField.Name
			if base == dimension || group.Name == dimension || unqualifiedName(group.Name) == dimension {
				return true
			}
		}
		if group.Grain != nil && group.Field != nil && (group.Field.Name == dimension || group.Name == dimension || unqualifiedName(group.Name) == dimension) {
			return true
		}
	}
	return false
}
func semiAdditiveRawOrderName(dimension string) string {
	return "__metis_ordered_" + strings.ReplaceAll(strings.TrimSpace(dimension), ".", "_")
}
func unqualifiedName(name string) string {
	if index := strings.LastIndex(name, "."); index >= 0 {
		return name[index+1:]
	}
	return name
}
