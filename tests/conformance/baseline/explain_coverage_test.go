package baseline_test

import (
	"testing"

	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/tests/conformance/baseline"
	"github.com/meaningforge/metis/tests/conformance/evidence"
	"github.com/meaningforge/metis/tests/conformance/scenarios"
)

// Explain and lineage consume SemanticPlan.
// Typed nodes are the plan-owned DAG authority.
func TestEveryPlanIsExplainable(t *testing.T) {
	for _, target := range evidence.CompilerTargets() {
		t.Run(target.Dialect, func(t *testing.T) {
			for _, scenario := range scenarios.Core {
				plan, _, err := baseline.PlanScenario(scenario, target.Dialect)
				if err != nil {
					t.Fatalf("%s: %v", scenario.Name, err)
				}

				explanation, err := semanticplan.Explain(plan)
				if err != nil {
					t.Errorf("%s: %v", scenario.Name, err)
					continue
				}
				if len(explanation.Nodes) != len(plan.Nodes) {
					t.Errorf("%s: explanation describes %d nodes, plan owns %d",
						scenario.Name, len(explanation.Nodes), len(plan.Nodes))
				}

				lineage := map[string]bool{}
				for _, entry := range explanation.Lineage {
					lineage[entry.Name] = true
				}
				for _, metric := range scenario.Query.Metrics {
					if !lineage[metric.Name] {
						t.Errorf("%s: metric %q has no lineage in the explanation", scenario.Name, metric.Name)
					}
				}
				for _, dimension := range scenario.Query.Dimensions {
					if !lineage[dimension.Name] {
						t.Errorf("%s: dimension %q has no lineage in the explanation", scenario.Name, dimension.Name)
					}
				}
			}
		})
	}
}

func TestEveryPlanReportsItsStructuralShape(t *testing.T) {
	for _, target := range evidence.CompilerTargets() {
		t.Run(target.Dialect, func(t *testing.T) {
			for _, scenario := range scenarios.Core {
				plan, _, err := baseline.PlanScenario(scenario, target.Dialect)
				if err != nil {
					t.Fatalf("%s: %v", scenario.Name, err)
				}
				summary := semanticplan.SummarizeSemanticPlan(plan)
				if summary.EvaluationNodes != len(plan.Nodes) {
					t.Errorf("%s: shape reports %d evaluation nodes, plan owns %d nodes",
						scenario.Name, summary.EvaluationNodes, len(plan.Nodes))
				}
			}
		})
	}
}
