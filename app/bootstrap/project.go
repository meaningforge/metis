package bootstrap

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/meaningforge/metis/app/service/policy"
	runtimeservice "github.com/meaningforge/metis/app/service/runtime"
	service "github.com/meaningforge/metis/app/service/semantic"
	"github.com/meaningforge/metis/app/service/source"
	"github.com/meaningforge/metis/compiler"
	"github.com/meaningforge/metis/execution"
	"github.com/meaningforge/metis/execution/datasource"
	"github.com/meaningforge/metis/execution/runner"
	"github.com/meaningforge/metis/extension"
	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/planner"
	"github.com/meaningforge/metis/renderer"
	"github.com/meaningforge/metis/renderer/builtin"
	"github.com/meaningforge/metis/resolver"
)

type Runtime struct {
	Config      *execution.ProjectConfig
	Configs     map[string]*execution.ProjectConfig
	Deployment  *DeploymentConfig
	DataSources *datasource.DataSourceRegistry
	Execution   *runner.Runner
	Generations *runtimeservice.Manager
	// The fields below expose the cold-bootstrap generation for legacy
	// embedders. Reload-aware callers must capture Current() once per request.
	SemanticManifest *manifest.SemanticManifest
	SemanticGraph    *manifest.SemanticGraph
	Discovery        *service.DiscoveryService
	Compile          *service.CompileService
	QueryMetrics     *service.QueryMetricsService
	DimensionValues  *service.DimensionValuesService
	AttributeMetric  *service.AttributeMetricService
	CompareMetrics   *service.CompareMetricsService
}

type ProjectRuntime = Runtime

// Current returns one Project's active immutable semantic generation.
func (r *Runtime) Current(projectID string) *runtimeservice.Generation {
	if r == nil || r.Generations == nil {
		return nil
	}
	return r.Generations.Current(projectID)
}

// WithProjectAuthorizationObserver attaches one bounded decision observer to
// every project-scoped service in this runtime.
func (r *Runtime) WithProjectAuthorizationObserver(observer service.ProjectAuthorizationObserver) *Runtime {
	if r == nil {
		return r
	}
	if r.Generations != nil {
		r.Generations.WithAuthorizationObserver(observer)
		r.Generations.Configure(func(generation *runtimeservice.Generation) {
			generation.Discovery.WithProjectAuthorizationObserver(observer)
			generation.Compile.WithProjectAuthorizationObserver(observer)
			generation.QueryMetrics.WithProjectAuthorizationObserver(observer)
			generation.DimensionValues.WithProjectAuthorizationObserver(observer)
			generation.AttributeMetric.WithProjectAuthorizationObserver(observer)
			generation.CompareMetrics.WithProjectAuthorizationObserver(observer)
		})
	}
	if r.Discovery != nil {
		r.Discovery.WithProjectAuthorizationObserver(observer)
		r.Compile.WithProjectAuthorizationObserver(observer)
		r.QueryMetrics.WithProjectAuthorizationObserver(observer)
		r.DimensionValues.WithProjectAuthorizationObserver(observer)
		r.AttributeMetric.WithProjectAuthorizationObserver(observer)
		r.CompareMetrics.WithProjectAuthorizationObserver(observer)
	}
	return r
}

// LoadProjectRuntime preserves the focused embedder API. Without a root
// registration the project key is derived from the project-config directory.
func LoadProjectRuntime(configPath string) (*ProjectRuntime, error) {
	project := filepath.Base(filepath.Dir(configPath))
	candidate, err := source.LoadProject(project, configPath)
	if err != nil {
		return nil, err
	}
	deployment := &DeploymentConfig{Projects: map[string]ProjectRegistration{project: {Path: configPath}}}
	return assembleRuntime(
		context.Background(), map[string]*execution.ProjectConfig{project: candidate.Config},
		map[string]runtimeservice.InitialProject{project: {Manifest: candidate.Manifest, ContentDigest: candidate.Bundle.ContentDigest}},
		deployment, nil, nil, service.AllAccessProjectAuthorizer{}, nil, nil, policy.NoRestrictionDataAccessPolicy{},
	)
}

