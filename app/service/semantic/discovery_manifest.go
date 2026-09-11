package semantic

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/serrors"
)

type AssetKind string

const (
	AssetModel           AssetKind = "model"
	AssetDataset         AssetKind = "dataset"
	AssetMetric          AssetKind = "metric"
	AssetDimension       AssetKind = "dimension"
	AssetRelationship    AssetKind = "relationship"
	AssetOntologyConcept AssetKind = "ontology_concept"
)

type MatchReason string

const (
	MatchReasonExactName         MatchReason = "exact_name"
	MatchReasonExactQualified    MatchReason = "exact_qualified_name"
	MatchReasonNameContains      MatchReason = "name_contains"
	MatchReasonQualifiedContains MatchReason = "qualified_name_contains"
	MatchReasonContextContains   MatchReason = "context_contains"
	MatchReasonTokenContains     MatchReason = "token_contains"
)

type Match struct {
	Kind         AssetKind                `json:"kind"`
	Project      string                   `json:"project"`
	Model        string                   `json:"model,omitempty"`
	Name         string                   `json:"name"`
	Qualified    string                   `json:"qualified_name,omitempty"`
	Description  string                   `json:"description,omitempty"`
	Dataset      string                   `json:"dataset,omitempty"`
	OntologyType string                   `json:"ontology_type,omitempty"`
	Extends      []string                 `json:"extends,omitempty"`
	Score        int                      `json:"score"`
	MatchReasons []MatchReason            `json:"match_reasons"`
	Governance   *AssetGovernanceEvidence `json:"governance,omitempty"`
}

type SearchResult struct {
	Matches []Match `json:"matches"`
}
type CompatibilityStatus string

const (
	CompatibilityCompatible  CompatibilityStatus = "compatible"
	CompatibilityUnreachable CompatibilityStatus = "unreachable"
	CompatibilityAmbiguous   CompatibilityStatus = "ambiguous"
)

type MetricDimensionCompatibilityResult struct {
	Project        string                   `json:"project"`
	Model          string                   `json:"model"`
	Metric         string                   `json:"metric"`
	SourceDatasets []string                 `json:"source_datasets"`
	Dimensions     []DimensionCompatibility `json:"dimensions"`
}

type DimensionCompatibility struct {
	Name      string                `json:"name"`
	Qualified string                `json:"qualified_name"`
	Dataset   string                `json:"dataset"`
	Status    CompatibilityStatus   `json:"status"`
	Paths     []DimensionSourcePath `json:"paths,omitempty"`
	Issues    []DimensionPathIssue  `json:"issues,omitempty"`
}

type DimensionSourcePath struct {
	SourceDataset string   `json:"source_dataset"`
	Datasets      []string `json:"datasets"`
	Relationships []string `json:"relationships,omitempty"`
}

type DimensionPathIssue struct {
	SourceDataset     string     `json:"source_dataset"`
	Code              string     `json:"code"`
	RelationshipPaths [][]string `json:"relationship_paths,omitempty"`
	DatasetPaths      [][]string `json:"dataset_paths,omitempty"`
}

