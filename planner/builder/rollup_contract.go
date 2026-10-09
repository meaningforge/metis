package builder

import (
	"fmt"
	"strings"

	"github.com/meaningforge/metis/expression"
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
//   - The selected expression is exactly the modeled aggregate call. A scalar
//     transform around an aggregate is not itself retained partial state.
//   - Every required partial component has a plan-owned source expression,
//     merge operator, and finalizer. Distributive aggregates retain one
//     component; AVG retains sum and count and finalizes their ratio.
func deriveRollupContract(metric string, resolved expression.ResolvedExpression, properties []expression.AggregationProperties) semanticplan.RollupContract {
	if len(properties) == 0 {
		return semanticplan.RollupContract{Reason: "metric expression declares no aggregation to roll up"}
	}
	if len(properties) > 1 {
		return semanticplan.RollupContract{Reason: "metric expression aggregates more than once, so it has no single partial state"}
	}
	property := properties[0]
	contract := semanticplan.RollupContract{Function: property.Function, Algebra: property.Rollup}
	call, ok := rootAggregateCall(resolved, property.Function)
	if !ok {
		contract.Reason = "evaluated metric is not exactly the aggregate partial state required for rollup"
		return contract
	}

	switch property.Rollup {
	case expression.RollupDistributive:
	case expression.RollupAlgebraic:
		return deriveAlgebraicRollupContract(metric, resolved, call, property, contract)
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
	contract.Components = []semanticplan.RollupComponent{{
		Name:       property.PartialState[0].Name,
		Column:     metric,
		Expression: resolved,
		Merge:      property.Merge,
	}}
	contract.Finalize = property.Finalize
	contract.Mergeable = true
	return contract
}

func deriveAlgebraicRollupContract(metric string, resolved expression.ResolvedExpression, call *expression.FunctionCallExpr, property expression.AggregationProperties, contract semanticplan.RollupContract) semanticplan.RollupContract {
	if !strings.EqualFold(property.Function, "AVG") || property.Finalize != expression.FinalizeRatio || len(property.PartialState) != 2 || property.Merge == "" {
		contract.Reason = "aggregation is algebraic, but its retained partial-state recipe is unsupported"
		return contract
	}
	if len(call.Args) != 1 {
		contract.Reason = "AVG rollup requires exactly one value expression"
		return contract
	}
	if distinct, ok := call.Args[0].(*expression.UnaryExpr); ok && strings.EqualFold(distinct.Op, "DISTINCT") {
		contract.Reason = "distinct aggregation is holistic and cannot be merged from bounded partial state"
		return contract
	}
	span := expression.SpanOf(call.Args[0])
	if !span.Valid() || span.Empty() || span.End > len(resolved.Source) {
		contract.Reason = "AVG argument source is unavailable for retained partial-state planning"
		return contract
	}
	argument := strings.TrimSpace(resolved.Source[span.Start:span.End])
	if argument == "" {
		contract.Reason = "AVG argument source is empty"
		return contract
	}
	for _, component := range property.PartialState {
		function := ""
		switch component.Name {
		case expression.PartialStateSum:
			function = "SUM"
		case expression.PartialStateCount:
			function = "COUNT"
		default:
			contract.Reason = "AVG declares an unsupported retained partial-state component"
			return contract
		}
		contract.Components = append(contract.Components, semanticplan.RollupComponent{
			Name:       component.Name,
			Column:     rollupStateColumn(metric, component.Name),
			Expression: expression.NewResolvedExpression(resolved.SourceDialect, fmt.Sprintf("%s(%s)", function, argument)),
			Merge:      property.Merge,
		})
	}
	contract.Finalize = property.Finalize
	contract.Mergeable = true
	return contract
}

func rootAggregateCall(resolved expression.ResolvedExpression, function string) (*expression.FunctionCallExpr, bool) {
	if resolved.Analysis == nil || resolved.Analysis.Bound.Expr == nil {
		return nil, false
	}
	call, ok := resolved.Analysis.Bound.Expr.(*expression.FunctionCallExpr)
	return call, ok && strings.EqualFold(call.Name, function)
}

func rollupStateColumn(metric, component string) string {
	var normalized strings.Builder
	for _, r := range metric {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' {
			normalized.WriteRune(r)
		} else {
			normalized.WriteByte('_')
		}
	}
	return "__metis_rollup_" + normalized.String() + "_" + component
}

// sourceRollupContract derives the contract from the exact dialect expression
// selected by Resolver. This prevents another dialect's algebra from governing
// the plan and gives retained components the same physical expression dialect.
func sourceRollupContract(metric string, resolved expression.ResolvedExpression) semanticplan.RollupContract {
	if resolved.Analysis == nil {
		return semanticplan.RollupContract{Reason: "metric has no analyzed expression"}
	}
	return deriveRollupContract(metric, resolved, resolved.AggregationPropertyValues())
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
			"define the rolled-up metric over a base with a supported retained aggregate state",
			"compute the metric at the required grain directly instead of rolling up a coarser aggregate",
		},
	}
}
