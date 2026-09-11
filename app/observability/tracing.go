package observability

import (
	"context"

	"github.com/meaningforge/metis/serrors"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

const InstrumentationScope = "github.com/meaningforge/metis/app/observability"

type Operation string

const (
	OperationHTTP         Operation = "metis.http"
	OperationMCP          Operation = "metis.mcp"
	OperationCompile      Operation = "metis.compile"
	OperationAnalysis     Operation = "metis.analysis"
	OperationPlanning     Operation = "metis.planning"
	OperationOptimization Operation = "metis.optimization"
	OperationRendering    Operation = "metis.rendering"
)

// Tracing owns the injected OpenTelemetry API boundary. A nil provider uses
// OpenTelemetry's no-op provider; exporters and sampling remain deployment
// concerns rather than compiler dependencies.
type Tracing struct {
	tracer     trace.Tracer
	propagator propagation.TextMapPropagator
}

func NewTracing(provider trace.TracerProvider) *Tracing {
	if provider == nil {
		provider = trace.NewNoopTracerProvider()
	}
	return &Tracing{
		tracer:     provider.Tracer(InstrumentationScope),
		propagator: propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}),
	}
}

// Start creates a span only for the closed operation vocabulary.
// Invalid operation names degrade to a no-op span instead of expanding the
// runtime API with arbitrary function identities.
func (t *Tracing) Start(ctx context.Context, operation Operation, options ...trace.SpanStartOption) (context.Context, trace.Span) {
	if ctx == nil {
		ctx = context.Background()
	}
	if t == nil || !validOperation(operation) {
		return trace.NewNoopTracerProvider().Tracer(InstrumentationScope).Start(ctx, string(operation), options...)
	}
	return t.tracer.Start(ctx, string(operation), options...)
}

// RecordError attaches an escaping error to the current span using stable OTel
// exception semantics, includes a Go stack trace, and marks the span failed.
// The stable Metis code is a span attribute; raw error text never becomes a
// metric label or span name.
func (t *Tracing) RecordError(ctx context.Context, err error, code serrors.ErrorCode) {
	if t == nil {
		return
	}
	RecordError(ctx, err, code)
}

// RecordError records an escaping Metis failure on the span already carried by
// ctx. Transport adapters use it without depending on a concrete exporter.
func RecordError(ctx context.Context, err error, code serrors.ErrorCode) {
	if ctx == nil || err == nil {
		return
	}
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}
	if !registeredErrorCode(code) {
		code = serrors.ErrInternal
	}
	span.SetAttributes(attribute.String("metis.error.code", string(code)))
	span.RecordError(err, trace.WithStackTrace(true))
	span.SetStatus(codes.Error, string(code))
}

func (t *Tracing) Extract(ctx context.Context, carrier propagation.TextMapCarrier) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if t == nil || carrier == nil {
		return ctx
	}
	return t.propagator.Extract(ctx, carrier)
}

func (t *Tracing) Inject(ctx context.Context, carrier propagation.TextMapCarrier) {
	if t == nil || ctx == nil || carrier == nil {
		return
	}
	t.propagator.Inject(ctx, carrier)
}

func validOperation(operation Operation) bool {
	switch operation {
	case OperationHTTP, OperationMCP, OperationCompile, OperationAnalysis, OperationPlanning, OperationOptimization, OperationRendering:
		return true
	default:
		return false
	}
}

func registeredErrorCode(code serrors.ErrorCode) bool {
	for _, registered := range serrors.Codes() {
		if code == registered {
			return true
		}
	}
	return false
}
