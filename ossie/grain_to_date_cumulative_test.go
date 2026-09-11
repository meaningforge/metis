package ossie

import "testing"

func TestGrainToDateCumulativeSpec(t *testing.T) {
	metric := &Metric{CustomExtensions: []CustomExtension{{
		VendorName: MetisExtensionVendor,
		Data:       `{"kind":"cumulative","base_metric":"revenue","time_dimension":"order_date","window":{"type":"grain_to_date","unit":"year"}}`,
	}}}
	spec, ok, err := CumulativeSpec(metric)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("grain-to-date cumulative extension was not detected")
	}
	if spec.Window.Type != "grain_to_date" || spec.Window.Unit != "year" || spec.Window.Count != 0 {
		t.Fatalf("window = %#v", spec.Window)
	}
}

func TestGrainToDateCumulativeValidation(t *testing.T) {
	for _, unit := range []string{"year", "quarter", "month", "week", "day"} {
		t.Run(unit, func(t *testing.T) {
			err := ValidateCumulativeSpec(CumulativeMetricSpec{
				Kind: MetricExtensionCumulative, BaseMetric: "revenue", TimeDimension: "order_date",
				Window: CumulativeWindow{Type: "grain_to_date", Unit: unit},
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
	for name, window := range map[string]CumulativeWindow{
		"missing unit":    {Type: "grain_to_date"},
		"hour reset":      {Type: "grain_to_date", Unit: "hour"},
		"count forbidden": {Type: "grain_to_date", Unit: "year", Count: 2},
	} {
		t.Run(name, func(t *testing.T) {
			err := ValidateCumulativeSpec(CumulativeMetricSpec{
				Kind: MetricExtensionCumulative, BaseMetric: "revenue", TimeDimension: "order_date", Window: window,
			})
			if err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}
