package bootstrap

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/meaningforge/metis/app/service/policy"
	runtimeservice "github.com/meaningforge/metis/app/service/runtime"
	service "github.com/meaningforge/metis/app/service/semantic"
	"github.com/meaningforge/metis/app/service/source"
	"github.com/meaningforge/metis/execution"
	"github.com/meaningforge/metis/execution/backend"
	"github.com/meaningforge/metis/execution/datasource"
	"github.com/meaningforge/metis/execution/runner"
	"github.com/meaningforge/metis/serrors"
	"go.yaml.in/yaml/v3"
)

// DeploymentConfig is the deployment root. Project map keys are canonical
// local project keys; semantic project files do not repeat project identity.
type DeploymentConfig struct {
	Version        int                            `yaml:"version" json:"version"`
	DefaultProject string                         `yaml:"default_project,omitempty" json:"default_project,omitempty"`
	Projects       map[string]ProjectRegistration `yaml:"projects" json:"projects"`
	DataSources    *DataSourceRegistryRef         `yaml:"data_sources,omitempty" json:"data_sources,omitempty"`
}

type ProjectRegistration struct {
	Path        string   `yaml:"path" json:"path"`
	DataSource  string   `yaml:"data_source,omitempty" json:"data_source,omitempty"`
	DataSources []string `yaml:"data_sources,omitempty" json:"data_sources,omitempty"`
}

// DataSourceRegistryRef identifies the one deployment-level DataSource file.
type DataSourceRegistryRef struct {
	Path string `yaml:"path" json:"path"`
}

type runtimeOptions struct {
	projectAuthorizerSet     bool
	assetVisibilityPolicySet bool
	dataAccessPolicy         policy.DataAccessPolicy
	backends                 *backend.BackendRegistry
	secretResolver           runner.SecretResolver
	projectAuthorizer        service.ProjectAuthorizer
	authorizationObserver    service.ProjectAuthorizationObserver
	assetVisibilityPolicy    service.AssetVisibilityPolicy
}

// WithProjectAuthorizer supplies the transport-neutral project/action policy.
// Omitting it uses the fail-closed Principal scope adapter.
func WithProjectAuthorizer(authorizer service.ProjectAuthorizer) RuntimeOption {
	return func(options *runtimeOptions) {
		options.projectAuthorizer = authorizer
		options.projectAuthorizerSet = true
	}
}

// WithLocalAllAccessProjectAuthorization opts a trusted offline runtime out of
// Principal scope checks while retaining the same project/action decision path.
func WithLocalAllAccessProjectAuthorization() RuntimeOption {
	return WithProjectAuthorizer(service.AllAccessProjectAuthorizer{})
}

func WithProjectAuthorizationObserver(observer service.ProjectAuthorizationObserver) RuntimeOption {
	return func(options *runtimeOptions) {
		options.authorizationObserver = observer
	}
}

// WithAssetVisibilityPolicy supplies the resource-level policy evaluated only
// after the shared Project/action decision. Omitting it keeps the explicit
// compatibility adapter that makes validated assets visible. Explicit nil
// policies, including typed nil adapters, cause runtime construction to fail.
func WithAssetVisibilityPolicy(policy service.AssetVisibilityPolicy) RuntimeOption {
	return func(options *runtimeOptions) {
		options.assetVisibilityPolicy = policy
		options.assetVisibilityPolicySet = true
	}
}

// RuntimeOption configures non-semantic runtime assembly dependencies.
type RuntimeOption func(*runtimeOptions)

// WithDataAccessPolicy installs the same adapter on every immutable semantic
// generation. A supplied nil is not replaced by the compatibility default.
func WithDataAccessPolicy(adapter policy.DataAccessPolicy) RuntimeOption {
	return func(options *runtimeOptions) { options.dataAccessPolicy = adapter }
}

// WithBackendRegistry supplies the type-level Backend implementations used to
// validate concrete DataSource instances and construct a query Runner.
func WithBackendRegistry(backends *backend.BackendRegistry) RuntimeOption {
	return func(options *runtimeOptions) {
		options.backends = backends
	}
}

// WithSecretResolver supplies the deployment secret boundary used while
// opening an Executor. Secret resolution is independent of platform storage.
func WithSecretResolver(resolver runner.SecretResolver) RuntimeOption {
	return func(options *runtimeOptions) {
		options.secretResolver = resolver
	}
}