func (s *DiscoveryService) search(ctx context.Context, project, query string) (*SearchResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(project) == "" {
		return nil, &serrors.Error{Code: serrors.ErrProjectRequired, Message: "project is required"}
	}
	q := normalize(query)
	if q == "" {
		return nil, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "discovery query is required"}
	}
	lookup, err := s.manifest.Lookup()
	if err != nil {
		return nil, err
	}
	if lookup == nil {
		return &SearchResult{}, nil
	}
	projectLookup, err := lookup.Project(project)
	if err != nil {
		return nil, err
	}
	p := projectLookup.ManifestProject()
	index, err := s.discoveryIndex()
	if err != nil {
		return nil, err
	}
	projectDiscovery := index.projects[project]
	if projectDiscovery == nil {
		return nil, &serrors.Error{Code: serrors.ErrProjectNotFound, Message: "project not found", Details: map[string]any{"project": project}}
	}
	searchTerms := append([]string{q}, strings.Fields(q)...)

	var matches []Match
	modelVisibility := make(map[string]bool)
	for _, ordinal := range projectDiscovery.assetSearch.candidates(searchTerms) {
		candidate := projectDiscovery.assets[ordinal]
		idx := p.Models[candidate.model]
		if candidate.kind != AssetOntologyConcept {
			visible, checked := modelVisibility[candidate.model]
			if !checked {
				visible = idx != nil && idx.Model != nil && s.canAccessAsset(ctx, project, ProjectActionDiscover, AssetModel, modelAssetRef(candidate.model), idx.Model.CustomExtensions)
				modelVisibility[candidate.model] = visible
			}
			if !visible {
				continue
			}
		}
		switch candidate.kind {
		case AssetModel:
			governance, governanceErr := governanceEvidence(idx.Model.CustomExtensions)
			if governanceErr != nil {
				return nil, governanceErr
			}
			matches = appendMatch(matches, candidate.kind, project, candidate.model, candidate.name, "", idx.Model.Description, "", q, governance)
		case AssetMetric:
			metric := idx.Metrics[candidate.name]
			if metric == nil || !s.visibleMetric(ctx, project, ProjectActionDiscover, candidate.model, candidate.name) {
				continue
			}
			governance, governanceErr := governanceEvidence(metric.CustomExtensions)
			if governanceErr != nil {
				return nil, governanceErr
			}
			matches = appendMatch(matches, candidate.kind, project, candidate.model, candidate.name, candidate.qualified, metric.Description, "", q, governance)
		case AssetDimension:
			handle := idx.Fields[candidate.qualified]
			dataset := idx.Datasets[candidate.dataset]
			if handle == nil || handle.Field == nil || dataset == nil || !s.canAccessAsset(ctx, project, ProjectActionDiscover, AssetDataset, datasetAssetRef(candidate.model, candidate.dataset), dataset.CustomExtensions) ||
				!s.canAccessAsset(ctx, project, ProjectActionDiscover, AssetDimension, dimensionAssetRef(candidate.model, candidate.dataset, handle.Field.Name), handle.Field.CustomExtensions) {
				continue
			}
			governance, governanceErr := governanceEvidence(handle.Field.CustomExtensions)
			if governanceErr != nil {
				return nil, governanceErr
			}
			matches = appendMatch(matches, candidate.kind, project, candidate.model, candidate.name, candidate.qualified, handle.Field.Description, candidate.dataset, q, governance)
		case AssetRelationship:
			relationship := idx.Relationships[candidate.name]
			if relationship == nil || !s.canAccessAsset(ctx, project, ProjectActionDiscover, AssetRelationship, relationshipAssetRef(candidate.model, candidate.name), relationship.CustomExtensions) {
				continue
			}
			from, to := idx.Datasets[relationship.From], idx.Datasets[relationship.To]
			if from == nil || to == nil || !s.canAccessAsset(ctx, project, ProjectActionDiscover, AssetDataset, datasetAssetRef(candidate.model, relationship.From), from.CustomExtensions) ||
				!s.canAccessAsset(ctx, project, ProjectActionDiscover, AssetDataset, datasetAssetRef(candidate.model, relationship.To), to.CustomExtensions) {
				continue
			}
			governance, governanceErr := governanceEvidence(relationship.CustomExtensions)
			if governanceErr != nil {
				return nil, governanceErr
			}
			matches = appendMatch(matches, candidate.kind, project, candidate.model, candidate.name, candidate.qualified, relationship.From+" to "+relationship.To, "", q, governance)
		case AssetOntologyConcept:
			concept := p.Ontology[strings.ToLower(candidate.name)]
			if concept == nil {
				continue
			}
			searchText := concept.Type + " " + strings.Join(concept.Extends, " ")
			score, reasons := rank(q, concept.Name, candidate.qualified, strings.TrimSpace(concept.Description+" "+searchText))
			if score != 0 {
				matches = append(matches, Match{Kind: candidate.kind, Project: project, Name: concept.Name, Qualified: candidate.qualified, Description: concept.Description, OntologyType: concept.Type, Extends: append([]string(nil), concept.Extends...), Score: score, MatchReasons: reasons})
			}
		}
	}

	matches = dedupe(matches)
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Score != matches[j].Score {
			return matches[i].Score > matches[j].Score
		}
		if matches[i].Kind != matches[j].Kind {
			return matches[i].Kind < matches[j].Kind
		}
		if matches[i].Model != matches[j].Model {
			return matches[i].Model < matches[j].Model
		}
		return matches[i].Qualified < matches[j].Qualified
	})
	return &SearchResult{Matches: matches}, nil
}

