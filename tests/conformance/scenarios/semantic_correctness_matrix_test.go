package scenarios

import "testing"

func TestHighRiskSemanticCapabilitiesRequireResultEvidence(t *testing.T) {
	highRisk := []Capability{
		CapabilityDerived,
		CapabilityRatio,
		CapabilityMultiSource,
		CapabilityCumulative,
		CapabilityTimeOffset,
		CapabilityMetricFilter,
		CapabilityDefinitionFilter,
		CapabilityConversion,
		CapabilitySemiAdditive,
		CapabilityOffsetToGrain,
		CapabilityCustomCalendar,
		CapabilityDenseCalendar,
	}

	for _, capability := range highRisk {
		covered := 0
		for _, scenario := range Core {
			if !scenario.RequiresCapability(capability) {
				continue
			}
			covered++
			if scenario.Verification.Result != ContractRequired || scenario.ExpectedResult == nil {
				t.Fatalf("high-risk capability %q has compiler-only scenario %q", capability, scenario.Name)
			}
		}
		if covered == 0 {
			t.Fatalf("high-risk capability %q has no canonical correctness scenario", capability)
		}
	}
}

func TestSemanticCorrectnessMatrixCoversComposedTransforms(t *testing.T) {
	required := []struct {
		name         string
		capabilities []Capability
	}{
		{name: "derived over time offset", capabilities: []Capability{CapabilityDerived, CapabilityTimeOffset}},
		{name: "multi-source derived metric", capabilities: []Capability{CapabilityDerived, CapabilityMultiSource}},
		{name: "cumulative with metric filter", capabilities: []Capability{CapabilityCumulative, CapabilityMetricFilter}},
		{name: "time offset with metric filter", capabilities: []Capability{CapabilityTimeOffset, CapabilityMetricFilter}},
		{name: "post-aggregation definition filter", capabilities: []Capability{CapabilityDerived, CapabilityDefinitionFilter}},
		{name: "custom calendar time offset", capabilities: []Capability{CapabilityCustomCalendar, CapabilityTimeOffset}},
		{name: "custom calendar cumulative", capabilities: []Capability{CapabilityCustomCalendar, CapabilityCumulative}},
		{name: "custom calendar semi-additive", capabilities: []Capability{CapabilityCustomCalendar, CapabilitySemiAdditive}},
		{name: "dense custom calendar", capabilities: []Capability{CapabilityCustomCalendar, CapabilityDenseCalendar}},
		{name: "conversion with filter", capabilities: []Capability{CapabilityConversion, CapabilityFilter}},
	}

	for _, expectation := range required {
		matched := false
		for _, scenario := range ExecutionCore() {
			if scenario.RequiresCapabilities(expectation.capabilities...) {
				matched = true
				break
			}
		}
		if !matched {
			t.Fatalf("semantic correctness matrix is missing result evidence for %s", expectation.name)
		}
	}
}

func (scenario Scenario) RequiresCapability(capability Capability) bool {
	for _, required := range scenario.Requires {
		if required == capability {
			return true
		}
	}
	return false
}

func (scenario Scenario) RequiresCapabilities(capabilities ...Capability) bool {
	for _, capability := range capabilities {
		if !scenario.RequiresCapability(capability) {
			return false
		}
	}
	return true
}
