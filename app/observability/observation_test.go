package observability_test

import (
	"context"
	"testing"
	"time"

	"github.com/meaningforge/metis/app/observability"
	"github.com/meaningforge/metis/execution/datasource"
	"github.com/meaningforge/metis/execution/runner"
	"github.com/meaningforge/metis/serrors"
)

func TestRecorderEmitsOnlyValidClosedObservations(t *testing.T) {
	memory := observability.NewMemorySink(10)
	recorder := observability.NewRecorder(memory)
	recorder.Record(context.Background(), observability.PhaseObservation{
		Phase: observability.PhasePlanning, Dialect: observability.DialectDoris, Duration: time.Millisecond,
	})
	recorder.Record(context.Background(), observability.PhaseObservation{
		Phase: "resolver.someFunction", Dialect: "DORIS", Duration: time.Millisecond,
	})
	recorder.Record(context.Background(), observability.CompileObservation{
		Dialect: observability.DialectDoris, Result: observability.ResultError, Code: serrors.ErrInvalidQuery, Duration: time.Millisecond,
	})
	recorder.Record(context.Background(), observability.CompileObservation{
		Dialect: "DORIS", Result: observability.ResultError, Code: "raw error message", Duration: time.Millisecond,
	})

	got := memory.Observations()
	if len(got) != 2 {
		t.Fatalf("observations = %#v, want two valid observations", got)
	}
	if _, ok := got[0].(observability.PhaseObservation); !ok {
		t.Fatalf("first observation type = %T", got[0])
	}
	if _, ok := got[1].(observability.CompileObservation); !ok {
		t.Fatalf("second observation type = %T", got[1])
	}
}

func TestRecorderContainsSinkPanicsAndContinuesFanout(t *testing.T) {
	memory := observability.NewMemorySink(10)
	recorder := observability.NewRecorder(panicSink{}, memory)
	recorder.Record(context.Background(), observability.OptimizerObservation{Outcome: observability.OptimizerNoop})
	if got := memory.Observations(); len(got) != 1 {
		t.Fatalf("observations after panicking sink = %#v, want one", got)
	}
}

func TestMemorySinkIsBounded(t *testing.T) {
	memory := observability.NewMemorySink(2)
	recorder := observability.NewRecorder(memory)
	for _, outcome := range []observability.OptimizerOutcome{
		observability.OptimizerNoop, observability.OptimizerRewritten, observability.OptimizerNoop,
	} {
		recorder.Record(context.Background(), observability.OptimizerObservation{Outcome: outcome})
	}
	got := memory.Observations()
	if len(got) != 2 {
		t.Fatalf("bounded observations = %#v", got)
	}
	first := got[0].(observability.OptimizerObservation)
	if first.Outcome != observability.OptimizerRewritten {
		t.Fatalf("oldest retained outcome = %s, want %s", first.Outcome, observability.OptimizerRewritten)
	}
}

func TestRecorderOwnsPointerObservationValue(t *testing.T) {
	memory := observability.NewMemorySink(1)
	recorder := observability.NewRecorder(memory)
	input := &observability.OptimizerObservation{Outcome: observability.OptimizerRewritten}
	recorder.Record(context.Background(), input)
	input.Outcome = observability.OptimizerNoop

	got := memory.Observations()[0].(observability.OptimizerObservation)
	if got.Outcome != observability.OptimizerRewritten {
		t.Fatalf("recorded outcome changed to %s", got.Outcome)
	}
}

func TestErrorCodeFailsClosedForUnknownTypedCode(t *testing.T) {
	err := &serrors.Error{Code: "FUTURE_ERROR", Message: "dynamic"}
	if got := observability.ErrorCode(err); got != serrors.ErrInternal {
		t.Fatalf("error code = %q, want %q", got, serrors.ErrInternal)
	}
}

func TestRecorderAdaptsRunnerExecutionWithoutDynamicIdentity(t *testing.T) {
	memory := observability.NewMemorySink(1)
	recorder := observability.NewRecorder(memory)
	recorder.ObserveExecution(context.Background(), runner.ExecutionObservation{
		QueryID: "customer-query-id", DataSourceType: datasource.Type("doris"),
		Code: runner.ExecutionResultContract, Duration: time.Second, Rows: 3, Bytes: 42,
	})
	got := memory.Observations()
	if len(got) != 1 {
		t.Fatalf("observations = %#v", got)
	}
	execution := got[0].(observability.ExecutionObservation)
	if execution.Backend != observability.BackendDoris || execution.Result != observability.ResultError || execution.Code != runner.ExecutionResultContract || execution.Rows != 3 || execution.Bytes != 42 {
		t.Fatalf("execution observation = %#v", execution)
	}
}

type panicSink struct{}

func (panicSink) Observe(context.Context, observability.Observation) { panic("sink failure") }
