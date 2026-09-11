package bootstrap_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/meaningforge/metis/app/auth"
	"github.com/meaningforge/metis/app/bootstrap"
	"github.com/meaningforge/metis/app/service/policy"
	service "github.com/meaningforge/metis/app/service/semantic"
	"github.com/meaningforge/metis/execution/backend"
	"github.com/meaningforge/metis/execution/datasource"
	"github.com/meaningforge/metis/execution/driver"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/renderer/doris"
	"github.com/meaningforge/metis/serrors"
)

type bootstrapDriverFactory struct{}

type bootstrapSecrets struct{}

func (bootstrapSecrets) ResolveSecret(context.Context, datasource.SecretRef) (string, error) {
	return "whsec_MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=", nil
}

type bootstrapDataPolicy struct{ calls int }

func (p *bootstrapDataPolicy) Evaluate(_ context.Context, req policy.Request) (policy.Decision, error) {
	p.calls++
	decision := policy.Decision{Effect: policy.Constrained}
	for _, source := range req.Sources {
		decision.Sources = append(decision.Sources, policy.SourceConstraint{Dataset: source.Dataset, RowPredicates: []policy.Predicate{{Field: policy.FieldRef{Dataset: source.Dataset, Field: "amount"}, Operator: query.FilterGT, Values: []any{int64(0)}}}})
	}
	return decision, nil
}

func (bootstrapDriverFactory) DataSourceType() datasource.Type { return "doris" }

func (bootstrapDriverFactory) OpenDataSource(context.Context, driver.OpenRequest) (driver.Runtime, error) {
	return nil, nil
}

func (bootstrapDriverFactory) ValidateConfig(config map[string]string) error {
	if host := config["host"]; strings.TrimSpace(host) != "" {
		return nil
	}
	return errors.New("host is required")
}

