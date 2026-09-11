package runner_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/meaningforge/metis/compiler/artifact"
	"github.com/meaningforge/metis/execution/backend"
	"github.com/meaningforge/metis/execution/datasource"
	"github.com/meaningforge/metis/execution/driver"
	execution "github.com/meaningforge/metis/execution/runner"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/renderer/doris"
	"github.com/meaningforge/metis/renderer/duckdb"
	sqlquery "github.com/meaningforge/metis/renderer/sql"
)

type runtimeDriverFactory struct {
	executor        *runtimeExecutor
	openErr         error
	opened          bool
	openedWithToken string
	openedSecretRef bool
	openedHostRef   bool
	openedHostValue string
	mutateConfig    bool
}

func (*runtimeDriverFactory) DataSourceType() datasource.Type { return "doris" }

func (*runtimeDriverFactory) ValidateConfig(map[string]string) error { return nil }

func (f *runtimeDriverFactory) OpenDataSource(_ context.Context, request driver.OpenRequest) (driver.Runtime, error) {
	f.opened = true
	reference, hasPassword, _ := datasource.ParseSecretRef(request.Config["password"])
	f.openedSecretRef = hasPassword && reference.Key != ""
	hostReference, hasHostReference, _ := datasource.ParseSecretRef(request.Config["host"])
	f.openedHostRef = hasHostReference && hostReference.Key == "METIS_HOST"
	if host, ok := request.Secrets.Value(hostReference); ok {
		f.openedHostValue = host
	}
	if token, ok := request.Secrets.Value(reference); ok {
		f.openedWithToken = token
	}
	if f.mutateConfig {
		request.Config["password"] = "mutated"
	}
	return &runtimeDataSourceRuntime{executor: f.executor}, f.openErr
}

type runtimeDataSourceRuntime struct{ executor *runtimeExecutor }

func (r *runtimeDataSourceRuntime) Acquire(context.Context) (driver.Executor, error) {
	if r == nil {
		return nil, errors.New("runtime is nil")
	}
	return r.executor, nil
}

func (r *runtimeDataSourceRuntime) Close(context.Context) error {
	if r == nil || r.executor == nil {
		return nil
	}
	return r.executor.Close()
}

type acquireFuncRuntime struct {
	acquire func(context.Context) (driver.Executor, error)
}

func (r *acquireFuncRuntime) Acquire(ctx context.Context) (driver.Executor, error) {
	return r.acquire(ctx)
}
func (*acquireFuncRuntime) Close(context.Context) error { return nil }

type executeFuncExecutor struct {
	execute func(context.Context, *artifact.CompiledQuery) (driver.ResultStream, error)
	closed  atomic.Bool
}

func (e *executeFuncExecutor) Execute(ctx context.Context, compiled *artifact.CompiledQuery) (driver.ResultStream, error) {
	return e.execute(ctx, compiled)
}
func (e *executeFuncExecutor) Close() error {
	e.closed.Store(true)
	return nil
}

type staticRuntimeFactory struct{ runtime driver.Runtime }

func (*staticRuntimeFactory) DataSourceType() datasource.Type        { return "doris" }
func (*staticRuntimeFactory) ValidateConfig(map[string]string) error { return nil }
func (f *staticRuntimeFactory) OpenDataSource(context.Context, driver.OpenRequest) (driver.Runtime, error) {
	return f.runtime, nil
}

type typedNilRuntime struct{}

func (*typedNilRuntime) Acquire(context.Context) (driver.Executor, error) {
	panic("typed-nil Runtime must not be acquired")
}
func (*typedNilRuntime) Close(context.Context) error {
	panic("typed-nil Runtime must not be closed")
}

type typedNilExecutorRuntime struct{}

func (*typedNilExecutorRuntime) Acquire(context.Context) (driver.Executor, error) {
	return (*typedNilExecutor)(nil), nil
}
func (*typedNilExecutorRuntime) Close(context.Context) error { return nil }

type typedNilExecutor struct{}

func (*typedNilExecutor) Execute(context.Context, *artifact.CompiledQuery) (driver.ResultStream, error) {
	panic("typed-nil Executor must not execute")
}
func (*typedNilExecutor) Close() error {
	panic("typed-nil Executor must not be closed")
}

type typedNilStreamRuntime struct{}

func (*typedNilStreamRuntime) Acquire(context.Context) (driver.Executor, error) {
	return typedNilStreamExecutor{}, nil
}
func (*typedNilStreamRuntime) Close(context.Context) error { return nil }

type typedNilStreamExecutor struct{}

func (typedNilStreamExecutor) Execute(context.Context, *artifact.CompiledQuery) (driver.ResultStream, error) {
	return (*typedNilStream)(nil), nil
}
func (typedNilStreamExecutor) Close() error { return nil }

type typedNilStream struct{}

func (*typedNilStream) Next(context.Context) ([]any, error) {
	panic("typed-nil ResultStream must not yield")
}
func (*typedNilStream) Close() error {
	panic("typed-nil ResultStream must not be closed")
}

type runtimeExecutor struct {
	stream       *runtimeStream
	closed       bool
	closeErr     error
	executeErr   error
	executed     bool
	seenCompiled *artifact.CompiledQuery
	mutate       func(*artifact.CompiledQuery)
}

func (e *runtimeExecutor) Execute(_ context.Context, compiled *artifact.CompiledQuery) (driver.ResultStream, error) {
	e.executed = true
	e.seenCompiled = compiled
	if e.mutate != nil {
		e.mutate(compiled)
	}
	return e.stream, e.executeErr
}

func (e *runtimeExecutor) Close() error {
	e.closed = true
	return e.closeErr
}

type runtimeStream struct {
	rows     [][]any
	index    int
	closed   bool
	closeErr error
	next     func(context.Context) ([]any, error)
}

func (s *runtimeStream) Next(ctx context.Context) ([]any, error) {
	if s.next != nil {
		return s.next(ctx)
	}
	if s.index == len(s.rows) {
		return nil, io.EOF
	}
	row := s.rows[s.index]
	s.index++
	return row, nil
}

func (s *runtimeStream) Close() error {
	s.closed = true
	return s.closeErr
}

type testSecretResolver struct {
	value   string
	err     error
	resolve func(context.Context, datasource.SecretRef) (string, error)
}

func (r testSecretResolver) ResolveSecret(ctx context.Context, reference datasource.SecretRef) (string, error) {
	if r.resolve != nil {
		return r.resolve(ctx, reference)
	}
	return r.value, r.err
}

type recordingObserver struct {
	observations []execution.ExecutionObservation
	panicOnCall  bool
}

