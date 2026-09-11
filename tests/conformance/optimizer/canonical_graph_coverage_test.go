package optimizer_test

import (
	"testing"

	"github.com/meaningforge/metis/planner/conversion"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/tests/conformance/evidence"
	"github.com/meaningforge/metis/tests/conformance/scenarios"
)

// The typed SemanticPlanNode DAG is the universal plan-owned
// semantic authority. Every scenario must therefore reach lowering with Nodes.
func TestEveryScenarioLowersFromPlanOwnership(t *testing.T) {
	for _, target := range evidence.CompilerTargets() {
		t.Run(target.Dialect, func(t *testing.T) {
			compact, composed := 0, 0
			for _, scenario := range scenarios.Core {
				plan, _, renderer := planScenario(t, scenario, target.Dialect)

				if len(plan.Nodes) == 0 {
					t.Errorf("%s: plan owns no typed nodes, so lowering has nothing to read", scenario.Name)
					continue
				}
				if _, err := conversion.BuildSQLPlan(plan, renderer); err != nil {
					t.Errorf("%s: %v", scenario.Name, err)
					continue
				}
				switch conversion.LoweringStrategyForPlan(plan) {
				case conversion.SemanticLoweringCompact:
					compact++
				default:
					composed++
				}
			}
			if compact+composed != len(scenarios.Core) {
				t.Errorf("lowered %d of %d scenarios", compact+composed, len(scenarios.Core))
			}
			t.Logf("%s: compact=%d composed=%d", target.Dialect, compact, composed)
		})
	}
}

// Plan DAG validation must accept every production plan built across the corpus.
func TestEveryStagedScenarioBuildsAGraphValidationAccepts(t *testing.T) {
	for _, target := range evidence.CompilerTargets() {
		t.Run(target.Dialect, func(t *testing.T) {
			for _, scenario := range scenarios.Core {
				plan, _, _ := planScenario(t, scenario, target.Dialect)
				if len(plan.Nodes) == 0 {
					continue
				}
				if err := semanticplan.ValidateSemanticPlanDAG(plan); err != nil {
					t.Errorf("%s: %v", scenario.Name, err)
				}
			}
		})
	}
}
