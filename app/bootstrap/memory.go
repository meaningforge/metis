package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/meaningforge/metis/app/service/policy"
	runtimeservice "github.com/meaningforge/metis/app/service/runtime"
	"github.com/meaningforge/metis/app/service/source"
	"github.com/meaningforge/metis/execution"
	"github.com/meaningforge/metis/execution/datasource"
)

// ProjectInput carries semantic bytes and optional applied DataSource names.
// Document paths are identities for validation; NewRuntime never opens them.
type ProjectInput struct {
	Config      *execution.ProjectConfig
	Documents   []source.SourceDocument
	DataSource  string
	DataSources []string
}

// RuntimeInput contains only process configuration, never Cloud storage,
// organization membership, environment revisions or deployment desired state.
// Sources and configuration are copied and validated during construction.
type RuntimeInput struct {
	DefaultProject string
	Projects       map[string]ProjectInput
	DataSources    map[string]datasource.DataSource
}

// NewRuntime builds a runtime from host-supplied bytes without filesystem or
// network source acquisition. It shares services, backend authority and policy
// composition with LoadRuntime. Hosts own authentication, source verification,
// durable deployment state and listener lifecycle; context scopes construction.
func NewRuntime(ctx context.Context, input RuntimeInput, options ...RuntimeOption) (*Runtime, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	opts := &runtimeOptions{dataAccessPolicy: policy.NoRestrictionDataAccessPolicy{}}
	for _, option := range options {
		if option != nil {
			option(opts)
		}
	}
	config := &DeploymentConfig{DefaultProject: input.DefaultProject, Projects: map[string]ProjectRegistration{}}
	for id, p := range input.Projects {
		config.Projects[id] = ProjectRegistration{Path: "<memory>", DataSource: p.DataSource, DataSources: append([]string(nil), p.DataSources...)}
	}
	if len(input.DataSources) > 0 {
		config.DataSources = &DataSourceRegistryRef{Path: "<memory>"}
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	// Apply the same namespace/default/reference shape rules as local bootstrap.
	config, err = loadDeploymentConfig(encoded)
	if err != nil {
		return nil, err
	}
	var registry *datasource.DataSourceRegistry
	if len(input.DataSources) > 0 {
		registry, err = datasource.NewDataSourceRegistry(input.DataSources)
		if err != nil {
			return nil, err
		}
		if err = opts.backends.ValidateDataSources(registry); err != nil {
			return nil, err
		}
	}
	keys := make([]string, 0, len(input.Projects))
	for id := range input.Projects {
		keys = append(keys, id)
	}
	sort.Strings(keys)
	configs := map[string]*execution.ProjectConfig{}
	projects := map[string]runtimeservice.InitialProject{}
	for _, id := range keys {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		p := input.Projects[id]
		for _, name := range registrationDataSources(config.Projects[id]) {
			if _, err := registry.Resolve(name); err != nil {
				return nil, invalidDeploymentConfig("project references an unknown DataSource", map[string]any{"project": id})
			}
		}
		loaded, err := source.LoadProjectDocuments(id, p.Config, p.Documents)
		if err != nil {
			return nil, fmt.Errorf("load project %q: %w", id, err)
		}
		configs[id] = loaded.Config
		projects[id] = runtimeservice.InitialProject{Manifest: loaded.Manifest, ContentDigest: loaded.Bundle.ContentDigest}
	}
	return assembleConfiguredRuntime(ctx, config, configs, projects, registry, opts)
}
