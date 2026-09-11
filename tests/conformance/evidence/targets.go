package evidence

import (
	"strings"

	"github.com/meaningforge/metis/tests/conformance/scenarios"
)

// Target describes which executable correctness layers are registered for one
// SQL dialect. It is test evidence only and must not be interpreted as a
// production-support or SLA claim.
type Target struct {
	Name         string
	Dialect      string
	Compiler     bool
	RealEngine   bool
	Capabilities scenarios.CapabilitySet
}

var realEngineCapabilities = scenarios.Capabilities(
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

var Targets = []Target{
	{Name: "DuckDB", Dialect: "DUCKDB", Compiler: true, RealEngine: true, Capabilities: cloneCapabilities(realEngineCapabilities)},
	{Name: "Doris", Dialect: "DORIS", Compiler: true, RealEngine: true, Capabilities: cloneCapabilities(realEngineCapabilities)},
	{Name: "ClickHouse", Dialect: "CLICKHOUSE", Compiler: true, RealEngine: true, Capabilities: cloneCapabilities(realEngineCapabilities)},
}

func CompilerTargets() []Target {
	return filterTargets(func(target Target) bool { return target.Compiler })
}

func RealEngineTargets() []Target {
	return filterTargets(func(target Target) bool { return target.RealEngine })
}

func IsRealEngineTarget(dialect string) bool {
	_, ok := RealEngineCapabilities(dialect)
	return ok
}

func RealEngineCapabilities(dialect string) (scenarios.CapabilitySet, bool) {
	for _, target := range Targets {
		if target.Dialect == strings.ToUpper(strings.TrimSpace(dialect)) && target.RealEngine {
			return cloneCapabilities(target.Capabilities), true
		}
	}
	return nil, false
}

func filterTargets(include func(Target) bool) []Target {
	filtered := make([]Target, 0, len(Targets))
	for _, target := range Targets {
		if include(target) {
			target.Capabilities = cloneCapabilities(target.Capabilities)
			filtered = append(filtered, target)
		}
	}
	return filtered
}

func cloneCapabilities(source scenarios.CapabilitySet) scenarios.CapabilitySet {
	if source == nil {
		return nil
	}
	cloned := make(scenarios.CapabilitySet, len(source))
	for capability := range source {
		cloned[capability] = struct{}{}
	}
	return cloned
}
