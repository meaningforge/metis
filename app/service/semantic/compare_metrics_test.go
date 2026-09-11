package semantic

import (
	"context"
	"errors"
	"io"
	"reflect"
	"sync"
	"testing"
	"time"

	analytics "github.com/meaningforge/metis/analytics/comparison"
	"github.com/meaningforge/metis/app/auth"
	"github.com/meaningforge/metis/app/observability"
	"github.com/meaningforge/metis/compiler/artifact"
	"github.com/meaningforge/metis/execution/backend"
	"github.com/meaningforge/metis/execution/datasource"
	"github.com/meaningforge/metis/execution/driver"
	"github.com/meaningforge/metis/execution/runner"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/renderer"
	"github.com/meaningforge/metis/renderer/builtin"
	"github.com/meaningforge/metis/renderer/sql"
	"github.com/meaningforge/metis/serrors"
)

func TestCompareMetricsNormalizesAndCompilesTwoOrdinaryQueries(t *testing.T) {
	service := comparisonServiceForTest(t)
	input := comparisonQueryForTest()
	input.Metrics = []string{"metric:sales.revenue", "metric:sales.conversion_rate"}
	input.Dimensions = []string{"dimension:sales.orders.region", "dimension:sales.orders.channel"}
	input.Baseline.Start = "2026-07-01T08:00:00+08:00"
	input.Baseline.End = "2026-08-01T08:00:00+08:00"
	normalized, err := service.normalizeComparison(input)
	if err != nil {
		t.Fatal(err)
	}
	if normalized.PublicBaseline.Start != "2026-07-01T00:00:00Z" || normalized.PublicBaseline.End != "2026-08-01T00:00:00Z" {
		t.Fatalf("baseline = %#v", normalized.PublicBaseline)
	}
	if got := []string{normalized.Metrics[0].Internal, normalized.Metrics[1].Internal}; !reflect.DeepEqual(got, []string{"conversion_rate", "revenue"}) {
		t.Fatalf("metrics = %v", got)
	}
	if got := []string{normalized.Dimensions[0].Internal, normalized.Dimensions[1].Internal}; !reflect.DeepEqual(got, []string{"orders.channel", "orders.region"}) {
		t.Fatalf("dimensions = %v", got)
	}
	baseline, current, descriptor, err := service.compileComparison(context.Background(), normalized, comparisonRendererForTest(t))
	if err != nil {
		t.Fatal(err)
	}
	if baseline.SqlStatement.SQL == "" || current.SqlStatement.SQL == "" || baseline.SqlStatement.SQL == current.SqlStatement.SQL && reflect.DeepEqual(baseline.SqlStatement.Parameters, current.SqlStatement.Parameters) {
		t.Fatalf("compiled periods = %#v %#v", baseline.SqlStatement, current.SqlStatement)
	}
	if !sameComparisonSchema(baseline.OutputSchema, current.OutputSchema) || len(descriptor.Metrics) != 2 || descriptor.Metrics[0].Public != "metric:sales.conversion_rate" {
		t.Fatalf("descriptor = %#v schemas=%#v/%#v", descriptor, baseline.OutputSchema, current.OutputSchema)
	}
}

func TestCompareMetricsRejectsInvalidClosedRequest(t *testing.T) {
	service := comparisonServiceForTest(t)
	base := comparisonQueryForTest()
	tests := []struct {
		name   string
		mutate func(*MetricComparisonQuery)
	}{
		{name: "no metrics", mutate: func(q *MetricComparisonQuery) { q.Metrics = nil }},
		{name: "duplicate metric", mutate: func(q *MetricComparisonQuery) { q.Metrics = append(q.Metrics, q.Metrics[0]) }},
		{name: "non canonical metric", mutate: func(q *MetricComparisonQuery) { q.Metrics[0] = "revenue" }},
		{name: "time as dimension", mutate: func(q *MetricComparisonQuery) { q.Dimensions = []string{q.TimeDimension} }},
		{name: "time as filter", mutate: func(q *MetricComparisonQuery) {
			q.Filters = []query.Filter{{Field: q.TimeDimension, Operator: query.FilterGTE, Value: "2026-01-01"}}
		}},
		{name: "period without offset", mutate: func(q *MetricComparisonQuery) { q.Baseline.Start = "2026-07-01T00:00:00" }},
		{name: "empty period", mutate: func(q *MetricComparisonQuery) { q.Baseline.End = q.Baseline.Start }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := base
			input.Metrics = append([]string(nil), base.Metrics...)
			input.Dimensions = append([]string(nil), base.Dimensions...)
			test.mutate(&input)
			if _, err := service.normalizeComparison(input); errorCode(err) != serrors.ErrInvalidQuery {
				t.Fatalf("error = %v (%s)", err, errorCode(err))
			}
		})
	}
}

