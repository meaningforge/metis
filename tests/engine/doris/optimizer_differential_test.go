package doris_test

import (
	"database/sql"
	"testing"

	_ "github.com/go-sql-driver/mysql"

	"github.com/meaningforge/metis/tests/conformance/scenarios"
	"github.com/meaningforge/metis/tests/engine/harness"
)

func TestOptimizerDifferentialExecution(t *testing.T) {
	dsn := dorisDSN(t)
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Ping(); err != nil {
		t.Fatalf("connect to Doris: %v", err)
	}
	waitForBackend(t, db)

	for _, scenario := range scenarios.OptimizerDifferentialCore() {
		scenario := scenario
		if scenario.ExpectedResult == nil {
			t.Fatalf("scenario %q has no normalized result contract", scenario.Name)
		}
		t.Run(scenario.Name, func(t *testing.T) {
			prepareFixture(t, db, scenario.Fixture)
			execution := openProductionExecution(t)
			optimized := execution.RunCompiled(t, scenario.Name+"/optimized", execution.CompileScenario(t, scenario, true))
			unoptimized := execution.RunCompiled(t, scenario.Name+"/unoptimized", execution.CompileScenario(t, scenario, false))
			if err := harness.CompareNormalizedResults(scenario.ExpectedResult.Comparison, optimized, unoptimized); err != nil {
				t.Fatalf("optimized/unoptimized result mismatch: %v", err)
			}
		})
	}
}
