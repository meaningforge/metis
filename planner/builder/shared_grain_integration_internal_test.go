package builder

import (
	"errors"
	"reflect"
	"testing"

	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/serrors"
)

func TestSemanticPlanSharedGrainUsesSemanticOutputGrainNotInternalStageGrain(t *testing.T) {
	semanticGrain := []semanticplan.GroupBy{{Dataset: "calendar", Name: "week"}}
	graph := semanticPlanForTest(semanticplan.SemanticPlan{Requested: []string{"inventory_balance"},

		Output: semanticplan.SemanticOutputContract{Grain: semanticGrain}}, []semanticNodeFixture{{
		ID:          "inventory_balance",
		SourceRoots: []string{"inventory"},
		OutputGrain: []semanticplan.GroupBy{
			{Dataset: "calendar", Name: "week"},
			{Dataset: "inventory", Name: "__metis_ordered_snapshot_date"},
		},
	}})

	resolution, err := resolvePlanSharedGrain(graph)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(resolution.Grain, semanticGrain) {
		t.Fatalf("shared grain = %#v, want semantic output grain %#v", resolution.Grain, semanticGrain)
	}
	if got := resolution.Metrics[0].GrainKey; got != canonicalGrainKey(semanticGrain) {
		t.Fatalf("metric grain key = %q, want %q", got, canonicalGrainKey(semanticGrain))
	}
}

func TestMapSharedGrainPlanningErrorPreservesTypedFailure(t *testing.T) {
	err := mapSharedGrainPlanningError(&semanticplan.SharedGrainError{
		Failure: semanticplan.SharedGrainIncompatible,
		Metric:  "orders",
	})
	var apiErr *serrors.Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %T %v", err, err)
	}
	if apiErr.Code != serrors.ErrIncompatibleQueryGrain {
		t.Fatalf("code = %q", apiErr.Code)
	}
	if apiErr.Details["failure"] != semanticplan.SharedGrainIncompatible || apiErr.Details["metric"] != "orders" {
		t.Fatalf("details = %#v", apiErr.Details)
	}
}