func TestCompareMetricsSchemasAreCompatibleAcrossBuiltInRenderers(t *testing.T) {
	service := comparisonServiceForTest(t)
	input := comparisonQueryForTest()
	input.Metrics = []string{"metric:sales.revenue", "metric:sales.conversion_rate"}
	input.Dimensions = []string{"dimension:sales.orders.region"}
	normalized, err := service.normalizeComparison(input)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := renderer.NewRegistry(builtin.Renderers()...)
	if err != nil {
		t.Fatal(err)
	}
	for _, dialect := range []sql.SQLDialect{"DUCKDB", "DORIS", "CLICKHOUSE"} {
		selected, resolveErr := registry.Resolve(dialect)
		if resolveErr != nil {
			t.Fatal(resolveErr)
		}
		baseline, current, descriptor, compileErr := service.compileComparison(context.Background(), normalized, selected)
		if compileErr != nil {
			t.Fatalf("dialect=%s: %v", dialect, compileErr)
		}
		if !sameComparisonSchema(baseline.OutputSchema, current.OutputSchema) {
			t.Fatalf("dialect=%s period schemas diverged", dialect)
		}
		if schemaErr := analytics.ValidateEvidenceSchema(descriptor, baseline.OutputSchema); schemaErr != nil {
			t.Fatalf("dialect=%s: %v", dialect, schemaErr)
		}
	}
}

func TestCompareMetricsExecutesTwoQueriesAndReturnsTypedUnion(t *testing.T) {
	state := &comparisonDriverState{rows: [][][]any{
		{{"east", "100"}, {"south", "50"}},
		{{"east", "120"}, {"west", "80"}},
	}}
	service := executableComparisonServiceForTest(t, state, 10)
	sink := observability.NewMemorySink(2)
	service.WithObservability(observability.NewRecorder(sink))
	ctx := auth.WithPrincipal(context.Background(), &auth.Principal{Scopes: []string{auth.ScopeSemanticExecute}})
	result, err := service.CompareMetrics(ctx, comparisonQueryForTest())
	if err != nil {
		t.Fatal(err)
	}
	if result.AnalysisID == "" || len(result.Rows) != 3 || result.Rows[0].Members[0].Dimension != "dimension:sales.orders.region" {
		t.Fatalf("result = %#v", result)
	}
	if result.Rows[0].BaselinePresent != true || result.Rows[0].CurrentPresent != true || string(*result.Rows[0].Values[0].Delta) != "20" {
		t.Fatalf("continuing row = %#v", result.Rows[0])
	}
	if !result.Rows[1].BaselinePresent || result.Rows[1].CurrentPresent || result.Rows[1].Values[0].CurrentValue != nil {
		t.Fatalf("exit row = %#v", result.Rows[1])
	}
	state.mu.Lock()
	calls := state.calls
	state.mu.Unlock()
	if calls != 2 {
		t.Fatalf("physical executions = %d, want 2", calls)
	}
	observations := sink.Observations()
	if len(observations) != 1 {
		t.Fatalf("comparison observations = %#v", observations)
	}
	observation, ok := observations[0].(observability.ComparisonObservation)
	if !ok || observation.Result != observability.ResultSuccess || observation.Queries != 2 || observation.Rows != 4 || observation.Bytes == 0 {
		t.Fatalf("comparison observations = %#v", observations)
	}
}

func TestCompareMetricsFailsClosedBeforeSecondQueryWhenBudgetIsConsumed(t *testing.T) {
	state := &comparisonDriverState{rows: [][][]any{{{"east", "100"}}, {{"east", "120"}}}}
	service := executableComparisonServiceForTest(t, state, 1)
	ctx := auth.WithPrincipal(context.Background(), &auth.Principal{Scopes: []string{auth.ScopeSemanticExecute}})
	_, err := service.CompareMetrics(ctx, comparisonQueryForTest())
	if errorCode(err) != serrors.ErrQueryExecutionLimit {
		t.Fatalf("error = %v (%s)", err, errorCode(err))
	}
	state.mu.Lock()
	calls := state.calls
	state.mu.Unlock()
	if calls != 1 {
		t.Fatalf("physical executions = %d, want 1", calls)
	}
}

