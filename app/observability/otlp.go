package observability

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
)

// NewOTLPTracing creates the operator-enabled OTLP/HTTP trace pipeline. The
// exporter follows the standard OTEL_EXPORTER_OTLP_* environment contract.
// Callers own the returned shutdown function and must invoke it during graceful
// process shutdown so the batch processor can flush pending spans.
func NewOTLPTracing(ctx context.Context, sampleRatio float64) (*Tracing, func(context.Context) error, error) {
	if sampleRatio < 0 || sampleRatio > 1 {
		return nil, nil, fmt.Errorf("trace sample ratio must be between 0 and 1")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	exporter, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("create OTLP trace exporter: %w", err)
	}
	resources, err := resource.New(ctx,
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
		resource.WithAttributes(semconv.ServiceName("metis")),
	)
	if err != nil {
		_ = exporter.Shutdown(ctx)
		return nil, nil, fmt.Errorf("create trace resource: %w", err)
	}
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(resources),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(sampleRatio))),
	)
	return NewTracing(provider), provider.Shutdown, nil
}
