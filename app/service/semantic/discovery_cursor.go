package semantic

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/meaningforge/metis/app/auth"
	"github.com/meaningforge/metis/serrors"
)

const discoveryCursorVersion = 1

// AssetVisibilityScopeProvider lets a mutable policy publish a bounded cache
// and pagination scope. Policies whose decisions can change independently of
// a semantic generation should implement this interface and change the scope
// whenever those decisions change.
type AssetVisibilityScopeProvider interface {
	AssetVisibilityScope(context.Context, AssetVisibilityScopeRequest) string
}

type AssetVisibilityScopeRequest struct {
	Principal *auth.Principal
	ProjectID string
	Action    ProjectAction
}

func (AllVisibleAssetPolicy) AssetVisibilityScope(context.Context, AssetVisibilityScopeRequest) string {
	return "all-visible:v1"
}

type discoveryCursor struct {
	Version    int    `json:"v"`
	Kind       string `json:"k"`
	Project    string `json:"p"`
	Query      string `json:"q"`
	Visibility string `json:"s"`
	Generation string `json:"g"`
	Offset     int    `json:"o,omitempty"`
	After      string `json:"a,omitempty"`
}

type discoveryCursorBinding struct {
	Kind       string
	Project    string
	Query      string
	Visibility string
	Generation string
}

func (s *DiscoveryService) cursorBinding(ctx context.Context, kind, project string, query any) discoveryCursorBinding {
	return discoveryCursorBinding{
		Kind:       kind,
		Project:    stableCursorHash(strings.TrimSpace(project)),
		Query:      stableCursorHash(query),
		Visibility: s.visibilityScope(ctx, project, ProjectActionDiscover),
		Generation: s.cursorGeneration(),
	}
}

func (s *DiscoveryService) cursorGeneration() string {
	if s != nil && strings.TrimSpace(s.generationIdentity) != "" {
		return stableCursorHash(s.generationIdentity)
	}
	if s != nil && s.manifest != nil {
		if current := s.manifest.Current(); current != nil {
			return stableCursorHash(current.Digest)
		}
	}
	return stableCursorHash("")
}

func (s *DiscoveryService) visibilityScope(ctx context.Context, project string, action ProjectAction) string {
	principal, _ := auth.PrincipalFromContext(ctx)
	request := AssetVisibilityScopeRequest{Principal: principal, ProjectID: strings.TrimSpace(project), Action: action}
	policyScope := ""
	if provider, ok := s.assetVisibility.(AssetVisibilityScopeProvider); ok {
		policyScope = strings.TrimSpace(provider.AssetVisibilityScope(ctx, request))
	}
	// The principal remains part of the binding even when a policy supplies a
	// version: a cursor can never be moved to a different caller accidentally.
	identity := struct {
		Policy     string   `json:"policy"`
		Scope      string   `json:"scope"`
		Tenant     string   `json:"tenant,omitempty"`
		Subject    string   `json:"subject,omitempty"`
		Credential string   `json:"credential,omitempty"`
		Scopes     []string `json:"scopes,omitempty"`
	}{Policy: fmt.Sprintf("%T", s.assetVisibility), Scope: policyScope}
	if principal != nil {
		identity.Tenant = principal.TenantID
		identity.Subject = principal.SubjectID
		identity.Credential = principal.APIKeyID
		identity.Scopes = append([]string(nil), principal.Scopes...)
		sort.Strings(identity.Scopes)
	}
	return stableCursorHash(identity)
}

func stableCursorHash(value any) string {
	encoded, _ := json.Marshal(value)
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:12])
}

func encodeDiscoveryCursor(binding discoveryCursorBinding, offset int, after string) string {
	cursor := discoveryCursor{
		Version: discoveryCursorVersion, Kind: binding.Kind, Project: binding.Project,
		Query: binding.Query, Visibility: binding.Visibility, Generation: binding.Generation,
		Offset: offset, After: after,
	}
	encoded, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(encoded)
}

func decodeDiscoveryCursor(raw string, binding discoveryCursorBinding) (discoveryCursor, error) {
	if strings.TrimSpace(raw) == "" {
		return discoveryCursor{}, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(raw))
	if err != nil {
		return discoveryCursor{}, invalidDiscoveryCursor()
	}
	var cursor discoveryCursor
	if json.Unmarshal(decoded, &cursor) != nil || cursor.Version != discoveryCursorVersion || cursor.Offset < 0 || cursor.Kind != binding.Kind || cursor.Project != binding.Project || cursor.Query != binding.Query {
		return discoveryCursor{}, invalidDiscoveryCursor()
	}
	if cursor.Generation != binding.Generation || cursor.Visibility != binding.Visibility {
		return discoveryCursor{}, &serrors.Error{
			Code: serrors.ErrSemanticPaginationRestart, Message: "semantic discovery pagination scope changed; restart from the first page",
		}
	}
	return cursor, nil
}

func invalidDiscoveryCursor() error {
	return &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "invalid semantic discovery cursor"}
}
