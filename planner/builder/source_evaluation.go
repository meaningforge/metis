package builder

import (
	"errors"
	"fmt"
	"sort"

	"github.com/meaningforge/metis/expression"
	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/serrors"
)

// SourceRequirement is the source-aware evidence needed to construct one
// source metric. Metric dependency edges intentionally remain in evaluation.
type SourceRequirement struct {
	Root     string
	Datasets []string
}

// BuildMetricSourceRequirement determines the unique directed source root for
// a source metric from already-resolved manifest dependency evidence.
func BuildMetricSourceRequirement(model *manifest.ModelIndex, metricName string, dependency manifest.MetricDependency) (SourceRequirement, error) {
	datasets := append([]string(nil), dependency.DirectDatasets...)
	if len(datasets) == 0 {
		datasets = append([]string(nil), dependency.Datasets...)
	}
	if len(datasets) == 0 {
		return SourceRequirement{}, &serrors.Error{Code: serrors.ErrInternalInvariant, Message: "source metric has no dataset", Details: map[string]any{"metric": metricName}}
	}

	root, err := resolveMetricSourceRoot(model, metricName, datasets)
	if err != nil {
		return SourceRequirement{}, &serrors.Error{
			Code:    serrors.ErrUnresolvedMetricSource,
			Message: "source metric root cannot be resolved to exactly one dataset",
			Details: map[string]any{"metric": metricName, "cause": err.Error()},
		}
	}
	return SourceRequirement{Root: root, Datasets: datasets}, nil
}

func resolveMetricSourceRoot(model *manifest.ModelIndex, metricName string, datasets []string) (string, error) {
	if len(datasets) == 1 {
		return datasets[0], nil
	}
	if model == nil || model.Graph == nil {
		return "", &serrors.Error{Code: serrors.ErrInternalInvariant, Message: "semantic model graph is not available", Details: map[string]any{"metric": metricName}}
	}

	viable := make([]string, 0, len(datasets))
	for _, candidate := range datasets {
		reachesAll := true
		for _, required := range datasets {
			if !model.Graph.CanReachDirected(candidate, required) {
				reachesAll = false
				break
			}
		}
		if reachesAll {
			viable = append(viable, candidate)
		}
	}
	if len(viable) == 1 {
		return viable[0], nil
	}
	return "", &serrors.Error{
		Code:    serrors.ErrAmbiguousMetricSource,
		Message: "source metric references multiple datasets and has no unique directed aggregate root",
		Details: map[string]any{"metric": metricName, "candidate_datasets": datasets, "viable_roots": viable},
	}
}

// PlanSourceEvaluation builds the source root, relationship path, and
// population-preservation evidence for one source metric. It does not inspect
// metric dependency topology or select a renderer.
func PlanSourceEvaluation(model *manifest.ModelIndex, metricName string, resolved expression.ResolvedExpression, requirement SourceRequirement, groups []semanticplan.GroupBy, predicates []semanticplan.Predicate) (string, []semanticplan.Join, []string, []semanticplan.PopulationPreservationEvidence, error) {
	if len(requirement.Datasets) == 0 {
		return "", nil, nil, nil, &serrors.Error{Code: serrors.ErrInternalInvariant, Message: "source metric has no dataset", Details: map[string]any{"metric": metricName}}
	}
	if requirement.Root == "" {
		return "", nil, nil, nil, &serrors.Error{Code: serrors.ErrUnresolvedMetricSource, Message: "source metric root cannot be resolved to exactly one dataset", Details: map[string]any{"metric": metricName}}
	}
	requiredSet := map[string]struct{}{}
	for _, dataset := range requirement.Datasets {
		requiredSet[dataset] = struct{}{}
	}
	for _, group := range groups {
		requiredSet[group.Dataset] = struct{}{}
	}
	for _, predicate := range predicates {
		requiredSet[predicate.Dataset] = struct{}{}
	}
	pathPlan, err := model.ResolveDatasetPaths(requirement.Root, sortedSet(requiredSet))
	if err != nil {
		requiredDataset := ""
		var apiErr *serrors.Error
		if errors.As(err, &apiErr) {
			requiredDataset, _ = apiErr.Details["to"].(string)
		}
		return "", nil, nil, nil, metricPathError(metricName, requirement.Root, requiredDataset, err)
	}
	joins, err := BuildJoinTree(requirement.Root, pathPlan.Relationships)
	if err != nil {
		return "", nil, nil, nil, err
	}
	for i := range joins {
		from, fromOK := model.Datasets[joins[i].FromDataset]
		to, toOK := model.Datasets[joins[i].ToDataset]
		if !fromOK || !toOK {
			return "", nil, nil, nil, &serrors.Error{Code: serrors.ErrInternalInvariant, Message: "metric evaluation join dataset is not present in semantic model", Details: map[string]any{"metric": metricName}}
		}
		joins[i].FromSource = from.Source
		joins[i].ToSource = to.Source
	}
	evidence, err := derivePopulationPreservationEvidence(model.Datasets, requirement.Root, requirement.Datasets, groups, predicates, joins)
	if err != nil {
		return "", nil, nil, nil, err
	}
	for i := range joins {
		if err := admitMetricRelationshipJoin(joins[i], &evidence[i], resolved); err != nil {
			return "", nil, nil, nil, err
		}
	}
	return requirement.Root, joins, pathPlan.RequiredDatasets, evidence, nil
}

