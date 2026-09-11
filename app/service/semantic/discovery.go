package semantic

import (
	"context"
	"fmt"
	"strings"

	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
)

// DiscoveryService exposes transport-neutral semantic discovery use cases.
// REST and MCP adapters both call this layer directly.
type DiscoveryService struct {
	manifest              *manifest.Store
	index                 discoveryIndexCache
	generationIdentity    string
	projectResolver       ProjectResolver
	authorizer            ProjectAuthorizer
	authorizationObserver ProjectAuthorizationObserver
	assetVisibility       AssetVisibilityPolicy
}

// ProjectResolver applies deployment-level project selection before semantic
// services access a manifest namespace.
type ProjectResolver interface {
	ResolveProject(explicit string) (string, error)
}

func NewDiscoveryService(store *manifest.Store) *DiscoveryService {
	return &DiscoveryService{manifest: store, authorizer: ScopeProjectAuthorizer{}, assetVisibility: AllVisibleAssetPolicy{}}
}

// PrepareIndex builds the immutable discovery read model before a runtime
// generation is published. Direct embedders may omit it; discovery then builds
// lazily on first use and rebuilds only if their manifest.Store is swapped.
func (s *DiscoveryService) PrepareIndex() error {
	_, err := s.discoveryIndex()
	return err
}

// WithGenerationIdentity binds pagination to one Project runtime generation.
// It is configured before publication and remains immutable while served.
func (s *DiscoveryService) WithGenerationIdentity(projectID string, generation uint64) *DiscoveryService {
	if s != nil {
		s.generationIdentity = strings.TrimSpace(projectID) + ":" + fmt.Sprint(generation)
	}
	return s
}

func (s *DiscoveryService) discoveryIndex() (*discoveryIndex, error) {
	if s == nil || s.manifest == nil {
		return nil, fmt.Errorf("discovery service is not configured")
	}
	semanticManifest := s.manifest.Current()
	if cached := s.index.value.Load(); cached != nil && cached.manifest == semanticManifest {
		return cached, nil
	}
	s.index.mu.Lock()
	defer s.index.mu.Unlock()
	if cached := s.index.value.Load(); cached != nil && cached.manifest == semanticManifest {
		return cached, nil
	}
	index, err := buildDiscoveryIndex(semanticManifest)
	if err != nil {
		return nil, err
	}
	s.index.value.Store(index)
	return index, nil
}

func (s *DiscoveryService) WithProjectResolver(resolver ProjectResolver) *DiscoveryService {
	if s != nil {
		s.projectResolver = resolver
	}
	return s
}

func (s *DiscoveryService) WithProjectAuthorizer(authorizer ProjectAuthorizer) *DiscoveryService {
	if s != nil {
		s.authorizer = authorizer
	}
	return s
}

func (s *DiscoveryService) WithProjectAuthorizationObserver(observer ProjectAuthorizationObserver) *DiscoveryService {
	if s != nil {
		s.authorizationObserver = observer
	}
	return s
}

func (s *DiscoveryService) ResolveProject(project string) (string, error) {
	project = strings.TrimSpace(project)
	if s != nil && s.projectResolver != nil {
		return s.projectResolver.ResolveProject(project)
	}
	return project, nil
}

// ResolveAuthorizedProject applies canonical project selection before the one
// shared project/action decision. No semantic inventory is opened first.
func (s *DiscoveryService) ResolveAuthorizedProject(ctx context.Context, project string, action ProjectAction) (string, error) {
	resolved, err := s.ResolveProject(project)
	if err != nil {
		return "", err
	}
	if err := authorizeProject(ctx, s.authorizer, s.authorizationObserver, resolved, action); err != nil {
		return "", err
	}
	return resolved, nil
}

func (s *DiscoveryService) CanAccessProject(ctx context.Context, project string, action ProjectAction) bool {
	if s == nil {
		return false
	}
	return canAuthorizeProject(ctx, s.authorizer, s.authorizationObserver, project, action)
}

