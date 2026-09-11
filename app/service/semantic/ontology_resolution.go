package semantic

import (
	"context"
	"sort"
	"strings"

	"github.com/meaningforge/metis/app/auth"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/serrors"
)

type OntologyResolutionRequest struct {
	ProjectID  string `json:"project_id,omitempty"`
	ConceptRef string `json:"concept_ref"`
	Model      string `json:"model,omitempty"`
	Limit      int    `json:"limit,omitempty"`
	Cursor     string `json:"cursor,omitempty"`
}
type OntologyEvidence struct {
	MappedConceptRef string `json:"mapped_concept_ref"`
	MatchKind        string `json:"match_kind"`
}
type OntologyCandidate struct {
	Ref       string             `json:"ref"`
	Model     string             `json:"model"`
	Dataset   string             `json:"dataset"`
	Dimension string             `json:"dimension"`
	Evidence  []OntologyEvidence `json:"evidence"`
}
type OntologyResolutionResult struct {
	ProjectID    string              `json:"project_id"`
	ConceptRef   string              `json:"concept_ref"`
	GenerationID string              `json:"generation_id"`
	State        string              `json:"state"`
	Candidates   []OntologyCandidate `json:"candidates"`
	Truncated    bool                `json:"truncated"`
	NextCursor   string              `json:"next_cursor,omitempty"`
}

func (s *DiscoveryService) ResolveOntologyConcept(ctx context.Context, req OntologyResolutionRequest) (*OntologyResolutionResult, error) {
	project, err := s.ResolveAuthorizedProject(ctx, req.ProjectID, ProjectActionDiscover)
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	ref := strings.TrimSpace(req.ConceptRef)
	if !strings.HasPrefix(ref, "ontology_concept:") || len(ref) > 1041 || len(req.Cursor) > 4096 || len(req.Model) > 1024 {
		return nil, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "invalid ontology resolution request"}
	}
	name := strings.TrimPrefix(ref, "ontology_concept:")
	if strings.TrimSpace(name) == "" {
		return nil, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "invalid ontology concept ref"}
	}
	limit, err := searchLimit(req.Limit)
	if err != nil {
		return nil, err
	}
	index, err := s.discoveryIndex()
	if err != nil {
		return nil, serrors.Internal("ontology resolution is unavailable", nil)
	}
	p, err := index.manifest.Project(project)
	if err != nil {
		return nil, err
	}
	if !p.OntologyResolution.Ready() {
		return nil, &serrors.Error{Code: serrors.ErrOntologyResolutionUnavailable, Message: "ontology resolution is unavailable"}
	}
	targets, exists := p.OntologyResolution.Targets(name)
	if !exists {
		return nil, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "ontology concept is not resolvable"}
	}
	model := strings.TrimSpace(req.Model)
	binding, err := s.ontologyCursorBinding(ctx, "ontology-resolution", project, []string{strings.ToLower(name), model})
	if err != nil {
		return nil, err
	}
	if s.generationIdentity == "" {
		binding.Generation = stableCursorHash(p.OntologyResolution.GenerationIdentity())
	}
	cursor, err := decodeDiscoveryCursor(req.Cursor, binding)
	if err != nil {
		return nil, err
	}
	candidates := map[string]*OntologyCandidate{}
	checked := map[string]bool{}
	for _, target := range targets {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		if model != "" && target.Model != model {
			continue
		}
		m := p.Models[target.Model]
		ds := m.Datasets[target.Dataset]
		field := m.Fields[target.Dataset+"."+target.Field].Field
		visible := true
		checks := []struct {
			kind       AssetKind
			ref        string
			extensions []ossie.CustomExtension
		}{
			{AssetModel, modelAssetRef(target.Model), m.Model.CustomExtensions},
			{AssetDataset, datasetAssetRef(target.Model, target.Dataset), ds.CustomExtensions},
			{AssetDimension, dimensionAssetRef(target.Model, target.Dataset, target.Field), field.CustomExtensions},
		}
		for _, check := range checks {
			allowed, ok := checked[check.ref]
			if !ok {
				allowed, err = s.ontologyAssetVisible(ctx, project, check.kind, check.ref, check.extensions)
				if err != nil {
					return nil, err
				}
				checked[check.ref] = allowed
			}
			if !allowed {
				visible = false
				break
			}
		}
		if !visible {
			continue
		}
		canonical := dimensionAssetRef(target.Model, target.Dataset, target.Field)
		item := candidates[canonical]
		if item == nil {
			item = &OntologyCandidate{Ref: canonical, Model: target.Model, Dataset: target.Dataset, Dimension: target.Field}
			candidates[canonical] = item
		}
		kind := "descendant"
		if strings.EqualFold(target.Concept, name) {
			kind = "direct"
		}
		evidence := OntologyEvidence{"ontology_concept:" + target.Concept, kind}
		duplicate := false
		for _, current := range item.Evidence {
			if current == evidence {
				duplicate = true
				break
			}
		}
		if !duplicate {
			item.Evidence = append(item.Evidence, evidence)
		}
	}
	all := make([]OntologyCandidate, 0, len(candidates))
	for _, item := range candidates {
		sort.Slice(item.Evidence, func(a, b int) bool {
			if item.Evidence[a].MatchKind != item.Evidence[b].MatchKind {
				return item.Evidence[a].MatchKind == "direct"
			}
			return item.Evidence[a].MappedConceptRef < item.Evidence[b].MappedConceptRef
		})
		all = append(all, *item)
	}
	sort.Slice(all, func(a, b int) bool {
		if all[a].Evidence[0].MatchKind != all[b].Evidence[0].MatchKind {
			return all[a].Evidence[0].MatchKind == "direct"
		}
		return all[a].Ref < all[b].Ref
	})
	if cursor.Offset > len(all) {
		return nil, invalidDiscoveryCursor()
	}
	result := &OntologyResolutionResult{ProjectID: project, ConceptRef: ref, GenerationID: binding.Generation, State: "no_match", Candidates: []OntologyCandidate{}}
	if len(all) == 1 {
		result.State = "unique"
	}
	if len(all) > 1 {
		result.State = "ambiguous"
	}
	end := cursor.Offset + limit
	if end > len(all) {
		end = len(all)
	}
	result.Candidates = append(result.Candidates, all[cursor.Offset:end]...)
	result.Truncated = end < len(all)
	if result.Truncated {
		result.NextCursor = encodeDiscoveryCursor(binding, end, "")
	}
	for ontologyResponseOversized(result) {
		if len(result.Candidates) <= 1 {
			return nil, ontologyResponseLimitError()
		}
		result.Candidates = result.Candidates[:len(result.Candidates)-1]
		end--
		result.Truncated = true
		result.NextCursor = encodeDiscoveryCursor(binding, end, "")
	}
	if err := s.checkOntologyVisibilityScope(ctx, binding, project, []string{strings.ToLower(name), model}); err != nil {
		return nil, err
	}
	return result, nil
}

