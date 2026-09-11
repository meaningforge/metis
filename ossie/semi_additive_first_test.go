package ossie

import "testing"

func TestSemiAdditiveFirstAggregationContract(t *testing.T) {
	spec := SemiAdditiveMetricSpec{
		Kind:                 MetricExtensionSemiAdditive,
		BaseMetric:           "inventory_balance",
		NonAdditiveDimension: "snapshot_date",
		Aggregation:          "first",
	}
	if err := ValidateSemiAdditiveSpec(spec); err != nil {
		t.Fatal(err)
	}
}

func TestSemiAdditiveAggregationRejectsUnknownPolicy(t *testing.T) {
	spec := SemiAdditiveMetricSpec{
		Kind:                 MetricExtensionSemiAdditive,
		BaseMetric:           "inventory_balance",
		NonAdditiveDimension: "snapshot_date",
		Aggregation:          "median",
	}
	if err := ValidateSemiAdditiveSpec(spec); err == nil {
		t.Fatal("expected unsupported semi-additive aggregation to fail")
	}
}