func TestLoadRuntimeComposesRegisteredProjectsAndCompilesExplicitDialect(t *testing.T) {
	dir := t.TempDir()
	writeProject(t, dir, "finance", "sales.ossie.yaml", salesModel)
	writeProject(t, dir, "growth", "customer.ossie.yaml", customerModel)
	mustWrite(t, filepath.Join(dir, "metis.yaml"), `
version: 1
default_project: finance
projects:
  finance:
    path: ./projects/finance/project.yaml
  growth:
    path: ./projects/growth/project.yaml
`)

	runtime, err := bootstrap.LoadRuntime(filepath.Join(dir, "metis.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.Config != nil {
		t.Fatalf("single-project Config must be nil for a multi-project runtime: %#v", runtime.Config)
	}
	if got, want := runtime.ProjectIDs(), []string{"finance", "growth"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("projects=%v want=%v", got, want)
	}
	if len(runtime.Configs) != 2 || runtime.Configs["finance"] == nil || runtime.Configs["growth"] == nil {
		t.Fatalf("configs=%#v", runtime.Configs)
	}
	if !strings.HasPrefix(runtime.SemanticManifest.Digest, "sha256:") {
		t.Fatalf("digest=%q", runtime.SemanticManifest.Digest)
	}

	agentSemantics := service.NewAgentSemanticService(runtime.Discovery)
	caller := auth.WithPrincipal(context.Background(), &auth.Principal{Scopes: []string{auth.ScopeSemanticRead, auth.ScopeSemanticCompile}})
	projects, err := agentSemantics.ListProjects(caller, service.ListProjectsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(projects.Projects) != 2 || projects.Projects[0].ProjectID != "finance" || projects.Projects[1].ProjectID != "growth" {
		t.Fatalf("list projects=%#v", projects.Projects)
	}

	for _, test := range []struct {
		project string
		model   string
		metric  string
	}{
		{project: "finance", model: "sales", metric: "total_revenue"},
		{project: "growth", model: "customer", metric: "customer_count"},
	} {
		result, err := runtime.Compile.Compile(caller, service.CompileRequest{Query: query.SemanticQuery{
			Project: test.project, Model: test.model, Metrics: []query.MetricRef{{Name: test.metric}},
		}, Dialect: "DUCKDB"})
		if err != nil {
			t.Fatalf("compile %s: %v", test.project, err)
		}
		physical := result.PhysicalQuery
		if physical.Dialect != "DUCKDB" {
			t.Fatalf("compile %s query=%#v", test.project, result.PhysicalQuery)
		}
	}

	compiled, err := runtime.Compile.Compile(caller, service.CompileRequest{Query: query.SemanticQuery{
		Model: "sales", Metrics: []query.MetricRef{{Name: "total_revenue"}},
	}, Dialect: "DUCKDB"})
	if err != nil {
		t.Fatalf("compile with default project: %v", err)
	}
	if query := compiled.PhysicalQuery; query.Dialect != "DUCKDB" {
		t.Fatalf("default-project compilation=%#v", compiled.PhysicalQuery)
	}
	search, err := runtime.Discovery.SearchSemantics(caller, service.SearchSemanticsRequest{Query: "revenue"})
	if err != nil || len(search.Matches) == 0 || search.Matches[0].Project != "finance" {
		t.Fatalf("default-project search=%#v, %v", search, err)
	}
	_, err = runtime.Compile.Compile(caller, service.CompileRequest{Query: query.SemanticQuery{
		Project: "missing", Model: "sales", Metrics: []query.MetricRef{{Name: "total_revenue"}},
	}, Dialect: "DUCKDB"})
	var semanticErr *serrors.Error
	if !errors.As(err, &semanticErr) || semanticErr.Code != serrors.ErrProjectNotFound {
		t.Fatalf("invalid explicit project error=%v", err)
	}
}

func TestDeploymentConfigResolvesProjectsDeterministically(t *testing.T) {
	config := &bootstrap.DeploymentConfig{
		DefaultProject: "finance",
		Projects: map[string]bootstrap.ProjectRegistration{
			"finance": {Path: "finance.yaml"},
			"growth":  {Path: "growth.yaml"},
		},
	}
	if got, err := config.ResolveProject("growth"); err != nil || got != "growth" {
		t.Fatalf("explicit project = %q, %v", got, err)
	}
	if got, err := config.ResolveProject(""); err != nil || got != "finance" {
		t.Fatalf("default project = %q, %v", got, err)
	}
	_, err := config.ResolveProject("missing")
	var semanticErr *serrors.Error
	if !errors.As(err, &semanticErr) || semanticErr.Code != serrors.ErrProjectNotFound {
		t.Fatalf("invalid explicit project error=%v", err)
	}

	config.DefaultProject = ""
	config.Projects = map[string]bootstrap.ProjectRegistration{"only": {Path: "only.yaml"}}
	if got, err := config.ResolveProject(""); err != nil || got != "only" {
		t.Fatalf("sole project = %q, %v", got, err)
	}
	config.Projects = map[string]bootstrap.ProjectRegistration{"one": {Path: "one.yaml"}, "two": {Path: "two.yaml"}}
	_, err = config.ResolveProject("")
	if !errors.As(err, &semanticErr) || semanticErr.Code != serrors.ErrProjectRequired {
		t.Fatalf("missing project error=%v", err)
	}
}

func TestProjectResolutionPathsCannotBypassAuthorization(t *testing.T) {
	deny := service.ProjectAuthorizerFunc(func(context.Context, service.ProjectAuthorizationRequest) service.ProjectAuthorizationDecision {
		return service.ProjectAuthorizationDecision{Effect: service.ProjectAuthorizationDeny, Reason: service.ProjectAuthorizationReasonPolicyDenied}
	})
	tests := []struct {
		name     string
		config   *bootstrap.DeploymentConfig
		explicit string
		code     serrors.ErrorCode
	}{
		{
			name: "explicit existing project",
			config: &bootstrap.DeploymentConfig{Projects: map[string]bootstrap.ProjectRegistration{
				"finance": {}, "growth": {},
			}},
			explicit: "growth", code: serrors.ErrProjectAccessDenied,
		},
		{
			name: "configured default",
			config: &bootstrap.DeploymentConfig{DefaultProject: "finance", Projects: map[string]bootstrap.ProjectRegistration{
				"finance": {}, "growth": {},
			}},
			code: serrors.ErrProjectAccessDenied,
		},
		{
			name: "sole registered project",
			config: &bootstrap.DeploymentConfig{Projects: map[string]bootstrap.ProjectRegistration{
				"finance": {},
			}},
			code: serrors.ErrProjectAccessDenied,
		},
		{
			name: "invalid explicit project never falls back",
			config: &bootstrap.DeploymentConfig{DefaultProject: "finance", Projects: map[string]bootstrap.ProjectRegistration{
				"finance": {},
			}},
			explicit: "missing", code: serrors.ErrProjectNotFound,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			discovery := service.NewDiscoveryService(nil).WithProjectResolver(test.config).WithProjectAuthorizer(deny)
			_, err := discovery.GetModel(context.Background(), service.GetModelRequest{Project: test.explicit, Model: "secret"})
			var semanticErr *serrors.Error
			if !errors.As(err, &semanticErr) || semanticErr.Code != test.code {
				t.Fatalf("error = %v, want %s", err, test.code)
			}
		})
	}
}

func TestLoadRuntimeRejectsInvalidDeploymentManifest(t *testing.T) {
	for _, test := range []struct {
		name     string
		manifest string
		contains string
	}{
		{name: "empty", manifest: "projects: {}\n", contains: "at least one project"},
		{name: "empty key", manifest: "projects: {'': {path: project.yaml}}\n", contains: "project key cannot be empty"},
		{name: "missing path", manifest: "projects: {finance: {}}\n", contains: "project manifest path is required"},
		{name: "unknown field", manifest: "projects: {finance: {path: project.yaml, extra: true}}\n", contains: "failed to parse deployment config"},
		{name: "missing default", manifest: "default_project: missing\nprojects: {finance: {path: project.yaml}}\n", contains: "default_project must reference"},
		{name: "legacy shape", manifest: "project: finance\nprojects: {finance: {path: project.yaml}}\n", contains: "failed to parse deployment config"},
		{name: "empty release store", manifest: "projects: {finance: {path: project.yaml}}\nrelease_store: {path: ''}\n", contains: "failed to parse deployment config"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			mustWrite(t, filepath.Join(dir, "metis.yaml"), test.manifest)
			_, err := bootstrap.LoadRuntime(filepath.Join(dir, "metis.yaml"))
			if err == nil || !strings.Contains(err.Error(), test.contains) {
				t.Fatalf("expected error containing %q, got %v", test.contains, err)
			}
		})
	}
}

