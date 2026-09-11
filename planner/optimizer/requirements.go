package optimizer

import (
	"fmt"

	"github.com/meaningforge/metis/planner/semanticplan"
)

// Requirements is the dependency closure that proves which semantic nodes and
// datasets remain observable from the requested output contract.
type Requirements struct {
	metricNodes map[string]struct{}
	datasets    map[string]struct{}
}

// RequiresNode reports whether a semantic DAG node participates in the output
// dependency closure.
func (requirements Requirements) RequiresNode(id string) bool {
	_, ok := requirements.metricNodes[id]
	return ok
}

// RequiresDataset reports whether a dataset participates in the output
// dependency closure.
func (requirements Requirements) RequiresDataset(name string) bool {
	_, ok := requirements.datasets[name]
	return ok
}

// CollectRequirements derives dependency closure strictly from the owned
// SemanticPlan DAG and its query-wide output contract.
func CollectRequirements(plan *semanticplan.SemanticPlan) (Requirements, error) {
	requirements := Requirements{
		metricNodes: map[string]struct{}{},
		datasets:    map[string]struct{}{},
	}
	hasConsumers := len(plan.Projections) > 0 || len(plan.Predicates) > 0 || len(plan.Groups) > 0 || len(plan.Sorts) > 0
	if plan.Root.Name != "" {
		requirements.datasets[plan.Root.Name] = struct{}{}
	}
	for _, projection := range plan.Projections {
		if projection.Dataset != "" {
			requirements.datasets[projection.Dataset] = struct{}{}
		}
		for _, dataset := range projection.Datasets {
			if dataset != "" {
				requirements.datasets[dataset] = struct{}{}
			}
		}
	}
	for _, predicate := range plan.Predicates {
		if predicate.Dataset != "" {
			requirements.datasets[predicate.Dataset] = struct{}{}
		}
	}
	for _, group := range plan.Groups {
		if group.Dataset != "" {
			requirements.datasets[group.Dataset] = struct{}{}
		}
	}
	for _, sort := range plan.Sorts {
		if sort.Dataset != "" {
			requirements.datasets[sort.Dataset] = struct{}{}
		}
	}
	if plan.DenseCalendar != nil && plan.DenseCalendar.Dataset.Name != "" {
		requirements.datasets[plan.DenseCalendar.Dataset.Name] = struct{}{}
		hasConsumers = true
	}
	if plan.CustomDenseCalendar != nil && plan.CustomDenseCalendar.Dataset.Name != "" {
		requirements.datasets[plan.CustomDenseCalendar.Dataset.Name] = struct{}{}
		hasConsumers = true
	}
	for _, dataset := range semanticplan.CustomCalendarDomainDatasets(plan.Nodes) {
		requirements.datasets[dataset] = struct{}{}
		hasConsumers = true
	}

	if semanticplan.RequiresComposedPlan(plan) {
		if len(plan.Requested) > 0 || len(plan.Output.Predicates) > 0 {
			hasConsumers = true
		}
		if err := collectPlanNodeRequirements(plan, &requirements); err != nil {
			return Requirements{}, err
		}
	}
	if !hasConsumers {
		retainJoinDatasets(requirements.datasets, plan.Joins)
	}
	return requirements, nil
}

func collectPlanNodeRequirements(plan *semanticplan.SemanticPlan, requirements *Requirements) error {
	byID := semanticplan.NodesByID(plan.Nodes)
	metricOwners := make(map[string]string)
	for _, node := range plan.Nodes {
		if node == nil {
			continue
		}
		if state, ok := semanticplan.NodeMetricState(node); ok {
			for _, metric := range state.Metrics {
				if metric != "" {
					metricOwners[metric] = node.NodeBase().ID
				}
			}
		}
	}
	var visit func(string) error
	visit = func(id string) error {
		if requirements.RequiresNode(id) {
			return nil
		}
		node, ok := byID[id]
		if !ok {
			return fmt.Errorf("required semantic node %q is not defined", id)
		}
		requirements.metricNodes[id] = struct{}{}
		if source, ok := semanticplan.NodeSourceState(node); ok {
			for _, dataset := range source.RequiredDatasets {
				if dataset != "" {
					requirements.datasets[dataset] = struct{}{}
				}
			}
		}
		for _, input := range node.NodeBase().Inputs {
			if err := visit(input.NodeID); err != nil {
				return err
			}
		}
		return nil
	}
	visitOutput := func(name string) error {
		if owner, ok := metricOwners[name]; ok {
			return visit(owner)
		}
		return visit(name)
	}

	seeded := false
	for _, requested := range plan.Requested {
		if requested != "" {
			seeded = true
			if err := visitOutput(requested); err != nil {
				return err
			}
		}
	}
	for _, projection := range plan.Projections {
		if projection.Kind == semanticplan.ProjectionMetric {
			seeded = true
			if err := visitOutput(projection.Name); err != nil {
				return err
			}
		}
	}
	for _, sort := range plan.Sorts {
		if sort.Kind == semanticplan.SortMetric {
			seeded = true
			if err := visitOutput(sort.Name); err != nil {
				return err
			}
		}
	}
	for _, predicate := range plan.Output.Predicates {
		if predicate.Name == "" {
			continue
		}
		if _, nodeExists := byID[predicate.Name]; !nodeExists {
			if _, outputExists := metricOwners[predicate.Name]; !outputExists {
				continue
			}
		}
		seeded = true
		if err := visitOutput(predicate.Name); err != nil {
			return err
		}
	}
	if !seeded {
		for _, node := range plan.Nodes {
			if node != nil {
				if err := visit(node.NodeBase().ID); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func retainJoinDatasets(datasets map[string]struct{}, joins []semanticplan.Join) {
	for _, join := range joins {
		if join.FromDataset != "" {
			datasets[join.FromDataset] = struct{}{}
		}
		if join.ToDataset != "" {
			datasets[join.ToDataset] = struct{}{}
		}
	}
}
