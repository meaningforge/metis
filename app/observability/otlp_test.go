package observability_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/meaningforge/metis/app/observability"
)

func TestNewOTLPTracingExportsAndFlushesClosedOperationSpan(t *testing.T) {
	var requests atomic.Int32
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(collector.Close)
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", collector.URL)

	tracing, shutdown, err := observability.NewOTLPTracing(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	_, span := tracing.Start(context.Background(), observability.OperationCompile)
	span.End()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("OTLP export requests = %d, want 1", got)
	}
}

func TestNewOTLPTracingRejectsInvalidSampleRatioBeforeExporterStartup(t *testing.T) {
	for _, ratio := range []float64{-0.01, 1.01} {
		tracing, shutdown, err := observability.NewOTLPTracing(context.Background(), ratio)
		if err == nil || tracing != nil || shutdown != nil {
			t.Fatalf("NewOTLPTracing(%v): tracing=%v shutdown_set=%t err=%v, want validation error", ratio, tracing, shutdown != nil, err)
		}
	}
}
