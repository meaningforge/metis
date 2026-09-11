package manifest

import (
	"fmt"
	"sort"

	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/serrors"
)

// SemanticGraph is the immutable, manifest-scoped topology read model. It
// contains only relationship and metric-dependency facts already proven by its
// SemanticManifest; it never selects a query path or owns a query plan.
type SemanticGraph struct {
	manifestDigest string
	projects       map[string]*ProjectSemanticGraph
}

// ProjectSemanticGraph is one project's immutable semantic topology.
type ProjectSemanticGraph struct {
	models map[string]*ModelSemanticGraph
}

// ModelSemanticGraph is one SemanticModel's immutable relationship and metric
// dependency topology.
type ModelSemanticGraph struct {
	relationships      *RelationshipGraph
	relationshipEdges  []RelationshipEdge
	metricDependencies *MetricDependencyGraph
}

// RelationshipEdge is an immutable value projection of one manifest
// relationship. Slices returned by SemanticGraph methods are defensive copies.
type RelationshipEdge struct {
	Name        string
	From        string
	To          string
	FromColumns []string
	ToColumns   []string
}

// BuildSemanticGraph deterministically rebuilds the manifest-scoped topology
// for one SemanticManifest. The graph is derived data and introduces no new
// semantic facts beyond the manifest's relationship and dependency indexes.
func BuildSemanticGraph(semanticManifest *SemanticManifest) (*SemanticGraph, error) {
	if semanticManifest == nil {
		return nil, fmt.Errorf("semantic manifest is nil")
	}

	graph := &SemanticGraph{
		manifestDigest: semanticManifest.Digest,
		projects:       make(map[string]*ProjectSemanticGraph, len(semanticManifest.Projects)),
	}
	for _, projectName := range sortedProjectNames(semanticManifest.Projects) {
		project := semanticManifest.Projects[projectName]
		if project == nil {
			return nil, fmt.Errorf("semantic manifest project %q is nil", projectName)
		}
		projectGraph := &ProjectSemanticGraph{models: make(map[string]*ModelSemanticGraph, len(project.Models))}
		seenModelIdentities := make(map[string]struct{}, len(project.Models))
		for _, modelName := range sortedModelNames(project.Models) {
			model := project.Models[modelName]
			if model == nil || model.Model == nil {
				return nil, fmt.Errorf("semantic manifest model %q in project %q is nil", modelName, projectName)
			}
			if model.Model.Name != modelName {
				return nil, fmt.Errorf("semantic manifest model key %q does not match SemanticModel identity %q", modelName, model.Model.Name)
			}
			if _, exists := seenModelIdentities[model.Model.Name]; exists {
				return nil, fmt.Errorf("duplicate SemanticModel %q in project %q", model.Model.Name, projectName)
			}
			seenModelIdentities[model.Model.Name] = struct{}{}

			edges := relationshipEdges(model.Relationships)
			relationships := make([]*ossie.Relationship, 0, len(edges))
			for index := range edges {
				relationships = append(relationships, edges[index].relationship())
			}
			dependencyGraph, _, err := buildMetricDependencyGraph(model.MetricDependencies)
			if err != nil {
				return nil, fmt.Errorf("build metric dependency topology for project %q model %q: %w", projectName, modelName, err)
			}
			projectGraph.models[modelName] = &ModelSemanticGraph{
				relationships:      NewRelationshipGraph(relationships),
				relationshipEdges:  cloneRelationshipEdges(edges),
				metricDependencies: dependencyGraph,
			}
		}
		graph.projects[projectName] = projectGraph
	}
	return graph, nil
}

// ManifestDigest identifies the SemanticManifest version this graph was built
// from. Rebuilding from the same manifest digest produces the same topology.
func (g *SemanticGraph) ManifestDigest() string {
	if g == nil {
		return ""
	}
	return g.manifestDigest
}

func (g *SemanticGraph) Project(name string) (*ProjectSemanticGraph, error) {
	if g == nil {
		return nil, fmt.Errorf("semantic graph is nil")
	}
	project, ok := g.projects[name]
	if !ok {
		return nil, &serrors.Error{Code: serrors.ErrProjectNotFound, Message: "project not found", Details: map[string]any{"project": name}}
	}
	return project, nil
}

func (g *SemanticGraph) ProjectNames() []string {
	if g == nil {
		return nil
	}
	return sortedProjectNames(g.projects)
}

