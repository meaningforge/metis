package harness

import (
	"strings"
	"testing"

	"github.com/meaningforge/metis/tests/conformance/scenarios"
)

func TestRatioAttributionBundleEvidenceIsCanonicalAndFilteredPerPeriod(t *testing.T) {
	queries := CompileRatioAttributionBundleEvidence(t, "DUCKDB", []string{"segment", "channel"}, true)
	if len(queries) != 2 || queries[0].Dimension != "channel" || queries[1].Dimension != "segment" {
		t.Fatalf("ratio attribution bundle = %#v", queries)
	}
	for _, query := range queries {
		if scans := strings.Count(query.SQLRenderResult.SQL, `FROM "analytics"."ratio_attribution_events"`); scans != 2 {
			t.Fatalf("dimension %q source scans = %d\nSQL:\n%s", query.Dimension, scans, query.SQLRenderResult.SQL)
		}
		filters := 0
		for _, parameter := range query.SQLRenderResult.Parameters {
			if parameter.Value == "included" {
				filters++
			}
		}
		if filters != 2 {
			t.Fatalf("dimension %q filter bindings = %d", query.Dimension, filters)
		}
	}
}

func TestRatioTruthAcceptsLogicalAndIntegerWireRepresentations(t *testing.T) {
	for _, value := range []scenarios.ResultValue{
		{Canonical: "true"},
		{Canonical: "1"},
	} {
		if !ratioTruth(value) {
			t.Fatalf("truthy ratio evidence rejected: %#v", value)
		}
	}
	for _, value := range []scenarios.ResultValue{
		{Canonical: "false"},
		{Canonical: "0"},
		{Canonical: "1", Null: true},
	} {
		if ratioTruth(value) {
			t.Fatalf("false ratio evidence accepted: %#v", value)
		}
	}
}