const (
	DefaultSearchLimit = 20
	MaxSearchLimit     = 100
)

type SearchSemanticsRequest struct {
	Project string      `json:"project"`
	Query   string      `json:"query"`
	Model   string      `json:"model,omitempty"`
	Kinds   []AssetKind `json:"kinds,omitempty"`
	Limit   int         `json:"limit,omitempty"`
}

type GetModelRequest struct {
	Project string `json:"project"`
	Model   string `json:"model"`
}

type GetMetricRequest struct {
	Project string `json:"project"`
	Model   string `json:"model"`
	Metric  string `json:"metric"`
}

type GetMetricDimensionsRequest struct {
	Project string `json:"project"`
	Model   string `json:"model"`
	Metric  string `json:"metric"`
}

type GetDimensionRequest struct {
	Project   string `json:"project"`
	Model     string `json:"model"`
	Dimension string `json:"dimension"`
}

type GetRelationshipsRequest struct {
	Project string `json:"project"`
	Model   string `json:"model"`
}

type DimensionResponse struct {
	Dataset string       `json:"dataset"`
	Field   *ossie.Field `json:"field"`
}

type RelationshipsResponse struct {
	Relationships []*ossie.Relationship `json:"relationships"`
}

func (s *DiscoveryService) SearchSemantics(ctx context.Context, req SearchSemanticsRequest) (*SearchResult, error) {
	project, err := s.ResolveAuthorizedProject(ctx, req.Project, ProjectActionDiscover)
	if err != nil {
		return nil, err
	}
	req.Project = project
	return s.searchWithRequest(ctx, req)
}

func (s *DiscoveryService) GetModel(ctx context.Context, req GetModelRequest) (*ossie.SemanticModel, error) {
	project, err := s.ResolveAuthorizedProject(ctx, req.Project, ProjectActionDiscover)
	if err != nil {
		return nil, err
	}
	req.Project = project
	return s.getModel(ctx, req.Project, req.Model)
}

func (s *DiscoveryService) GetMetric(ctx context.Context, req GetMetricRequest) (*ossie.Metric, error) {
	project, err := s.ResolveAuthorizedProject(ctx, req.Project, ProjectActionDiscover)
	if err != nil {
		return nil, err
	}
	req.Project = project
	return s.getMetric(ctx, req.Project, req.Model, req.Metric)
}

func (s *DiscoveryService) GetMetricDimensions(ctx context.Context, req GetMetricDimensionsRequest) (*MetricDimensionCompatibilityResult, error) {
	project, err := s.ResolveAuthorizedProject(ctx, req.Project, ProjectActionDiscover)
	if err != nil {
		return nil, err
	}
	req.Project = project
	return s.getMetricDimensions(ctx, req.Project, req.Model, req.Metric)
}

func (s *DiscoveryService) GetDimension(ctx context.Context, req GetDimensionRequest) (*DimensionResponse, error) {
	project, err := s.ResolveAuthorizedProject(ctx, req.Project, ProjectActionDiscover)
	if err != nil {
		return nil, err
	}
	req.Project = project
	handle, err := s.getDimension(ctx, req.Project, req.Model, req.Dimension)
	if err != nil {
		return nil, err
	}
	return &DimensionResponse{Dataset: handle.Dataset, Field: handle.Field}, nil
}

func (s *DiscoveryService) GetRelationships(ctx context.Context, req GetRelationshipsRequest) (*RelationshipsResponse, error) {
	project, err := s.ResolveAuthorizedProject(ctx, req.Project, ProjectActionDiscover)
	if err != nil {
		return nil, err
	}
	req.Project = project
	relationships, err := s.getRelationships(ctx, req.Project, req.Model)
	if err != nil {
		return nil, err
	}
	return &RelationshipsResponse{Relationships: relationships}, nil
}