func (o *recordingObserver) ObserveExecution(_ context.Context, observation execution.ExecutionObservation) {
	if o.panicOnCall {
		panic("observer failure")
	}
	o.observations = append(o.observations, observation)
}

func TestExecutionRuntimeResolvesSecretsBeforeOpeningAndClosesResources(t *testing.T) {
	stream := &runtimeStream{rows: [][]any{{"north"}, {"south"}}}
	executor := &runtimeExecutor{stream: stream}
	factory := &runtimeDriverFactory{executor: executor}
	observer := &recordingObserver{}
	runtime := newExecutionRuntime(t, factory, testSecretResolver{value: "secret-value"}, observer, policy(10*time.Second, 10, 1024))

	query := sqlquery.SqlStatement{Dialect: "DORIS", SQL: "SELECT region"}
	schema := artifact.OutputSchema{Columns: []artifact.OutputColumn{{Name: "region"}}}
	compiled := &artifact.CompiledQuery{SqlStatement: query, OutputSchema: schema}
	result, err := runtime.Execute(execution.WithQueryID(context.Background(), "query-123"), "doris-prod", compiled, execution.ExecutionOptions{MaxRows: 5})
	if err != nil {
		t.Fatal(err)
	}
	if !factory.opened || factory.openedWithToken != "secret-value" || !factory.openedHostRef || factory.openedHostValue != "secret-value" {
		t.Fatalf("factory opened=%t token=%q host_ref=%t host_value=%q", factory.opened, factory.openedWithToken, factory.openedHostRef, factory.openedHostValue)
	}
	seenQuery := executor.seenCompiled.SqlStatement
	if !executor.executed || executor.seenCompiled == compiled || seenQuery.SQL != query.SQL || executor.seenCompiled.OutputSchema.Columns[0].Name != "region" {
		t.Fatalf("executor invocation = %#v", executor)
	}
	if !stream.closed || !executor.closed {
		t.Fatalf("resources closed: stream=%t executor=%t", stream.closed, executor.closed)
	}
	if result.Count != 2 || len(result.Rows) != 2 || result.Bytes == 0 {
		t.Fatalf("result = %#v", result)
	}
	if len(observer.observations) != 1 || observer.observations[0].QueryID != "query-123" || observer.observations[0].Code != "" || observer.observations[0].DataSourceType != "doris" {
		t.Fatalf("observations = %#v", observer.observations)
	}
}

func TestExecutionRuntimePreservesSecretRefConfigForDriver(t *testing.T) {
	stream := &runtimeStream{rows: [][]any{{"north"}}}
	factory := &runtimeDriverFactory{executor: &runtimeExecutor{stream: stream}, mutateConfig: true}
	runtime := newExecutionRuntime(t, factory, testSecretResolver{value: "secret"}, nil, policy(time.Second, 10, 1024))
	if _, err := runtime.Execute(context.Background(), "doris-prod", compiledSQL("SELECT value"), execution.ExecutionOptions{}); err != nil {
		t.Fatal(err)
	}
	if !factory.openedSecretRef {
		t.Fatal("Driver Config lost external value reference")
	}
	resolved, err := runtime.ResolveDataSource("doris-prod")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Source.Config["host"] != "${METIS_HOST}" || resolved.Source.Config["password"] != "${METIS_PASSWORD}" {
		t.Fatalf("Driver Config mutation reached DataSource: %#v", resolved.Source.Config)
	}
}

func TestExecutionRuntimeSupportsInjectedSecretProviderWithoutPersistingValue(t *testing.T) {
	stream := &runtimeStream{rows: [][]any{{"north"}}}
	factory := &runtimeDriverFactory{executor: &runtimeExecutor{stream: stream}}
	config := map[string]string{"password": "secret://aws-secrets-manager/prod/metis/doris-password"}
	resolver := testSecretResolver{resolve: func(_ context.Context, reference datasource.SecretRef) (string, error) {
		if reference != (datasource.SecretRef{Provider: "aws-secrets-manager", Key: "prod/metis/doris-password"}) {
			return "", errors.New("unexpected secret reference")
		}
		return "vault-value", nil
	}}
	runtime := newExecutionRuntimeWithConfig(t, factory, resolver, nil, policy(time.Second, 10, 1024), config)
	if _, err := runtime.Execute(context.Background(), "doris-prod", compiledSQL("SELECT value"), execution.ExecutionOptions{}); err != nil {
		t.Fatal(err)
	}
	if factory.openedWithToken != "vault-value" {
		t.Fatalf("Driver secret value = %q", factory.openedWithToken)
	}
	resolved, err := runtime.ResolveDataSource("doris-prod")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Source.Config["password"] != "secret://aws-secrets-manager/prod/metis/doris-password" {
		t.Fatalf("DataSource Config contains a resolved value: %#v", resolved.Source.Config)
	}
}

func TestExecutionRuntimeRejectsTypedNilDriverValues(t *testing.T) {
	for _, test := range []struct {
		name  string
		value driver.Runtime
		want  execution.ExecutionErrorCode
	}{
		{name: "Runtime", value: (*typedNilRuntime)(nil), want: execution.ExecutionOpen},
		{name: "Executor", value: &typedNilExecutorRuntime{}, want: execution.ExecutionOpen},
		{name: "ResultStream", value: &typedNilStreamRuntime{}, want: execution.ExecutionDriver},
	} {
		t.Run(test.name, func(t *testing.T) {
			runtime := newExecutionRuntime(t, &staticRuntimeFactory{runtime: test.value}, testSecretResolver{value: "secret"}, nil, policy(time.Second, 1, 1024))
			_, err := runtime.Execute(context.Background(), "doris-prod", compiledSQL("SELECT value"), execution.ExecutionOptions{})
			assertExecutionCode(t, err, test.want)
		})
	}
}

func TestExecutionRuntimeFailsClosedOnLimitsWithoutPartialResult(t *testing.T) {
	stream := &runtimeStream{rows: [][]any{{"first"}, {"second"}}}
	executor := &runtimeExecutor{stream: stream}
	runtime := newExecutionRuntime(t, &runtimeDriverFactory{executor: executor}, testSecretResolver{value: "secret"}, nil, policy(time.Second, 2, 1024))

	result, err := runtime.Execute(context.Background(), "doris-prod", compiledSQL("SELECT value"), execution.ExecutionOptions{MaxRows: 1})
	assertExecutionCode(t, err, execution.ExecutionLimit)
	if result.Count != 0 || len(result.Rows) != 0 {
		t.Fatalf("limit result must not be partial: %#v", result)
	}
	if !stream.closed || !executor.closed {
		t.Fatalf("resources closed: stream=%t executor=%t", stream.closed, executor.closed)
	}
}

