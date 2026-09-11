package ossie

import "testing"

func TestMetricQualityClaimsParsesTypedEvidence(t *testing.T) {
	metric := &Metric{CustomExtensions: []CustomExtension{{
		VendorName: MetisExtensionVendor,
		Data:       `{"kind":"quality_claims","expected_type":"Decimal","expected_aggregation":"AGGREGATE"}`,
	}}}
	spec, ok, err := MetricQualityClaims(metric)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || spec.ExpectedType != DataTypeDecimal || spec.ExpectedAggregation != QualityAggregationAggregate {
		t.Fatalf("quality claims = %#v, present=%v", spec, ok)
	}
}

func TestMetricQualityClaimsRejectsUnstructuredOrUnsupportedEvidence(t *testing.T) {
	for _, data := range []string{
		`{"kind":"quality_claims"}`,
		`{"kind":"quality_claims","expected_aggregation":"sometimes"}`,
		`{"kind":"quality_claims","expected_type":"Money"}`,
	} {
		metric := &Metric{CustomExtensions: []CustomExtension{{VendorName: MetisExtensionVendor, Data: data}}}
		if _, _, err := MetricQualityClaims(metric); err == nil {
			t.Fatalf("expected invalid quality claims to fail: %s", data)
		}
	}
}
