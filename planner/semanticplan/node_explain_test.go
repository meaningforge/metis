package semanticplan

import (
	"reflect"
	"testing"
)

func TestExplainProjectsCanonicalNodeOwnedEvidence(t *testing.T) {
	plan := &SemanticPlan{
		Requested: []string{"region"},
		Output:    SemanticOutputContract{Grain: []GroupBy{{Name: "region", Dataset: "customers"}}},
		Nodes: []SemanticPlanNode{SourceSelectionNode{
			Base: SemanticPlanNodeBase{
				ID:                        "regions",
				Boundary:                  SemanticPlanNodeBoundarySourceSelection,
				PredicateBoundaryEvidence: PredicateBoundaryEvidence(SemanticPlanNodeBoundarySourceSelection),
				Dimensions:                []string{"region"},
				OutputGrain:               []GroupBy{{Name: "region", Dataset: "customers"}},
			},
			Source: SemanticSourceState{
				Root:             DatasetRef{Name: "customers"},
				SourceRoots:      []string{"customers"},
				RequiredDatasets: []string{"customers"},
			},
			Mode: SourceSelectionGroupedValues,
		}},
	}

	explanation, err := Explain(plan)
	if err != nil {
		t.Fatalf("explain semantic plan: %v", err)
	}
	if len(explanation.Nodes) != 1 || explanation.Nodes[0].ID != "regions" {
		t.Fatalf("nodes = %#v", explanation.Nodes)
	}
	want := []SemanticOutputLineage{{
		Kind:           SemanticOutputDimension,
		Name:           "region",
		NodeIDs:        []string{"regions"},
		SourceDatasets: []string{"customers"},
	}}
	if !reflect.DeepEqual(explanation.Lineage, want) {
		t.Fatalf("lineage = %#v, want %#v", explanation.Lineage, want)
	}
}