func TestExecutionRuntimeFailsClosedOnByteLimitAndSchemaMismatch(t *testing.T) {
	for _, test := range []struct {
		name   string
		stream *runtimeStream
		policy datasource.DataSourcePolicy
		want   execution.ExecutionErrorCode
	}{
		{
			name:   "byte limit",
			stream: &runtimeStream{rows: [][]any{{"too-large"}}},
			policy: policy(time.Second, 2, 3),
			want:   execution.ExecutionLimit,
		},
		{
			name:   "schema mismatch",
			stream: &runtimeStream{rows: [][]any{{"one", "two"}}},
			policy: policy(time.Second, 2, 1024),
			want:   execution.ExecutionDriver,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			executor := &runtimeExecutor{stream: test.stream}
			runtime := newExecutionRuntime(t, &runtimeDriverFactory{executor: executor}, testSecretResolver{value: "secret"}, nil, test.policy)
			result, err := runtime.Execute(context.Background(), "doris-prod", compiledSQL("SELECT value"), execution.ExecutionOptions{})
			assertExecutionCode(t, err, test.want)
			if result.Count != 0 || len(result.Rows) != 0 || !test.stream.closed || !executor.closed {
				t.Fatalf("result=%#v stream=%t executor=%t", result, test.stream.closed, executor.closed)
			}
		})
	}
}

func TestExecutionRuntimeRejectsSecretFailureBeforeOpening(t *testing.T) {
	executor := &runtimeExecutor{stream: &runtimeStream{}}
	factory := &runtimeDriverFactory{executor: executor}
	runtime := newExecutionRuntime(t, factory, testSecretResolver{err: errors.New("password=leaked")}, nil, policy(time.Second, 1, 1024))

	_, err := runtime.Execute(context.Background(), "doris-prod", compiledSQL("SELECT value"), execution.ExecutionOptions{})
	assertExecutionCode(t, err, execution.ExecutionSecret)
	if strings.Contains(err.Error(), "leaked") || factory.opened {
		t.Fatalf("secret failure leaked or opened factory: err=%v opened=%t", err, factory.opened)
	}
}

func TestExecutionRuntimePropagatesTimeoutAndClosesResources(t *testing.T) {
	stream := &runtimeStream{next: func(ctx context.Context) ([]any, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	executor := &runtimeExecutor{stream: stream}
	runtime := newExecutionRuntime(t, &runtimeDriverFactory{executor: executor}, testSecretResolver{value: "secret"}, nil, policy(5*time.Millisecond, 1, 1024))

	_, err := runtime.Execute(context.Background(), "doris-prod", compiledSQL("SELECT value"), execution.ExecutionOptions{})
	assertExecutionCode(t, err, execution.ExecutionTimeout)
	if !stream.closed || !executor.closed {
		t.Fatalf("resources closed: stream=%t executor=%t", stream.closed, executor.closed)
	}
}

func TestExecutionRuntimeDoesNotOpenAfterCancellation(t *testing.T) {
	executor := &runtimeExecutor{stream: &runtimeStream{}}
	factory := &runtimeDriverFactory{executor: executor}
	runtime := newExecutionRuntime(t, factory, testSecretResolver{value: "secret"}, nil, policy(time.Second, 1, 1024))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := runtime.Execute(ctx, "doris-prod", compiledSQL("SELECT value"), execution.ExecutionOptions{})
	assertExecutionCode(t, err, execution.ExecutionCancelled)
	if factory.opened || executor.closed {
		t.Fatalf("cancelled execution opened or closed resources: opened=%t closed=%t", factory.opened, executor.closed)
	}
}

func TestExecutionRuntimeDoesNotTreatCancelledEOFAsSuccess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	stream := &runtimeStream{next: func(context.Context) ([]any, error) {
		cancel()
		return nil, io.EOF
	}}
	executor := &runtimeExecutor{stream: stream}
	runtime := newExecutionRuntime(t, &runtimeDriverFactory{executor: executor}, testSecretResolver{value: "secret"}, nil, policy(time.Second, 1, 1024))

	result, err := runtime.Execute(ctx, "doris-prod", compiledSQL("SELECT value"), execution.ExecutionOptions{})
	assertExecutionCode(t, err, execution.ExecutionCancelled)
	if result.Count != 0 || !stream.closed || !executor.closed {
		t.Fatalf("cancelled EOF result=%#v stream=%t executor=%t", result, stream.closed, executor.closed)
	}
}

func TestExecutionRuntimeClosesResourcesReturnedWithErrors(t *testing.T) {
	t.Run("open error", func(t *testing.T) {
		executor := &runtimeExecutor{}
		factory := &runtimeDriverFactory{executor: executor, openErr: errors.New("open failed")}
		runtime := newExecutionRuntime(t, factory, testSecretResolver{value: "secret"}, nil, policy(time.Second, 1, 1024))

		_, err := runtime.Execute(context.Background(), "doris-prod", compiledSQL("SELECT value"), execution.ExecutionOptions{})
		assertExecutionCode(t, err, execution.ExecutionOpen)
		if !executor.closed {
			t.Fatal("Executor returned with an open error was not closed")
		}
	})

	t.Run("execute error", func(t *testing.T) {
		stream := &runtimeStream{}
		executor := &runtimeExecutor{stream: stream, executeErr: errors.New("execute failed")}
		runtime := newExecutionRuntime(t, &runtimeDriverFactory{executor: executor}, testSecretResolver{value: "secret"}, nil, policy(time.Second, 1, 1024))

		_, err := runtime.Execute(context.Background(), "doris-prod", compiledSQL("SELECT value"), execution.ExecutionOptions{})
		assertExecutionCode(t, err, execution.ExecutionDriver)
		if !stream.closed || !executor.closed {
			t.Fatalf("resources closed: stream=%t executor=%t", stream.closed, executor.closed)
		}
	})
}

func TestExecutionRuntimeAppliesDeploymentTimeoutToSecretResolution(t *testing.T) {
	resolver := testSecretResolver{resolve: func(ctx context.Context, _ datasource.SecretRef) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	}}
	runtime := newExecutionRuntime(t, &runtimeDriverFactory{}, resolver, nil, policy(5*time.Millisecond, 1, 1024))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	started := time.Now()
	_, err := runtime.Execute(ctx, "doris-prod", compiledSQL("SELECT value"), execution.ExecutionOptions{})
	assertExecutionCode(t, err, execution.ExecutionTimeout)
	if elapsed := time.Since(started); elapsed >= 500*time.Millisecond {
		t.Fatalf("secret resolution elapsed %s, want deployment timeout", elapsed)
	}
}