func TestLoadRuntimeLoadsDataSourceRegistryAndValidatesProjectReferences(t *testing.T) {
	dir := t.TempDir()
	writeProject(t, dir, "finance", "sales.ossie.yaml", salesModel)
	mustWrite(t, filepath.Join(dir, "datasources.yaml"), `
doris-prod:
  type: doris
  config:
    host: doris.internal
    port: "9030"
    password: "${METIS_DORIS_PASSWORD}"
  policy:
    query_timeout: 30s
    max_rows: 100000
    max_bytes: 67108864
    max_concurrency: 8
`)
	mustWrite(t, filepath.Join(dir, "metis.yaml"), `
version: 1
projects:
  finance:
    path: ./projects/finance/project.yaml
    data_source: doris-prod
data_sources:
  path: ./datasources.yaml
`)
	backends, err := backend.NewBackendRegistry(backend.Backend{
		Type:          "doris",
		Renderer:      doris.New(),
		DriverFactory: bootstrapDriverFactory{},
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := bootstrap.LoadRuntime(filepath.Join(dir, "metis.yaml"), bootstrap.WithBackendRegistry(backends))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.Deployment == nil || runtime.Deployment.Projects["finance"].DataSource != "doris-prod" {
		t.Fatalf("deployment = %#v", runtime.Deployment)
	}
	source, err := runtime.DataSources.Resolve("doris-prod")
	if err != nil || source.Type != "doris" {
		t.Fatalf("DataSource = %#v, %v", source, err)
	}
	if runtime.Execution == nil || runtime.QueryMetrics == nil || runtime.DimensionValues == nil || runtime.CompareMetrics == nil {
		t.Fatalf("runtime execution services = execution:%#v query_metrics:%#v dimension_values:%#v compare_metrics:%#v", runtime.Execution, runtime.QueryMetrics, runtime.DimensionValues, runtime.CompareMetrics)
	}
	executionContext := auth.WithPrincipal(context.Background(), &auth.Principal{Scopes: []string{auth.ScopeSemanticRead, auth.ScopeSemanticCompile, auth.ScopeSemanticExecute}})
	projects, err := service.NewAgentSemanticService(runtime.Discovery, runtime.QueryMetrics).ListProjects(executionContext, service.ListProjectsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(projects.Projects) != 1 || !reflect.DeepEqual(projects.Projects[0].Capabilities, []service.AgentProjectCapability{service.AgentCapabilityCompileSQL, service.AgentCapabilityQueryMetrics}) {
		t.Fatalf("executable project capabilities = %#v", projects.Projects)
	}
	unauthorized, err := service.NewAgentSemanticService(runtime.Discovery, runtime.QueryMetrics).ListProjects(context.Background(), service.ListProjectsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(unauthorized.Projects) != 0 {
		t.Fatalf("unauthorized project capabilities = %#v", unauthorized.Projects)
	}
}

func TestLoadRuntimeRegistersUnavailableQueryMetricsForCompileOnlyDeployment(t *testing.T) {
	dir := t.TempDir()
	writeProject(t, dir, "finance", "sales.ossie.yaml", salesModel)
	mustWrite(t, filepath.Join(dir, "metis.yaml"), `
version: 1
projects:
  finance:
    path: ./projects/finance/project.yaml
`)
	runtime, err := bootstrap.LoadRuntime(filepath.Join(dir, "metis.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.Execution != nil || runtime.QueryMetrics == nil || runtime.DimensionValues == nil || runtime.CompareMetrics == nil {
		t.Fatalf("compile-only runtime = execution:%#v query_metrics:%#v dimension_values:%#v compare_metrics:%#v", runtime.Execution, runtime.QueryMetrics, runtime.DimensionValues, runtime.CompareMetrics)
	}
	caller := auth.WithPrincipal(context.Background(), &auth.Principal{Scopes: []string{auth.ScopeSemanticRead, auth.ScopeSemanticCompile}})
	projects, err := service.NewAgentSemanticService(runtime.Discovery, runtime.QueryMetrics).ListProjects(caller, service.ListProjectsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(projects.Projects) != 1 || !reflect.DeepEqual(projects.Projects[0].Capabilities, []service.AgentProjectCapability{service.AgentCapabilityCompileSQL}) {
		t.Fatalf("compile-only project capabilities = %#v", projects.Projects)
	}
}

func TestLoadRuntimeRejectsUnknownProjectDataSource(t *testing.T) {
	dir := t.TempDir()
	writeProject(t, dir, "finance", "sales.ossie.yaml", salesModel)
	mustWrite(t, filepath.Join(dir, "datasources.yaml"), "doris-prod:\n  type: doris\n  config:\n    host: doris.internal\n    port: \"9030\"\n  policy:\n    query_timeout: 30s\n    max_rows: 100000\n    max_bytes: 67108864\n    max_concurrency: 8\n")
	mustWrite(t, filepath.Join(dir, "metis.yaml"), "projects:\n  finance:\n    path: ./projects/finance/project.yaml\n    data_source: missing\ndata_sources:\n  path: ./datasources.yaml\n")
	backends, err := backend.NewBackendRegistry(backend.Backend{Type: "doris", Renderer: doris.New(), DriverFactory: bootstrapDriverFactory{}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = bootstrap.LoadRuntime(filepath.Join(dir, "metis.yaml"), bootstrap.WithBackendRegistry(backends))
	if err == nil || !strings.Contains(err.Error(), "project references an unknown DataSource") {
		t.Fatalf("LoadRuntime() error = %v", err)
	}
}

func TestLoadRuntimeRejectsDataSourceWithoutBackend(t *testing.T) {
	dir := t.TempDir()
	writeProject(t, dir, "finance", "sales.ossie.yaml", salesModel)
	mustWrite(t, filepath.Join(dir, "datasources.yaml"), "doris-prod:\n  type: doris\n  config:\n    host: doris.internal\n    port: \"9030\"\n  policy:\n    query_timeout: 30s\n    max_rows: 100000\n    max_bytes: 67108864\n    max_concurrency: 8\n")
	mustWrite(t, filepath.Join(dir, "metis.yaml"), "projects:\n  finance:\n    path: ./projects/finance/project.yaml\n    data_source: doris-prod\ndata_sources:\n  path: ./datasources.yaml\n")
	_, err := bootstrap.LoadRuntime(filepath.Join(dir, "metis.yaml"))
	if err == nil || !strings.Contains(err.Error(), "invalid DataSource registry") {
		t.Fatalf("LoadRuntime() error = %v", err)
	}
}

func TestLoadRuntimeValidatesProjectDataSourceReferencesDeterministically(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "datasources.yaml"), "doris-prod:\n  type: doris\n  config:\n    host: doris.internal\n    port: \"9030\"\n  policy:\n    query_timeout: 30s\n    max_rows: 100000\n    max_bytes: 67108864\n    max_concurrency: 8\n")
	mustWrite(t, filepath.Join(dir, "metis.yaml"), "projects:\n  zeta:\n    path: ./zeta.yaml\n    data_source: missing-zeta\n  alpha:\n    path: ./alpha.yaml\n    data_source: missing-alpha\ndata_sources:\n  path: ./datasources.yaml\n")
	backends, err := backend.NewBackendRegistry(backend.Backend{Type: "doris", Renderer: doris.New(), DriverFactory: bootstrapDriverFactory{}})
	if err != nil {
		t.Fatal(err)
	}
	for range 50 {
		_, err := bootstrap.LoadRuntime(filepath.Join(dir, "metis.yaml"), bootstrap.WithBackendRegistry(backends))
		var semanticErr *serrors.Error
		if !errors.As(err, &semanticErr) {
			t.Fatalf("LoadRuntime() error = %v", err)
		}
		if project := semanticErr.Details["project"]; project != "alpha" {
			t.Fatalf("error project = %v, want alpha", project)
		}
	}
}

func writeProject(t *testing.T, root, project, modelFile, model string) {
	t.Helper()
	dir := filepath.Join(root, "projects", project)
	mustWrite(t, filepath.Join(dir, modelFile), model)
	mustWrite(t, filepath.Join(dir, "project.yaml"), "semantic_sources:\n  model:\n    path: ./"+modelFile+"\n")
}

func TestRuntimeRejectsUnsupportedConfigurationAndMissingHostAuthorizer(t *testing.T) {
	for _, field := range []string{"release_store", "managed_git", "roles", "data_access_policy"} {
		t.Run(field, func(t *testing.T) {
			body := "projects: {demo: {path: project.yaml}}\n" + field + ": {}\n"
			path := filepath.Join(t.TempDir(), "metis.yaml")
			if err := os.WriteFile(path, []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := bootstrap.LoadRuntime(path); err == nil {
				t.Fatalf("runtime accepted unsupported field %s", field)
			}
		})
	}
	if _, err := bootstrap.LoadRuntime("../../examples/demo/metis.yaml", bootstrap.WithProjectAuthorizer(nil)); err == nil {
		t.Fatal("explicitly missing host authorizer accepted")
	}
}
