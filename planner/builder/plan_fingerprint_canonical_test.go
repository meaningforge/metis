package builder

import (
	"testing"

	"github.com/meaningforge/metis/planner/semanticplan"
)

func TestFingerprintSemanticPlanUsesCanonicalGraph(t *testing.T) {
	graph := *semanticPlanForTest(semanticplan.SemanticPlan{}, []semanticNodeFixture{{
		ID:               "revenue",
		Kind:             semanticplan.SemanticPlanNodeSourceAggregate,
		Boundary:         semanticplan.SemanticPlanNodeBoundarySourceAggregate,
		SourceRoots:      []string{"orders"},
		RequiredDatasets: []string{"orders"},
		Node:             semanticplan.SourceAggregateNode{Base: semanticplan.SemanticPlanNodeBase{ID: "revenue"}},
	}})
	base := semanticPlanForTest(semanticplan.SemanticPlan{
		Root:      semanticplan.DatasetRef{Name: "orders"},
		Requested: graph.Requested,
	}, semanticPlanNodeFixturesForTest(&graph))

	baseFingerprint, err := semanticplan.Fingerprint(base)
	if err != nil {
		t.Fatalf("fingerprint base plan: %v", err)
	}

	canonicalChange := semanticplan.ClonePlan(base)
	changedStages := semanticPlanNodeFixturesForTest(canonicalChange)
	changedStages[0].RequiredDatasets = []string{"orders", "customers"}
	replaceSemanticPlanNodeFixturesForTest(canonicalChange, changedStages)
	changedFingerprint, err := semanticplan.Fingerprint(canonicalChange)
	if err != nil {
		t.Fatalf("fingerprint canonical change: %v", err)
	}
	if baseFingerprint == changedFingerprint {
		t.Fatalf("a stage change did not change the plan fingerprint: %q", baseFingerprint)
	}
}

func TestFingerprintSemanticPlanNormalizesCanonicalGraphSets(t *testing.T) {
	leftGraph := *semanticPlanForTest(semanticplan.SemanticPlan{}, []semanticNodeFixture{{
		ID:               "revenue",
		Kind:             semanticplan.SemanticPlanNodeSourceAggregate,
		SourceRoots:      []string{"orders", "customers"},
		RequiredDatasets: []string{"orders", "customers"},
		Node:             semanticplan.SourceAggregateNode{Base: semanticplan.SemanticPlanNodeBase{ID: "revenue"}},
	}})
	rightGraph := *semanticPlanForTest(semanticplan.SemanticPlan{}, []semanticNodeFixture{{
		ID:               "revenue",
		Kind:             semanticplan.SemanticPlanNodeSourceAggregate,
		SourceRoots:      []string{"customers", "orders"},
		RequiredDatasets: []string{"customers", "orders"},
		Node:             semanticplan.SourceAggregateNode{Base: semanticplan.SemanticPlanNodeBase{ID: "revenue"}},
	}})
	left := semanticPlanForTest(semanticplan.SemanticPlan{}, semanticPlanNodeFixturesForTest(&leftGraph))
	right := semanticPlanForTest(semanticplan.SemanticPlan{}, semanticPlanNodeFixturesForTest(&rightGraph))

	leftFingerprint, err := semanticplan.Fingerprint(left)
	if err != nil {
		t.Fatalf("fingerprint left plan: %v", err)
	}
	rightFingerprint, err := semanticplan.Fingerprint(right)
	if err != nil {
		t.Fatalf("fingerprint right plan: %v", err)
	}
	if leftFingerprint != rightFingerprint {
		t.Fatalf("canonical graph set ordering changed fingerprint: left=%q right=%q", leftFingerprint, rightFingerprint)
	}
}