func TestExecutionRuntimeBoundsAcquisitionAndExecutionWithOneDeadline(t *testing.T) {
	t.Run("acquire", func(t *testing.T) {
		resource := &acquireFuncRuntime{acquire: func(ctx context.Context) (driver.Executor, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		}}
		runtime := newExecutionRuntime(t, &staticRuntimeFactory{runtime: resource}, testSecretResolver{value: "secret"}, nil, policy(5*time.Millisecond, 1, 1024))
		_, err := runtime.Execute(context.Background(), "doris-prod", compiledSQL("SELECT value"), execution.ExecutionOptions{})
		assertExecutionCode(t, err, execution.ExecutionTimeout)
	})

	t.Run("execute", func(t *testing.T) {
		executor := &executeFuncExecutor{execute: func(ctx context.Context, _ *artifact.CompiledQuery) (driver.ResultStream, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		}}
		resource := &acquireFuncRuntime{acquire: func(context.Context) (driver.Executor, error) { return executor, nil }}
		runtime := newExecutionRuntime(t, &staticRuntimeFactory{runtime: resource}, testSecretResolver{value: "secret"}, nil, policy(5*time.Millisecond, 1, 1024))
		_, err := runtime.Execute(context.Background(), "doris-prod", compiledSQL("SELECT value"), execution.ExecutionOptions{})
		assertExecutionCode(t, err, execution.ExecutionTimeout)
		if !executor.closed.Load() {
			t.Fatal("timed-out Executor was not closed")
		}
	})
}

func TestExecutionRuntimePartialStreamFailureIsAtomicAndRedacted(t *testing.T) {
	const (
		parameterValue = "sql-parameter-must-not-leak"
		rowValue       = "row-value-must-not-leak"
		driverDetail   = "endpoint-user:password@warehouse.internal"
	)
	index := 0
	stream := &runtimeStream{next: func(context.Context) ([]any, error) {
		if index == 0 {
			index++
			return []any{rowValue}, nil
		}
		if index == 1 {
			index++
			return nil, errors.New(driverDetail)
		}
		return nil, io.EOF
	}}
	executor := &runtimeExecutor{stream: stream}
	runtime := newExecutionRuntime(t, &runtimeDriverFactory{executor: executor}, testSecretResolver{value: "secret"}, nil, policy(time.Second, 10, 1024))
	compiled := compiledSQL("SELECT ?")
	query := compiled.SqlStatement
	query.Parameters = []sqlquery.QueryParameter{{Value: parameterValue}}
	compiled.SqlStatement = query

	result, err := runtime.Execute(context.Background(), "doris-prod", compiled, execution.ExecutionOptions{})
	assertExecutionCode(t, err, execution.ExecutionDriver)
	if result.Count != 0 || result.Bytes != 0 || len(result.Rows) != 0 {
		t.Fatalf("partial stream failure returned data: %#v", result)
	}
	if !stream.closed || !executor.closed {
		t.Fatalf("partial stream resources closed: stream=%t executor=%t", stream.closed, executor.closed)
	}
	for _, forbidden := range []string{parameterValue, rowValue, driverDetail, "warehouse.internal"} {
		if strings.Contains(err.Error(), forbidden) {
			t.Fatalf("execution failure leaked %q: %v", forbidden, err)
		}
	}
	if _, retryErr := runtime.Execute(context.Background(), "doris-prod", compiled, execution.ExecutionOptions{}); retryErr != nil {
		t.Fatalf("execution permit was not reusable after stream fault: %v", retryErr)
	}
}

