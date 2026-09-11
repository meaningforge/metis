package scenarios

import "testing"

func TestOptimizerDifferentialCoreHasResultContracts(t *testing.T) {
	seen := map[string]struct{}{}
	for _, scenario := range OptimizerDifferentialCore() {
		if _, ok := seen[scenario.Name]; ok {
			t.Fatalf("duplicate optimizer differential scenario %q", scenario.Name)
		}
		seen[scenario.Name] = struct{}{}
		if scenario.Verification.Result != ContractRequired || scenario.ExpectedResult == nil {
			t.Fatalf("optimizer differential scenario %q lacks result evidence", scenario.Name)
		}
	}
}

func TestOptimizerDifferentialCoreCoversHighRiskSemantics(t *testing.T) {
	required := []struct {
		name         string
		capabilities []Capability
	}{
		{name: "derived", capabilities: []Capability{CapabilityDerived}},
		{name: "multi-source", capabilities: []Capability{CapabilityMultiSource}},
		{name: "relationship filter and ordering", capabilities: []Capability{CapabilityRelationship, CapabilityFilter, CapabilityOrdering}},
		{name: "temporal relationship", capabilities: []Capability{CapabilityTemporalRelationship}},
		{name: "cumulative", capabilities: []Capability{CapabilityCumulative}},
		{name: "cumulative metric filter", capabilities: []Capability{CapabilityCumulative, CapabilityMetricFilter}},
		{name: "time offset", capabilities: []Capability{CapabilityTimeOffset}},
		{name: "derived time offset", capabilities: []Capability{CapabilityDerived, CapabilityTimeOffset}},
		{name: "metric filter", capabilities: []Capability{CapabilityMetricFilter}},
		{name: "definition filter", capabilities: []Capability{CapabilityDefinitionFilter}},
		{name: "conversion with filter", capabilities: []Capability{CapabilityConversion, CapabilityFilter}},
		{name: "semi-additive", capabilities: []Capability{CapabilitySemiAdditive}},
		{name: "semi-additive tie break", capabilities: []Capability{CapabilitySemiAdditive, CapabilitySemiAdditiveTieBreak}},
		{name: "semi-additive null skip", capabilities: []Capability{CapabilitySemiAdditive, CapabilitySemiAdditiveNullSkip}},
		{name: "semi-additive grouped rollup", capabilities: []Capability{CapabilitySemiAdditive, CapabilitySemiAdditiveWindowGrouping, CapabilitySemiAdditiveRollup}},
		{name: "offset to grain", capabilities: []Capability{CapabilityOffsetToGrain}},
		{name: "custom calendar dense", capabilities: []Capability{CapabilityCustomCalendar, CapabilityDenseCalendar}},
		{name: "custom calendar cumulative", capabilities: []Capability{CapabilityCustomCalendar, CapabilityCumulative}},
		{name: "custom calendar cumulative filter", capabilities: []Capability{CapabilityCustomCalendar, CapabilityCumulative, CapabilityFilter}},
		{name: "custom calendar semi-additive", capabilities: []Capability{CapabilityCustomCalendar, CapabilitySemiAdditive}},
		{name: "distinct values", capabilities: []Capability{CapabilityDistinctValues}},
	}

	corpus := OptimizerDifferentialCore()
	for _, expectation := range required {
		matched := false
		for _, scenario := range corpus {
			if scenario.RequiresCapabilities(expectation.capabilities...) {
				matched = true
				break
			}
		}
		if !matched {
			t.Fatalf("optimizer differential corpus is missing %s evidence", expectation.name)
		}
	}
}
