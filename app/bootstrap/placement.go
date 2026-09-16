package bootstrap

import (
	"sort"

	service "github.com/meaningforge/metis/app/service/semantic"
	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
)

// executionPlacementResolver combines deployment-owned applied source names
// with semantic-model placement declared in the immutable Ossie manifest.
// It is rebuilt for every semantic generation.
type executionPlacementResolver struct {
	service.ExecutionProjectResolver
	models   map[string]map[string]string
	projects map[string][]string
}

func newExecutionPlacementResolver(base service.ExecutionProjectResolver, semanticManifest *manifest.SemanticManifest) (*executionPlacementResolver, error) {
	result := &executionPlacementResolver{ExecutionProjectResolver: base, models: map[string]map[string]string{}, projects: map[string][]string{}}
	if semanticManifest == nil {
		return result, nil
	}
	projects := make([]string, 0, len(semanticManifest.Projects))
	for project := range semanticManifest.Projects {
		projects = append(projects, project)
	}
	sort.Strings(projects)
	for _, project := range projects {
		applied := appliedDataSources(base, project)
		if len(applied) == 0 {
			continue
		}
		allowed := make(map[string]struct{}, len(applied))
		for _, name := range applied {
			allowed[name] = struct{}{}
		}
		result.projects[project] = append([]string(nil), applied...)
		result.models[project] = map[string]string{}
		modelNames := make([]string, 0, len(semanticManifest.Projects[project].Models))
		for model := range semanticManifest.Projects[project].Models {
			modelNames = append(modelNames, model)
		}
		sort.Strings(modelNames)
		for _, model := range modelNames {
			placement, explicit, err := ossie.SemanticModelDataSource(semanticManifest.Projects[project].Models[model].Model)
			if err != nil {
				return nil, invalidDeploymentConfig("invalid semantic model DataSource placement", map[string]any{"project": project, "model": model, "cause": err.Error()})
			}
			name := placement.Name
			if !explicit && len(applied) == 1 {
				name = applied[0]
			}
			if name == "" {
				return nil, invalidDeploymentConfig("semantic model requires explicit DataSource placement", map[string]any{"project": project, "model": model})
			}
			if _, exists := allowed[name]; !exists {
				return nil, invalidDeploymentConfig("semantic model references an unapplied DataSource", map[string]any{"project": project, "model": model, "data_source": name})
			}
			result.models[project][model] = name
		}
	}
	return result, nil
}

func appliedDataSources(base service.ExecutionProjectResolver, project string) []string {
	if resolver, ok := base.(interface{ DataSourcesForProject(string) []string }); ok {
		return resolver.DataSourcesForProject(project)
	}
	name, ok := base.DataSourceForProject(project)
	if !ok {
		return nil
	}
	return []string{name}
}

func (r *executionPlacementResolver) DataSourceForModel(project, model string) (string, bool) {
	if r == nil {
		return "", false
	}
	name, ok := r.models[project][model]
	return name, ok
}

func (r *executionPlacementResolver) DataSourcesForProject(project string) []string {
	if r == nil {
		return nil
	}
	return append([]string(nil), r.projects[project]...)
}