func TestCompareMetricsDiscardsBaselineWhenCurrentExecutionFails(t *testing.T) {
	state := &comparisonDriverState{
		rows:   [][][]any{{{"east", "100"}}},
		errors: []error{nil, errors.New("current query failed")},
	}
	service := executableComparisonServiceForTest(t, state, 10)
	ctx := auth.WithPrincipal(context.Background(), &auth.Principal{Scopes: []string{auth.ScopeSemanticExecute}})
	result, err := service.CompareMetrics(ctx, comparisonQueryForTest())
	if result != nil || errorCode(err) != serrors.ErrQueryExecutionFailed {
		t.Fatalf("result = %#v, error = %v (%s)", result, err, errorCode(err))
	}
	state.mu.Lock()
	calls := state.calls
	state.mu.Unlock()
	if calls != 2 {
		t.Fatalf("physical executions = %d, want 2", calls)
	}
}

func TestCompareMetricsRequiresExecutionScope(t *testing.T) {
	service := comparisonServiceForTest(t)
	_, err := service.CompareMetrics(context.Background(), comparisonQueryForTest())
	if errorCode(err) != serrors.ErrProjectAccessDenied {
		t.Fatalf("error = %v", err)
	}
}

func comparisonServiceForTest(t *testing.T) *CompareMetricsService {
	t.Helper()
	base := attributionServiceForTest(t)
	return NewCompareMetricsService(base.compile, base.discovery, nil, nil)
}

func comparisonRendererForTest(t *testing.T) renderer.Renderer {
	t.Helper()
	return attributionRendererForTest(t)
}

func comparisonQueryForTest() MetricComparisonQuery {
	return MetricComparisonQuery{
		ProjectID: "analytics", Metrics: []string{"metric:sales.revenue"},
		TimeDimension: "dimension:sales.orders.event_time",
		Baseline:      analytics.Period{Start: "2026-07-01T00:00:00Z", End: "2026-08-01T00:00:00Z"},
		Current:       analytics.Period{Start: "2026-08-01T00:00:00Z", End: "2026-09-01T00:00:00Z"},
		Dimensions:    []string{"dimension:sales.orders.region"},
	}
}

func executableComparisonServiceForTest(t *testing.T, state *comparisonDriverState, maxRows int64) *CompareMetricsService {
	t.Helper()
	base := comparisonServiceForTest(t)
	selected := comparisonRendererForTest(t)
	maxBytes := int64(1 << 20)
	concurrency := 1
	sources, err := datasource.NewDataSourceRegistry(map[string]datasource.DataSource{"warehouse": {Type: "doris", Policy: datasource.DataSourcePolicy{QueryTimeout: time.Second.String(), MaxRows: &maxRows, MaxBytes: &maxBytes, MaxConcurrency: &concurrency}}})
	if err != nil {
		t.Fatal(err)
	}
	backends, err := backend.NewBackendRegistry(backend.Backend{Type: "doris", Renderer: selected, DriverFactory: comparisonDriverFactory{state: state}})
	if err != nil {
		t.Fatal(err)
	}
	return NewCompareMetricsService(base.compile, base.discovery, queryMetricsProjects{}, runner.New(sources, backends, nil, nil))
}

type comparisonDriverState struct {
	mu     sync.Mutex
	calls  int
	rows   [][][]any
	errors []error
}

type comparisonDriverFactory struct{ state *comparisonDriverState }

func (f comparisonDriverFactory) DataSourceType() datasource.Type        { return "doris" }
func (f comparisonDriverFactory) ValidateConfig(map[string]string) error { return nil }
func (f comparisonDriverFactory) OpenDataSource(context.Context, driver.OpenRequest) (driver.Runtime, error) {
	return comparisonDataSourceRuntime{state: f.state}, nil
}

type comparisonDataSourceRuntime struct{ state *comparisonDriverState }

func (r comparisonDataSourceRuntime) Acquire(context.Context) (driver.Executor, error) {
	return comparisonExecutor{state: r.state}, nil
}
func (comparisonDataSourceRuntime) Close(context.Context) error { return nil }

type comparisonExecutor struct{ state *comparisonDriverState }

func (e comparisonExecutor) Execute(context.Context, *artifact.CompiledQuery) (driver.ResultStream, error) {
	e.state.mu.Lock()
	index := e.state.calls
	e.state.calls++
	var rows [][]any
	if index < len(e.state.rows) {
		rows = e.state.rows[index]
	}
	var err error
	if index < len(e.state.errors) {
		err = e.state.errors[index]
	}
	e.state.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return &comparisonStream{rows: rows}, nil
}
func (comparisonExecutor) Close() error { return nil }

type comparisonStream struct {
	rows  [][]any
	index int
}

func (s *comparisonStream) Next(context.Context) ([]any, error) {
	if s.index >= len(s.rows) {
		return nil, io.EOF
	}
	row := s.rows[s.index]
	s.index++
	return row, nil
}
func (*comparisonStream) Close() error { return nil }
