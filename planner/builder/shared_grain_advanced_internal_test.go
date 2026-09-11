package builder

import (
	"reflect"
	"testing"

	"github.com/meaningforge/metis/planner/semanticplan"
)

func TestAdvancedMetricInternalStageGrainDoesNotLeakIntoSharedSemanticGrain(t *testing.T) {
	semanticGrain := []semanticplan.GroupBy{{Dataset: "calendar", Name: "month"}}
	advancedKinds := []semanticplan.SemanticPlanNodeKind{
		semanticplan.SemanticPlanNodeCumulativeWindow,
		semanticplan.SemanticPlanNodeTimeOffset,
		semanticplan.SemanticPlanNodeOffsetToGrain,
		semanticplan.SemanticPlanNodeConversion,
		semanticplan.SemanticPlanNodeSemiAdditiveLast,
		semanticplan.SemanticPlanNodeSemiAdditiveFirst,
	}

	for _, kind := range advancedKinds {
		t.Run(string(kind), func(t *testing.T) {
			stageGrain := cloneGroups(semanticGrain)
			stageGrain = append(stageGrain, semanticplan.GroupBy{Dataset: "facts", Name: "__metis_internal_stage_key"})
			graph := semanticPlanForTest(semanticplan.SemanticPlan{Requested: []string{"metric"},

				Output: semanticplan.SemanticOutputContract{Grain: semanticGrain}}, []semanticNodeFixture{{
				ID:          "metric",
				Kind:        kind,
				SourceRoots: []string{"facts"},
				OutputGrain: stageGrain,
			}})

			resolution, err := resolvePlanSharedGrain(graph)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(resolution.Grain, semanticGrain) {
				t.Fatalf("shared grain = %#v, want semantic grain %#v", resolution.Grain, semanticGrain)
			}
			if len(resolution.Metrics) != 1 || resolution.Metrics[0].GrainKey != canonicalGrainKey(semanticGrain) {
				t.Fatalf("shared-grain evidence = %#v", resolution.Metrics)
			}
		})
	}
}
