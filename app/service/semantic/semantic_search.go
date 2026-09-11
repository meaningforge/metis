package semantic

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/serrors"
)

type SemanticSearchRequest struct {
	Project string      `json:"project"`
	Query   string      `json:"query,omitempty"`
	Kinds   []AssetKind `json:"kinds,omitempty"`
	Model   string      `json:"model,omitempty"`
	Limit   int         `json:"limit,omitempty"`
	Cursor  string      `json:"cursor,omitempty"`
}

type SemanticSearchItem struct {
	Ref         string                   `json:"ref"`
	Kind        AssetKind                `json:"kind"`
	Name        string                   `json:"name"`
	Model       string                   `json:"model,omitempty"`
	Description string                   `json:"description,omitempty"`
	Governance  *AssetGovernanceEvidence `json:"governance,omitempty"`
}

type SemanticSearchResult struct {
	Items      []SemanticSearchItem `json:"items"`
	NextCursor string               `json:"next_cursor,omitempty"`
}

// SemanticSearchService is the compact Agent read surface. A non-empty query
// performs deterministic SemanticManifest retrieval; an absent query deterministically
// enumerates the selected SemanticManifest scope. Query text is expected to contain
// Agent-selected manifest terms, not a natural-language question for Metis to
// interpret. Both modes return compact identities without constructing or
// mutating a SemanticQuery.
type SemanticSearchService struct {
	discovery *DiscoveryService
}

func NewSemanticSearchService(discovery *DiscoveryService) *SemanticSearchService {
	return &SemanticSearchService{discovery: discovery}
}

func (s *SemanticSearchService) Search(ctx context.Context, req SemanticSearchRequest) (*SemanticSearchResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s == nil || s.discovery == nil || s.discovery.manifest == nil {
		return nil, fmt.Errorf("semantic search service is not configured")
	}
	project, err := s.discovery.ResolveAuthorizedProject(ctx, req.Project, ProjectActionDiscover)
	if err != nil {
		return nil, err
	}
	if project == "" {
		return nil, &serrors.Error{Code: serrors.ErrProjectRequired, Message: "project is required"}
	}
	model := strings.TrimSpace(req.Model)
	kinds, err := searchKinds(req.Kinds)
	if err != nil {
		return nil, err
	}
	limit, err := searchLimit(req.Limit)
	if err != nil {
		return nil, err
	}
	queryText := strings.TrimSpace(req.Query)
	if queryText != "" {
		if strings.TrimSpace(req.Cursor) != "" {
			return nil, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "semantic search cursor is only supported for enumeration"}
		}
		result, err := s.discovery.searchWithRequest(ctx, SearchSemanticsRequest{
			Project: project,
			Query:   queryText,
			Model:   model,
			Kinds:   req.Kinds,
			Limit:   limit,
		})
		if err != nil {
			return nil, err
		}
		items := make([]SemanticSearchItem, 0, len(result.Matches))
		for _, match := range result.Matches {
			items = append(items, compactSearchMatch(match))
		}
		return &SemanticSearchResult{Items: items}, nil
	}
	return s.enumerate(ctx, project, model, kinds, limit, req.Cursor)
}

func compactSearchMatch(match Match) SemanticSearchItem {
	name := match.Name
	if match.Kind == AssetDimension && match.Qualified != "" {
		name = match.Qualified
	}
	return SemanticSearchItem{
		Ref:         semanticSearchRef(match.Kind, match.Model, name),
		Kind:        match.Kind,
		Name:        name,
		Model:       match.Model,
		Description: match.Description,
		Governance:  match.Governance,
	}
}

func (s *SemanticSearchService) enumerate(ctx context.Context, project, model string, kinds map[AssetKind]struct{}, limit int, cursor string) (*SemanticSearchResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	lookup, err := s.discovery.manifest.Lookup()
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
	projectIndex := projectLookup.ManifestProject()
	if model != "" {
		modelIndex, err := projectIndex.Model(model)
		if err != nil {
			return nil, err
		}
		if modelIndex.Model == nil || !s.discovery.canAccessAsset(ctx, project, ProjectActionDiscover, AssetModel, modelAssetRef(model), modelIndex.Model.CustomExtensions) {
			return nil, modelNotFound(model)
		}
	}
	kindNames := make([]string, 0, len(kinds))
	for kind := range kinds {
		kindNames = append(kindNames, string(kind))
	}
	sort.Strings(kindNames)
	binding := s.discovery.cursorBinding(ctx, "semantic_search", project, struct {
		Model string   `json:"model,omitempty"`
		Kinds []string `json:"kinds,omitempty"`
	}{Model: model, Kinds: kindNames})
	decodedCursor, err := decodeDiscoveryCursor(cursor, binding)
	if err != nil {
		return nil, err
	}
	after := decodedCursor.After
	items, err := s.enumerateSemanticSearchItems(ctx, project, projectIndex, model, kinds, after, limit+1)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return &SemanticSearchResult{}, nil
	}
	end := min(limit, len(items))
	result := &SemanticSearchResult{Items: append([]SemanticSearchItem(nil), items[:end]...)}
	if end < len(items) {
		result.NextCursor = encodeDiscoveryCursor(binding, 0, semanticSearchItemKey(items[end-1]))
	}
	return result, nil
}

