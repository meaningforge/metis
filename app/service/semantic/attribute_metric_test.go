package semantic

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"

	analytics "github.com/meaningforge/metis/analytics/attribution"
	"github.com/meaningforge/metis/app/auth"
	"github.com/meaningforge/metis/app/observability"
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
	"github.com/meaningforge/metis/renderer/builtin"
	"github.com/meaningforge/metis/renderer/sql"
	"github.com/meaningforge/metis/resolver"
	"github.com/meaningforge/metis/serrors"
)

func TestAttributeMetricNormalizesExactPeriodsAndBuildsProductionBundle(t *testing.T) {
	service := attributionServiceForTest(t)
	normalized, err := service.normalize(MetricAttributionQuery{
		ProjectID: "analytics", Metric: "metric:sales.revenue", TimeDimension: "dimension:sales.orders.event_time",
		Baseline:   analytics.AttributionPeriod{Start: "2026-01-01T08:00:00+08:00", End: "2026-02-01T08:00:00+08:00"},
		Current:    analytics.AttributionPeriod{Start: "2026-02-01T00:00:00Z", End: "2026-03-01T00:00:00Z"},
		Dimensions: []string{"dimension:sales.orders.channel", "dimension:sales.orders.region"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if normalized.PublicBaseline.Start != "2026-01-01T00:00:00Z" || normalized.PublicBaseline.End != "2026-02-01T00:00:00Z" {
		t.Fatalf("normalized baseline = %#v", normalized.PublicBaseline)
	}
	selected := attributionRendererForTest(t)
	compiled, strategy, refs, err := service.compileBundle(context.Background(), normalized, selected)
	if err != nil {
		t.Fatalf("%v: %#v", err, serrors.PayloadFrom(err))
	}
	if strategy != analytics.AttributionStrategyAdditive || len(compiled.Queries) != 2 || compiled.Queries[0].Dimension != "orders.channel" || refs["orders.region"] != "dimension:sales.orders.region" {
		t.Fatalf("compiled bundle = %#v strategy=%q refs=%#v", compiled, strategy, refs)
	}
	for _, query := range compiled.Queries {
		if query.SqlRenderResult.SQL == "" || len(query.OutputSchema.Columns) != 6 {
			t.Fatalf("compiled query = %#v", query)
		}
	}
}

func TestAttributeMetricRejectsNonOffsetPeriodsAndTimeDimensionReuse(t *testing.T) {
	service := attributionServiceForTest(t)
	base := MetricAttributionQuery{ProjectID: "analytics", Metric: "metric:sales.revenue", TimeDimension: "dimension:sales.orders.event_time", Baseline: analytics.AttributionPeriod{Start: "2026-01-01T00:00:00Z", End: "2026-02-01T00:00:00Z"}, Current: analytics.AttributionPeriod{Start: "2026-02-01T00:00:00Z", End: "2026-03-01T00:00:00Z"}, Dimensions: []string{"dimension:sales.orders.region"}}
	invalid := base
	invalid.Baseline.Start = "2026-01-01T00:00:00"
	if _, err := service.normalize(invalid); errorCode(err) != serrors.ErrInvalidQuery {
		t.Fatalf("period error = %v", err)
	}
	reused := base
	reused.Dimensions = []string{reused.TimeDimension}
	if _, err := service.normalize(reused); errorCode(err) != serrors.ErrInvalidQuery {
		t.Fatalf("time dimension reuse error = %v", err)
	}
	unqualifiedFilter := base
	unqualifiedFilter.Filters = []query.Filter{{Field: "orders.region", Operator: query.FilterEQ, Value: "east"}}
	if _, err := service.normalize(unqualifiedFilter); errorCode(err) != serrors.ErrInvalidQuery {
		t.Fatalf("unqualified filter error = %v", err)
	}

	missingManifest := &AttributeMetricService{discovery: &DiscoveryService{}}
	if _, err := missingManifest.normalize(base); errorCode(err) != serrors.ErrInternalInvariant {
		t.Fatalf("missing manifest error = %v", err)
	}
}

func TestAttributeMetricBuildsRatioBundleFromGovernedMetricTopology(t *testing.T) {
	service := attributionServiceForTest(t)
	normalized, err := service.normalize(MetricAttributionQuery{
		ProjectID: "analytics", Metric: "metric:sales.conversion_rate", TimeDimension: "dimension:sales.orders.event_time",
		Baseline:   analytics.AttributionPeriod{Start: "2026-01-01T00:00:00Z", End: "2026-02-01T00:00:00Z"},
		Current:    analytics.AttributionPeriod{Start: "2026-02-01T00:00:00Z", End: "2026-03-01T00:00:00Z"},
		Dimensions: []string{"dimension:sales.orders.region"},
	})
	if err != nil {
		t.Fatal(err)
	}
	compiled, strategy, _, err := service.compileBundle(context.Background(), normalized, attributionRendererForTest(t))
	if err != nil {
		t.Fatalf("%v: %#v", err, serrors.PayloadFrom(err))
	}
	if strategy != analytics.AttributionStrategyRatio || len(compiled.Queries) != 1 || len(compiled.Queries[0].OutputSchema.Columns) != 25 {
		t.Fatalf("ratio compilation = %#v strategy=%q", compiled, strategy)
	}
}

func TestAttributeMetricEvidenceSchemasAreCompatibleAcrossBuiltInRenderers(t *testing.T) {
	service := attributionServiceForTest(t)
	registry, err := renderer.NewRegistry(builtin.Renderers()...)
	if err != nil {
		t.Fatal(err)
	}
	for _, metric := range []string{"metric:sales.revenue", "metric:sales.conversion_rate"} {
		input := additiveAttributionQueryForTest()
		input.Metric = metric
		input.Dimensions = input.Dimensions[:1]
		normalized, normalizeErr := service.normalize(input)
		if normalizeErr != nil {
			t.Fatal(normalizeErr)
		}
		for _, dialect := range []sql.SQLDialect{"DUCKDB", "DORIS", "CLICKHOUSE"} {
			selected, resolveErr := registry.Resolve(dialect)
			if resolveErr != nil {
				t.Fatal(resolveErr)
			}
			compiled, strategy, refs, compileErr := service.compileBundle(context.Background(), normalized, selected)
			if compileErr != nil {
				t.Fatalf("metric=%s dialect=%s: %v", metric, dialect, compileErr)
			}
			for _, item := range compiled.Queries {
				if schemaErr := analytics.ValidateEvidenceSchema(strategy, refs[item.Dimension], item.Dimension, item.OutputSchema); schemaErr != nil {
					t.Fatalf("metric=%s dialect=%s: %v", metric, dialect, schemaErr)
				}
			}
		}
	}
}

func TestAttributeMetricExecutesCompleteBundleAndReturnsTypedResult(t *testing.T) {
	state := &attributionDriverState{}
	service := executableAttributionServiceForTest(t, state, 10)
	sink := observability.NewMemorySink(2)
	service.WithObservability(observability.NewRecorder(sink))
	ctx := auth.WithPrincipal(context.Background(), &auth.Principal{Scopes: []string{auth.ScopeSemanticExecute}})
	result, err := service.AttributeMetric(ctx, additiveAttributionQueryForTest())
	if err != nil {
		t.Fatal(err)
	}
	if result.AnalysisID == "" || result.Strategy != analytics.AttributionStrategyAdditive || len(result.Dimensions) != 2 || result.Dimensions[0].Dimension != "dimension:sales.orders.channel" {
		t.Fatalf("result = %#v", result)
	}
	state.mu.Lock()
	calls := state.calls
	state.mu.Unlock()
	if calls != 2 {
		t.Fatalf("physical executions = %d, want 2", calls)
	}
	observations := sink.Observations()
	if len(observations) != 1 {
		t.Fatalf("attribution observations = %#v", observations)
	}
	observation, ok := observations[0].(observability.AttributionObservation)
	if !ok || observation.Result != observability.ResultSuccess || observation.Queries != 2 || observation.Rows != 2 || observation.Bytes == 0 {
		t.Fatalf("attribution observation = %#v", observations[0])
	}
}

func TestAttributeMetricAppliesCumulativeRowBudgetAndReturnsNoPartialResult(t *testing.T) {
	state := &attributionDriverState{}
	service := executableAttributionServiceForTest(t, state, 1)
	ctx := auth.WithPrincipal(context.Background(), &auth.Principal{Scopes: []string{auth.ScopeSemanticExecute}})
	result, err := service.AttributeMetric(ctx, additiveAttributionQueryForTest())
	if result != nil || errorCode(err) != serrors.ErrQueryExecutionLimit {
		t.Fatalf("result=%#v error=%v", result, err)
	}
	state.mu.Lock()
	calls := state.calls
	state.mu.Unlock()
	if calls != 1 {
		t.Fatalf("physical executions = %d, want first query only", calls)
	}
}

func attributionServiceForTest(t *testing.T) *AttributeMetricService {
	t.Helper()
	doc, err := ossie.NewLoader().Load([]byte(`
version: "0.2.0.dev0"
semantic_model:
  - name: sales
    datasets:
      - name: orders
        source: analytics.orders
        fields:
          - {name: event_time, datatype: DateTime, expression: {dialects: [{dialect: ANSI_SQL, expression: orders.event_time}]}, dimension: {is_time: true}}
          - {name: region, datatype: String, expression: {dialects: [{dialect: ANSI_SQL, expression: orders.region}]}, dimension: {}}
          - {name: channel, datatype: String, expression: {dialects: [{dialect: ANSI_SQL, expression: orders.channel}]}, dimension: {}}
          - {name: amount, datatype: Decimal, expression: {dialects: [{dialect: ANSI_SQL, expression: orders.amount}]}}
          - {name: converted, datatype: Decimal, expression: {dialects: [{dialect: ANSI_SQL, expression: orders.converted}]}}
          - {name: sessions, datatype: Decimal, expression: {dialects: [{dialect: ANSI_SQL, expression: orders.sessions}]}}
    metrics:
      - name: revenue
        datatype: Decimal
        expression: {dialects: [{dialect: ANSI_SQL, expression: "SUM(orders.amount)"}]}
        custom_extensions:
          - {vendor_name: METIS, data: '{"kind":"fill","policy":"zero"}'}
          - {vendor_name: METIS, data: '{"kind":"time_binding","time_dimension":"event_time"}'}
      - name: converted
        datatype: Decimal
        expression: {dialects: [{dialect: ANSI_SQL, expression: "SUM(orders.converted)"}]}
        custom_extensions:
          - {vendor_name: METIS, data: '{"kind":"fill","policy":"zero"}'}
          - {vendor_name: METIS, data: '{"kind":"time_binding","time_dimension":"event_time"}'}
      - name: sessions
        datatype: Decimal
        expression: {dialects: [{dialect: ANSI_SQL, expression: "SUM(orders.sessions)"}]}
        custom_extensions:
          - {vendor_name: METIS, data: '{"kind":"fill","policy":"zero"}'}
          - {vendor_name: METIS, data: '{"kind":"time_binding","time_dimension":"event_time"}'}
      - name: conversion_rate
        datatype: Decimal
        expression: {dialects: [{dialect: ANSI_SQL, expression: "converted / sessions"}]}
`))
	if err != nil {
		t.Fatal(err)
	}
	semanticManifest, err := manifest.BuildProjectManifest("analytics", doc)
	if err != nil {
		t.Fatal(err)
	}
	store := manifest.NewStore(semanticManifest)
	discovery := NewDiscoveryService(store).WithProjectAuthorizer(AllAccessProjectAuthorizer{})
	renderers, err := renderer.NewRegistry(builtin.Renderers()...)
	if err != nil {
		t.Fatal(err)
	}
	compile := NewCompileService(resolver.New(store), planner.New(), compiler.NewCompiler(renderers)).WithProjectAuthorizer(AllAccessProjectAuthorizer{}).WithDiscovery(discovery)
	return NewAttributeMetricService(compile, discovery, nil, nil)
}

func attributionRendererForTest(t *testing.T) renderer.Renderer {
	t.Helper()
	registry, err := renderer.NewRegistry(builtin.Renderers()...)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := registry.Resolve("DORIS")
	if err != nil {
		t.Fatal(err)
	}
	return selected
}

func additiveAttributionQueryForTest() MetricAttributionQuery {
	return MetricAttributionQuery{ProjectID: "analytics", Metric: "metric:sales.revenue", TimeDimension: "dimension:sales.orders.event_time", Baseline: analytics.AttributionPeriod{Start: "2026-01-01T00:00:00Z", End: "2026-02-01T00:00:00Z"}, Current: analytics.AttributionPeriod{Start: "2026-02-01T00:00:00Z", End: "2026-03-01T00:00:00Z"}, Dimensions: []string{"dimension:sales.orders.region", "dimension:sales.orders.channel"}}
}

func executableAttributionServiceForTest(t *testing.T, state *attributionDriverState, maxRows int64) *AttributeMetricService {
	t.Helper()
	base := attributionServiceForTest(t)
	selected := attributionRendererForTest(t)
	maxBytes := int64(1 << 20)
	concurrency := 1
	sources, err := datasource.NewDataSourceRegistry(map[string]datasource.DataSource{"warehouse": {Type: "doris", Policy: datasource.DataSourcePolicy{QueryTimeout: time.Second.String(), MaxRows: &maxRows, MaxBytes: &maxBytes, MaxConcurrency: &concurrency}}})
	if err != nil {
		t.Fatal(err)
	}
	backends, err := backend.NewBackendRegistry(backend.Backend{Type: "doris", Renderer: selected, DriverFactory: attributionDriverFactory{state: state}})
	if err != nil {
		t.Fatal(err)
	}
	return NewAttributeMetricService(base.compile, base.discovery, queryMetricsProjects{}, runner.New(sources, backends, nil, nil))
}

type attributionDriverState struct {
	mu    sync.Mutex
	calls int
}
type attributionDriverFactory struct{ state *attributionDriverState }

func (f attributionDriverFactory) DataSourceType() datasource.Type        { return "doris" }
func (f attributionDriverFactory) ValidateConfig(map[string]string) error { return nil }
func (f attributionDriverFactory) OpenDataSource(context.Context, driver.OpenRequest) (driver.Runtime, error) {
	return attributionDataSourceRuntime{state: f.state}, nil
}

type attributionDataSourceRuntime struct{ state *attributionDriverState }

func (r attributionDataSourceRuntime) Acquire(context.Context) (driver.Executor, error) {
	return attributionExecutor{state: r.state}, nil
}
func (attributionDataSourceRuntime) Close(context.Context) error { return nil }

type attributionExecutor struct{ state *attributionDriverState }

func (e attributionExecutor) Execute(_ context.Context, compiled *artifact.CompiledQuery) (driver.ResultStream, error) {
	e.state.mu.Lock()
	e.state.calls++
	e.state.mu.Unlock()
	return &attributionStream{row: []any{"member", "1", "3", "2", "2", "100"}}, nil
}
func (attributionExecutor) Close() error { return nil }

type attributionStream struct {
	row  []any
	sent bool
}

func (s *attributionStream) Next(context.Context) ([]any, error) {
	if s.sent {
		return nil, io.EOF
	}
	s.sent = true
	return s.row, nil
}
func (*attributionStream) Close() error { return nil }

func errorCode(err error) serrors.ErrorCode {
	if semantic, ok := err.(*serrors.Error); ok {
		return semantic.Code
	}
	return ""
}