func TestExecutionRuntimeCopiesMutableResultValues(t *testing.T) {
	buffer := []byte("a")
	container := map[string]any{"values": []any{buffer}}
	index := 0
	stream := &runtimeStream{next: func(context.Context) ([]any, error) {
		switch index {
		case 0:
			index++
			return []any{container}, nil
		case 1:
			index++
			buffer[0] = 'b'
			container["added"] = true
			return []any{container}, nil
		default:
			return nil, io.EOF
		}
	}}
	runtime := newExecutionRuntime(t, &runtimeDriverFactory{executor: &runtimeExecutor{stream: stream}}, testSecretResolver{value: "secret"}, nil, policy(time.Second, 2, 1024))

	result, err := runtime.Execute(context.Background(), "doris-prod", compiledSQL("SELECT value"), execution.ExecutionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	first := result.Rows[0][0].(map[string]any)
	values := first["values"].([]any)
	if got := string(values[0].([]byte)); got != "a" || first["added"] != nil {
		t.Fatalf("first row retained driver aliases: %#v", first)
	}
}

func TestExecutionRuntimeRejectsUnsupportedParameterBeforeOpening(t *testing.T) {
	compiled := compiledSQL("SELECT ?")
	query := compiled.SqlStatement
	query.Parameters = []sqlquery.QueryParameter{{Value: &struct{ Value string }{Value: "mutable"}}}
	compiled.SqlStatement = query
	factory := &runtimeDriverFactory{executor: &runtimeExecutor{stream: &runtimeStream{}}}
	runtime := newExecutionRuntime(t, factory, testSecretResolver{value: "secret"}, nil, policy(time.Second, 1, 1024))

	_, err := runtime.Execute(context.Background(), "doris-prod", compiled, execution.ExecutionOptions{})
	assertExecutionCode(t, err, execution.ExecutionInvalidInput)
	if factory.opened {
		t.Fatal("invalid compiler artifact opened a DriverFactory")
	}
}

func TestExecutionRuntimeRejectsUnknownDriverResultObject(t *testing.T) {
	type driverObject struct{ Value string }
	stream := &runtimeStream{rows: [][]any{{&driverObject{Value: "mutable"}}}}
	executor := &runtimeExecutor{stream: stream}
	runtime := newExecutionRuntime(t, &runtimeDriverFactory{executor: executor}, testSecretResolver{value: "secret"}, nil, policy(time.Second, 1, 1024))

	result, err := runtime.Execute(context.Background(), "doris-prod", compiledSQL("SELECT value"), execution.ExecutionOptions{})
	assertExecutionCode(t, err, execution.ExecutionResultContract)
	if result.Count != 0 || len(result.Rows) != 0 || !stream.closed || !executor.closed {
		t.Fatalf("unknown driver object escaped: result=%#v stream=%t executor=%t", result, stream.closed, executor.closed)
	}
}

func TestExecutionRuntimeNormalizesValuesUsingOutputSchema(t *testing.T) {
	schema := artifact.OutputSchema{Columns: []artifact.OutputColumn{
		{Name: "region", Datatype: ossie.DataTypeString},
		{Name: "revenue", Datatype: ossie.DataTypeDecimal},
		{Name: "orders", Datatype: ossie.DataTypeInteger},
		{Name: "ratio", Datatype: ossie.DataTypeFloat},
		{Name: "active", Datatype: ossie.DataTypeBoolean},
		{Name: "event_date", Datatype: ossie.DataTypeDate},
	}}
	compiled := &artifact.CompiledQuery{
		SqlStatement: sqlquery.SqlStatement{Dialect: "DORIS", SQL: "SELECT result"},
		OutputSchema: schema,
	}
	stream := &runtimeStream{rows: [][]any{{
		[]byte("APAC"),
		[]byte("123456789012345678.6400"),
		[]byte("42"),
		[]byte("0.125"),
		int64(1),
		[]byte("2026-08-29"),
	}}}
	runtime := newExecutionRuntime(t, &runtimeDriverFactory{executor: &runtimeExecutor{stream: stream}}, testSecretResolver{value: "secret"}, nil, policy(time.Second, 1, 1024))

	result, err := runtime.Execute(context.Background(), "doris-prod", compiled, execution.ExecutionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	want := []any{"APAC", "123456789012345678.6400", json.Number("42"), json.Number("0.125"), true, "2026-08-29"}
	if got := result.Rows[0]; !equalResultRow(got, want) {
		t.Fatalf("normalized row = %#v, want %#v", got, want)
	}
	encoded, err := json.Marshal(result.Rows[0])
	if err != nil {
		t.Fatal(err)
	}
	if got, wantJSON := string(encoded), `["APAC","123456789012345678.6400",42,0.125,true,"2026-08-29"]`; got != wantJSON {
		t.Fatalf("Agent JSON = %s, want %s", got, wantJSON)
	}
}

func TestExecutionRuntimeRejectsNonBooleanIntegerResult(t *testing.T) {
	compiled := &artifact.CompiledQuery{
		SqlStatement: sqlquery.SqlStatement{Dialect: "DORIS", SQL: "SELECT active"},
		OutputSchema: artifact.OutputSchema{Columns: []artifact.OutputColumn{{
			Name: "active", Datatype: ossie.DataTypeBoolean,
		}}},
	}
	stream := &runtimeStream{rows: [][]any{{int64(2)}}}
	runtime := newExecutionRuntime(t, &runtimeDriverFactory{executor: &runtimeExecutor{stream: stream}}, testSecretResolver{value: "secret"}, nil, policy(time.Second, 1, 1024))

	result, err := runtime.Execute(context.Background(), "doris-prod", compiled, execution.ExecutionOptions{})
	assertExecutionCode(t, err, execution.ExecutionResultContract)
	if result.Count != 0 || len(result.Rows) != 0 {
		t.Fatalf("non-boolean integer result must not be returned: %#v", result)
	}
}

func TestExecutionRuntimeRejectsLossyDecimalResult(t *testing.T) {
	compiled := &artifact.CompiledQuery{
		SqlStatement: sqlquery.SqlStatement{Dialect: "DORIS", SQL: "SELECT revenue"},
		OutputSchema: artifact.OutputSchema{Columns: []artifact.OutputColumn{{
			Name: "revenue", Datatype: ossie.DataTypeDecimal,
		}}},
	}
	stream := &runtimeStream{rows: [][]any{{float64(0.1)}}}
	runtime := newExecutionRuntime(t, &runtimeDriverFactory{executor: &runtimeExecutor{stream: stream}}, testSecretResolver{value: "secret"}, nil, policy(time.Second, 1, 1024))

	result, err := runtime.Execute(context.Background(), "doris-prod", compiled, execution.ExecutionOptions{})
	assertExecutionCode(t, err, execution.ExecutionResultContract)
	var contractErr *execution.ExecutionError
	if !errors.As(err, &contractErr) || contractErr.ResultContract == nil || contractErr.ResultContract.Column != "revenue" || contractErr.ResultContract.ExpectedDatatype != "Decimal" || contractErr.ResultContract.ObservedFamily != execution.ResultFamilyFloat {
		t.Fatalf("result contract error = %#v", err)
	}
	if result.Count != 0 || len(result.Rows) != 0 {
		t.Fatalf("lossy decimal result must not be returned: %#v", result)
	}
}

func TestExecutionRuntimeSnapshotsCompiledQueryOwnership(t *testing.T) {
	parameterValue := []byte("a")
	compiled := &artifact.CompiledQuery{
		SqlStatement: sqlquery.SqlStatement{
			Dialect:    "DORIS",
			SQL:        "SELECT value",
			Parameters: []sqlquery.QueryParameter{{Value: parameterValue}},
		},
		OutputSchema: oneColumnSchema(),
	}
	stream := &runtimeStream{rows: [][]any{{"north"}}}
	executor := &runtimeExecutor{stream: stream, mutate: func(received *artifact.CompiledQuery) {
		query := received.SqlStatement
		query.Dialect = "CLICKHOUSE"
		query.Parameters[0].Value.([]byte)[0] = 'x'
		received.SqlStatement = query
		received.OutputSchema.Columns[0].Name = "mutated"
	}}
	runtime := newExecutionRuntime(t, &runtimeDriverFactory{executor: executor}, testSecretResolver{value: "secret"}, nil, policy(time.Second, 1, 1024))

	result, err := runtime.Execute(context.Background(), "doris-prod", compiled, execution.ExecutionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	originalQuery := compiled.SqlStatement
	if executor.seenCompiled == compiled || originalQuery.Dialect != "DORIS" || string(originalQuery.Parameters[0].Value.([]byte)) != "a" {
		t.Fatalf("compiled query ownership leaked: original=%#v seen=%#v", compiled, executor.seenCompiled)
	}
	if compiled.OutputSchema.Columns[0].Name != "value" || result.Schema.Columns[0].Name != "value" {
		t.Fatalf("schema mutation leaked: original=%#v result=%#v", compiled.OutputSchema, result.Schema)
	}
}

func TestExecutionRuntimeFailsClosedOnCleanupAndIgnoresObserverFailure(t *testing.T) {
	stream := &runtimeStream{closeErr: errors.New("close failed")}
	executor := &runtimeExecutor{stream: stream}
	runtime := newExecutionRuntime(t, &runtimeDriverFactory{executor: executor}, testSecretResolver{value: "secret"}, &recordingObserver{panicOnCall: true}, policy(time.Second, 1, 1024))

	result, err := runtime.Execute(context.Background(), "doris-prod", compiledSQL("SELECT value"), execution.ExecutionOptions{})
	assertExecutionCode(t, err, execution.ExecutionCleanup)
	if result.Count != 0 || !stream.closed || !executor.closed {
		t.Fatalf("cleanup result=%#v stream=%t executor=%t", result, stream.closed, executor.closed)
	}
}

func TestExecutionRuntimeReusesOneDataSourceRuntimeAndBoundsAdmission(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	resource := &blockingDataSourceRuntime{started: started, release: release}
	factory := &countingDataSourceFactory{resource: resource}
	sourcePolicy := policy(time.Second, 10, 1024)
	maxConcurrency := 1
	sourcePolicy.MaxConcurrency = &maxConcurrency
	runtime := newExecutionRuntime(t, factory, testSecretResolver{value: "secret"}, nil, sourcePolicy)

	firstDone := make(chan error, 1)
	go func() {
		_, err := runtime.Execute(context.Background(), "doris-prod", compiledSQL("SELECT value"), execution.ExecutionOptions{})
		firstDone <- err
	}()
	<-started
	_, err := runtime.Execute(context.Background(), "doris-prod", compiledSQL("SELECT value"), execution.ExecutionOptions{})
	assertExecutionCode(t, err, execution.ExecutionCapacity)
	if factory.opens.Load() != 1 || resource.acquires.Load() != 1 {
		t.Fatalf("factory opens=%d resource acquires=%d, want 1 each", factory.opens.Load(), resource.acquires.Load())
	}
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first execution: %v", err)
	}
	if _, err := runtime.Execute(context.Background(), "doris-prod", compiledSQL("SELECT value"), execution.ExecutionOptions{}); err != nil {
		t.Fatalf("second sequential execution: %v", err)
	}
	if factory.opens.Load() != 1 || resource.acquires.Load() != 2 {
		t.Fatalf("factory opens=%d resource acquires=%d, want 1 and 2", factory.opens.Load(), resource.acquires.Load())
	}
}

func TestExecutionRuntimeCloseStopsAdmissionAndClosesSharedResource(t *testing.T) {
	release := make(chan struct{})
	close(release)
	resource := &blockingDataSourceRuntime{release: release}
	factory := &countingDataSourceFactory{resource: resource}
	runtime := newExecutionRuntime(t, factory, testSecretResolver{value: "secret"}, nil, policy(time.Second, 10, 1024))
	if _, err := runtime.Execute(context.Background(), "doris-prod", compiledSQL("SELECT value"), execution.ExecutionOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if resource.closes.Load() != 1 {
		t.Fatalf("resource closes=%d, want 1", resource.closes.Load())
	}
	_, err := runtime.Execute(context.Background(), "doris-prod", compiledSQL("SELECT value"), execution.ExecutionOptions{})
	assertExecutionCode(t, err, execution.ExecutionClosed)
}

func TestExecutionRuntimeRecycleDataSourceClosesAndReopensScopedResource(t *testing.T) {
	release := make(chan struct{})
	close(release)
	first := &blockingDataSourceRuntime{release: release}
	second := &blockingDataSourceRuntime{release: release}
	factory := &orderedDataSourceFactory{resources: []driver.Runtime{first, second}}
	runtime := newExecutionRuntime(t, factory, testSecretResolver{value: "secret"}, nil, policy(time.Second, 10, 1024))
	if _, err := runtime.Execute(context.Background(), "doris-prod", compiledSQL("SELECT value"), execution.ExecutionOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := runtime.RecycleDataSource(context.Background(), "doris-prod"); err != nil {
		t.Fatal(err)
	}
	if first.closes.Load() != 1 {
		t.Fatalf("first resource closes = %d, want 1", first.closes.Load())
	}
	if _, err := runtime.Execute(context.Background(), "doris-prod", compiledSQL("SELECT value"), execution.ExecutionOptions{}); err != nil {
		t.Fatal(err)
	}
	if second.acquires.Load() != 1 || second.closes.Load() != 0 {
		t.Fatalf("second resource acquires/closes = %d/%d, want 1/0", second.acquires.Load(), second.closes.Load())
	}
}

func TestExecutionRuntimeCoalescesConcurrentFirstUse(t *testing.T) {
	release := make(chan struct{})
	resource := &blockingDataSourceRuntime{release: release}
	factory := &delayedDataSourceFactory{resource: resource, started: make(chan struct{}), allow: make(chan struct{})}
	sourcePolicy := policy(time.Second, 10, 1024)
	maxConcurrency := 128
	sourcePolicy.MaxConcurrency = &maxConcurrency
	runtime := newExecutionRuntime(t, factory, testSecretResolver{value: "secret"}, nil, sourcePolicy)

	const requests = 100
	var group sync.WaitGroup
	errors := make(chan error, requests)
	group.Add(requests)
	for range requests {
		go func() {
			defer group.Done()
			_, err := runtime.Execute(context.Background(), "doris-prod", compiledSQL("SELECT value"), execution.ExecutionOptions{})
			errors <- err
		}()
	}
	<-factory.started
	close(factory.allow)
	close(release)
	group.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatalf("concurrent execution: %v", err)
		}
	}
	if factory.opens.Load() != 1 {
		t.Fatalf("DataSource runtime creations=%d, want 1", factory.opens.Load())
	}
}

func TestExecutionRuntimeSharesFailedConcurrentInitializationAttempt(t *testing.T) {
	factory := &failingDataSourceFactory{started: make(chan struct{}), allow: make(chan struct{})}
	sourcePolicy := policy(time.Second, 10, 1024)
	maxConcurrency := 128
	sourcePolicy.MaxConcurrency = &maxConcurrency
	runtime := newExecutionRuntime(t, factory, testSecretResolver{value: "secret"}, nil, sourcePolicy)

	const requests = 100
	start := make(chan struct{})
	ready := make(chan struct{}, requests)
	var group sync.WaitGroup
	errors := make(chan error, requests)
	group.Add(requests)
	for range requests {
		go func() {
			defer group.Done()
			ready <- struct{}{}
			<-start
			_, err := runtime.Execute(context.Background(), "doris-prod", compiledSQL("SELECT value"), execution.ExecutionOptions{})
			errors <- err
		}()
	}
	for range requests {
		<-ready
	}
	close(start)
	<-factory.started
	// Keep attempt #1 open long enough for the released request wave to join it.
	time.Sleep(20 * time.Millisecond)
	close(factory.allow)
	group.Wait()
	close(errors)
	for err := range errors {
		assertExecutionCode(t, err, execution.ExecutionOpen)
	}
	if factory.opens.Load() != 1 {
		t.Fatalf("failed DataSource runtime creations=%d, want 1 for one concurrent wave", factory.opens.Load())
	}

	_, err := runtime.Execute(context.Background(), "doris-prod", compiledSQL("SELECT value"), execution.ExecutionOptions{})
	assertExecutionCode(t, err, execution.ExecutionOpen)
	if factory.opens.Load() != 2 {
		t.Fatalf("failed DataSource runtime creations=%d, want 2 after one later retry", factory.opens.Load())
	}
}

func TestExecutionRuntimeBoundsQueueBeforeAcquiringExecutor(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	resource := &blockingDataSourceRuntime{started: started, release: release}
	factory := &countingDataSourceFactory{resource: resource}
	sourcePolicy := policy(time.Second, 10, 1024)
	maxConcurrency, maxQueue := 1, 1
	sourcePolicy.MaxConcurrency = &maxConcurrency
	sourcePolicy.MaxQueue = &maxQueue
	sourcePolicy.QueueTimeout = "20ms"
	runtime := newExecutionRuntime(t, factory, testSecretResolver{value: "secret"}, nil, sourcePolicy)

	firstDone := make(chan error, 1)
	go func() {
		_, err := runtime.Execute(context.Background(), "doris-prod", compiledSQL("SELECT value"), execution.ExecutionOptions{})
		firstDone <- err
	}()
	<-started
	queuedDone := make(chan error, 1)
	go func() {
		_, err := runtime.Execute(context.Background(), "doris-prod", compiledSQL("SELECT value"), execution.ExecutionOptions{})
		queuedDone <- err
	}()
	time.Sleep(5 * time.Millisecond)
	_, err := runtime.Execute(context.Background(), "doris-prod", compiledSQL("SELECT value"), execution.ExecutionOptions{})
	assertExecutionCode(t, err, execution.ExecutionCapacity)
	assertExecutionCode(t, <-queuedDone, execution.ExecutionCapacity)
	if resource.acquires.Load() != 1 {
		t.Fatalf("Executor acquisitions=%d, want 1 before capacity release", resource.acquires.Load())
	}
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first execution: %v", err)
	}
}

func TestExecutionRuntimeShutdownCancelsInFlightWorkAtDeadline(t *testing.T) {
	started := make(chan struct{})
	resource := &blockingDataSourceRuntime{started: started, release: make(chan struct{})}
	factory := &countingDataSourceFactory{resource: resource}
	runtime := newExecutionRuntime(t, factory, testSecretResolver{value: "secret"}, nil, policy(time.Second, 10, 1024))

	done := make(chan error, 1)
	go func() {
		_, err := runtime.Execute(context.Background(), "doris-prod", compiledSQL("SELECT value"), execution.ExecutionOptions{})
		done <- err
	}()
	<-started
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	if err := runtime.Close(shutdownCtx); err == nil {
		t.Fatal("Close() succeeded with an active request after its deadline")
	}
	assertExecutionCode(t, <-done, execution.ExecutionCancelled)
	awaitAtomicValue(t, &resource.closes, 1)
}

func TestExecutionRuntimeShutdownBoundsCleanupForAllDataSources(t *testing.T) {
	first := newDeadlineCloseDataSourceRuntime()
	second := newDeadlineCloseDataSourceRuntime()
	factory := &orderedDataSourceFactory{resources: []driver.Runtime{first, second}}
	sourcePolicy := policy(time.Second, 10, 1024)
	sources, err := datasource.NewDataSourceRegistry(map[string]datasource.DataSource{
		"doris-first":  {Type: "doris", Policy: sourcePolicy},
		"doris-second": {Type: "doris", Policy: sourcePolicy},
	})
	if err != nil {
		t.Fatal(err)
	}
	backends, err := backend.NewBackendRegistry(backend.Backend{Type: "doris", Renderer: doris.New(), DriverFactory: factory})
	if err != nil {
		t.Fatal(err)
	}
	runtime := execution.New(sources, backends, nil, nil)

	firstDone := make(chan error, 1)
	go func() {
		_, executeErr := runtime.Execute(context.Background(), "doris-first", compiledSQL("SELECT value"), execution.ExecutionOptions{})
		firstDone <- executeErr
	}()
	<-first.started
	secondDone := make(chan error, 1)
	go func() {
		_, executeErr := runtime.Execute(context.Background(), "doris-second", compiledSQL("SELECT value"), execution.ExecutionOptions{})
		secondDone <- executeErr
	}()
	<-second.started

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	closeDone := make(chan error, 1)
	go func() { closeDone <- runtime.Close(shutdownCtx) }()
	select {
	case closeErr := <-closeDone:
		if closeErr == nil {
			t.Fatal("Close() succeeded after the drain deadline")
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("Close() exceeded its deadline during DataSource cleanup")
	}
	assertExecutionCode(t, <-firstDone, execution.ExecutionCancelled)
	assertExecutionCode(t, <-secondDone, execution.ExecutionCancelled)
	awaitAtomicValue(t, &first.closes, 1)
	awaitAtomicValue(t, &second.closes, 1)
}

func TestExecutionRuntimeFreezesDeploymentRegistries(t *testing.T) {
	maxRows, maxBytes := int64(10), int64(1024)
	maxConcurrency := 1
	sources, err := datasource.NewDataSourceRegistry(map[string]datasource.DataSource{
		"doris-prod": {Type: "doris", Policy: datasource.DataSourcePolicy{QueryTimeout: time.Second.String(), MaxRows: &maxRows, MaxBytes: &maxBytes, MaxConcurrency: &maxConcurrency}},
	})
	if err != nil {
		t.Fatal(err)
	}
	backends, err := backend.NewBackendRegistry(backend.Backend{Type: "doris", Renderer: doris.New(), DriverFactory: &countingDataSourceFactory{}})
	if err != nil {
		t.Fatal(err)
	}
	_ = execution.New(sources, backends, nil, nil)
	if err := sources.Register("later", datasource.DataSource{Type: "doris", Policy: datasource.DataSourcePolicy{QueryTimeout: time.Second.String(), MaxRows: &maxRows, MaxBytes: &maxBytes, MaxConcurrency: &maxConcurrency}}); err == nil {
		t.Fatal("frozen DataSource registry accepted registration")
	}
	if err := backends.Register(backend.Backend{Type: "duckdb", Renderer: duckdb.New(), DriverFactory: &countingDataSourceFactory{}}); err == nil {
		t.Fatal("frozen Backend registry accepted registration")
	}
}

type countingDataSourceFactory struct {
	resource *blockingDataSourceRuntime
	opens    atomic.Int64
}

type delayedDataSourceFactory struct {
	resource *blockingDataSourceRuntime
	started  chan struct{}
	allow    chan struct{}
	once     sync.Once
	opens    atomic.Int64
}

type failingDataSourceFactory struct {
	started chan struct{}
	allow   chan struct{}
	once    sync.Once
	opens   atomic.Int64
}

func (*failingDataSourceFactory) DataSourceType() datasource.Type        { return "doris" }
func (*failingDataSourceFactory) ValidateConfig(map[string]string) error { return nil }
func (f *failingDataSourceFactory) OpenDataSource(ctx context.Context, _ driver.OpenRequest) (driver.Runtime, error) {
	f.opens.Add(1)
	f.once.Do(func() { close(f.started) })
	select {
	case <-f.allow:
		return nil, errors.New("database unavailable")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

type orderedDataSourceFactory struct {
	mu        sync.Mutex
	resources []driver.Runtime
	next      int
}

func (*orderedDataSourceFactory) DataSourceType() datasource.Type        { return "doris" }
func (*orderedDataSourceFactory) ValidateConfig(map[string]string) error { return nil }
func (f *orderedDataSourceFactory) OpenDataSource(context.Context, driver.OpenRequest) (driver.Runtime, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.next >= len(f.resources) {
		return nil, errors.New("no test resource")
	}
	resource := f.resources[f.next]
	f.next++
	return resource, nil
}

func (*delayedDataSourceFactory) DataSourceType() datasource.Type        { return "doris" }
func (*delayedDataSourceFactory) ValidateConfig(map[string]string) error { return nil }
func (f *delayedDataSourceFactory) OpenDataSource(ctx context.Context, _ driver.OpenRequest) (driver.Runtime, error) {
	f.opens.Add(1)
	f.once.Do(func() { close(f.started) })
	select {
	case <-f.allow:
		return f.resource, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (*countingDataSourceFactory) DataSourceType() datasource.Type        { return "doris" }
func (*countingDataSourceFactory) ValidateConfig(map[string]string) error { return nil }
func (f *countingDataSourceFactory) OpenDataSource(context.Context, driver.OpenRequest) (driver.Runtime, error) {
	f.opens.Add(1)
	return f.resource, nil
}

type blockingDataSourceRuntime struct {
	started   chan struct{}
	release   chan struct{}
	startOnce sync.Once
	acquires  atomic.Int64
	closes    atomic.Int64
}

type deadlineCloseDataSourceRuntime struct {
	*blockingDataSourceRuntime
	closes atomic.Int64
}

func newDeadlineCloseDataSourceRuntime() *deadlineCloseDataSourceRuntime {
	return &deadlineCloseDataSourceRuntime{blockingDataSourceRuntime: &blockingDataSourceRuntime{started: make(chan struct{}), release: make(chan struct{})}}
}

func (r *deadlineCloseDataSourceRuntime) Close(ctx context.Context) error {
	r.closes.Add(1)
	if ctx == nil {
		ctx = context.Background()
	}
	<-ctx.Done()
	return ctx.Err()
}

func (r *blockingDataSourceRuntime) Acquire(context.Context) (driver.Executor, error) {
	r.acquires.Add(1)
	return blockingExecutor{runtime: r}, nil
}

func (r *blockingDataSourceRuntime) Close(context.Context) error {
	r.closes.Add(1)
	return nil
}

type blockingExecutor struct{ runtime *blockingDataSourceRuntime }

func (e blockingExecutor) Execute(context.Context, *artifact.CompiledQuery) (driver.ResultStream, error) {
	e.runtime.startOnce.Do(func() {
		if e.runtime.started != nil {
			close(e.runtime.started)
		}
	})
	return &blockingStream{release: e.runtime.release}, nil
}
func (blockingExecutor) Close() error { return nil }

type blockingStream struct {
	release chan struct{}
	once    sync.Once
}

func (s *blockingStream) Next(ctx context.Context) ([]any, error) {
	if s.release != nil {
		select {
		case <-s.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	called := false
	s.once.Do(func() { called = true })
	if called {
		return []any{"value"}, nil
	}
	return nil, io.EOF
}
func (*blockingStream) Close() error { return nil }

func awaitAtomicValue(t *testing.T, value *atomic.Int64, want int64) {
	t.Helper()
	deadline := time.Now().Add(250 * time.Millisecond)
	for time.Now().Before(deadline) {
		if value.Load() == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("value=%d, want %d", value.Load(), want)
}

func newExecutionRuntime(t *testing.T, factory driver.Factory, secrets execution.SecretResolver, observer execution.ExecutionObserver, sourcePolicy datasource.DataSourcePolicy) *execution.Runner {
	t.Helper()
	return newExecutionRuntimeWithConfig(t, factory, secrets, observer, sourcePolicy, map[string]string{
		"host": "${METIS_HOST}", "password": "${METIS_PASSWORD}",
	})
}

func newExecutionRuntimeWithConfig(t *testing.T, factory driver.Factory, secrets execution.SecretResolver, observer execution.ExecutionObserver, sourcePolicy datasource.DataSourcePolicy, config map[string]string) *execution.Runner {
	t.Helper()
	sources, err := datasource.NewDataSourceRegistry(map[string]datasource.DataSource{
		"doris-prod": {
			Type:   "doris",
			Config: config,
			Policy: sourcePolicy,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	backends, err := backend.NewBackendRegistry(backend.Backend{Type: "doris", Renderer: doris.New(), DriverFactory: factory})
	if err != nil {
		t.Fatal(err)
	}
	return execution.New(sources, backends, secrets, observer)
}

func policy(timeout time.Duration, maxRows, maxBytes int64) datasource.DataSourcePolicy {
	maxConcurrency := 8
	return datasource.DataSourcePolicy{QueryTimeout: timeout.String(), MaxRows: &maxRows, MaxBytes: &maxBytes, MaxConcurrency: &maxConcurrency}
}

func oneColumnSchema() artifact.OutputSchema {
	return artifact.OutputSchema{Columns: []artifact.OutputColumn{{Name: "value"}}}
}

func compiledSQL(sql string) *artifact.CompiledQuery {
	return &artifact.CompiledQuery{SqlStatement: sqlquery.SqlStatement{Dialect: "DORIS", SQL: sql}, OutputSchema: oneColumnSchema()}
}

func equalResultRow(left, right []any) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func assertExecutionCode(t *testing.T, err error, want execution.ExecutionErrorCode) {
	t.Helper()
	var executionErr *execution.ExecutionError
	if !errors.As(err, &executionErr) || executionErr.Code != want {
		t.Fatalf("error = %#v, want %s", err, want)
	}
}
