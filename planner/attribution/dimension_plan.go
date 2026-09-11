package attribution

import (
	"fmt"

	"github.com/meaningforge/metis/planner/semanticplan"
)

// BuildDimensionPlan converts one ordinarily resolved and planned
// metric+dimension+time-dimension query into the independent attribution DAG
// for that decomposition dimension. The ordinary plan is used only as settled
// source, relationship, expression, and predicate evidence; no manifest lookup
// or semantic re-resolution occurs here.
func BuildDimensionPlan(request ResolvedMetricAttributionRequest, proof MetricAttributionPlan, dimension string, ordinary *semanticplan.SemanticPlan) (*semanticplan.SemanticPlan, error) {
	if err := ValidateResolvedMetricAttributionRequest(request); err != nil {
		return nil, err
	}
	if err := ValidateMetricAttributionPlan(proof); err != nil {
		return nil, err
	}
	if proof.Exactness != MetricAttributionExact {
		return nil, fmt.Errorf("metric %q does not have an exact attribution proof", proof.Metric)
	}
	if ordinary == nil {
		return nil, fmt.Errorf("metric attribution dimension %q requires a planned semantic query", dimension)
	}
	if err := semanticplan.ValidateSemanticPlan(ordinary); err != nil {
		return nil, fmt.Errorf("metric attribution source plan is invalid: %w", err)
	}
	dimensionGroup, ok := groupByName(ordinary.Groups, dimension)
	if !ok {
		return nil, fmt.Errorf("metric attribution source plan lacks decomposition dimension %q", dimension)
	}
	timeGroup, ok := groupByName(ordinary.Groups, request.TimeDimensionRef)
	if !ok {
		return nil, fmt.Errorf("metric attribution source plan lacks time dimension %q", request.TimeDimensionRef)
	}

	components := make([]semanticplan.SourceAggregateNode, 0, len(proof.Components))
	for _, component := range proof.Components {
		source, found := sourceAggregateByMetric(ordinary, component.Metric)
		if !found {
			return nil, fmt.Errorf("metric attribution component %q is not a governed source aggregate", component.Metric)
		}
		source.Base.Dimensions = []string{dimension}
		source.Base.OutputGrain = []semanticplan.GroupBy{dimensionGroup}
		source.MetricState.ShareGroup = ""
		source.MetricState.SharedGrainEvidence = nil
		components = append(components, source)
	}

	var attributionNode semanticplan.SemanticPlanNode
	switch proof.Strategy {
	case semanticplan.MetricAttributionAdditiveContribution:
		node, err := BuildAdditiveAttributionNode(
			request.MetricRef+"__attribution__"+dimension,
			request,
			proof,
			semanticplan.SemanticPlanNodeInput{NodeID: components[0].Base.ID, Grain: []semanticplan.GroupBy{dimensionGroup}},
			dimensionGroup,
			timeGroup,
		)
		if err != nil {
			return nil, err
		}
		attributionNode = node
	case semanticplan.MetricAttributionRatioMixRate:
		node, err := BuildRatioAttributionNode(
			request.MetricRef+"__attribution__"+dimension,
			request,
			proof,
			semanticplan.SemanticPlanNodeInput{NodeID: components[0].Base.ID, Grain: []semanticplan.GroupBy{dimensionGroup}},
			semanticplan.SemanticPlanNodeInput{NodeID: components[1].Base.ID, Grain: []semanticplan.GroupBy{dimensionGroup}},
			dimensionGroup,
			timeGroup,
		)
		if err != nil {
			return nil, err
		}
		attributionNode = node
	default:
		return nil, fmt.Errorf("unsupported metric attribution strategy %q", proof.Strategy)
	}

	nodes := make([]semanticplan.SemanticPlanNode, 0, len(components)+1)
	for _, component := range components {
		nodes = append(nodes, component)
	}
	nodes = append(nodes, attributionNode)
	plan := &semanticplan.SemanticPlan{
		PolicyScope: ordinary.PolicyScope,
		Model:       ordinary.Model,
		Root:        ordinary.Root,
		Predicates:  append([]semanticplan.Predicate(nil), ordinary.Predicates...),
		Groups:      []semanticplan.GroupBy{dimensionGroup},
		Requested:   []string{request.MetricRef},
		Nodes:       nodes,
		Output:      semanticplan.SemanticOutputContract{Grain: []semanticplan.GroupBy{dimensionGroup}},
	}
	if err := semanticplan.ValidateSemanticPlan(plan); err != nil {
		return nil, fmt.Errorf("metric attribution dimension plan is invalid: %w", err)
	}
	return plan, nil
}

func groupByName(groups []semanticplan.GroupBy, name string) (semanticplan.GroupBy, bool) {
	for _, group := range groups {
		if group.Name == name {
			return group, true
		}
	}
	return semanticplan.GroupBy{}, false
}

func sourceAggregateByMetric(plan *semanticplan.SemanticPlan, metric string) (semanticplan.SourceAggregateNode, bool) {
	for _, node := range plan.Nodes {
		source, ok := node.(semanticplan.SourceAggregateNode)
		if ok && source.Base.ID == metric {
			return source, true
		}
	}
	return semanticplan.SourceAggregateNode{}, false
}
