package bootstrap_test

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/meaningforge/metis/app/bootstrap"
	"github.com/meaningforge/metis/app/observability"
	service "github.com/meaningforge/metis/app/service/semantic"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/serrors"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestCompileObservabilityRecordsOneOperationWithoutChangingOutput(t *testing.T) {
	runtime := observedRuntime(t)
	req := service.CompileRequest{Query: query.SemanticQuery{
		Project: runtime.ProjectIDs()[0], Model: "sales",
		Metrics: []query.MetricRef{{Name: "total_revenue"}},
	}, Dialect: "DORIS"}
	want, err := runtime.Compile.Compile(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}

	memory := observability.NewMemorySink(10)
	exporter := tracetest.NewInMemoryExporter()
	provider := trace.NewTracerProvider(trace.WithSyncer(exporter))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	runtime.Compile.WithObservability(observability.NewRecorder(memory), observability.NewTracing(provider))
	if _, err := runtime.Compile.Validate(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if observations := memory.Observations(); len(observations) != 0 {
		t.Fatalf("Validate emitted compile observations: %#v", observations)
	}
	if spans := exporter.GetSpans(); len(spans) != 0 {
		t.Fatalf("Validate emitted compile spans: %#v", spans)
	}
	got, err := runtime.Compile.Compile(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("instrumented compile changed output:\ngot  %#v\nwant %#v", got, want)
	}

	observations := memory.Observations()
	if len(observations) != 6 {
		t.Fatalf("compile observations = %#v, want compile + four phases + optimizer", observations)
	}
	var compile observability.CompileObservation
	phases := map[observability.Phase]int{}
	optimizerRuns := 0
	for _, observation := range observations {
		switch value := observation.(type) {
		case observability.CompileObservation:
			compile = value
		case observability.PhaseObservation:
			phases[value.Phase]++
			if value.Dialect != observability.DialectDoris {
				t.Fatalf("phase observation = %#v", value)
			}
		case observability.OptimizerObservation:
			optimizerRuns++
			if value.Outcome != observability.OptimizerRewritten {
				t.Fatalf("optimizer observation = %#v", value)
			}
		}
	}
	if compile.Dialect != observability.DialectDoris || compile.Result != observability.ResultSuccess || compile.Code != "" {
		t.Fatalf("compile observation = %#v", compile)
	}
	for _, phase := range []observability.Phase{observability.PhaseAnalysis, observability.PhasePlanning, observability.PhaseOptimization, observability.PhaseRendering} {
		if phases[phase] != 1 {
			t.Fatalf("phase counts = %#v", phases)
		}
	}
	if optimizerRuns != 1 {
		t.Fatalf("optimizer runs = %d", optimizerRuns)
	}
	spans := exporter.GetSpans()
	if len(spans) != 5 {
		t.Fatalf("compile spans = %#v", spans)
	}
	byName := make(map[string]tracetest.SpanStub, len(spans))
	for _, span := range spans {
		byName[span.Name] = span
	}
	compileSpan := byName[string(observability.OperationCompile)]
	for _, operation := range []observability.Operation{observability.OperationAnalysis, observability.OperationPlanning, observability.OperationOptimization, observability.OperationRendering} {
		if got, want := byName[string(operation)].Parent.SpanID(), compileSpan.SpanContext.SpanID(); got != want {
			t.Fatalf("%s parent = %s, want compile span %s", operation, got, want)
		}
	}
}

func TestCompileObservabilityClassifiesStableErrorsWithoutRawLabels(t *testing.T) {
	runtime := observedRuntime(t)
	memory := observability.NewMemorySink(10)
	exporter := tracetest.NewInMemoryExporter()
	provider := trace.NewTracerProvider(trace.WithSyncer(exporter))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	runtime.Compile.WithObservability(observability.NewRecorder(memory), observability.NewTracing(provider))

	_, err := runtime.Compile.Compile(context.Background(), service.CompileRequest{Query: query.SemanticQuery{
		Project: runtime.ProjectIDs()[0], Model: "sales", Metrics: []query.MetricRef{{Name: "secret_missing_metric"}},
	}, Dialect: "DORIS"})
	if err == nil {
		t.Fatal("compile succeeded, want missing metric error")
	}
	observations := memory.Observations()
	if len(observations) != 2 {
		t.Fatalf("compile observations = %#v", observations)
	}
	analysis := observations[0].(observability.PhaseObservation)
	compile := observations[1].(observability.CompileObservation)
	if analysis.Phase != observability.PhaseAnalysis {
		t.Fatalf("analysis observation = %#v", analysis)
	}
	if compile.Result != observability.ResultError || compile.Code != serrors.ErrMetricNotFound || compile.Dialect != observability.DialectDoris {
		t.Fatalf("compile observation = %#v", compile)
	}
	spans := exporter.GetSpans()
	if len(spans) != 2 {
		t.Fatalf("compile exception span = %#v", spans)
	}
	for _, span := range spans {
		if len(span.Events) != 1 || span.Events[0].Name != "exception" {
			t.Fatalf("compile exception span = %#v", span)
		}
	}
}

func observedRuntime(t *testing.T) *bootstrap.ProjectRuntime {
	t.Helper()
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "sales.ossie.yaml"), salesModel)
	mustWrite(t, filepath.Join(dir, "metis.yaml"), `
semantic_sources:
  sales:
    path: ./sales.ossie.yaml
`)
	runtime, err := bootstrap.LoadProjectRuntime(filepath.Join(dir, "metis.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return runtime
}