func (g *ProjectSemanticGraph) Model(name string) (*ModelSemanticGraph, error) {
	if g == nil {
		return nil, fmt.Errorf("project semantic graph is nil")
	}
	model, ok := g.models[name]
	if !ok {
		return nil, &serrors.Error{Code: serrors.ErrModelNotFound, Message: "semantic model not found", Details: map[string]any{"model": name}}
	}
	return model, nil
}

func (g *ProjectSemanticGraph) ModelNames() []string {
	if g == nil {
		return nil
	}
	return sortedModelNames(g.models)
}

// RelationshipEdges returns the model's relationship topology in deterministic
// identity order.
func (g *ModelSemanticGraph) RelationshipEdges() []RelationshipEdge {
	if g == nil {
		return nil
	}
	return cloneRelationshipEdges(g.relationshipEdges)
}

// ShortestRelationshipPath preserves the existing manifest relationship-path
// semantics while returning immutable value projections rather than Ossie
// pointers.
func (g *ModelSemanticGraph) ShortestRelationshipPath(from, to string) ([]RelationshipEdge, error) {
	if g == nil || g.relationships == nil {
		return nil, &serrors.Error{Code: serrors.ErrInvalidModel, Message: "relationship graph is not available"}
	}
	path, err := g.relationships.ShortestPath(from, to)
	if err != nil {
		return nil, err
	}
	edges := make([]RelationshipEdge, 0, len(path))
	for _, relationship := range path {
		edges = append(edges, relationshipEdge(relationship))
	}
	return edges, nil
}

func (g *ModelSemanticGraph) CanReachDirected(from, to string) bool {
	return g != nil && g.relationships != nil && g.relationships.CanReachDirected(from, to)
}

func (g *ModelSemanticGraph) DirectMetricDependencies(metric string) ([]string, bool) {
	if g == nil || g.metricDependencies == nil {
		return nil, false
	}
	return g.metricDependencies.DirectDependencies(metric)
}

func (g *ModelSemanticGraph) MetricEvaluationOrder(metrics ...string) ([]string, error) {
	if g == nil || g.metricDependencies == nil {
		return nil, &serrors.Error{Code: serrors.ErrInvalidModel, Message: "metric dependency graph is not available"}
	}
	return g.metricDependencies.EvaluationOrder(metrics...)
}

func relationshipEdges(relationships map[string]*ossie.Relationship) []RelationshipEdge {
	edges := make([]RelationshipEdge, 0, len(relationships))
	for _, relationship := range relationships {
		if relationship != nil {
			edges = append(edges, relationshipEdge(relationship))
		}
	}
	sort.Slice(edges, func(i, j int) bool {
		return relationshipEdgeIdentity(edges[i]) < relationshipEdgeIdentity(edges[j])
	})
	return edges
}

func relationshipEdge(relationship *ossie.Relationship) RelationshipEdge {
	if relationship == nil {
		return RelationshipEdge{}
	}
	return RelationshipEdge{
		Name:        relationship.Name,
		From:        relationship.From,
		To:          relationship.To,
		FromColumns: append([]string(nil), relationship.FromColumns...),
		ToColumns:   append([]string(nil), relationship.ToColumns...),
	}
}

func (edge RelationshipEdge) relationship() *ossie.Relationship {
	return &ossie.Relationship{
		Name:        edge.Name,
		From:        edge.From,
		To:          edge.To,
		FromColumns: append([]string(nil), edge.FromColumns...),
		ToColumns:   append([]string(nil), edge.ToColumns...),
	}
}

func relationshipEdgeIdentity(edge RelationshipEdge) string {
	return relationshipIdentity(edge.relationship())
}

func cloneRelationshipEdges(edges []RelationshipEdge) []RelationshipEdge {
	cloned := make([]RelationshipEdge, len(edges))
	for index, edge := range edges {
		cloned[index] = RelationshipEdge{
			Name:        edge.Name,
			From:        edge.From,
			To:          edge.To,
			FromColumns: append([]string(nil), edge.FromColumns...),
			ToColumns:   append([]string(nil), edge.ToColumns...),
		}
	}
	return cloned
}

func sortedProjectNames[T any](projects map[string]T) []string {
	names := make([]string, 0, len(projects))
	for name := range projects {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func sortedModelNames[T any](models map[string]T) []string {
	names := make([]string, 0, len(models))
	for name := range models {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
