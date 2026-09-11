package semantic

import (
	"context"
	"strings"

	"github.com/meaningforge/metis/serrors"
)

func (s *DiscoveryService) searchWithRequest(ctx context.Context, req SearchSemanticsRequest) (*SearchResult, error) {
	if req.Model != "" {
		if _, err := s.getModel(ctx, req.Project, req.Model); err != nil {
			return nil, err
		}
	}
	kinds, err := searchKinds(req.Kinds)
	if err != nil {
		return nil, err
	}
	limit, err := searchLimit(req.Limit)
	if err != nil {
		return nil, err
	}
	result, err := s.search(ctx, req.Project, req.Query)
	if err != nil {
		return nil, err
	}
	filtered := make([]Match, 0, min(limit, len(result.Matches)))
	for _, match := range result.Matches {
		if req.Model != "" && match.Model != req.Model {
			continue
		}
		if len(kinds) != 0 {
			if _, ok := kinds[match.Kind]; !ok {
				continue
			}
		}
		filtered = append(filtered, match)
		if len(filtered) == limit {
			break
		}
	}
	return &SearchResult{Matches: filtered}, nil
}

func searchKinds(values []AssetKind) (map[AssetKind]struct{}, error) {
	if len(values) == 0 {
		return nil, nil
	}
	allowed := map[AssetKind]struct{}{}
	for _, kind := range []AssetKind{AssetModel, AssetMetric, AssetDimension, AssetRelationship, AssetOntologyConcept} {
		allowed[kind] = struct{}{}
	}
	out := make(map[AssetKind]struct{}, len(values))
	for _, value := range values {
		kind := AssetKind(strings.TrimSpace(string(value)))
		if _, ok := allowed[kind]; !ok {
			return nil, &serrors.Error{
				Code:    serrors.ErrInvalidQuery,
				Message: "unsupported semantic search asset kind",
				Details: map[string]any{"kind": value},
			}
		}
		out[kind] = struct{}{}
	}
	return out, nil
}

func searchLimit(value int) (int, error) {
	if value == 0 {
		return DefaultSearchLimit, nil
	}
	if value < 0 || value > MaxSearchLimit {
		return 0, &serrors.Error{
			Code:    serrors.ErrInvalidQuery,
			Message: "semantic search limit is out of range",
			Details: map[string]any{"limit": value, "max_limit": MaxSearchLimit},
		}
	}
	return value, nil
}
