package baseline_test

import (
	"reflect"
	"testing"

	"github.com/meaningforge/metis/planner/conversion"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/sqlplan"
	"github.com/meaningforge/metis/tests/conformance/baseline"
	"github.com/meaningforge/metis/tests/conformance/evidence"
	"github.com/meaningforge/metis/tests/conformance/scenarios"
)

func TestSQLPlanRenderersAreDeterministicAndImmutableAcrossFullCorpus(t *testing.T) {
	for _, targetEvidence := range evidence.CompilerTargets() {
		targetEvidence := targetEvidence
		t.Run(targetEvidence.Dialect, func(t *testing.T) {
			renderer := mustRenderer(t, targetEvidence.Dialect)
			compared := 0
			for _, scenario := range scenarios.Core {
				semantic, _, err := baseline.PlanScenario(scenario, targetEvidence.Dialect)
				if err != nil {
					t.Fatalf("%s: plan: %v", scenario.Name, err)
				}
				before, err := semanticplan.Fingerprint(semantic)
				if err != nil {
					t.Fatalf("%s: fingerprint before: %v", scenario.Name, err)
				}
				physical, err := conversion.BuildSQLPlan(semantic, renderer)
				if err != nil {
					t.Fatalf("%s: BuildSQLPlan: %v", scenario.Name, err)
				}
				after, err := semanticplan.Fingerprint(semantic)
				if err != nil || before != after {
					t.Fatalf("%s: BuildSQLPlan mutated SemanticPlan", scenario.Name)
				}
				originalPhysical, err := sqlplan.Fingerprint(physical)
				if err != nil {
					t.Fatalf("%s: fingerprint SQLPlan: %v", scenario.Name, err)
				}
				firstQuery, err := renderer.Render(physical)
				if err != nil {
					t.Fatalf("%s: first render: %v", scenario.Name, err)
				}
				secondQuery, err := renderer.Render(physical)
				if err != nil {
					t.Fatalf("%s: second render: %v", scenario.Name, err)
				}
				if !reflect.DeepEqual(firstQuery, secondQuery) {
					t.Errorf("%s: renderer is nondeterministic", scenario.Name)
				}
				afterRender, err := sqlplan.Fingerprint(physical)
				if err != nil || afterRender != originalPhysical {
					t.Errorf("%s: renderer mutated SQLPlan", scenario.Name)
				}
				if len(physical.Blocks) != 0 && len(physical.Blocks[0].Projections) != 0 {
					physical.Blocks[0].Projections[0].Alias += "_mutated"
				}
				rebuilt, err := conversion.BuildSQLPlan(semantic, renderer)
				if err != nil {
					t.Fatalf("%s: rebuild: %v", scenario.Name, err)
				}
				rebuiltFingerprint, err := sqlplan.Fingerprint(rebuilt)
				if err != nil || rebuiltFingerprint != originalPhysical {
					t.Errorf("%s: returned SQLPlan is not fully owned", scenario.Name)
				}
				compared++
			}
			if compared != 97 {
				t.Fatalf("compared %d scenarios, want 97", compared)
			}
		})
	}
}
