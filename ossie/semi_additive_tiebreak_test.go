package ossie

import "testing"

func TestSemiAdditiveTieBreakDimension(t *testing.T) {
	metric := &Metric{CustomExtensions: []CustomExtension{{
		VendorName: MetisExtensionVendor,
		Data:       `{"kind":"semi_additive","base_metric":"inventory_balance","non_additive_dimension":"snapshot_date","aggregation":"last","tie_break_dimension":"snapshot_sequence"}`,
	}}}
	spec, ok, err := SemiAdditiveSpec(metric)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("semi-additive extension was not detected")
	}
	if spec.TieBreakDimension != "snapshot_sequence" {
		t.Fatalf("tie_break_dimension = %q", spec.TieBreakDimension)
	}
}

func TestSemiAdditiveTieBreakDimensionIsOptional(t *testing.T) {
	spec := SemiAdditiveMetricSpec{Kind: MetricExtensionSemiAdditive, BaseMetric: "inventory_balance", NonAdditiveDimension: "snapshot_date", Aggregation: "first"}
	if err := ValidateSemiAdditiveSpec(spec); err != nil {
		t.Fatal(err)
	}
}

func TestSemiAdditiveTieBreakMustDifferFromOrderingDimension(t *testing.T) {
	spec := SemiAdditiveMetricSpec{Kind: MetricExtensionSemiAdditive, BaseMetric: "inventory_balance", NonAdditiveDimension: "snapshot_date", Aggregation: "last", TieBreakDimension: "snapshot_date"}
	if err := ValidateSemiAdditiveSpec(spec); err == nil {
		t.Fatal("expected duplicate ordering and tie-break dimensions to fail")
	}
}
