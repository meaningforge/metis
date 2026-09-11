package ossie

import "testing"

func TestMetricFillSpecParsesZeroForNumericMetric(t *testing.T) {
	metric := &Metric{
		Name:     "revenue",
		Datatype: DataTypeDecimal,
		CustomExtensions: []CustomExtension{{
			VendorName: MetisExtensionVendor,
			Data:       `{"kind":"fill","policy":"zero"}`,
		}},
	}
	spec, ok, err := FillSpec(metric)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("fill spec not found")
	}
	if spec.Kind != MetricExtensionFill || spec.Policy != MetricFillZero {
		t.Fatalf("fill spec = %#v", spec)
	}
}

func TestMetricFillSpecPreservesNoneForNonNumericMetric(t *testing.T) {
	metric := &Metric{
		Name:     "status",
		Datatype: DataTypeString,
		CustomExtensions: []CustomExtension{{
			VendorName: MetisExtensionVendor,
			Data:       `{"kind":"fill","policy":"none"}`,
		}},
	}
	spec, ok, err := FillSpec(metric)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || spec.Policy != MetricFillNone {
		t.Fatalf("fill spec = %#v, found = %v", spec, ok)
	}
}

func TestMetricFillZeroFailsClosedForNonNumericMetric(t *testing.T) {
	metric := &Metric{
		Name:     "status",
		Datatype: DataTypeString,
		CustomExtensions: []CustomExtension{{
			VendorName: MetisExtensionVendor,
			Data:       `{"kind":"fill","policy":"zero"}`,
		}},
	}
	if _, _, err := FillSpec(metric); err == nil {
		t.Fatal("expected non-numeric zero fill to fail")
	}
}

func TestMetricFillRejectsUnknownPolicy(t *testing.T) {
	metric := &Metric{
		Name:     "revenue",
		Datatype: DataTypeDecimal,
		CustomExtensions: []CustomExtension{{
			VendorName: MetisExtensionVendor,
			Data:       `{"kind":"fill","policy":"literal"}`,
		}},
	}
	if _, _, err := FillSpec(metric); err == nil {
		t.Fatal("expected unsupported fill policy to fail")
	}
}
