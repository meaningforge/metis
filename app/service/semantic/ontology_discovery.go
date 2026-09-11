package semantic

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/meaningforge/metis/serrors"
)

// OntologyConceptSearchRequest accepts metadata terms, never mapping expressions.
type OntologyConceptSearchRequest struct {
	ProjectID string `json:"project_id,omitempty"`
	Query     string `json:"query"`
	Limit     int    `json:"limit,omitempty"`
	Cursor    string `json:"cursor,omitempty"`
}

type OntologyConcept struct {
	ConceptRef  string   `json:"concept_ref"`
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Description string   `json:"description,omitempty"`
	Extends     []string `json:"extends,omitempty"`
}

type OntologyConceptSearchResult struct {
	ProjectID    string            `json:"project_id"`
	GenerationID string            `json:"generation_id"`
	Concepts     []OntologyConcept `json:"concepts"`
	Truncated    bool              `json:"truncated"`
	NextCursor   string            `json:"next_cursor,omitempty"`
}

// SearchOntologyConcepts searches only authored concept metadata. Mapping-derived
// identities cannot affect matches, ranking, pagination, or returned evidence.
func (s *DiscoveryService) SearchOntologyConcepts(ctx context.Context, req OntologyConceptSearchRequest) (*OntologyConceptSearchResult, error) {
	project, err := s.ResolveAuthorizedProject(ctx, req.ProjectID, ProjectActionDiscover)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	q := normalize(req.Query)
	if q == "" || len(req.Query) > 1024 || len(req.Cursor) > 4096 {
		return nil, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "invalid ontology concept search request"}
	}
	limit, err := searchLimit(req.Limit)
	if err != nil {
		return nil, err
	}
	index, err := s.discoveryIndex()
	if err != nil {
		return nil, serrors.Internal("ontology discovery is unavailable", nil)
	}
	p, err := index.manifest.Project(project)
	if err != nil {
		return nil, err
	}
	binding, err := s.ontologyCursorBinding(ctx, "ontology-concepts", project, q)
	if err != nil {
		return nil, err
	}
	// Pin direct embedders to the same snapshot used for the entire response.
	if s.generationIdentity == "" {
		binding.Generation = stableCursorHash(p.OntologyResolution.GenerationIdentity())
	}
	cursor, err := decodeDiscoveryCursor(req.Cursor, binding)
	if err != nil {
		return nil, err
	}
	type scored struct {
		concept OntologyConcept
		score   int
	}
	matches := make([]scored, 0)
	for _, c := range p.Ontology {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		score, _ := rank(q, c.Name, "ontology_concept:"+c.Name, c.Description+" "+c.Type+" "+strings.Join(c.Extends, " "))
		if score == 0 {
			continue
		}
		matches = append(matches, scored{OntologyConcept{
			ConceptRef: "ontology_concept:" + c.Name, Name: c.Name, Type: c.Type,
			Description: c.Description, Extends: append([]string(nil), c.Extends...),
		}, score})
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].score != matches[j].score {
			return matches[i].score > matches[j].score
		}
		return matches[i].concept.ConceptRef < matches[j].concept.ConceptRef
	})
	if cursor.Offset > len(matches) {
		return nil, invalidDiscoveryCursor()
	}
	result := &OntologyConceptSearchResult{ProjectID: project, GenerationID: binding.Generation, Concepts: []OntologyConcept{}}
	end := cursor.Offset + limit
	if end > len(matches) {
		end = len(matches)
	}
	for _, item := range matches[cursor.Offset:end] {
		result.Concepts = append(result.Concepts, item.concept)
	}
	result.Truncated = end < len(matches)
	if result.Truncated {
		result.NextCursor = encodeDiscoveryCursor(binding, end, "")
	}
	for ontologyResponseOversized(result) {
		if len(result.Concepts) <= 1 {
			return nil, ontologyResponseLimitError()
		}
		result.Concepts = result.Concepts[:len(result.Concepts)-1]
		end--
		result.Truncated = true
		result.NextCursor = encodeDiscoveryCursor(binding, end, "")
	}
	if err := s.checkOntologyVisibilityScope(ctx, binding, project, q); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *DiscoveryService) ontologyCursorBinding(ctx context.Context, kind, project string, query any) (binding discoveryCursorBinding, err error) {
	defer func() {
		if recover() != nil {
			err = &serrors.Error{Code: serrors.ErrOntologyResolutionUnavailable, Message: "ontology resolution is unavailable"}
		}
	}()
	return s.cursorBinding(ctx, kind, project, query), nil
}
func (s *DiscoveryService) checkOntologyVisibilityScope(ctx context.Context, binding discoveryCursorBinding, project string, query any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	current, err := s.ontologyCursorBinding(ctx, binding.Kind, project, query)
	if err != nil {
		return err
	}
	if current.Visibility != binding.Visibility {
		return &serrors.Error{Code: serrors.ErrSemanticPaginationRestart, Message: "ontology visibility scope changed; restart from the first page"}
	}
	return nil
}

const ontologyResponseBytes = 64 << 10

func ontologyResponseOversized(value any) bool {
	encoded, err := json.Marshal(value)
	return err != nil || len(encoded) > ontologyResponseBytes
}
func ontologyResponseLimitError() error {
	return &serrors.Error{Code: serrors.ErrOntologyResolutionUnavailable, Message: "ontology evidence exceeds response bounds"}
}
