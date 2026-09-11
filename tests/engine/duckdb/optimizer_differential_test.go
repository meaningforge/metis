//go:build duckdb

package duckdb_test

import (
	"context"
	"testing"

	"github.com/meaningforge/metis/tests/conformance/scenarios"
	"github.com/meaningforge/metis/tests/engine/duckdb/fixture"
	"github.com/meaningforge/metis/tests/engine/harness"
)

// Optimizing a query must not change its answer.
//
// Compiler conformance compares each renderer against its own golden SQL, so an
// optimizer rewrite that is wrong for one engine's evaluation order still looks
// correct there. Only executing both forms on the engine settles it, which is
// why Doris and ClickHouse each run this and why DuckDB needs to as well.
func TestOptimizerDifferentialExecution(t *testing.T) {
	backend, err := fixture.New(databasePath(t))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	t.Cleanup(func() { _ = backend.Close(ctx) })

	for _, scenario := range scenarios.OptimizerDifferentialCore() {
		if scenario.ExpectedResult == nil {
			t.Fatalf("scenario %q has no normalized result contract", scenario.Name)
		}
		t.Run(scenario.Name, func(t *testing.T) {
			if err := backend.PrepareFixture(ctx, scenario.Fixture); err != nil {
				t.Fatal(err)
			}
			execution := openProductionExecution(t, backend.Database)
			optimized := execution.RunCompiled(t, scenario.Name+"/optimized", execution.CompileScenario(t, scenario, true))
			unoptimized := execution.RunCompiled(t, scenario.Name+"/unoptimized", execution.CompileScenario(t, scenario, false))
			if err := harness.CompareNormalizedResults(scenario.ExpectedResult.Comparison, optimized, unoptimized); err != nil {
				t.Fatalf("optimized/unoptimized result mismatch: %v", err)
			}
		})
	}
}