func (s *DiscoveryService) getModel(ctx context.Context, project, name string) (*ossie.SemanticModel, error) {
	idx, err := s.model(ctx, project, name)
	if err != nil {
		return nil, err
	}
	if idx.Model == nil || !s.canAccessAsset(ctx, project, ProjectActionDiscover, AssetModel, modelAssetRef(name), idx.Model.CustomExtensions) {
		return nil, modelNotFound(name)
	}
	projected := *idx.Model
	projected.Datasets = nil
	projected.Metrics = nil
	projected.Relationships = nil
	for datasetIndex := range idx.Model.Datasets {
		dataset := &idx.Model.Datasets[datasetIndex]
		if !s.canAccessAsset(ctx, project, ProjectActionDiscover, AssetDataset, datasetAssetRef(name, dataset.Name), dataset.CustomExtensions) {
			continue
		}
		copyDataset := *dataset
		copyDataset.Fields = nil
		hiddenFields := make(map[string]struct{})
		for fieldIndex := range dataset.Fields {
			field := &dataset.Fields[fieldIndex]
			if field.Dimension != nil && !s.canAccessAsset(ctx, project, ProjectActionDiscover, AssetDimension, dimensionAssetRef(name, dataset.Name, field.Name), field.CustomExtensions) {
				hiddenFields[field.Name] = struct{}{}
				continue
			}
			copyDataset.Fields = append(copyDataset.Fields, *field)
		}
		copyDataset.PrimaryKey = filterHiddenFieldNames(dataset.PrimaryKey, hiddenFields)
		copyDataset.UniqueKeys = filterHiddenUniqueKeys(dataset.UniqueKeys, hiddenFields)
		projected.Datasets = append(projected.Datasets, copyDataset)
	}
	for metricIndex := range idx.Model.Metrics {
		metric := &idx.Model.Metrics[metricIndex]
		if s.visibleMetric(ctx, project, ProjectActionDiscover, name, metric.Name) {
			projected.Metrics = append(projected.Metrics, *metric)
		}
	}
	for relationshipIndex := range idx.Model.Relationships {
		relationship := &idx.Model.Relationships[relationshipIndex]
		from, to := idx.Datasets[relationship.From], idx.Datasets[relationship.To]
		if from == nil || to == nil || !s.canAccessAsset(ctx, project, ProjectActionDiscover, AssetRelationship, relationshipAssetRef(name, relationship.Name), relationship.CustomExtensions) ||
			!s.canAccessAsset(ctx, project, ProjectActionDiscover, AssetDataset, datasetAssetRef(name, relationship.From), from.CustomExtensions) ||
			!s.canAccessAsset(ctx, project, ProjectActionDiscover, AssetDataset, datasetAssetRef(name, relationship.To), to.CustomExtensions) {
			continue
		}
		projected.Relationships = append(projected.Relationships, *relationship)
	}
	return &projected, nil
}

func filterHiddenFieldNames(values []string, hidden map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, denied := hidden[value]; !denied {
			result = append(result, value)
		}
	}
	return result
}

func filterHiddenUniqueKeys(values [][]string, hidden map[string]struct{}) [][]string {
	result := make([][]string, 0, len(values))
	for _, key := range values {
		visible := true
		for _, field := range key {
			if _, denied := hidden[field]; denied {
				visible = false
				break
			}
		}
		if visible {
			result = append(result, append([]string(nil), key...))
		}
	}
	return result
}
func (s *DiscoveryService) getMetric(ctx context.Context, project, model, name string) (*ossie.Metric, error) {
	idx, err := s.model(ctx, project, model)
	if err != nil {
		return nil, err
	}
	if idx.Model == nil || !s.canAccessAsset(ctx, project, ProjectActionDiscover, AssetModel, modelAssetRef(model), idx.Model.CustomExtensions) {
		return nil, modelNotFound(model)
	}
	metric, err := idx.Metric(name)
	if err != nil {
		return nil, err
	}
	if !s.visibleMetric(ctx, project, ProjectActionDiscover, model, name) {
		return nil, &serrors.Error{Code: serrors.ErrMetricNotFound, Message: "metric not found", Details: map[string]any{"model": model, "metric": name}}
	}
	return metric, nil
}

