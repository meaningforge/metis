package builder

import "github.com/meaningforge/metis/ossie"

func conversionDependenciesMatch(dependencies []string, spec ossie.ConversionMetricSpec) bool {
	if len(dependencies) != 2 {
		return false
	}
	seen := map[string]int{}
	for _, dependency := range dependencies {
		seen[dependency]++
	}
	return seen[spec.BaseMetric] == 1 && seen[spec.ConversionMetric] == 1
}
