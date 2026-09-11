package builder

import (
	"testing"

	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/tests/conformance/scenarios"
)

func TestSemanticPlanNodeClosesOverConformanceCorpus(t *testing.T) {
	seenKinds := map[semanticplan.SemanticPlanNodeKind]int{}
	plans := 0
	nodes := 0

	for _, dialect := range []string{"DUCKDB", "DORIS", "CLICKHOUSE"} {
		t.Run(dialect, func(t *testing.T) {
			for _, scenario := range semanticPlanNodeClosureScenarios(t) {
				plan := planCorpusScenario(t, scenario, dialect)
				plans++
				if len(plan.Nodes) == 0 {
					t.Fatalf("%s: production planner emitted no canonical semantic nodes", scenario.Name)
				}

				before, err := semanticplan.Fingerprint(plan)
				if err != nil {
					t.Fatalf("%s: fingerprint before node validation: %v", scenario.Name, err)
				}
				for _, node := range plan.Nodes {
					if err := semanticplan.ValidateNode(node); err != nil {
						t.Fatalf("%s/%s: validate canonical node: %v", scenario.Name, semanticplan.NodeBaseID(node), err)
					}
					seenKinds[node.Kind()]++
					nodes++
				}
				after, err := semanticplan.Fingerprint(plan)
				if err != nil {
					t.Fatalf("%s: fingerprint after node validation: %v", scenario.Name, err)
				}
				if before != after {
					t.Fatalf("%s: canonical node validation mutated plan fingerprint: %q != %q", scenario.Name, before, after)
				}
			}
		})
	}

	if plans == 0 || nodes == 0 {
		t.Fatalf("node closure inspected plans=%d nodes=%d; corpus evidence is empty", plans, nodes)
	}
	for _, kind := range semanticPlanNodeKinds() {
		if seenKinds[kind] == 0 {
			t.Errorf("node closure corpus emitted no %q node", kind)
		}
	}
}

func semanticPlanNodeClosureScenarios(t *testing.T) []scenarios.Scenario {
	t.Helper()
	out := append([]scenarios.Scenario(nil), scenarios.Core...)
	for _, scenario := range scenarios.Core {
		if scenario.Name != "multi_source_derived_at_grain" {
			continue
		}
		scalar := scenario
		scalar.Name = "multi_source_derived_ungrouped_node_closure"
		scalar.Query.Dimensions = nil
		return append(out, scalar)
	}
	t.Fatal("node closure requires multi_source_derived_at_grain conformance scenario")
	return nil
}

func semanticPlanNodeKinds() []semanticplan.SemanticPlanNodeKind {
	return []semanticplan.SemanticPlanNodeKind{
		semanticplan.SemanticPlanNodeSourceAggregate,
		semanticplan.SemanticPlanNodePostAggregate,
		semanticplan.SemanticPlanNodeJoinAggregates,
		semanticplan.SemanticPlanNodeCrossJoinAggregates,
		semanticplan.SemanticPlanNodeCumulativeWindow,
		semanticplan.SemanticPlanNodeTimeOffset,
		semanticplan.SemanticPlanNodeOffsetToGrain,
		semanticplan.SemanticPlanNodeConversion,
		semanticplan.SemanticPlanNodeSemiAdditiveLast,
		semanticplan.SemanticPlanNodeSemiAdditiveFirst,
		semanticplan.SemanticPlanNodeSourceSelection,
	}
}