func (s *DiscoveryService) getMetricDimensions(ctx context.Context, project, model, metric string) (*MetricDimensionCompatibilityResult, error) {
	idx, err := s.model(ctx, project, model)
	if err != nil {
		return nil, err
	}
	metricDefinition, err := idx.Metric(metric)
	if err != nil {
		return nil, err
	}
	if idx.Model == nil || !s.canAccessAsset(ctx, project, ProjectActionDiscover, AssetModel, modelAssetRef(model), idx.Model.CustomExtensions) {
		return nil, modelNotFound(model)
	}
	if !s.canAccessAsset(ctx, project, ProjectActionDiscover, AssetMetric, metricAssetRef(model, metric), metricDefinition.CustomExtensions) {
		return nil, &serrors.Error{Code: serrors.ErrMetricNotFound, Message: "metric not found", Details: map[string]any{"model": model, "metric": metric}}
	}
	sourceMetric := metric
	conversion, isConversion, err := ossie.ConversionSpec(metricDefinition)
	if err != nil {
		return nil, err
	}
	if isConversion {
		// Conversion grouping is owned by the base-event population. The
		// conversion extension supplies the deterministic cross-event link, so
		// treating the conversion-event dataset as an ordinary relationship
		// source incorrectly marks base dimensions such as campaign unreachable.
		sourceMetric = conversion.BaseMetric
	}
	metricSources, err := idx.MetricSources(sourceMetric)
	if err != nil {
		return nil, err
	}
	if idx.MetricDependencyGraph != nil {
		order, orderErr := idx.MetricDependencyGraph.EvaluationOrder(sourceMetric)
		if orderErr == nil {
			for _, dependencyName := range order {
				dependency := idx.Metrics[dependencyName]
				if dependency == nil || !s.canAccessAsset(ctx, project, ProjectActionDiscover, AssetMetric, metricAssetRef(model, dependencyName), dependency.CustomExtensions) {
					return nil, &serrors.Error{Code: serrors.ErrMetricNotFound, Message: "metric not found", Details: map[string]any{"model": model, "metric": metric}}
				}
			}
		}
	}
	sourceSet := map[string]struct{}{}
	for _, source := range metricSources {
		dataset := idx.Datasets[source.Dataset]
		if dataset == nil || !s.canAccessAsset(ctx, project, ProjectActionDiscover, AssetDataset, datasetAssetRef(model, source.Dataset), dataset.CustomExtensions) {
			return nil, &serrors.Error{Code: serrors.ErrMetricNotFound, Message: "metric not found", Details: map[string]any{"model": model, "metric": metric}}
		}
		sourceSet[source.Dataset] = struct{}{}
	}
	sources := sortedStrings(sourceSet)
	result := &MetricDimensionCompatibilityResult{Project: project, Model: model, Metric: metric, SourceDatasets: sources}

	datasetNames := make([]string, 0, len(idx.Datasets))
	for name := range idx.Datasets {
		datasetNames = append(datasetNames, name)
	}
	sort.Strings(datasetNames)
	for _, datasetName := range datasetNames {
		dataset := idx.Datasets[datasetName]
		if dataset == nil || !s.canAccessAsset(ctx, project, ProjectActionDiscover, AssetDataset, datasetAssetRef(model, datasetName), dataset.CustomExtensions) {
			continue
		}
	dimensionLoop:
		for i := range dataset.Fields {
			field := &dataset.Fields[i]
			if field.Dimension == nil {
				continue
			}
			if !s.canAccessAsset(ctx, project, ProjectActionDiscover, AssetDimension, dimensionAssetRef(model, datasetName, field.Name), field.CustomExtensions) {
				continue
			}
			dimension := DimensionCompatibility{
				Name: field.Name, Qualified: datasetName + "." + field.Name, Dataset: datasetName, Status: CompatibilityCompatible,
			}
			for _, source := range sources {
				pathPlan, pathErr := idx.ResolveDatasetPaths(source, []string{datasetName})
				if pathErr != nil {
					issue := dimensionIssue(source, pathErr)
					dimension.Issues = append(dimension.Issues, issue)
					if issue.Code == string(serrors.ErrAmbiguousRelationshipPath) {
						dimension.Status = CompatibilityAmbiguous
					} else if dimension.Status != CompatibilityAmbiguous {
						dimension.Status = CompatibilityUnreachable
					}
					continue
				}
				path := datasetPath(pathPlan, datasetName)
				for _, relationship := range path {
					if relationship == nil || !s.canAccessAsset(ctx, project, ProjectActionDiscover, AssetRelationship, relationshipAssetRef(model, relationship.Name), relationship.CustomExtensions) {
						continue dimensionLoop
					}
				}
				dimension.Paths = append(dimension.Paths, resolvedDimensionPath(source, path))
			}
			result.Dimensions = append(result.Dimensions, dimension)
		}
	}
	sort.Slice(result.Dimensions, func(i, j int) bool {
		return result.Dimensions[i].Qualified < result.Dimensions[j].Qualified
	})
	return result, nil
}
func (s *DiscoveryService) getDimension(ctx context.Context, project, model, name string) (*manifest.FieldHandle, error) {
	idx, err := s.model(ctx, project, model)
	if err != nil {
		return nil, err
	}
	if idx.Model == nil || !s.canAccessAsset(ctx, project, ProjectActionDiscover, AssetModel, modelAssetRef(model), idx.Model.CustomExtensions) {
		return nil, modelNotFound(model)
	}
	handle, err := idx.Dimension(name)
	if err != nil {
		return nil, err
	}
	dataset := idx.Datasets[handle.Dataset]
	if dataset == nil || !s.canAccessAsset(ctx, project, ProjectActionDiscover, AssetDataset, datasetAssetRef(model, handle.Dataset), dataset.CustomExtensions) ||
		!s.canAccessAsset(ctx, project, ProjectActionDiscover, AssetDimension, dimensionAssetRef(model, handle.Dataset, handle.Field.Name), handle.Field.CustomExtensions) {
		return nil, &serrors.Error{Code: serrors.ErrDimensionNotFound, Message: "dimension not found", Details: map[string]any{"model": model, "dimension": name}}
	}
	return handle, nil
}
func (s *DiscoveryService) getRelationships(ctx context.Context, project, model string) ([]*ossie.Relationship, error) {
	idx, err := s.model(ctx, project, model)
	if err != nil {
		return nil, err
	}
	if idx.Model == nil || !s.canAccessAsset(ctx, project, ProjectActionDiscover, AssetModel, modelAssetRef(model), idx.Model.CustomExtensions) {
		return nil, modelNotFound(model)
	}
	names := make([]string, 0, len(idx.Relationships))
	for name := range idx.Relationships {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]*ossie.Relationship, 0, len(names))
	for _, name := range names {
		relationship := idx.Relationships[name]
		if relationship == nil || !s.canAccessAsset(ctx, project, ProjectActionDiscover, AssetRelationship, relationshipAssetRef(model, name), relationship.CustomExtensions) {
			continue
		}
		from, to := idx.Datasets[relationship.From], idx.Datasets[relationship.To]
		if from == nil || to == nil || !s.canAccessAsset(ctx, project, ProjectActionDiscover, AssetDataset, datasetAssetRef(model, relationship.From), from.CustomExtensions) ||
			!s.canAccessAsset(ctx, project, ProjectActionDiscover, AssetDataset, datasetAssetRef(model, relationship.To), to.CustomExtensions) {
			continue
		}
		out = append(out, relationship)
	}
	return out, nil
}
func (s *DiscoveryService) model(ctx context.Context, project, name string) (*manifest.ModelIndex, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(project) == "" {
		return nil, &serrors.Error{Code: serrors.ErrProjectRequired, Message: "project is required"}
	}
	lookup, err := s.manifest.Lookup()
	if err != nil {
		return nil, err
	}
	if lookup == nil {
		return nil, &serrors.Error{Code: serrors.ErrProjectNotFound, Message: "project not found", Details: map[string]any{"project": project}}
	}
	projectLookup, err := lookup.Project(project)
	if err != nil {
		return nil, err
	}
	modelLookup, err := projectLookup.Model(name)
	if err != nil {
		return nil, err
	}
	return modelLookup.ManifestModel(), nil
}

