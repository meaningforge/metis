package manifest

import (
	"fmt"

	"github.com/meaningforge/metis/ossie"
)

// SemanticManifestLookup is the read-only facade for one SemanticManifest and
// the SemanticGraph built from the same manifest digest. It exposes facts and
// evidence only; query-specific semantic decisions remain in resolver.
type SemanticManifestLookup struct {
	semanticManifest *SemanticManifest
	semanticGraph    *SemanticGraph
}

// ProjectLookup scopes manifest and graph evidence to one project.
type ProjectLookup struct {
	project *ProjectIndex
	graph   *ProjectSemanticGraph
}

// ModelLookup scopes manifest and graph evidence to one SemanticModel.
type ModelLookup struct {
	model *ModelIndex
	graph *ModelSemanticGraph
}

// MetricLookup exposes immutable metric identity, dependency, and analysis
// evidence for one model.
type MetricLookup struct {
	model *ModelIndex
}

// DimensionLookup exposes immutable field and dimension identity evidence for
// one model.
type DimensionLookup struct {
	model *ModelIndex
}

// GraphLookup exposes immutable relationship and metric-dependency topology
// for one model. It never chooses an ambiguous path for a caller.
type GraphLookup struct {
	graph *ModelSemanticGraph
}

// NewSemanticManifestLookup binds one manifest to the graph derived from it.
// A caller may omit graph to build the deterministic derived topology now.
func NewSemanticManifestLookup(semanticManifest *SemanticManifest, semanticGraph *SemanticGraph) (*SemanticManifestLookup, error) {
	if semanticManifest == nil {
		return nil, fmt.Errorf("semantic manifest is nil")
	}
	if semanticGraph == nil {
		var err error
		semanticGraph, err = BuildSemanticGraph(semanticManifest)
		if err != nil {
			return nil, err
		}
	}
	if semanticGraph.ManifestDigest() != semanticManifest.Digest {
		return nil, fmt.Errorf("semantic graph digest %q does not match semantic manifest digest %q", semanticGraph.ManifestDigest(), semanticManifest.Digest)
	}
	return &SemanticManifestLookup{semanticManifest: semanticManifest, semanticGraph: semanticGraph}, nil
}

func (l *SemanticManifestLookup) ManifestDigest() string {
	if l == nil || l.semanticManifest == nil {
		return ""
	}
	return l.semanticManifest.Digest
}

func (l *SemanticManifestLookup) Project(name string) (*ProjectLookup, error) {
	if l == nil || l.semanticManifest == nil || l.semanticGraph == nil {
		return nil, fmt.Errorf("semantic manifest lookup is not configured")
	}
	project, err := l.semanticManifest.Project(name)
	if err != nil {
		return nil, err
	}
	graph, err := l.semanticGraph.Project(name)
	if err != nil {
		return nil, err
	}
	return &ProjectLookup{project: project, graph: graph}, nil
}

func (l *SemanticManifestLookup) Model(project, model string) (*ModelLookup, error) {
	projectLookup, err := l.Project(project)
	if err != nil {
		return nil, err
	}
	return projectLookup.Model(model)
}

func (l *SemanticManifestLookup) ProjectNames() []string {
	if l == nil || l.semanticGraph == nil {
		return nil
	}
	return l.semanticGraph.ProjectNames()
}

func (l *ProjectLookup) Model(name string) (*ModelLookup, error) {
	if l == nil || l.project == nil || l.graph == nil {
		return nil, fmt.Errorf("project lookup is not configured")
	}
	model, err := l.project.Model(name)
	if err != nil {
		return nil, err
	}
	graph, err := l.graph.Model(name)
	if err != nil {
		return nil, err
	}
	return &ModelLookup{model: model, graph: graph}, nil
}

func (l *ProjectLookup) ModelNames() []string {
	if l == nil || l.graph == nil {
		return nil
	}
	return l.graph.ModelNames()
}

// ManifestProject is the immutable manifest project projection. It exists only
// for current discovery consumers while later phases narrow their read models;
// callers must not mutate it.
func (l *ProjectLookup) ManifestProject() *ProjectIndex {
	if l == nil {
		return nil
	}
	return l.project
}

// ManifestModel is the immutable manifest model projection. Resolver and planner
// consumers must not mutate it.
func (l *ModelLookup) ManifestModel() *ModelIndex {
	if l == nil {
		return nil
	}
	return l.model
}

func (l *ModelLookup) Metrics() MetricLookup {
	return MetricLookup{model: l.ManifestModel()}
}

func (l *ModelLookup) Dimensions() DimensionLookup {
	return DimensionLookup{model: l.ManifestModel()}
}

func (l *ModelLookup) Graph() GraphLookup {
	if l == nil {
		return GraphLookup{}
	}
	return GraphLookup{graph: l.graph}
}

func (l MetricLookup) Metric(name string) (*ossie.Metric, error) {
	if l.model == nil {
		return nil, fmt.Errorf("metric lookup is not configured")
	}
	return l.model.Metric(name)
}

func (l MetricLookup) Dependency(name string) (MetricDependency, bool) {
	if l.model == nil {
		return MetricDependency{}, false
	}
	return l.model.MetricDependency(name)
}

func (l MetricLookup) Analysis(name string) (MetricAnalysis, bool) {
	if l.model == nil {
		return MetricAnalysis{}, false
	}
	return l.model.MetricAnalysis(name)
}

func (l MetricLookup) Sources(names ...string) ([]MetricSource, error) {
	if l.model == nil {
		return nil, fmt.Errorf("metric lookup is not configured")
	}
	return l.model.MetricSources(names...)
}

func (l DimensionLookup) Dimension(name string) (*FieldHandle, error) {
	if l.model == nil {
		return nil, fmt.Errorf("dimension lookup is not configured")
	}
	return l.model.Dimension(name)
}

func (l DimensionLookup) Field(name string) (*FieldHandle, error) {
	if l.model == nil {
		return nil, fmt.Errorf("dimension lookup is not configured")
	}
	if handle, ok := l.model.Fields[name]; ok {
		return handle, nil
	}
	return nil, fmt.Errorf("field %q is not found", name)
}

func (l GraphLookup) CanReachDirected(from, to string) bool {
	return l.graph != nil && l.graph.CanReachDirected(from, to)
}

func (l GraphLookup) ShortestRelationshipPath(from, to string) ([]RelationshipEdge, error) {
	if l.graph == nil {
		return nil, fmt.Errorf("graph lookup is not configured")
	}
	return l.graph.ShortestRelationshipPath(from, to)
}

func (l GraphLookup) MetricEvaluationOrder(metrics ...string) ([]string, error) {
	if l.graph == nil {
		return nil, fmt.Errorf("graph lookup is not configured")
	}
	return l.graph.MetricEvaluationOrder(metrics...)
}
