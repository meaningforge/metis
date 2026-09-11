package extension

import (
	"testing"

	"github.com/meaningforge/metis/ossie"
)

func TestMetricScaleInterpreterProducesCriticalRequirementAndTypedEvidence(t *testing.T) {
	metric := &ossie.Metric{
		Name: "revenue",
		CustomExtensions: []ossie.CustomExtension{{
			VendorName: MetricScaleVendor,
			Data:       `{"kind":"metric_scale","version":"1","factor":2}`,
		}},
	}
	interpreter := MetricScaleInterpreter{}
	requirements, err := interpreter.Requirements(Location{Scope: "metric", Owner: metric.Name}, metric.CustomExtensions[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(requirements) != 1 || !requirements[0].Critical || requirements[0].Capability != MetricScaleCapability {
		t.Fatalf("requirements = %#v", requirements)
	}
	evidence, err := MetricScaleEvidenceForMetric(metric)
	if err != nil {
		t.Fatal(err)
	}
	if len(evidence) != 1 || evidence[0].Metric != "revenue" || evidence[0].Factor != 2 {
		t.Fatalf("evidence = %#v", evidence)
	}

	target := RendererContext{Dialect: "DORIS"}
	registry := NewRegistry()
	if got := registry.Resolve(requirements[0], target); got.State != StateUnsupportedCritical || got.Reason != ReasonCapabilityMissing {
		t.Fatalf("unregistered resolution = %#v", got)
	}
	if err := registry.Register(MetricScaleRegistration(target)); err != nil {
		t.Fatal(err)
	}
	if got := registry.Resolve(requirements[0], target); got.State != StateSupported {
		t.Fatalf("registered resolution = %#v", got)
	}
	if got := registry.Resolve(requirements[0], RendererContext{Dialect: "CLICKHOUSE"}); got.State != StateUnsupportedCritical {
		t.Fatalf("foreign target resolution = %#v", got)
	}
}

func TestMetricScaleInterpreterRejectsInvalidSemanticPayload(t *testing.T) {
	_, err := (MetricScaleInterpreter{}).Requirements(
		Location{Scope: "metric", Owner: "revenue"},
		ossie.CustomExtension{VendorName: MetricScaleVendor, Data: `{"kind":"metric_scale","version":"1","factor":0}`},
	)
	if err == nil {
		t.Fatal("expected invalid scale factor to fail closed")
	}
}