func appendMatch(dst []Match, kind AssetKind, project, model, name, qualified, description, dataset, query string, governance ...AssetGovernanceEvidence) []Match {
	score, reasons := rank(query, name, qualified, description)
	if score == 0 {
		return dst
	}
	evidence := AssetGovernanceEvidence{Lifecycle: ossie.AssetLifecycleActive, Certification: ossie.AssetCertificationUncertified}
	if len(governance) > 0 {
		evidence = governance[0]
	}
	return append(dst, Match{
		Kind: kind, Project: project, Model: model, Name: name, Qualified: qualified,
		Description: description, Dataset: dataset, Score: score, MatchReasons: reasons, Governance: &evidence,
	})
}

func modelNotFound(name string) error {
	return &serrors.Error{Code: serrors.ErrModelNotFound, Message: "model not found", Details: map[string]any{"model": name}}
}
func rank(query, name, qualified, description string) (int, []MatchReason) {
	nameN, qualifiedN, descN := normalize(name), normalize(qualified), normalize(description)
	switch {
	case qualifiedN != "" && query == qualifiedN:
		return 100, []MatchReason{MatchReasonExactQualified}
	case query == nameN:
		return 95, []MatchReason{MatchReasonExactName}
	case strings.Contains(nameN, query):
		return 80, []MatchReason{MatchReasonNameContains}
	case qualifiedN != "" && strings.Contains(qualifiedN, query):
		return 70, []MatchReason{MatchReasonQualifiedContains}
	case descN != "" && strings.Contains(descN, query):
		return 50, []MatchReason{MatchReasonContextContains}
	default:
		for _, token := range strings.Fields(query) {
			if token != "" && (strings.Contains(nameN, token) || strings.Contains(descN, token)) {
				return 30, []MatchReason{MatchReasonTokenContains}
			}
		}
	}
	return 0, nil
}
func normalize(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = discoveryTermReplacer.Replace(s)
	return strings.Join(strings.Fields(s), " ")
}

