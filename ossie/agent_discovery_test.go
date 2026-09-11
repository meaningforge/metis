package ossie

import (
	"slices"
	"testing"
)

func TestAgentDiscoverySpecReturnsValidatedAuthoredAliases(t *testing.T) {
	metric := &Metric{CustomExtensions: []CustomExtension{{
		VendorName: MetisExtensionVendor,
		Data:       `{"kind":"agent_discovery","aliases":["merchandise sales","GMV"]}`,
	}}}
	spec, ok, err := AgentDiscoverySpec(metric)
	if err != nil || !ok {
		t.Fatalf("AgentDiscoverySpec() = %#v, %v, %v", spec, ok, err)
	}
	if !slices.Equal(spec.Aliases, []string{"GMV", "merchandise sales"}) {
		t.Fatalf("aliases = %v", spec.Aliases)
	}
}

func TestAgentDiscoverySpecRejectsDuplicateAliases(t *testing.T) {
	metric := &Metric{CustomExtensions: []CustomExtension{{
		VendorName: MetisExtensionVendor,
		Data:       `{"kind":"agent_discovery","aliases":["GMV"," gmv "]}`,
	}}}
	if _, _, err := AgentDiscoverySpec(metric); err == nil {
		t.Fatal("expected duplicate alias error")
	}
}
