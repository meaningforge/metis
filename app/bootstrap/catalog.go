package bootstrap

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/meaningforge/metis/app/service/semantic"
	"github.com/meaningforge/metis/execution/datasource"
	"github.com/meaningforge/metis/execution/runner"
	"go.yaml.in/yaml/v3"
)

// CatalogAccessAuthorizer enforces host-specific physical metadata access after
// Project author permission, but before any configuration or secret lookup.
// Nil fails closed. Trusted-local callers must opt in explicitly.
type CatalogAccessAuthorizer func(context.Context, string, string) bool

func LocalCatalogAccess(context.Context, string, string) bool { return true }

// LoadCatalogRunner is authoring-only assembly. The registered project path is
// validated as configuration but never read: a semantic model need not exist yet.
// Runtime serving still uses LoadRuntime and its normal source validation.
func LoadCatalogRunner(ctx context.Context, configPath, project, dataSource string, access CatalogAccessAuthorizer, options ...RuntimeOption) (*runner.Runner, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	opts := &runtimeOptions{projectAuthorizer: semantic.ScopeProjectAuthorizer{}}
	for _, option := range options {
		if option != nil {
			option(opts)
		}
	}
	if err := semantic.AuthorizeProject(ctx, opts.projectAuthorizer, opts.authorizationObserver, project, semantic.ProjectActionAuthor); err != nil {
		return nil, err
	}
	if access == nil || !access(ctx, project, dataSource) {
		return nil, catalogConfigError("catalog metadata access denied")
	}
	if ctx.Err() != nil {
		return nil, catalogConfigError("catalog inspection cancelled")
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, catalogConfigError("cannot read deployment configuration")
	}
	config, err := loadDeploymentConfig(data)
	if err != nil {
		return nil, catalogConfigError("invalid deployment configuration")
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var document any
	if decoder.Decode(&document) != nil || decoder.Decode(&document) != io.EOF {
		return nil, catalogConfigError("deployment configuration must contain exactly one document")
	}
	if !slices.Contains(config.DataSourcesForProject(project), dataSource) {
		return nil, catalogConfigError("selected DataSource is not applied to the Project")
	}
	if config.DataSources == nil || opts.backends == nil {
		return nil, catalogConfigError("catalog Backend configuration is unavailable")
	}
	registryPath := config.DataSources.Path
	if !filepath.IsAbs(registryPath) {
		registryPath = filepath.Join(filepath.Dir(configPath), registryPath)
	}
	registry, err := datasource.LoadDataSourceRegistryFile(registryPath)
	if err != nil {
		return nil, catalogConfigError("invalid DataSource configuration")
	}
	selected, err := registry.Resolve(dataSource)
	if err != nil {
		return nil, catalogConfigError("selected DataSource is unavailable")
	}
	if ceilings := opts.executionCeilings; ceilings != nil {
		if ceilings.MaxRows < 0 || ceilings.MaxBytes < 0 || ceilings.QueryTimeout < 0 {
			return nil, catalogConfigError("catalog execution ceilings must be nonnegative")
		}
		if ceilings.MaxRows > 0 && ceilings.MaxRows < *selected.Policy.MaxRows {
			*selected.Policy.MaxRows = ceilings.MaxRows
		}
		if ceilings.MaxBytes > 0 && ceilings.MaxBytes < *selected.Policy.MaxBytes {
			*selected.Policy.MaxBytes = ceilings.MaxBytes
		}
		timeout, err := time.ParseDuration(selected.Policy.QueryTimeout)
		if err != nil {
			return nil, catalogConfigError("invalid catalog timeout")
		}
		if ceilings.QueryTimeout > 0 && ceilings.QueryTimeout < timeout {
			selected.Policy.QueryTimeout = ceilings.QueryTimeout.String()
		}
	}
	if selected.Type != "doris" && selected.Type != "clickhouse" {
		return nil, catalogConfigError("catalog inspection supports Doris and ClickHouse")
	}
	binding, err := opts.backends.Resolve(selected.Type)
	if err != nil || binding.DriverFactory.ValidateConfig(selected.ConfigSnapshot()) != nil {
		return nil, catalogConfigError("selected DataSource Backend configuration is invalid")
	}
	// Only the selected instance becomes executable. Other registered sources
	// cannot open pools or resolve credentials through this authoring operation.
	selectedRegistry, err := datasource.NewDataSourceRegistry(map[string]datasource.DataSource{dataSource: selected})
	if err != nil {
		return nil, catalogConfigError("selected DataSource configuration is invalid")
	}
	return runner.New(selectedRegistry, opts.backends, opts.secretResolver, nil), nil
}

func catalogConfigError(message string) error {
	return &runner.ExecutionError{Code: runner.ExecutionConfig, Message: message}
}