// BuildJoinTree produces a stable root-connected join tree from resolved
// relationships, including temporal relationship evidence.
func BuildJoinTree(root string, relationships []*ossie.Relationship) ([]semanticplan.Join, error) {
	if len(relationships) == 0 {
		return nil, nil
	}
	type edge struct{ rel *ossie.Relationship }
	adj := map[string][]edge{}
	for _, rel := range relationships {
		if rel == nil {
			continue
		}
		adj[rel.From] = append(adj[rel.From], edge{rel: rel})
		adj[rel.To] = append(adj[rel.To], edge{rel: rel})
	}
	for node := range adj {
		sort.Slice(adj[node], func(i, j int) bool { return relationshipKey(adj[node][i].rel) < relationshipKey(adj[node][j].rel) })
	}
	visitedNodes := map[string]bool{root: true}
	usedRelationships := map[string]bool{}
	queue := []string{root}
	joins := make([]semanticplan.Join, 0, len(relationships))
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, edge := range adj[current] {
			rel := edge.rel
			key := relationshipKey(rel)
			if usedRelationships[key] {
				continue
			}
			next := rel.To
			if rel.To == current {
				next = rel.From
			}
			if visitedNodes[next] {
				usedRelationships[key] = true
				continue
			}
			join := semanticplan.Join{Relationship: rel, FromDataset: current, ToDataset: next}
			if spec, ok, err := ossie.TemporalRelationship(rel); err != nil {
				return nil, &serrors.Error{Code: serrors.ErrInvalidRelationship, Message: "invalid temporal relationship extension", Details: map[string]any{"relationship": rel.Name, "cause": err.Error()}}
			} else if ok {
				copy := spec
				join.Temporal = &copy
			}
			joins = append(joins, join)
			usedRelationships[key] = true
			visitedNodes[next] = true
			queue = append(queue, next)
		}
	}
	for _, rel := range relationships {
		if rel != nil && !usedRelationships[relationshipKey(rel)] {
			return nil, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "resolved relationships are not connected to root dataset", Details: map[string]any{"root_dataset": root, "relationship": rel.Name}}
		}
	}
	return joins, nil
}

func metricPathError(metric, root, dataset string, cause error) error {
	details := map[string]any{"metric": metric, "root_dataset": root, "required_dataset": dataset}
	if cause != nil {
		details["cause"] = cause.Error()
	}
	return &serrors.Error{Code: serrors.ErrMetricSourceUnreachable, Message: "metric source cannot reach a required grain or filter dataset", Details: details}
}

func relationshipKey(rel *ossie.Relationship) string {
	if rel == nil {
		return ""
	}
	if rel.Name != "" {
		return rel.Name
	}
	return fmt.Sprintf("%s->%s", rel.From, rel.To)
}

func sortedSet(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
