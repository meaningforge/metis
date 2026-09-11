package observability_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/meaningforge/metis/app/observability"
	"github.com/meaningforge/metis/serrors"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestTracingRecordsExceptionStatusMessageAndStackTrace(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := trace.NewTracerProvider(trace.WithSyncer(exporter))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	tracing := observability.NewTracing(provider)

	ctx, span := tracing.Start(context.Background(), observability.OperationCompile)
	tracing.RecordError(ctx, errors.New("planner invariant failed"), serrors.ErrInternalInvariant)
	span.End()

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("spans = %#v, want one", spans)
	}
	got := spans[0]
	if got.Name != string(observability.OperationCompile) {
		t.Fatalf("span name = %q", got.Name)
	}
	if got.Status.Code != codes.Error || got.Status.Description != string(serrors.ErrInternalInvariant) {
		t.Fatalf("span status = %#v", got.Status)
	}
	if attributeValue(got.Attributes, "metis.error.code") != string(serrors.ErrInternalInvariant) {
		t.Fatalf("metis.error.code = %q", attributeValue(got.Attributes, "metis.error.code"))
	}
	if len(got.Events) != 1 || got.Events[0].Name != "exception" {
		t.Fatalf("exception events = %#v", got.Events)
	}
	event := got.Events[0]
	if exceptionType := attributeValue(event.Attributes, "exception.type"); exceptionType == "" {
		t.Fatal("exception.type is empty")
	}
	if attributeValue(event.Attributes, "exception.message") != "planner invariant failed" {
		t.Fatalf("exception.message = %q", attributeValue(event.Attributes, "exception.message"))
	}
	if stack := attributeValue(event.Attributes, "exception.stacktrace"); !strings.Contains(stack, "TestTracingRecordsExceptionStatusMessageAndStackTrace") {
		t.Fatalf("exception.stacktrace does not identify call site: %q", stack)
	}
}

func TestTracingPreservesMetisOperationHierarchy(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := trace.NewTracerProvider(trace.WithSyncer(exporter))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	tracing := observability.NewTracing(provider)

	httpCtx, httpSpan := tracing.Start(context.Background(), observability.OperationHTTP)
	compileCtx, compileSpan := tracing.Start(httpCtx, observability.OperationCompile)
	_, planningSpan := tracing.Start(compileCtx, observability.OperationPlanning)
	planningSpan.End()
	compileSpan.End()
	httpSpan.End()

	spans := exporter.GetSpans()
	if len(spans) != 3 {
		t.Fatalf("spans = %#v, want three", spans)
	}
	byName := make(map[string]tracetest.SpanStub, len(spans))
	for _, span := range spans {
		byName[span.Name] = span
	}
	if got, want := byName[string(observability.OperationCompile)].Parent.SpanID(), byName[string(observability.OperationHTTP)].SpanContext.SpanID(); got != want {
		t.Fatalf("compile parent = %s, want HTTP span %s", got, want)
	}
	if got, want := byName[string(observability.OperationPlanning)].Parent.SpanID(), byName[string(observability.OperationCompile)].SpanContext.SpanID(); got != want {
		t.Fatalf("planning parent = %s, want compile span %s", got, want)
	}
}

func TestTracingPropagatesW3CParentIntoCompileSpan(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := trace.NewTracerProvider(trace.WithSyncer(exporter))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	tracing := observability.NewTracing(provider)
	carrier := propagation.MapCarrier{
		"traceparent": "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
	}

	parent := tracing.Extract(context.Background(), carrier)
	_, span := tracing.Start(parent, observability.OperationCompile)
	span.End()

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("spans = %#v, want one", spans)
	}
	if got := spans[0].Parent.TraceID().String(); got != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("parent trace ID = %s", got)
	}
	if got := spans[0].Parent.SpanID().String(); got != "00f067aa0ba902b7" {
		t.Fatalf("parent span ID = %s", got)
	}
}

func TestTracingRejectsArbitraryFunctionSpanNames(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := trace.NewTracerProvider(trace.WithSyncer(exporter))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	tracing := observability.NewTracing(provider)
	_, span := tracing.Start(context.Background(), observability.Operation("resolver.selectMetricExpression"))
	span.End()
	if spans := exporter.GetSpans(); len(spans) != 0 {
		t.Fatalf("arbitrary operation produced spans: %#v", spans)
	}
}

func attributeValue(attributes []attribute.KeyValue, key string) string {
	for _, value := range attributes {
		if string(value.Key) == key {
			return value.Value.AsString()
		}
	}
	return ""
}
