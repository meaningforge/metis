package semantic

import (
	"context"
	"io"
	"testing"
	"time"

	comparisonanalytics "github.com/meaningforge/metis/analytics/comparison"
	"github.com/meaningforge/metis/app/auth"
	"github.com/meaningforge/metis/compiler"
	"github.com/meaningforge/metis/compiler/artifact"
	"github.com/meaningforge/metis/execution/backend"
	"github.com/meaningforge/metis/execution/datasource"
	"github.com/meaningforge/metis/execution/driver"
	"github.com/meaningforge/metis/execution/runner"
	"github.com/meaningforge/metis/manifest"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/renderer"
	"github.com/meaningforge/metis/renderer/sql"
	"github.com/meaningforge/metis/resolver"
	"github.com/meaningforge/metis/serrors"
	"github.com/meaningforge/metis/sqlplan"
)

type queryMetricsProjects struct{}

func (queryMetricsProjects) ResolveProject(explicit string) (string, error) { return "analytics", nil }
func (queryMetricsProjects) DataSourceForProject(project string) (string, bool) {
	return "warehouse", project == "analytics"
}

type queryMetricsDriver struct{}

func intPointer(value int) *int { return &value }

func (queryMetricsDriver) DataSourceType() datasource.Type        { return "doris" }
func (queryMetricsDriver) ValidateConfig(map[string]string) error { return nil }
func (queryMetricsDriver) OpenDataSource(context.Context, driver.OpenRequest) (driver.Runtime, error) {
	return queryMetricsDataSourceRuntime{}, nil
}

type queryMetricsDataSourceRuntime struct{}

func (queryMetricsDataSourceRuntime) Acquire(context.Context) (driver.Executor, error) {
	return queryMetricsExecutor{}, nil
}

func (queryMetricsDataSourceRuntime) Close(context.Context) error { return nil }

type queryMetricsExecutor struct{}

func (queryMetricsExecutor) Execute(context.Context, *artifact.CompiledQuery) (driver.ResultStream, error) {
	return &queryMetricsStream{}, nil
}
func (queryMetricsExecutor) Close() error { return nil }

type queryMetricsStream struct{ sent bool }

func (s *queryMetricsStream) Next(context.Context) ([]any, error) {
	if s.sent {
		return nil, io.EOF
	}
	s.sent = true
	return []any{"12.50"}, nil
}
func (*queryMetricsStream) Close() error { return nil }

type countingRendererResolver struct {
	renderer renderer.Renderer
	calls    int
}

func (r *countingRendererResolver) Resolve(dialect sql.SQLDialect) (renderer.Renderer, error) {
	r.calls++
	return r.renderer, nil
}

type identityRenderer struct {
	delegate          renderer.Renderer
	expressionCalls   int
	capabilitiesCalls int
	renderCalls       int
}

func (r *identityRenderer) SQLDialect() sql.SQLDialect { return r.delegate.SQLDialect() }
func (r *identityRenderer) ExpressionDialect() string {
	r.expressionCalls++
	return r.delegate.ExpressionDialect()
}
func (r *identityRenderer) Capabilities() renderer.Capabilities {
	r.capabilitiesCalls++
	return r.delegate.Capabilities()
}
func (r *identityRenderer) Render(plan *sqlplan.Plan) (sql.SQLRenderResult, error) {
	r.renderCalls++
	return r.delegate.Render(plan)
}