var discoveryTermReplacer = strings.NewReplacer("_", " ", "-", " ", ".", " ")

func dedupe(in []Match) []Match {
	best := map[string]Match{}
	for _, m := range in {
		key := string(m.Kind) + "|" + m.Project + "|" + m.Model + "|" + m.Qualified
		if current, ok := best[key]; !ok || m.Score > current.Score {
			best[key] = m
		}
	}
	out := make([]Match, 0, len(best))
	for _, m := range best {
		out = append(out, m)
	}
	return out
}

func resolvedDimensionPath(source string, relationships []*ossie.Relationship) DimensionSourcePath {
	path := DimensionSourcePath{SourceDataset: source, Datasets: []string{source}}
	current := source
	for _, relationship := range relationships {
		path.Relationships = append(path.Relationships, relationship.Name)
		next := relationship.To
		if relationship.To == current {
			next = relationship.From
		}
		path.Datasets = append(path.Datasets, next)
		current = next
	}
	return path
}

func datasetPath(plan *manifest.DatasetPathPlan, dataset string) []*ossie.Relationship {
	for _, path := range plan.Paths {
		if path.Dataset == dataset {
			return path.Relationships
		}
	}
	return nil
}

func dimensionIssue(source string, err error) DimensionPathIssue {
	issue := DimensionPathIssue{SourceDataset: source, Code: string(serrors.ErrRelationshipNotFound)}
	var metisErr *serrors.Error
	if !errors.As(err, &metisErr) {
		return issue
	}
	issue.Code = string(metisErr.Code)
	issue.RelationshipPaths, _ = metisErr.Details["relationship_paths"].([][]string)
	issue.DatasetPaths, _ = metisErr.Details["dataset_paths"].([][]string)
	return issue
}

func sortedStrings(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
