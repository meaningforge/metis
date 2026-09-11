package builder

import (
	"errors"
	"sort"

	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/serrors"
)

// resolvePlanSharedGrain derives composed shared-grain feasibility directly from
// plan-owned typed nodes.
func resolvePlanSharedGrain(plan *semanticplan.SemanticPlan) (*semanticplan.SharedGrainResolution, error) {
	if plan == nil || len(plan.Requested) == 0 {
		return nil, nil
	}

	byID := semanticplan.NodesByID(plan.Nodes)
	candidates := make([]semanticplan.SharedGrainCandidate, 0, len(plan.Requested))
	for _, name := range plan.Requested {
		node, ok := byID[name]
		if !ok {
			return nil, mapSharedGrainPlanningError(&semanticplan.SharedGrainError{
				Failure: semanticplan.SharedGrainInvalidCandidate,
				Metric:  name,
				Details: map[string]any{"cause": "requested metric is missing from semantic plan"},
			})
		}
		source, ok := semanticplan.NodeSourceState(node)
		if !ok {
			return nil, mapSharedGrainPlanningError(&semanticplan.SharedGrainError{
				Failure: semanticplan.SharedGrainInvalidCandidate,
				Metric:  name,
				Details: map[string]any{"cause": "requested metric has no semantic source state"},
			})
		}
		roots := canonicalStrings(source.SourceRoots)
		paths := nodeRelationshipPaths(name, byID)
		candidate := semanticplan.SharedGrainCandidate{
			Metric:            name,
			OutputGrain:       cloneGroups(plan.Output.Grain),
			RootDatasets:      roots,
			RelationshipPaths: paths,
			FanoutSafe:        true,
		}
		if len(roots) > 0 {
			candidate.RootDataset = roots[0]
		}
		if len(paths) == 1 {
			candidate.RelationshipPath = append([]string(nil), paths[0]...)
		}
		candidates = append(candidates, candidate)
	}

	resolution, err := ResolveSharedGrain(candidates)
	if err != nil {
		return nil, mapSharedGrainPlanningError(err)
	}
	return &resolution, nil
}

func nodeRelationshipPaths(name string, byID map[string]semanticplan.SemanticPlanNode) [][]string {
	seen := map[string]struct{}{}
	var paths [][]string
	var visit func(string)
	visit = func(current string) {
		node, ok := byID[current]
		if !ok || node == nil {
			return
		}
		base := node.NodeBase()
		if len(base.Inputs) == 0 {
			source, ok := semanticplan.NodeSourceState(node)
			if !ok {
				return
			}
			path := make([]string, 0, len(source.Joins))
			for _, join := range source.Joins {
				path = append(path, relationshipKey(join.Relationship))
			}
			key := stringPathKey(path)
			if _, exists := seen[key]; !exists {
				seen[key] = struct{}{}
				paths = append(paths, path)
			}
			return
		}
		for _, input := range base.Inputs {
			visit(input.NodeID)
		}
	}
	visit(name)
	sort.Slice(paths, func(i, j int) bool { return stringPathKey(paths[i]) < stringPathKey(paths[j]) })
	return cloneStringPaths(paths)
}

func applyPlanSharedGrainEvidence(plan *semanticplan.SemanticPlan, resolution *semanticplan.SharedGrainResolution) error {
	if plan == nil || resolution == nil {
		return nil
	}
	byMetric := make(map[string]semanticplan.MetricSharedGrainEvidence, len(resolution.Metrics))
	for _, evidence := range resolution.Metrics {
		byMetric[evidence.Metric] = semanticplan.CloneMetricSharedGrainEvidence(evidence)
	}
	for i, node := range plan.Nodes {
		if node == nil {
			continue
		}
		evidence, ok := byMetric[node.NodeBase().ID]
		if !ok {
			continue
		}
		state, ok := semanticplan.NodeMetricState(node)
		if !ok {
			continue
		}
		owned := evidence
		state.SharedGrainEvidence = &owned
		updated, err := semanticplan.WithNodeMetricState(node, state)
		if err != nil {
			return err
		}
		plan.Nodes[i] = updated
	}
	return nil
}

func mapSharedGrainPlanningError(err error) error {
	var sharedErr *semanticplan.SharedGrainError
	if !errors.As(err, &sharedErr) {
		return err
	}
	details := make(map[string]any, len(sharedErr.Details)+2)
	for key, value := range sharedErr.Details {
		details[key] = value
	}
	details["failure"] = sharedErr.Failure
	if sharedErr.Metric != "" {
		details["metric"] = sharedErr.Metric
	}
	return &serrors.Error{
		Code:    serrors.ErrIncompatibleQueryGrain,
		Message: "multi-metric shared-grain feasibility failed",
		Details: details,
	}
}
