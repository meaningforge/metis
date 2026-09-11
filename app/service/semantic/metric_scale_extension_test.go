package semantic

import (
	"errors"
	"testing"

	"github.com/meaningforge/metis/extension"
	"github.com/meaningforge/metis/ossie"
)

func TestMetricScaleCapabilityFailsClosedUntilSelectedTargetRegistersSupport(t *testing.T) {
	model := &ossie.SemanticModel{
		Name: "sales",
		Metrics: []ossie.Metric{{
			Name: "revenue",
			CustomExtensions: []ossie.CustomExtension{{
				VendorName: extension.MetricScaleVendor,
				Data:       `{"kind":"metric_scale","version":"1","factor":2}`,
			}},
		}},
	}
	target := mustRenderer(t, "DORIS")
	service := &CompileService{extensionInventory: extension.NewInventory(), extensionCapabilities: extension.NewRegistry()}

	err := service.validateExtensionCapabilities(model, target)
	var unsupported *extension.UnsupportedError
	if !errors.As(err, &unsupported) || unsupported.Resolution.Reason != extension.ReasonCapabilityMissing {
		t.Fatalf("unsupported metric scale error = %v", err)
	}

	capabilities := extension.NewRegistry()
	if err := capabilities.Register(extension.MetricScaleRegistration(extension.RendererContext{Dialect: string(target.SQLDialect())})); err != nil {
		t.Fatal(err)
	}
	service.extensionCapabilities = capabilities
	if err := service.validateExtensionCapabilities(model, target); err != nil {
		t.Fatalf("registered metric scale target rejected: %v", err)
	}
	if err := service.validateExtensionCapabilities(model, mustRenderer(t, "CLICKHOUSE")); err == nil {
		t.Fatal("metric scale capability leaked across targets")
	}
}
