package ossie

import "testing"

func TestTimeOffsetFixedUnits(t *testing.T) {
	for _, unit := range []string{"hour", "day", "week", "month", "quarter", "year"} {
		t.Run(unit, func(t *testing.T) {
			spec := TimeOffsetMetricSpec{
				Kind:          MetricExtensionTimeOffset,
				BaseMetric:    "revenue",
				TimeDimension: "order_date",
				Offset:        TimeOffset{Count: -1, Unit: unit},
			}
			if err := ValidateTimeOffsetSpec(spec); err != nil {
				t.Fatalf("fixed %s offset rejected: %v", unit, err)
			}
		})
	}
}

func TestTimeOffsetFixedUnitsRejectInvalidContract(t *testing.T) {
	cases := []TimeOffsetMetricSpec{
		{Kind: MetricExtensionTimeOffset, BaseMetric: "revenue", TimeDimension: "order_date", Offset: TimeOffset{Count: 0, Unit: "day"}},
		{Kind: MetricExtensionTimeOffset, BaseMetric: "revenue", TimeDimension: "order_date", Offset: TimeOffset{Count: 1, Unit: "week"}},
		{Kind: MetricExtensionTimeOffset, BaseMetric: "revenue", TimeDimension: "order_date", Offset: TimeOffset{Count: -1, Unit: ""}},
	}
	for i, spec := range cases {
		if err := ValidateTimeOffsetSpec(spec); err == nil {
			t.Fatalf("case %d unexpectedly accepted: %#v", i, spec)
		}
	}
}
