package builder

import (
	"reflect"
	"strings"
	"testing"

	"github.com/meaningforge/metis/planner/semanticplan"
)

func TestExplainSemanticPlanRequiresPlanOwnedNodes(t *testing.T) {
	_, err := semanticplan.Explain(&semanticplan.SemanticPlan{})
	if err == nil || !strings.Contains(err.Error(), "owns no nodes") {
		t.Fatalf("nodeless plan error = %v", err)
	}
}

func TestExplainSemanticPlanExplainsAPlanWithNoGraph(t *testing.T) {
	plan := semanticPlanForTest(semanticplan.SemanticPlan{
		Requested: []string{"region"},
	}, canonicalNodeFixtures([]semanticNodeFixture{{
		ID:                        "source_selection",
		Kind:                      semanticplan.SemanticPlanNodeSourceSelection,
		Boundary:                  semanticplan.SemanticPlanNodeBoundarySourceSelection,
		PredicateBoundaryEvidence: semanticplan.PredicateBoundaryEvidence(semanticplan.SemanticPlanNodeBoundarySourceSelection),
		Dimensions:                []string{"region"},
		RequiredDatasets:          []string{"customers"},
		Node:                      semanticplan.SourceSelectionNode{Mode: semanticplan.SourceSelectionGroupedValues},
	}}))

	explanation, err := semanticplan.Explain(plan)
	if err != nil {
		t.Fatalf("a metric-free plan that owns its node was not explained: %v", err)
	}
	if len(explanation.Nodes) != 1 || explanation.Nodes[0].ID != "source_selection" {
		t.Fatalf("nodes = %#v", explanation.Nodes)
	}
	if len(explanation.Lineage) == 0 {
		t.Error("a dimension the query selected produced no lineage, so the explanation does not account for its output")
	}
}

func TestSemanticPlanGraphAccessorReturnsOwnedCanonicalGraph(t *testing.T) {
	graph := *semanticPlanForTest(semanticplan.SemanticPlan{Requested: []string{"revenue"}}, canonicalNodeFixtures([]semanticNodeFixture{{
		ID:                        "revenue",
		Kind:                      semanticplan.SemanticPlanNodeSourceAggregate,
		Boundary:                  semanticplan.SemanticPlanNodeBoundarySourceAggregate,
		PredicateBoundaryEvidence: semanticplan.PredicateBoundaryEvidence(semanticplan.SemanticPlanNodeBoundarySourceAggregate),
		Metrics:                   []string{"revenue"},
		RequiredDatasets:          []string{"orders"},
	}}))
	plan := &semanticplan.SemanticPlan{}
	semanticplan.SetOwnedDAG(plan, graph.Requested, graph.Nodes, graph.Output.Grain, graph.Output.Predicates)

	if err := semanticplan.ValidateSemanticPlanDAG(plan); err != nil {
		t.Fatal(err)
	}
	got, err := semanticNodeFixturesFromPlanNodes(plan.Nodes)
	if err != nil {
		t.Fatal(err)
	}
	got[0].Metrics[0] = "mutated"
	owned := semanticPlanNodeFixturesForTest(plan)
	if !reflect.DeepEqual(owned[0].Metrics, []string{"revenue"}) {
		t.Fatalf("the stage planner exposed the plan's own storage: %#v", owned)
	}
}

func TestExplainSemanticPlanReadsCanonicalGraphInsteadOfPlanSideState(t *testing.T) {
	graph := *semanticPlanForTest(semanticplan.SemanticPlan{Requested: []string{"revenue"}}, canonicalNodeFixtures([]semanticNodeFixture{{
		ID:                        "revenue",
		Kind:                      semanticplan.SemanticPlanNodeSourceAggregate,
		Boundary:                  semanticplan.SemanticPlanNodeBoundarySourceAggregate,
		PredicateBoundaryEvidence: semanticplan.PredicateBoundaryEvidence(semanticplan.SemanticPlanNodeBoundarySourceAggregate),
		Metrics:                   []string{"revenue"},
		RequiredDatasets:          []string{"orders"},
		ShareGroup:                "source_001",
	}}))
	plan := semanticPlanForTest(semanticplan.SemanticPlan{
		Projections: []semanticplan.Projection{{Name: "compatibility_only", Kind: semanticplan.ProjectionMetric}},
		Requested:   graph.Requested,
	}, semanticPlanNodeFixturesForTest(&graph))

	explanation, err := semanticplan.Explain(plan)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(explanation.Requested, []string{"revenue"}) {
		t.Fatalf("requested = %#v", explanation.Requested)
	}
	if len(explanation.Nodes) != 1 || explanation.Nodes[0].ID != "revenue" {
		t.Fatalf("stages = %#v", explanation.Nodes)
	}
	for _, lineage := range explanation.Lineage {
		if lineage.Name == "compatibility_only" {
			t.Fatalf("explain reconstructed compatibility projection: %#v", explanation.Lineage)
		}
	}
}

func TestExplainSemanticPlanDAGIgnoresOptimizerOnlyAnnotations(t *testing.T) {
	base := *semanticPlanForTest(semanticplan.SemanticPlan{Requested: []string{"revenue"}}, canonicalNodeFixtures([]semanticNodeFixture{{
		ID:                        "revenue",
		Kind:                      semanticplan.SemanticPlanNodeSourceAggregate,
		Boundary:                  semanticplan.SemanticPlanNodeBoundarySourceAggregate,
		PredicateBoundaryEvidence: semanticplan.PredicateBoundaryEvidence(semanticplan.SemanticPlanNodeBoundarySourceAggregate),
		Metrics:                   []string{"revenue"},
		RequiredDatasets:          []string{"orders"},
	}}))
	optimized := semanticplan.ClonePlan(&base)
	mutateSemanticPlanNodeFixturesForTest(optimized, func(stages []semanticNodeFixture) { stages[0].ShareGroup = "source_001" })

	before, err := semanticplan.Explain(&base)
	if err != nil {
		t.Fatal(err)
	}
	after, err := semanticplan.Explain(optimized)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("optimizer-only annotation changed semantic explain: before=%#v after=%#v", before, after)
	}
}

func TestExplainSemanticPlanDAGRejectsNonCanonicalMetadata(t *testing.T) {
	graph := *semanticPlanForTest(semanticplan.SemanticPlan{Requested: []string{"revenue", "revenue"}}, canonicalNodeFixtures([]semanticNodeFixture{{
		ID:                        "revenue",
		Kind:                      semanticplan.SemanticPlanNodeSourceAggregate,
		Boundary:                  semanticplan.SemanticPlanNodeBoundarySourceAggregate,
		PredicateBoundaryEvidence: semanticplan.PredicateBoundaryEvidence(semanticplan.SemanticPlanNodeBoundarySourceAggregate),
		Metrics:                   []string{"revenue"},
	}}))
	_, err := semanticplan.Explain(&graph)
	if err == nil || !strings.Contains(err.Error(), "duplicate value") {
		t.Fatalf("non-canonical explain error = %v", err)
	}
}
