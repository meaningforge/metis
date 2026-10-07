package bootstrap

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"

	"github.com/meaningforge/metis/app/service/policy"
	runtimeservice "github.com/meaningforge/metis/app/service/runtime"
	service "github.com/meaningforge/metis/app/service/semantic"
	"github.com/meaningforge/metis/app/service/source"
	"github.com/meaningforge/metis/execution"
	"go.yaml.in/yaml/v3"
)

// LoadValidationRuntime authorizes before reading configuration, then validates
// only the selected semantic project before consulting the DataSource registry.
// Pools and credentials remain lazy until all queries have been prepared.
func LoadValidationRuntime(ctx context.Context, configPath, project string, options ...RuntimeOption) (*Runtime, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	opts := &runtimeOptions{projectAuthorizer: service.ScopeProjectAuthorizer{}, dataAccessPolicy: policy.NoRestrictionDataAccessPolicy{}}
	for _, option := range options {
		if option != nil {
			option(opts)
		}
	}
	for _, action := range []service.ProjectAction{service.ProjectActionAuthor, service.ProjectActionCompile} {
		if err := service.AuthorizeProject(ctx, opts.projectAuthorizer, opts.authorizationObserver, project, action); err != nil {
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, catalogConfigError("cannot read deployment configuration")
	}
	deployment, err := loadDeploymentConfig(data)
	if err != nil {
		return nil, catalogConfigError("invalid deployment configuration")
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var document any
	if decoder.Decode(&document) != nil || decoder.Decode(&document) != io.EOF {
		return nil, catalogConfigError("deployment configuration requires one document")
	}
	registration, ok := deployment.Projects[project]
	if !ok {
		return nil, catalogConfigError("project is unavailable")
	}
	base := filepath.Dir(configPath)
	path := registration.Path
	if !filepath.IsAbs(path) {
		path = filepath.Join(base, path)
	}
	candidate, err := source.LoadProject(project, path)
	if err != nil {
		return nil, catalogConfigError("offline candidate validation failed")
	}
	if !candidate.Quality.Publishable {
		return nil, catalogConfigError("offline candidate quality threshold failed")
	}
	// Limit assembly and routing to the explicitly selected project.
	deployment.Projects = map[string]ProjectRegistration{project: registration}
	deployment.DefaultProject = project
	sources, err := loadDataSources(base, deployment, opts.backends)
	if err != nil {
		return nil, catalogConfigError("DataSource configuration is unavailable")
	}
	return assembleConfiguredRuntime(ctx, deployment,
		map[string]*execution.ProjectConfig{project: candidate.Config},
		map[string]runtimeservice.InitialProject{project: {Manifest: candidate.Manifest, ContentDigest: candidate.Bundle.ContentDigest}}, sources, opts)
}