func TestCompilationSelectsRendererOnceAndUsesTheSameInstanceThroughout(t *testing.T) {
	doc, err := ossie.NewLoader().Load([]byte(`
version: "0.2.0.dev0"
semantic_model:
  - name: sales
    datasets:
      - name: orders
        source: analytics.orders
        fields:
          - name: amount
            datatype: Decimal
            expression:
              dialects: [{dialect: ANSI_SQL, expression: orders.amount}]
    metrics:
      - name: revenue
        datatype: Decimal
        expression:
          dialects: [{dialect: ANSI_SQL, expression: SUM(orders.amount)}]
`))
	if err != nil {
		t.Fatal(err)
	}
	semanticManifest, err := manifest.BuildProjectManifest("analytics", doc)
	if err != nil {
		t.Fatal(err)
	}
	delegate := mustRenderer(t, "DORIS")
	selected := &identityRenderer{delegate: delegate}
	lookup := &countingRendererResolver{renderer: selected}
	svc := NewCompileService(
		resolver.New(manifest.NewStore(semanticManifest)),
		planner.New(),
		compiler.NewCompiler(lookup),
	).WithProjectAuthorizer(AllAccessProjectAuthorizer{})

	compiled, err := svc.Compile(context.Background(), CompileRequest{Query: query.SemanticQuery{
		Project: "analytics", Model: "sales", Metrics: []query.MetricRef{{Name: "revenue"}},
	}, Dialect: "DORIS"})
	if err != nil {
		t.Fatal(err)
	}
	if compiled.PhysicalQuery.SQL == "" {
		t.Fatalf("physical query = %#v", compiled.PhysicalQuery)
	}
	if lookup.calls != 1 {
		t.Fatalf("Renderer registry lookups = %d, want exactly one", lookup.calls)
	}
	if selected.expressionCalls == 0 || selected.capabilitiesCalls == 0 || selected.renderCalls != 1 {
		t.Fatalf("same Renderer was not used across resolution/capabilities/rendering: expression=%d capabilities=%d render=%d",
			selected.expressionCalls, selected.capabilitiesCalls, selected.renderCalls)
	}
}

