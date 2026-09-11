package runtime

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	semantic "github.com/meaningforge/metis/app/service/semantic"
	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/serrors"
)

type SemanticServices struct {
	Discovery       *semantic.DiscoveryService
	Compile         *semantic.CompileService
	QueryMetrics    *semantic.QueryMetricsService
	DimensionValues *semantic.DimensionValuesService
	AttributeMetric *semantic.AttributeMetricService
	CompareMetrics  *semantic.CompareMetricsService
}

// Generation is one Project's complete immutable semantic runtime. It shares
// only process-owned execution resources with other Project generations.
type Generation struct {
	ID               uint64
	ProjectID        string
	SemanticManifest *manifest.SemanticManifest
	SemanticGraph    *manifest.SemanticGraph
	SemanticServices
	contentDigest string
	activatedAt   time.Time
}

type InitialProject struct {
	Manifest      *manifest.SemanticManifest
	ContentDigest string
}

type Builder func(context.Context, *manifest.SemanticManifest) (SemanticServices, error)

type ProjectResolver interface {
	ResolveProject(explicit string) (string, error)
}

type Options struct {
	Authorizer            semantic.ProjectAuthorizer
	AuthorizationObserver semantic.ProjectAuthorizationObserver
	Now                   func() time.Time
}

type projectRuntime struct {
	current atomic.Pointer[Generation]
	mu      sync.Mutex
}

// Manager owns independent Project generation pointers and activation locks.
// There is no deployment-wide generation counter or atomic commit point.
type Manager struct {
	projects map[string]*projectRuntime

	configurationMu       sync.Mutex
	resolver              ProjectResolver
	builder               Builder
	authorizer            semantic.ProjectAuthorizer
	authorizationObserver semantic.ProjectAuthorizationObserver
	now                   func() time.Time
	configurers           []func(*Generation)
}