func assembleRuntime(
	ctx context.Context,
	configs map[string]*execution.ProjectConfig,
	projects map[string]runtimeservice.InitialProject,
	deployment *DeploymentConfig,
	dataSources *datasource.DataSourceRegistry,
	executionRuntime *runner.Runner,
	authorizer service.ProjectAuthorizer,
	authorizationObserver service.ProjectAuthorizationObserver,
	assetVisibilityPolicy service.AssetVisibilityPolicy,
	dataAccessPolicy policy.DataAccessPolicy,
) (*Runtime, error) {
	builder := func(ctx context.Context, semanticManifest *manifest.SemanticManifest) (runtimeservice.SemanticServices, error) {
		if err := ctx.Err(); err != nil {
			return runtimeservice.SemanticServices{}, err
		}
		return assembleSemanticServices(semanticManifest, deployment, executionRuntime, authorizer, authorizationObserver, assetVisibilityPolicy, dataAccessPolicy)
	}
	managerOptions := runtimeservice.Options{Authorizer: authorizer, AuthorizationObserver: authorizationObserver}
	manager, err := runtimeservice.NewManager(ctx, projects, deployment, builder, managerOptions)
	if err != nil {
		return nil, err
	}
	projectIDs := manager.ProjectIDs()
	manifests := make([]*manifest.SemanticManifest, 0, len(projectIDs))
	for _, projectID := range projectIDs {
		generation := manager.Current(projectID)
		if generation == nil {
			return nil, fmt.Errorf("initial semantic generation for project %q is unavailable", projectID)
		}
		manifests = append(manifests, generation.SemanticManifest)
	}
	aggregateManifest, err := manifest.MergeManifests(manifests...)
	if err != nil {
		return nil, fmt.Errorf("merge cold-bootstrap semantic manifests: %w", err)
	}
	aggregateGraph, err := manifest.BuildSemanticGraph(aggregateManifest)
	if err != nil {
		return nil, fmt.Errorf("build cold-bootstrap semantic graph: %w", err)
	}
	aggregateServices, err := assembleSemanticServices(aggregateManifest, deployment, executionRuntime, authorizer, authorizationObserver, assetVisibilityPolicy, dataAccessPolicy)
	if err != nil {
		return nil, err
	}
	var singleConfig *execution.ProjectConfig
	if len(configs) == 1 {
		for _, cfg := range configs {
			singleConfig = cfg
		}
	}
	return &Runtime{
		Config: singleConfig, Configs: configs, Deployment: deployment, DataSources: dataSources,
		Execution: executionRuntime, Generations: manager,
		SemanticManifest: aggregateManifest, SemanticGraph: aggregateGraph,
		Discovery: aggregateServices.Discovery, Compile: aggregateServices.Compile, QueryMetrics: aggregateServices.QueryMetrics,
		DimensionValues: aggregateServices.DimensionValues, AttributeMetric: aggregateServices.AttributeMetric, CompareMetrics: aggregateServices.CompareMetrics,
	}, nil
}

func assembleSemanticServices(semanticManifest *manifest.SemanticManifest, projectResolver service.ProjectResolver, executionRuntime *runner.Runner, authorizer service.ProjectAuthorizer, authorizationObserver service.ProjectAuthorizationObserver, assetVisibilityPolicy service.AssetVisibilityPolicy, dataAccessPolicy policy.DataAccessPolicy) (runtimeservice.SemanticServices, error) {
	executionResolver, ok := projectResolver.(service.ExecutionProjectResolver)
	if !ok {
		return runtimeservice.SemanticServices{}, fmt.Errorf("project resolver does not provide execution placement")
	}
	executionResolver, err := newExecutionPlacementResolver(executionResolver, semanticManifest)
	if err != nil {
		return runtimeservice.SemanticServices{}, err
	}
	store := manifest.NewStore(semanticManifest)
	discoveryService := service.NewDiscoveryService(store).
		WithProjectResolver(projectResolver).
		WithProjectAuthorizer(authorizer).
		WithProjectAuthorizationObserver(authorizationObserver)
	if assetVisibilityPolicy != nil {
		discoveryService.WithAssetVisibilityPolicy(assetVisibilityPolicy)
	}
	if err := discoveryService.PrepareIndex(); err != nil {
		return runtimeservice.SemanticServices{}, fmt.Errorf("build semantic discovery index: %w", err)
	}
	semanticResolver := resolver.New(store)
	semanticPlanner := planner.New()
	renderers, err := renderer.NewRegistry(builtin.Renderers()...)
	if err != nil {
		return runtimeservice.SemanticServices{}, fmt.Errorf("create built-in Renderer registry: %w", err)
	}
	renderers.Freeze()
	compiler := compiler.NewCompiler(renderers)
	extensionCapabilities := extension.NewRegistry()
	for _, dialect := range renderers.Dialects() {
		if err := extensionCapabilities.Register(extension.MetricScaleRegistration(extension.RendererContext{Dialect: string(dialect)})); err != nil {
			return runtimeservice.SemanticServices{}, err
		}
	}
	compileService := service.NewCompileService(semanticResolver, semanticPlanner, compiler).
		WithDataAccessPolicy(dataAccessPolicy).
		WithProjectResolver(projectResolver).
		WithProjectAuthorizer(authorizer).
		WithProjectAuthorizationObserver(authorizationObserver).
		WithDiscovery(discoveryService).
		WithExtensionCapabilities(extension.NewInventory(), extensionCapabilities)

	queryMetrics := service.NewQueryMetricsService(compileService, executionResolver, executionRuntime).WithProjectAuthorizer(authorizer).WithProjectAuthorizationObserver(authorizationObserver)
	dimensionValues := service.NewDimensionValuesService(compileService, discoveryService, executionResolver, executionRuntime).WithProjectAuthorizer(authorizer).WithProjectAuthorizationObserver(authorizationObserver)
	attributeMetric := service.NewAttributeMetricService(compileService, discoveryService, executionResolver, executionRuntime).WithProjectAuthorizer(authorizer).WithProjectAuthorizationObserver(authorizationObserver)
	compareMetrics := service.NewCompareMetricsService(compileService, discoveryService, executionResolver, executionRuntime).WithProjectAuthorizer(authorizer).WithProjectAuthorizationObserver(authorizationObserver)
	return runtimeservice.SemanticServices{
		Discovery: discoveryService, Compile: compileService, QueryMetrics: queryMetrics,
		DimensionValues: dimensionValues, AttributeMetric: attributeMetric, CompareMetrics: compareMetrics,
	}, nil
}

// Close releases process-owned execution resources. Hosts stop accepting new
// requests and drain their transports before calling Close with a deadline.
func (r *Runtime) Close(ctx context.Context) error {
	if r == nil || r.Execution == nil {
		return nil
	}
	return r.Execution.Close(ctx)
}
