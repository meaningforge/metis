package manifest

import (
	"crypto/sha256"
	"fmt"
	"sort"

	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/serrors"
)

type SemanticManifest struct {
	Version  string
	Digest   string
	Projects map[string]*ProjectIndex
}

type ProjectIndex struct {
	Name               string
	DisplayName        string
	Description        string
	Models             map[string]*ModelIndex
	Ontology           map[string]*OntologyConceptIndex
	OntologyResolution *OntologyResolutionIndex
}

type ModelIndex struct {
	Model                 *ossie.SemanticModel
	Datasets              map[string]*ossie.Dataset
	Metrics               map[string]*ossie.Metric
	Fields                map[string]*FieldHandle
	Dimensions            map[string][]*FieldHandle
	Relationships         map[string]*ossie.Relationship
	MetricAnalyses        map[string]MetricAnalysis
	MetricDependencies    map[string]MetricDependency
	MetricDependencyGraph *MetricDependencyGraph
	Graph                 *RelationshipGraph
}

type FieldHandle struct {
	Dataset string
	Field   *ossie.Field
}

func BuildProjectManifest(project string, doc *ossie.Document) (*SemanticManifest, error) {
	return BuildProjectManifestWithAnalyzer(project, doc, DefaultExpressionAnalyzer())
}

func BuildProjectManifestWithAnalyzer(project string, doc *ossie.Document, analyzer ExpressionAnalyzer) (*SemanticManifest, error) {
	if project == "" {
		return nil, &serrors.Error{Code: serrors.ErrProjectRequired, Message: "project is required"}
	}
	if err := ossie.ValidateDocument(doc); err != nil {
		return nil, err
	}
	digest, err := ossie.Digest(doc)
	if err != nil {
		return nil, err
	}

	models := make(map[string]*ModelIndex, len(doc.SemanticModel))
	for i := range doc.SemanticModel {
		m := &doc.SemanticModel[i]
		idx := &ModelIndex{
			Model: m, Datasets: map[string]*ossie.Dataset{}, Metrics: map[string]*ossie.Metric{},
			Fields: map[string]*FieldHandle{}, Dimensions: map[string][]*FieldHandle{},
			Relationships: map[string]*ossie.Relationship{}, MetricAnalyses: map[string]MetricAnalysis{}, MetricDependencies: map[string]MetricDependency{},
		}
		for j := range m.Datasets {
			ds := &m.Datasets[j]
			idx.Datasets[ds.Name] = ds
			for k := range ds.Fields {
				f := &ds.Fields[k]
				h := &FieldHandle{Dataset: ds.Name, Field: f}
				idx.Fields[ds.Name+"."+f.Name] = h
				if f.Dimension != nil {
					idx.Dimensions[f.Name] = append(idx.Dimensions[f.Name], h)
					idx.Dimensions[ds.Name+"."+f.Name] = []*FieldHandle{h}
				}
			}
		}
		for j := range m.Metrics {
			metric := &m.Metrics[j]
			idx.Metrics[metric.Name] = metric
		}
		for j := range m.Relationships {
			rel := &m.Relationships[j]
			idx.Relationships[rel.Name] = rel
		}
		rels := make([]*ossie.Relationship, 0, len(m.Relationships))
		for j := range m.Relationships {
			rels = append(rels, &m.Relationships[j])
		}
		idx.Graph = NewRelationshipGraph(rels)
		analyses, dependencies, err := buildMetricDependencyIndexWithAnalyzer(idx, analyzer)
		if err != nil {
			return nil, err
		}
		graph, dependencies, err := buildMetricDependencyGraph(dependencies)
		if err != nil {
			return nil, err
		}
		idx.MetricAnalyses = analyses
		idx.MetricDependencies = dependencies
		idx.MetricDependencyGraph = graph
		models[m.Name] = idx
	}

	projectIndex := &ProjectIndex{Name: project, DisplayName: project, Models: models, Ontology: buildOntologyIndex(doc.Ontology)}
	projectIndex.OntologyResolution = buildOntologyResolution(doc, models)
	projectIndex.OntologyResolution.generation = digest
	return &SemanticManifest{Version: doc.Version, Digest: digest, Projects: map[string]*ProjectIndex{project: projectIndex}}, nil
}

func MergeManifests(manifests ...*SemanticManifest) (*SemanticManifest, error) {
	out := &SemanticManifest{Projects: map[string]*ProjectIndex{}}
	projectDigests := map[string]string{}
	for _, semanticManifest := range manifests {
		if semanticManifest == nil {
			continue
		}
		if out.Version == "" {
			out.Version = semanticManifest.Version
		} else if semanticManifest.Version != "" && semanticManifest.Version != out.Version {
			return nil, fmt.Errorf("semantic manifest version mismatch: %s != %s", semanticManifest.Version, out.Version)
		}
		for name, project := range semanticManifest.Projects {
			if _, exists := out.Projects[name]; exists {
				return nil, fmt.Errorf("duplicate project %q", name)
			}
			out.Projects[name] = project
			projectDigests[name] = semanticManifest.Digest
		}
	}
	projectNames := make([]string, 0, len(projectDigests))
	for name := range projectDigests {
		projectNames = append(projectNames, name)
	}
	sort.Strings(projectNames)
	digest := sha256.New()
	for _, name := range projectNames {
		_, _ = fmt.Fprintf(digest, "%d:%s%d:%s", len(name), name, len(projectDigests[name]), projectDigests[name])
	}
	out.Digest = fmt.Sprintf("sha256:%x", digest.Sum(nil))
	return out, nil
}

func (s *SemanticManifest) Project(name string) (*ProjectIndex, error) {
	if s == nil {
		return nil, fmt.Errorf("semantic manifest is nil")
	}
	if name == "" {
		return nil, &serrors.Error{Code: serrors.ErrProjectRequired, Message: "project is required"}
	}
	p, ok := s.Projects[name]
	if !ok {
		return nil, &serrors.Error{Code: serrors.ErrProjectNotFound, Message: "project not found", Details: map[string]any{"project": name}}
	}
	return p, nil
}

func (p *ProjectIndex) Model(name string) (*ModelIndex, error) {
	if p == nil {
		return nil, fmt.Errorf("project index is nil")
	}
	m, ok := p.Models[name]
	if !ok {
		return nil, &serrors.Error{Code: serrors.ErrModelNotFound, Message: "semantic model not found", Details: map[string]any{"project": p.Name, "model": name}}
	}
	return m, nil
}

func (m *ModelIndex) Metric(name string) (*ossie.Metric, error) {
	metric, ok := m.Metrics[name]
	if !ok {
		return nil, &serrors.Error{Code: serrors.ErrMetricNotFound, Message: "metric not found", Details: map[string]any{"metric": name}}
	}
	return metric, nil
}

func (m *ModelIndex) Dimension(name string) (*FieldHandle, error) {
	matches := m.Dimensions[name]
	if len(matches) == 0 {
		return nil, &serrors.Error{Code: serrors.ErrDimensionNotFound, Message: "dimension not found", Details: map[string]any{"dimension": name}}
	}
	if len(matches) > 1 {
		return nil, &serrors.Error{Code: serrors.ErrAmbiguousField, Message: "dimension name is ambiguous; use dataset.field", Details: map[string]any{"dimension": name}}
	}
	return matches[0], nil
}