func NewManager(ctx context.Context, projects map[string]InitialProject, resolver ProjectResolver, builder Builder, options Options) (*Manager, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if len(projects) == 0 {
		return nil, fmt.Errorf("at least one initial semantic project is required")
	}
	if resolver == nil {
		return nil, fmt.Errorf("project resolver is required")
	}
	if builder == nil {
		return nil, fmt.Errorf("semantic generation builder is required")
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	manager := &Manager{
		projects: make(map[string]*projectRuntime, len(projects)), resolver: resolver,
		builder: builder, authorizer: options.Authorizer,
		authorizationObserver: options.AuthorizationObserver, now: now,
	}
	activatedAt := now().UTC()
	for projectID, project := range projects {
		projectID = strings.TrimSpace(projectID)
		if projectID == "" || project.Manifest == nil || strings.TrimSpace(project.ContentDigest) == "" {
			return nil, fmt.Errorf("initial semantic project identity is incomplete")
		}
		if len(project.Manifest.Projects) != 1 || project.Manifest.Projects[projectID] == nil {
			return nil, fmt.Errorf("initial semantic manifest is not scoped to project %q", projectID)
		}
		generation, err := manager.build(ctx, projectID, 1, project.Manifest, project.ContentDigest, activatedAt)
		if err != nil {
			return nil, err
		}
		runtime := &projectRuntime{}
		runtime.current.Store(generation)
		manager.projects[projectID] = runtime
	}
	return manager, nil
}

func (m *Manager) ProjectIDs() []string {
	if m == nil {
		return nil
	}
	projects := make([]string, 0, len(m.projects))
	for project := range m.projects {
		projects = append(projects, project)
	}
	sort.Strings(projects)
	return projects
}

func (m *Manager) ResolveProject(explicit string) (string, error) {
	if m == nil || m.resolver == nil {
		return "", semanticActivationUnavailable()
	}
	return m.resolver.ResolveProject(strings.TrimSpace(explicit))
}

func (m *Manager) Current(projectID string) *Generation {
	if m == nil {
		return nil
	}
	runtime := m.projects[projectID]
	if runtime == nil {
		return nil
	}
	return runtime.current.Load()
}

func (m *Manager) Configure(configurer func(*Generation)) {
	if m == nil || configurer == nil {
		return
	}
	m.configurationMu.Lock()
	defer m.configurationMu.Unlock()
	m.configurers = append(m.configurers, configurer)
	for _, projectID := range m.ProjectIDs() {
		configureSafely(configurer, m.Current(projectID))
	}
}

func (m *Manager) WithAuthorizationObserver(observer semantic.ProjectAuthorizationObserver) *Manager {
	if m == nil {
		return m
	}
	m.configurationMu.Lock()
	m.authorizationObserver = observer
	m.configurationMu.Unlock()
	return m
}

func (m *Manager) build(ctx context.Context, projectID string, id uint64, semanticManifest *manifest.SemanticManifest, contentDigest string, activatedAt time.Time) (*Generation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	graph, err := manifest.BuildSemanticGraph(semanticManifest)
	if err != nil {
		return nil, err
	}
	services, err := m.builder(ctx, semanticManifest)
	if err != nil {
		return nil, err
	}
	if services.Discovery != nil {
		services.Discovery.WithGenerationIdentity(projectID, id)
	}
	return &Generation{ID: id, ProjectID: projectID, SemanticManifest: semanticManifest, SemanticGraph: graph, SemanticServices: services, contentDigest: contentDigest, activatedAt: activatedAt}, nil
}

func (m *Manager) resolveAuthorized(ctx context.Context, projectID string) (string, error) {
	resolved, err := m.ResolveProject(projectID)
	if err != nil {
		return "", err
	}
	m.configurationMu.Lock()
	authorizer, observer := m.authorizer, m.authorizationObserver
	m.configurationMu.Unlock()
	if err := semantic.AuthorizeProject(ctx, authorizer, observer, resolved, semantic.ProjectActionActivate); err != nil {
		return "", err
	}
	return resolved, nil
}

func configureSafely(configurer func(*Generation), generation *Generation) {
	if configurer == nil || generation == nil {
		return
	}
	defer func() { _ = recover() }()
	configurer(generation)
}

func generationConflict(expected, current uint64) error {
	return &serrors.Error{Code: serrors.ErrSemanticGenerationConflict, Message: "project semantic generation changed before activation", Details: map[string]any{"expected_generation": expected, "current_generation": current}}
}

func semanticActivationUnavailable() error {
	return &serrors.Error{Code: serrors.ErrSemanticActivationUnavailable, Message: "semantic runtime is unavailable"}
}

type requestPins struct {
	manager *Manager
	mu      sync.Mutex
	values  map[string]*Generation
}

type requestPinsContextKey struct{}

// Pin installs a request-local, lazy Project generation cache. No Project is
// read until routing resolves it, and each resolved Project is captured once.
// Repeated pins by the same Manager preserve that snapshot. A different Manager
// installs a fresh scope without changing the parent context or its snapshots.
func (m *Manager) Pin(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if pins, ok := ctx.Value(requestPinsContextKey{}).(*requestPins); ok && pins != nil && pins.manager == m {
		return ctx
	}
	return context.WithValue(ctx, requestPinsContextKey{}, &requestPins{manager: m, values: make(map[string]*Generation)})
}

// FromContext resolves a Project and returns its request-pinned generation.
// The boolean is false when no Manager pin scope is installed.
func FromContext(ctx context.Context, explicitProject string) (*Generation, bool, error) {
	if ctx == nil {
		return nil, false, nil
	}
	pins, ok := ctx.Value(requestPinsContextKey{}).(*requestPins)
	if !ok || pins == nil || pins.manager == nil {
		return nil, false, nil
	}
	projectID, err := pins.manager.ResolveProject(explicitProject)
	if err != nil {
		return nil, true, err
	}
	pins.mu.Lock()
	defer pins.mu.Unlock()
	if generation := pins.values[projectID]; generation != nil {
		return generation, true, nil
	}
	generation := pins.manager.Current(projectID)
	if generation == nil {
		return nil, true, serrors.Internal("project semantic generation is unavailable", nil)
	}
	pins.values[projectID] = generation
	return generation, true, nil
}

// Replace installs a validated manifest in the current process. This embedding
// hook has no ReleaseID, durable store, history, rollback or network endpoint.
// expected identifies only the current in-memory snapshot for CAS. Managed
// hosts own publication, environment revisions and durable recovery externally.
func (m *Manager) Replace(ctx context.Context, projectID string, next *manifest.SemanticManifest, contentDigest string, expected uint64) (*Generation, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	projectID, err := m.resolveAuthorized(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if next == nil || len(next.Projects) != 1 || next.Projects[projectID] == nil || strings.TrimSpace(contentDigest) == "" {
		return nil, fmt.Errorf("replacement must contain exactly the resolved project and a content digest")
	}
	current := m.Current(projectID)
	if current == nil {
		return nil, semanticActivationUnavailable()
	}
	if expected != current.ID || current.ID == math.MaxUint64 {
		return nil, generationConflict(expected, current.ID)
	}
	prepared, err := m.build(ctx, projectID, current.ID+1, next, contentDigest, m.now().UTC())
	if err != nil {
		return nil, err
	}
	project := m.projects[projectID]
	project.mu.Lock()
	defer project.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if project.current.Load() != current {
		return nil, generationConflict(expected, project.current.Load().ID)
	}
	m.configurationMu.Lock()
	defer m.configurationMu.Unlock()
	if err := semantic.AuthorizeProject(ctx, m.authorizer, m.authorizationObserver, projectID, semantic.ProjectActionActivate); err != nil {
		return nil, err
	}
	for _, configure := range m.configurers {
		configureSafely(configure, prepared)
	}
	project.current.Store(prepared)
	return prepared, nil
}
