package observability_test

import (
	"context"
	"errors"
	"testing"

	"github.com/meaningforge/metis/app/observability"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestPipelineIsInactiveOutsidePhysicalCompile(t *testing.T) {
	memory := observability.NewMemorySink(10)
	exporter := tracetest.NewInMemoryExporter()
	provider := trace.NewTracerProvider(trace.WithSyncer(exporter))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	pipeline := observability.NewPipeline(observability.NewRecorder(memory), observability.NewTracing(provider))

	_, finish := pipeline.StartPlanning(context.Background(), "doris")
	finish(nil)
	if observations := memory.Observations(); len(observations) != 0 {
		t.Fatalf("inactive pipeline observations = %#v", observations)
	}
	if spans := exporter.GetSpans(); len(spans) != 0 {
		t.Fatalf("inactive pipeline spans = %#v", spans)
	}
}

func TestPipelineRecordsClosedPhasesOptimizerOutcomesAndErrors(t *testing.T) {
	memory := observability.NewMemorySink(10)
	exporter := tracetest.NewInMemoryExporter()
	provider := trace.NewTracerProvider(trace.WithSyncer(exporter))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	tracing := observability.NewTracing(provider)
	pipeline := observability.NewPipeline(observability.NewRecorder(memory), tracing)
	compileCtx, compileSpan := tracing.Start(context.Background(), observability.OperationCompile)
	compileCtx = pipeline.Activate(compileCtx)

	_, finishPlanning := pipeline.StartPlanning(compileCtx, "doris")
	finishPlanning(nil)
	_, finishNoop := pipeline.StartOptimization(compileCtx, "doris")
	finishNoop(false, nil)
	_, finishRewritten := pipeline.StartOptimization(compileCtx, "doris")
	finishRewritten(true, nil)
	_, finishRendering := pipeline.StartPhase(compileCtx, observability.PhaseRendering, observability.DialectDoris)
	finishRendering(errors.New("renderer failed"))
	compileSpan.End()

	observations := memory.Observations()
	var noop, rewritten int
	for _, observation := range observations {
		if optimizer, ok := observation.(observability.OptimizerObservation); ok {
			switch optimizer.Outcome {
			case observability.OptimizerNoop:
				noop++
			case observability.OptimizerRewritten:
				rewritten++
			}
		}
	}
	if noop != 1 || rewritten != 1 {
		t.Fatalf("optimizer outcomes = noop:%d rewritten:%d; observations=%#v", noop, rewritten, observations)
	}
	spans := exporter.GetSpans()
	byName := make(map[string][]tracetest.SpanStub)
	for _, span := range spans {
		byName[span.Name] = append(byName[span.Name], span)
	}
	if len(byName[string(observability.OperationPlanning)]) != 1 || len(byName[string(observability.OperationOptimization)]) != 2 || len(byName[string(observability.OperationRendering)]) != 1 {
		t.Fatalf("pipeline spans = %#v", spans)
	}
	rendering := byName[string(observability.OperationRendering)][0]
	if rendering.Status.Code != codes.Error || len(rendering.Events) != 1 || rendering.Events[0].Name != "exception" {
		t.Fatalf("rendering error span = %#v", rendering)
	}
}
