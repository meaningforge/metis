package attribution

import (
	"fmt"
	"strings"

	"github.com/meaningforge/metis/planner/semanticplan"
)

// BuildRatioAttributionNode projects an already-proven ratio attribution plan
// into the typed SemanticPlan DAG. It does not inspect manifest models, reparse
// metric expressions, or construct physical SQL.
func BuildRatioAttributionNode(
	nodeID string,
	request ResolvedMetricAttributionRequest,
	attribution MetricAttributionPlan,
	numeratorInput semanticplan.SemanticPlanNodeInput,
	denominatorInput semanticplan.SemanticPlanNodeInput,
	output semanticplan.GroupBy,
	timeDimension semanticplan.GroupBy,
) (semanticplan.RatioAttributionNode, error) {
	if err := ValidateResolvedMetricAttributionRequest(request); err != nil {
		return semanticplan.RatioAttributionNode{}, err
	}
	if err := ValidateMetricAttributionPlan(attribution); err != nil {
		return semanticplan.RatioAttributionNode{}, err
	}
	if strings.TrimSpace(nodeID) == "" {
		return semanticplan.RatioAttributionNode{}, fmt.Errorf("ratio attribution node requires an id")
	}
	if attribution.Metric != request.MetricRef || attribution.Exactness != MetricAttributionExact || attribution.Strategy != semanticplan.MetricAttributionRatioMixRate {
		return semanticplan.RatioAttributionNode{}, fmt.Errorf("ratio attribution node requires an exact ratio attribution plan for metric %q", request.MetricRef)
	}
	if len(attribution.Components) != 2 || attribution.Components[0].Role != MetricAttributionNumerator || attribution.Components[1].Role != MetricAttributionDenominator {
		return semanticplan.RatioAttributionNode{}, fmt.Errorf("ratio attribution plan does not identify ordered numerator and denominator components")
	}
	if attribution.Reconciliation != semanticplan.MetricAttributionReconcileMixRate {
		return semanticplan.RatioAttributionNode{}, fmt.Errorf("ratio attribution plan has incompatible reconciliation %q", attribution.Reconciliation)
	}
	numerator := attribution.Components[0].Metric
	denominator := attribution.Components[1].Metric
	if numeratorInput.NodeID != numerator || denominatorInput.NodeID != denominator {
		return semanticplan.RatioAttributionNode{}, fmt.Errorf("ratio attribution inputs must match ordered numerator %q and denominator %q", numerator, denominator)
	}
	dimension := output.Name
	if !containsMetricAttributionDimension(request.Dimensions, dimension) {
		return semanticplan.RatioAttributionNode{}, fmt.Errorf("ratio attribution output dimension %q is not present in the resolved request", dimension)
	}
	if !semanticplan.SameGrain(numeratorInput.Grain, []semanticplan.GroupBy{output}) || !semanticplan.SameGrain(denominatorInput.Grain, []semanticplan.GroupBy{output}) {
		return semanticplan.RatioAttributionNode{}, fmt.Errorf("ratio attribution numerator and denominator input grains must match the decomposition grain")
	}
	if timeDimension.Name != request.TimeDimensionRef {
		return semanticplan.RatioAttributionNode{}, fmt.Errorf("ratio attribution time dimension %q does not match resolved time dimension %q", timeDimension.Name, request.TimeDimensionRef)
	}
	if strings.TrimSpace(timeDimension.Dataset) == "" || !timeDimension.Expression.IsResolved() {
		return semanticplan.RatioAttributionNode{}, fmt.Errorf("ratio attribution time dimension %q requires resolved dataset and expression evidence", request.TimeDimensionRef)
	}

	node := semanticplan.RatioAttributionNode{
		Base: semanticplan.SemanticPlanNodeBase{
			ID:                        nodeID,
			Boundary:                  semanticplan.SemanticPlanNodeBoundarySemanticIsolation,
			PredicateBoundaryEvidence: semanticplan.PredicateBoundaryEvidence(semanticplan.SemanticPlanNodeBoundarySemanticIsolation),
			Inputs: []semanticplan.SemanticPlanNodeInput{
				{NodeID: numeratorInput.NodeID, Grain: append([]semanticplan.GroupBy(nil), numeratorInput.Grain...)},
				{NodeID: denominatorInput.NodeID, Grain: append([]semanticplan.GroupBy(nil), denominatorInput.Grain...)},
			},
			Dimensions:  []string{dimension},
			OutputGrain: []semanticplan.GroupBy{output},
		},
		MetricState:      semanticplan.SemanticMetricState{Metrics: []string{request.MetricRef}},
		MetricRef:        request.MetricRef,
		NumeratorRef:     numerator,
		DenominatorRef:   denominator,
		TimeDimensionRef: request.TimeDimensionRef,
		TimeDimension:    timeDimension,
		DimensionRef:     dimension,
		Baseline:         request.Baseline,
		Current:          request.Current,
	}
	if err := semanticplan.ValidateNode(node); err != nil {
		return semanticplan.RatioAttributionNode{}, err
	}
	return node, nil
}

func ratioAttributionDecompositionKind(semanticplan.RatioAttributionNode) string { return "ratio" }

func ratioAttributionStrategy(semanticplan.RatioAttributionNode) semanticplan.MetricAttributionStrategy {
	return semanticplan.MetricAttributionRatioMixRate
}

func ratioAttributionReconciliation(semanticplan.RatioAttributionNode) semanticplan.MetricAttributionReconciliation {
	return semanticplan.MetricAttributionReconcileMixRate
}

func ratioAttributionUndefinedRatioPolicy(semanticplan.RatioAttributionNode) semanticplan.RatioAttributionUndefinedRatioPolicy {
	return semanticplan.RatioAttributionUndefinedNullWithDefinedFlag
}

func ratioAttributionPopulationAlignment(semanticplan.RatioAttributionNode) semanticplan.RatioAttributionPopulationAlignment {
	return semanticplan.RatioAttributionFullUnionEntryExit
}