func LoadRuntime(configPath string, options ...RuntimeOption) (*Runtime, error) {
	runtimeOptions := &runtimeOptions{dataAccessPolicy: policy.NoRestrictionDataAccessPolicy{}}
	for _, option := range options {
		if option != nil {
			option(runtimeOptions)
		}
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}
	config, err := loadDeploymentConfig(data)
	if err != nil {
		return nil, err
	}
	baseDir := filepath.Dir(configPath)
	dataSources, err := loadDataSources(baseDir, config, runtimeOptions.backends)
	if err != nil {
		return nil, err
	}

	keys := make([]string, 0, len(config.Projects))
	for key := range config.Projects {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	configs := make(map[string]*execution.ProjectConfig, len(keys))
	projects := make(map[string]runtimeservice.InitialProject, len(keys))
	for _, key := range keys {
		registration := config.Projects[key]
		path := strings.TrimSpace(registration.Path)
		if !filepath.IsAbs(path) {
			path = filepath.Join(baseDir, path)
		}
		candidate, err := source.LoadProject(key, filepath.Clean(path))
		if err != nil {
			return nil, fmt.Errorf("load project %q: %w", key, err)
		}
		configs[key] = candidate.Config
		projects[key] = runtimeservice.InitialProject{Manifest: candidate.Manifest, ContentDigest: candidate.Bundle.ContentDigest}
	}
	return assembleConfiguredRuntime(context.Background(), config, configs, projects, dataSources, runtimeOptions)
}

func assembleConfiguredRuntime(ctx context.Context, config *DeploymentConfig, configs map[string]*execution.ProjectConfig, projects map[string]runtimeservice.InitialProject, dataSources *datasource.DataSourceRegistry, runtimeOptions *runtimeOptions) (*Runtime, error) {
	if runtimeOptions.assetVisibilityPolicySet && isNilAssetVisibilityPolicy(runtimeOptions.assetVisibilityPolicy) {
		return nil, invalidDeploymentConfig("explicit asset visibility policy must not be nil", nil)
	}
	authorizer := runtimeOptions.projectAuthorizer
	if authorizer == nil && runtimeOptions.projectAuthorizerSet {
		return nil, invalidDeploymentConfig("explicit project authorizer is required", nil)
	}
	if authorizer == nil {
		authorizer = service.ScopeProjectAuthorizer{}
	}
	var executionRuntime *runner.Runner
	if dataSources != nil && runtimeOptions.backends != nil {
		executionRuntime = runner.New(dataSources, runtimeOptions.backends, runtimeOptions.secretResolver, nil)
	}
	runtime, err := assembleRuntime(ctx, configs, projects, config, dataSources, executionRuntime, authorizer, runtimeOptions.authorizationObserver, runtimeOptions.assetVisibilityPolicy, runtimeOptions.dataAccessPolicy)
	if err != nil {
		return nil, err
	}
	return runtime, nil
}

// An interface holding a nil pointer or function is non-nil itself, but cannot
// serve as an initialized policy adapter.
func isNilAssetVisibilityPolicy(adapter service.AssetVisibilityPolicy) bool {
	if adapter == nil {
		return true
	}
	value := reflect.ValueOf(adapter)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func loadDataSources(baseDir string, config *DeploymentConfig, backends *backend.BackendRegistry) (*datasource.DataSourceRegistry, error) {
	if config == nil || config.DataSources == nil {
		return nil, nil
	}
	path := config.DataSources.Path
	if !filepath.IsAbs(path) {
		path = filepath.Join(baseDir, path)
	}
	registry, err := datasource.LoadDataSourceRegistryFile(filepath.Clean(path))
	if err != nil {
		return nil, invalidDeploymentConfig("failed to load DataSource registry", map[string]any{"cause": err.Error()})
	}
	if err := backends.ValidateDataSources(registry); err != nil {
		return nil, invalidDeploymentConfig("invalid DataSource registry", map[string]any{"cause": err.Error()})
	}
	projects := make([]string, 0, len(config.Projects))
	for project := range config.Projects {
		projects = append(projects, project)
	}
	sort.Strings(projects)
	for _, project := range projects {
		registration := config.Projects[project]
		for _, name := range registrationDataSources(registration) {
			if _, err := registry.Resolve(name); err != nil {
				return nil, invalidDeploymentConfig("project references an unknown DataSource", map[string]any{"project": project, "data_source": name})
			}
		}
	}
	return registry, nil
}

func (r *Runtime) ProjectIDs() []string {
	if r == nil || r.Generations == nil {
		return nil
	}
	return r.Generations.ProjectIDs()
}

// ResolveProject applies the one canonical project precedence contract:
// explicit > configured default > sole project > PROJECT_REQUIRED.
func (c *DeploymentConfig) ResolveProject(explicit string) (string, error) {
	if c == nil {
		return "", invalidDeploymentConfig("deployment config is required", nil)
	}
	if key := strings.TrimSpace(explicit); key != "" {
		if _, ok := c.Projects[key]; !ok {
			return "", &serrors.Error{Code: serrors.ErrProjectNotFound, Message: "project not found", Details: map[string]any{"project": key}}
		}
		return key, nil
	}
	if key := strings.TrimSpace(c.DefaultProject); key != "" {
		return key, nil
	}
	if len(c.Projects) == 1 {
		for key := range c.Projects {
			return key, nil
		}
	}
	return "", &serrors.Error{Code: serrors.ErrProjectRequired, Message: "project is required"}
}

// DataSourceForProject returns the sole deployment-owned DataSource for one
// already resolved project. It returns false for zero or several applied
// sources and deliberately does not apply project selection or fallback.
func (c *DeploymentConfig) DataSourceForProject(project string) (string, bool) {
	if c == nil {
		return "", false
	}
	registration, ok := c.Projects[project]
	if !ok {
		return "", false
	}
	names := registrationDataSources(registration)
	if len(names) != 1 {
		return "", false
	}
	return names[0], true
}

// DataSourcesForProject returns the complete applied source set for one
// already resolved project. The returned slice is owned by the caller.
func (c *DeploymentConfig) DataSourcesForProject(project string) []string {
	if c == nil {
		return nil
	}
	registration, ok := c.Projects[project]
	if !ok {
		return nil
	}
	return registrationDataSources(registration)
}

func registrationDataSources(registration ProjectRegistration) []string {
	if registration.DataSource != "" {
		return []string{registration.DataSource}
	}
	return append([]string(nil), registration.DataSources...)
}

func loadDeploymentConfig(data []byte) (*DeploymentConfig, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var config DeploymentConfig
	if err := decoder.Decode(&config); err != nil {
		return nil, invalidDeploymentConfig("failed to parse deployment config", map[string]any{"cause": err.Error()})
	}
	if len(config.Projects) == 0 {
		return nil, invalidDeploymentConfig("deployment config must define at least one project", nil)
	}
	for key, project := range config.Projects {
		if strings.TrimSpace(key) == "" {
			return nil, invalidDeploymentConfig("project key cannot be empty", nil)
		}
		if strings.TrimSpace(project.Path) == "" {
			return nil, invalidDeploymentConfig("project manifest path is required", map[string]any{"project": key})
		}
		if project.DataSource != strings.TrimSpace(project.DataSource) {
			return nil, invalidDeploymentConfig("project DataSource reference must not have surrounding whitespace", map[string]any{"project": key})
		}
		if project.DataSource != "" && len(project.DataSources) > 0 {
			return nil, invalidDeploymentConfig("project must use either data_source or data_sources", map[string]any{"project": key})
		}
		seenSources := map[string]struct{}{}
		for _, name := range project.DataSources {
			if name == "" || name != strings.TrimSpace(name) {
				return nil, invalidDeploymentConfig("project DataSource reference must be non-empty and trimmed", map[string]any{"project": key})
			}
			if _, exists := seenSources[name]; exists {
				return nil, invalidDeploymentConfig("project data_sources must not contain duplicates", map[string]any{"project": key, "data_source": name})
			}
			seenSources[name] = struct{}{}
		}
		if (project.DataSource != "" || len(project.DataSources) > 0) && config.DataSources == nil {
			return nil, invalidDeploymentConfig("project DataSource reference requires data_sources registry", map[string]any{"project": key})
		}
	}
	if config.DataSources != nil && strings.TrimSpace(config.DataSources.Path) == "" {
		return nil, invalidDeploymentConfig("data_sources path is required", nil)
	}
	if key := strings.TrimSpace(config.DefaultProject); key != "" {
		if _, ok := config.Projects[key]; !ok {
			return nil, invalidDeploymentConfig("default_project must reference a registered project", map[string]any{"default_project": key})
		}
	}
	return &config, nil
}

func invalidDeploymentConfig(message string, details map[string]any) error {
	return &serrors.Error{Code: serrors.ErrInvalidExecutionConfig, Message: message, Details: details}
}