func (s *SemanticSearchService) enumerateSemanticSearchItems(ctx context.Context, projectID string, project *manifest.ProjectIndex, model string, kinds map[AssetKind]struct{}, after string, limit int) ([]SemanticSearchItem, error) {
	index, err := s.discovery.discoveryIndex()
	if err != nil {
		return nil, err
	}
	projectDiscovery := index.projects[projectID]
	if projectDiscovery == nil {
		return nil, &serrors.Error{Code: serrors.ErrProjectNotFound, Message: "project not found", Details: map[string]any{"project": projectID}}
	}
	items := make([]SemanticSearchItem, 0, limit)
	include := func(candidate AssetKind) bool {
		if len(kinds) == 0 {
			return true
		}
		_, ok := kinds[candidate]
		return ok
	}
	modelVisibility := make(map[string]bool)
	datasetVisibility := make(map[string]bool)
	start := 0
	if after != "" {
		start = sort.Search(len(projectDiscovery.assets), func(i int) bool {
			return indexedSemanticSearchKey(projectDiscovery.assets[i]) > after
		})
	}
	for _, candidate := range projectDiscovery.assets[start:] {
		if !include(candidate.kind) || (model != "" && candidate.model != model) || (candidate.kind == AssetOntologyConcept && model != "") {
			continue
		}
		idx := project.Models[candidate.model]
		if candidate.kind != AssetOntologyConcept {
			visible, checked := modelVisibility[candidate.model]
			if !checked {
				visible = idx != nil && idx.Model != nil && s.discovery.canAccessAsset(ctx, projectID, ProjectActionDiscover, AssetModel, modelAssetRef(candidate.model), idx.Model.CustomExtensions)
				modelVisibility[candidate.model] = visible
			}
			if !visible {
				continue
			}
		}
		item := SemanticSearchItem{Ref: semanticSearchRef(candidate.kind, candidate.model, candidate.name), Kind: candidate.kind, Name: candidate.name, Model: candidate.model}
		switch candidate.kind {
		case AssetModel:
			item.Description = idx.Model.Description
			governance, governanceErr := governanceEvidence(idx.Model.CustomExtensions)
			if governanceErr != nil {
				return nil, governanceErr
			}
			item.Governance = &governance
		case AssetMetric:
			metric := idx.Metrics[candidate.name]
			if metric == nil || !s.discovery.visibleMetric(ctx, projectID, ProjectActionDiscover, candidate.model, candidate.name) {
				continue
			}
			item.Description = metric.Description
			governance, governanceErr := governanceEvidence(metric.CustomExtensions)
			if governanceErr != nil {
				return nil, governanceErr
			}
			item.Governance = &governance
		case AssetDimension:
			handle := idx.Fields[candidate.qualified]
			dataset := idx.Datasets[candidate.dataset]
			datasetKey := candidate.model + "\x00" + candidate.dataset
			datasetVisible, checked := datasetVisibility[datasetKey]
			if !checked {
				datasetVisible = dataset != nil && s.discovery.canAccessAsset(ctx, projectID, ProjectActionDiscover, AssetDataset, datasetAssetRef(candidate.model, candidate.dataset), dataset.CustomExtensions)
				datasetVisibility[datasetKey] = datasetVisible
			}
			if handle == nil || handle.Field == nil || !datasetVisible || !s.discovery.canAccessAsset(ctx, projectID, ProjectActionDiscover, AssetDimension, dimensionAssetRef(candidate.model, candidate.dataset, handle.Field.Name), handle.Field.CustomExtensions) {
				continue
			}
			item.Name = candidate.qualified
			item.Ref = semanticSearchRef(candidate.kind, candidate.model, candidate.qualified)
			item.Description = handle.Field.Description
			governance, governanceErr := governanceEvidence(handle.Field.CustomExtensions)
			if governanceErr != nil {
				return nil, governanceErr
			}
			item.Governance = &governance
		case AssetRelationship:
			relationship := idx.Relationships[candidate.name]
			if relationship == nil || !s.discovery.canAccessAsset(ctx, projectID, ProjectActionDiscover, AssetRelationship, relationshipAssetRef(candidate.model, candidate.name), relationship.CustomExtensions) {
				continue
			}
			endpointsVisible := true
			for _, datasetName := range []string{relationship.From, relationship.To} {
				dataset := idx.Datasets[datasetName]
				key := candidate.model + "\x00" + datasetName
				visible, checked := datasetVisibility[key]
				if !checked {
					visible = dataset != nil && s.discovery.canAccessAsset(ctx, projectID, ProjectActionDiscover, AssetDataset, datasetAssetRef(candidate.model, datasetName), dataset.CustomExtensions)
					datasetVisibility[key] = visible
				}
				endpointsVisible = endpointsVisible && visible
			}
			if !endpointsVisible {
				continue
			}
			item.Description = relationship.From + " to " + relationship.To
			governance, governanceErr := governanceEvidence(relationship.CustomExtensions)
			if governanceErr != nil {
				return nil, governanceErr
			}
			item.Governance = &governance
		case AssetOntologyConcept:
			concept := project.Ontology[strings.ToLower(candidate.name)]
			if concept == nil {
				continue
			}
			item.Ref = semanticSearchRef(candidate.kind, "", concept.Name)
			item.Model = ""
			item.Description = concept.Description
		}
		items = append(items, item)
		if len(items) == limit {
			break
		}
	}
	return items, nil
}

func semanticSearchRef(kind AssetKind, model, name string) string {
	switch kind {
	case AssetModel:
		return string(kind) + ":" + name
	case AssetOntologyConcept:
		return string(kind) + ":" + name
	default:
		if model != "" {
			return string(kind) + ":" + model + "." + name
		}
		return string(kind) + ":" + name
	}
}

func semanticSearchItemKey(item SemanticSearchItem) string {
	return string(item.Kind) + "\x00" + item.Model + "\x00" + item.Name
}