// Resolution distinguishes a policy outage from a hidden target: an outage
// must not turn an incomplete result into unique evidence. No adapter details escape.
func (s *DiscoveryService) ontologyAssetVisible(ctx context.Context, project string, kind AssetKind, ref string, extensions []ossie.CustomExtension) (visible bool, err error) {
	unavailable := func() error {
		return &serrors.Error{Code: serrors.ErrOntologyResolutionUnavailable, Message: "ontology resolution is unavailable"}
	}
	defer func() {
		if recover() != nil {
			visible = false
			err = unavailable()
		}
	}()
	if s.assetVisibility == nil {
		return false, unavailable()
	}
	governance, e := ossie.EffectiveAssetGovernance(extensions)
	if e != nil {
		return false, unavailable()
	}
	principal, _ := auth.PrincipalFromContext(ctx)
	if principal != nil {
		copied := *principal
		copied.Scopes = append([]string(nil), principal.Scopes...)
		principal = &copied
	}
	tags := append([]string(nil), governance.PolicyTags...)
	sort.Strings(tags)
	d := s.assetVisibility.EvaluateAssetVisibility(ctx, AssetVisibilityRequest{Principal: principal, ProjectID: project, Action: ProjectActionDiscover, AssetRef: ref, AssetKind: kind, PolicyTags: tags})
	if !validAssetVisibilityDecision(d) {
		return false, unavailable()
	}
	if d.Reason == AssetVisibilityReasonPolicyUnavailable || d.Reason == AssetVisibilityReasonInvalidDecision || d.Reason == AssetVisibilityReasonInvalidRequest {
		return false, unavailable()
	}
	if e = ctx.Err(); e != nil {
		return false, e
	}
	return d.Visible(), nil
}
