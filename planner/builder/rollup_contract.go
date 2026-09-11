package builder

import (
	"github.com/meaningforge/metis/expression"
	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/serrors"
)

// deriveRollupContract decides mergeability from a source metric's derived
// aggregation properties.
//
// Three conditions must all hold, and each rules out a distinct way of being
// silently wrong:
//
//   - Exactly one aggregation. A metric whose expression aggregates twice, or
//     not at all, has no single value for a rollup to merge.
//   - Distributive algebra. Algebraic and holistic aggregations cannot be
//     computed from partial results at all in the holistic case, or not from
//     the partials that survive, in the algebraic one.
//   - The emitted column *is* the partial state: one component, finalized by
//     identity. This is the "retained partial state" requirement. AVG fails it
//     even though it is algebraic, because the CTE emits the finished ratio and
//     not the (sum, count) pair a merge would need.
func deriveRollupContract(properties []expression.AggregationProperties) semanticplan.RollupContract {
	if len(properties) == 0 {
		return semanticplan.RollupContract{Reason: "metric expression declares no aggregation to roll up"}
	}
	if len(properties) > 1 {
		return semanticplan.RollupContract{Reason: "metric expression aggregates more than once, so it has no single partial state"}
	}
	property := properties[0]
	contract := semanticplan.RollupContract{Function: property.Function, Algebra: property.Rollup}

	switch property.Rollup {
	case expression.RollupDistributive:
	case expression.RollupAlgebraic:
		contract.Reason = "aggregation is algebraic, and the evaluated column retains its result rather than the partial state a merge would need"
		return contract
	case expression.RollupHolistic:
		contract.Reason = "aggregation is holistic and cannot be computed from partial results at any grain"
		return contract
	default:
		contract.Reason = "aggregation algebra is not derivable, so merging it cannot be proven correct"
		return contract
	}

	if len(property.PartialState) != 1 || property.Finalize != expression.FinalizeIdentity {
		contract.Reason = "aggregation requires partial state the evaluated column does not retain"
		return contract
	}
	if property.Merge == "" {
		contract.Reason = "aggregation declares no merge operator"
		return contract
	}
	contract.Merge = property.Merge
	contract.Mergeable = true
	return contract
}

// sourceRollupContract derives the contract for one source metric from the
// analyzed expression of the dialect the resolver actually selected.
//
// Selecting by dialect matters: a metric may declare an ANSI COUNT(DISTINCT x)
// alongside a target-native uniqExact. Those have different derived algebra, and
// the one that governs is the one being compiled.
func sourceRollupContract(model *manifest.ModelIndex, metric, dialect string) semanticplan.RollupContract {
	analysis, ok := model.MetricAnalysis(metric)
	if !ok {
		return semanticplan.RollupContract{Reason: "metric has no analyzed expression"}
	}
	for _, analyzed := range analysis.Expressions {
		if analyzed.Dialect == dialect {
			return deriveRollupContract(analyzed.AggregationProperties)
		}
	}
	return semanticplan.RollupContract{Reason: "metric declares no expression for the selected dialect"}
}

// requireMergeableRollup fails closed when a rollup site cannot prove its merge.
//
// The condition belongs to the model: a cumulative or otherwise rolled-up metric
// was declared over a base whose aggregation cannot be merged. No query avoids
// it and no target changes it, so only the model author can act.
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
	return &serrors.Error{
		Code:    serrors.ErrInvalidMetricRollup,
		Message: "metric rolls up a base metric whose aggregation cannot be merged",
		Details: details,
		Suggestions: []string{
			"define the rolled-up metric over a base that aggregates with SUM, COUNT, MIN, or MAX",
			"compute the metric at the required grain directly instead of rolling up a coarser aggregate",
		},
	}
}