func TestQueryMetricsUsesResolvedBackendRendererWithoutRegistryLookup(t *testing.T) {
	doc, err := ossie.NewLoader().Load([]byte(`
version: "0.2.0.dev0"
semantic_model:
  - name: sales
    datasets:
      - name: orders
        source: analytics.orders
        fields:
          - name: amount
            datatype: Decimal
            expression:
              dialects: [{dialect: ANSI_SQL, expression: orders.amount}]
    metrics:
      - name: revenue
        datatype: Decimal
        expression:
          dialects: [{dialect: ANSI_SQL, expression: SUM(orders.amount)}]
`))
	if err != nil {
		t.Fatal(err)
	}
	semanticManifest, err := manifest.BuildProjectManifest("analytics", doc)
	if err != nil {
		t.Fatal(err)
	}
	delegate := mustRenderer(t, "DORIS")
	selected := &identityRenderer{delegate: delegate}
	lookup := &countingRendererResolver{renderer: selected}
	compile := NewCompileService(resolver.New(manifest.NewStore(semanticManifest)), planner.New(), compiler.NewCompiler(lookup)).WithProjectAuthorizer(AllAccessProjectAuthorizer{})

	maxRows, maxBytes := int64(10), int64(1024)
	sources, err := datasource.NewDataSourceRegistry(map[string]datasource.DataSource{
		"warehouse": {Type: "doris", Policy: datasource.DataSourcePolicy{QueryTimeout: time.Second.String(), MaxRows: &maxRows, MaxBytes: &maxBytes, MaxConcurrency: intPointer(1)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	backends, err := backend.NewBackendRegistry(backend.Backend{Type: "doris", Renderer: selected, DriverFactory: queryMetricsDriver{}})
	if err != nil {
		t.Fatal(err)
	}
	service := NewQueryMetricsService(compile, queryMetricsProjects{}, runner.New(sources, backends, nil, nil))
	ctx := auth.WithPrincipal(context.Background(), &auth.Principal{Scopes: []string{auth.ScopeSemanticExecute}})
	result, err := service.QueryMetrics(ctx, QueryMetricsRequest{Query: query.SemanticQuery{Model: "sales", Metrics: []query.MetricRef{{Name: "revenue"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if result.QueryID == "" || result.Count != 1 || len(result.Rows) != 1 {
		t.Fatalf("query result = %#v", result)
	}
	if lookup.calls != 0 {
		t.Fatalf("compile registry lookups = %d, want 0", lookup.calls)
	}
	if selected.expressionCalls == 0 || selected.capabilitiesCalls == 0 || selected.renderCalls != 1 {
		t.Fatalf("resolved Backend Renderer was not used throughout: expression=%d capabilities=%d render=%d", selected.expressionCalls, selected.capabilitiesCalls, selected.renderCalls)
	}
}

func TestQueryMetricsRequiresExecuteScope(t *testing.T) {
	service := NewQueryMetricsService(nil, nil, nil)
	ctx := auth.WithPrincipal(context.Background(), &auth.Principal{Scopes: []string{auth.ScopeSemanticCompile}})
	_, err := service.QueryMetrics(ctx, QueryMetricsRequest{Query: query.SemanticQuery{Metrics: []query.MetricRef{{Name: "revenue"}}}})
	semanticErr, ok := err.(*serrors.Error)
	if !ok || semanticErr.Code != serrors.ErrProjectAccessDenied {
		t.Fatalf("query_metrics without runtime error = %v", err)
	}
}

func TestCompareMetricsUsesOneResolvedBackendRendererForBothQueries(t *testing.T) {
	doc, err := ossie.NewLoader().Load([]byte(`
version: "0.2.0.dev0"
semantic_model:
  - name: sales
    datasets:
      - name: orders
        source: analytics.orders
        fields:
          - {name: event_time, datatype: DateTime, expression: {dialects: [{dialect: ANSI_SQL, expression: orders.event_time}]}, dimension: {is_time: true}}
          - {name: amount, datatype: Decimal, expression: {dialects: [{dialect: ANSI_SQL, expression: orders.amount}]}}
    metrics:
      - name: revenue
        datatype: Decimal
        expression: {dialects: [{dialect: ANSI_SQL, expression: SUM(orders.amount)}]}
`))
	if err != nil {
		t.Fatal(err)
	}
	semanticManifest, err := manifest.BuildProjectManifest("analytics", doc)
	if err != nil {
		t.Fatal(err)
	}
	store := manifest.NewStore(semanticManifest)
	delegate := mustRenderer(t, "DORIS")
	selected := &identityRenderer{delegate: delegate}
	lookup := &countingRendererResolver{renderer: selected}
	compile := NewCompileService(resolver.New(store), planner.New(), compiler.NewCompiler(lookup)).WithProjectAuthorizer(AllAccessProjectAuthorizer{})
	discovery := NewDiscoveryService(store).WithProjectAuthorizer(AllAccessProjectAuthorizer{})

	maxRows, maxBytes := int64(10), int64(1024)
	sources, err := datasource.NewDataSourceRegistry(map[string]datasource.DataSource{
		"warehouse": {Type: "doris", Policy: datasource.DataSourcePolicy{QueryTimeout: time.Second.String(), MaxRows: &maxRows, MaxBytes: &maxBytes, MaxConcurrency: intPointer(1)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	backends, err := backend.NewBackendRegistry(backend.Backend{Type: "doris", Renderer: selected, DriverFactory: queryMetricsDriver{}})
	if err != nil {
		t.Fatal(err)
	}
	service := NewCompareMetricsService(compile, discovery, queryMetricsProjects{}, runner.New(sources, backends, nil, nil))
	ctx := auth.WithPrincipal(context.Background(), &auth.Principal{Scopes: []string{auth.ScopeSemanticExecute}})
	result, err := service.CompareMetrics(ctx, MetricComparisonQuery{
		Metrics: []string{"metric:sales.revenue"}, TimeDimension: "dimension:sales.orders.event_time",
		Baseline: comparisonanalytics.Period{Start: "2026-07-01T00:00:00Z", End: "2026-08-01T00:00:00Z"},
		Current:  comparisonanalytics.Period{Start: "2026-08-01T00:00:00Z", End: "2026-09-01T00:00:00Z"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.AnalysisID == "" || len(result.Rows) != 1 {
		t.Fatalf("comparison result = %#v", result)
	}
	if lookup.calls != 0 {
		t.Fatalf("compile registry lookups = %d, want 0", lookup.calls)
	}
	if selected.expressionCalls == 0 || selected.capabilitiesCalls == 0 || selected.renderCalls != 2 {
		t.Fatalf("resolved Backend Renderer did not own both queries: expression=%d capabilities=%d render=%d", selected.expressionCalls, selected.capabilitiesCalls, selected.renderCalls)
	}
}
