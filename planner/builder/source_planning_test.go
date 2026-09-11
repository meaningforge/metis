package builder

import (
	"strings"
	"testing"

	"github.com/meaningforge/metis/planner/semanticplan"
)

func TestSourceSelectionStageMustBeMetricFreeAndTyped(t *testing.T) {
	valid := semanticNodeFixture{
		ID:                        "dimensions",
		Kind:                      semanticplan.SemanticPlanNodeSourceSelection,
		Boundary:                  semanticNodeFixtureBoundaryForKind(semanticplan.SemanticPlanNodeSourceSelection),
		PredicateBoundaryEvidence: semanticplan.PredicateBoundaryEvidence(semanticplan.SemanticPlanNodeBoundarySourceSelection),
		Node:                      semanticplan.SourceSelectionNode{Mode: semanticplan.SourceSelectionGroupedValues},
	}
	if err := validateSourceSelectionStage(valid); err != nil {
		t.Fatalf("a well-formed source-selection stage was rejected: %v", err)
	}

	for _, invalid := range []struct {
		name  string
		stage func(semanticNodeFixture) semanticNodeFixture
		want  string
	}{
		{
			name: "no mode",
			stage: func(s semanticNodeFixture) semanticNodeFixture {
				s.Node = semanticplan.SourceSelectionNode{}
				return s
			},
			want: "no mode",
		},
		{
			name: "unknown mode",
			stage: func(s semanticNodeFixture) semanticNodeFixture {
				s.Node = semanticplan.SourceSelectionNode{Mode: "sampled_values"}
				return s
			},
			want: "unsupported source-selection mode",
		},
		{
			name: "wrong node type",
			stage: func(s semanticNodeFixture) semanticNodeFixture {
				s.Node = semanticplan.SourceAggregateNode{}
				return s
			},
			want: "typed node",
		},
		{
			name: "evaluates metrics",
			stage: func(s semanticNodeFixture) semanticNodeFixture {
				s.Metrics = []string{"revenue"}
				return s
			},
			want: "evaluates metrics",
		},
		{
			name: "joins a share group",
			stage: func(s semanticNodeFixture) semanticNodeFixture {
				s.ShareGroup = "orders"
				return s
			},
			want: "share group",
		},
	} {
		t.Run(invalid.name, func(t *testing.T) {
			err := validateSourceSelectionStage(invalid.stage(valid))
			if err == nil {
				t.Fatalf("%s was accepted", invalid.name)
			}
			if !strings.Contains(err.Error(), invalid.want) {
				t.Errorf("error = %q, want it to mention %q", err, invalid.want)
			}
		})
	}
}

func TestSourceSelectionBoundaryAllowsProvenPredicateMovement(t *testing.T) {
	if boundary := semanticNodeFixtureBoundaryForKind(semanticplan.SemanticPlanNodeSourceSelection); boundary != semanticplan.SemanticPlanNodeBoundarySourceSelection {
		t.Fatalf("boundary = %q, want %q", boundary, semanticplan.SemanticPlanNodeBoundarySourceSelection)
	}
	evidence := semanticplan.PredicateBoundaryEvidence(semanticplan.SemanticPlanNodeBoundarySourceSelection)
	if evidence.Movement != semanticplan.SemanticPredicateBoundaryAllowedWithProof {
		t.Errorf("movement = %q, want predicates to be movable with proof", evidence.Movement)
	}
	if evidence.Proof != semanticplan.SemanticPredicateProofDatasetReachability {
		t.Errorf("proof = %q, want dataset reachability", evidence.Proof)
	}
}

func TestGraphValidationAcceptsASourceSelectionNode(t *testing.T) {
	graph := *semanticPlanForTest(semanticplan.SemanticPlan{Requested: []string{"region"}}, []semanticNodeFixture{{
		ID:                        "region",
		Kind:                      semanticplan.SemanticPlanNodeSourceSelection,
		Boundary:                  semanticNodeFixtureBoundaryForKind(semanticplan.SemanticPlanNodeSourceSelection),
		PredicateBoundaryEvidence: semanticplan.PredicateBoundaryEvidence(semanticplan.SemanticPlanNodeBoundarySourceSelection),
		Dimensions:                []string{"region"},
		Node:                      semanticplan.SourceSelectionNode{Mode: semanticplan.SourceSelectionGroupedValues},
	}})
	if err := semanticplan.ValidateSemanticPlanDAG(&graph); err != nil {
		t.Fatalf("validation rejected a source-selection node: %v", err)
	}
	if _, ok := graph.Nodes[0].(semanticplan.SourceSelectionNode); !ok {
		t.Fatalf("source-selection graph stored %T, want semanticplan.SourceSelectionNode", graph.Nodes[0])
	}
}
