package ossie

import "testing"

func TestCustomCalendarRollingCumulativeContract(t *testing.T) {
	model := &SemanticModel{CustomExtensions: []CustomExtension{{
		VendorName: MetisExtensionVendor,
		Data:       `{"kind":"custom_calendar","dataset":"calendar","base_time_dimension":"order_date","grains":[{"name":"fiscal_week","bucket_dimension":"fiscal_week_start","ordinal_dimension":"fiscal_week_ordinal","dense_mapping":true}]}`,
	}}}
	spec := CumulativeMetricSpec{
		Kind:          MetricExtensionCumulative,
		BaseMetric:    "revenue",
		TimeDimension: "order_date",
		Window:        CumulativeWindow{Type: "rolling", Count: 3, Unit: "fiscal_week"},
	}
	if err := ValidateCumulativeSpec(spec); err != nil {
		t.Fatalf("structural validation rejected custom rolling unit: %v", err)
	}
	if err := ValidateCumulativeCalendarModel(model, spec); err != nil {
		t.Fatalf("custom rolling calendar validation: %v", err)
	}
}

func TestCustomCalendarRollingCumulativeRequiresDenseMapping(t *testing.T) {
	model := &SemanticModel{CustomExtensions: []CustomExtension{{
		VendorName: MetisExtensionVendor,
		Data:       `{"kind":"custom_calendar","dataset":"calendar","base_time_dimension":"order_date","grains":[{"name":"fiscal_week","bucket_dimension":"fiscal_week_start","ordinal_dimension":"fiscal_week_ordinal"}]}`,
	}}}
	spec := CumulativeMetricSpec{Kind: MetricExtensionCumulative, BaseMetric: "revenue", TimeDimension: "order_date", Window: CumulativeWindow{Type: "rolling", Count: 3, Unit: "fiscal_week"}}
	if err := ValidateCumulativeCalendarModel(model, spec); err == nil {
		t.Fatal("expected dense_mapping requirement")
	}
}

func TestCustomCalendarRollingCumulativeRejectsUnknownOrWrongBase(t *testing.T) {
	model := &SemanticModel{CustomExtensions: []CustomExtension{{
		VendorName: MetisExtensionVendor,
		Data:       `{"kind":"custom_calendar","dataset":"calendar","base_time_dimension":"order_date","grains":[{"name":"fiscal_week","bucket_dimension":"fiscal_week_start","ordinal_dimension":"fiscal_week_ordinal","dense_mapping":true}]}`,
	}}}
	unknown := CumulativeMetricSpec{Kind: MetricExtensionCumulative, BaseMetric: "revenue", TimeDimension: "order_date", Window: CumulativeWindow{Type: "rolling", Count: 3, Unit: "retail_period"}}
	if err := ValidateCumulativeCalendarModel(model, unknown); err == nil {
		t.Fatal("expected unknown custom rolling unit to be rejected")
	}
	wrongBase := CumulativeMetricSpec{Kind: MetricExtensionCumulative, BaseMetric: "revenue", TimeDimension: "ship_date", Window: CumulativeWindow{Type: "rolling", Count: 3, Unit: "fiscal_week"}}
	if err := ValidateCumulativeCalendarModel(model, wrongBase); err == nil {
		t.Fatal("expected non-canonical custom-calendar time dimension to be rejected")
	}
}

func TestBuiltInRollingCumulativeDoesNotRequireCustomCalendar(t *testing.T) {
	spec := CumulativeMetricSpec{Kind: MetricExtensionCumulative, BaseMetric: "revenue", TimeDimension: "order_date", Window: CumulativeWindow{Type: "rolling", Count: 3, Unit: "month"}}
	if err := ValidateCumulativeCalendarModel(&SemanticModel{}, spec); err != nil {
		t.Fatalf("built-in rolling unit should bypass custom-calendar validation: %v", err)
	}
}
