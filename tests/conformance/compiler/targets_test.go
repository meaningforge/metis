package compiler_test

import (
	"testing"

	"github.com/meaningforge/metis/tests/conformance/evidence"
	"github.com/meaningforge/metis/tests/conformance/scenarios"
)

type compilerTarget struct {
	Name         string
	Dialect      string
	Capabilities scenarios.CapabilitySet
}

func nativeCapabilities() scenarios.CapabilitySet {
	return scenarios.Capabilities(
		scenarios.CapabilityAggregation,
		scenarios.CapabilityDimension,
		scenarios.CapabilityRelationship,
		scenarios.CapabilityTemporalRelationship,
		scenarios.CapabilityFilter,
		scenarios.CapabilityOrdering,
		scenarios.CapabilityTimeGrain,
		scenarios.CapabilityDerived,
		scenarios.CapabilityRatio,
		scenarios.CapabilityMultiSource,
		scenarios.CapabilityCumulative,
		scenarios.CapabilityTimeOffset,
		scenarios.CapabilityMetricFilter,
		scenarios.CapabilitySemiAdditive,
		scenarios.CapabilitySemiAdditiveTieBreak,
		scenarios.CapabilitySemiAdditiveNullSkip,
		scenarios.CapabilitySemiAdditiveQueryGrain,
		scenarios.CapabilitySemiAdditiveWindowGrouping,
		scenarios.CapabilitySemiAdditiveRollup,
		scenarios.CapabilityDistinctValues,
		scenarios.CapabilityDefinitionFilter,
		scenarios.CapabilityConversion,
		scenarios.CapabilityOffsetToGrain,
		scenarios.CapabilityCustomCalendar,
		scenarios.CapabilityDenseCalendar,
	)
}

var compilerTargets = func() []compilerTarget {
	registered := evidence.CompilerTargets()
	targets := make([]compilerTarget, len(registered))
	for i, target := range registered {
		targets[i] = compilerTarget{Name: target.Name, Dialect: target.Dialect, Capabilities: nativeCapabilities()}
	}
	return targets
}()

func TestCompilerTargetRegistry(t *testing.T) {
	seenNames := map[string]struct{}{}
	seenDialects := map[string]struct{}{}
	if len(compilerTargets) == 0 {
		t.Fatal("compiler target registry is empty")
	}
	for _, target := range compilerTargets {
		if target.Name == "" || target.Dialect == "" {
			t.Fatalf("compiler target must have name and dialect: %#v", target)
		}
		if _, ok := seenNames[target.Name]; ok {
			t.Fatalf("duplicate compiler target name %q", target.Name)
		}
		if _, ok := seenDialects[target.Dialect]; ok {
			t.Fatalf("duplicate compiler target dialect %q", target.Dialect)
		}
		seenNames[target.Name] = struct{}{}
		seenDialects[target.Dialect] = struct{}{}
	}
}
