package clickhouse_test

import (
	"testing"

	"github.com/meaningforge/metis/tests/conformance/scenarios"
	"github.com/meaningforge/metis/tests/engine/harness"
)

func TestOptimizerDifferentialExecution(t *testing.T) {
	url := clickHouseURL(t)

	for _, scenario := range scenarios.OptimizerDifferentialCore() {
		scenario := scenario
		if scenario.ExpectedResult == nil {
			t.Fatalf("scenario %q has no normalized result contract", scenario.Name)
		}
		t.Run(scenario.Name, func(t *testing.T) {
			prepareFixture(t, url, scenario.Fixture)
			execution := openProductionExecution(t)
			optimized := execution.RunCompiled(t, scenario.Name+"/optimized", execution.CompileScenario(t, scenario, true))
			unoptimized := execution.RunCompiled(t, scenario.Name+"/unoptimized", execution.CompileScenario(t, scenario, false))
			if err := harness.CompareNormalizedResults(scenario.ExpectedResult.Comparison, optimized, unoptimized); err != nil {
				t.Fatalf("optimized/unoptimized result mismatch: %v", err)
			}
		})
	}
}
