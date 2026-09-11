package observability_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/meaningforge/metis/app/observability"
	"github.com/prometheus/client_golang/prometheus"
)

func TestMetricsUsesPrivateRegistryWithRuntimeCollectors(t *testing.T) {
	metrics := observability.NewMetrics()
	custom := prometheus.NewCounter(prometheus.CounterOpts{Name: "metis_test_private_total", Help: "test collector"})
	if err := metrics.Registerer().Register(custom); err != nil {
		t.Fatal(err)
	}
	custom.Inc()

	recorder := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	text := recorder.Body.String()
	for _, name := range []string{"go_goroutines", "process_cpu_seconds", "metis_test_private_total"} {
		if !strings.Contains(text, name) {
			t.Fatalf("metrics exposition does not contain %q", name)
		}
	}
	if !strings.Contains(recorder.Header().Get("Content-Type"), "text/plain") {
		t.Fatalf("content type = %q, want Prometheus text", recorder.Header().Get("Content-Type"))
	}
}

func TestMetricsRegistryDoesNotUsePrometheusGlobals(t *testing.T) {
	metrics := observability.NewMetrics()
	globalName := "metis_test_global_only_total"
	global := prometheus.NewCounter(prometheus.CounterOpts{Name: globalName, Help: "global-only test collector"})
	if err := prometheus.Register(global); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { prometheus.Unregister(global) })

	families, err := metrics.Gatherer().Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, family := range families {
		if family.GetName() == globalName {
			t.Fatalf("private registry gathered global collector %q", globalName)
		}
	}
}
