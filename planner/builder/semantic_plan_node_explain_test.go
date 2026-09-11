package builder

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/meaningforge/metis/planner/semanticplan"
)

func TestSemanticPlanExplanationJSONUsesNodeVocabulary(t *testing.T) {
	payload, err := json.Marshal(semanticplan.SemanticPlanExplanation{
		Nodes: []semanticplan.SemanticPlanNodeExplanation{{ID: "revenue"}},
		Lineage: []semanticplan.SemanticOutputLineage{{
			Kind:    semanticplan.SemanticOutputMetric,
			Name:    "revenue",
			NodeIDs: []string{"revenue"},
		}},
		Predicates: []semanticplan.SemanticPredicateEvidence{{
			Scope:       semanticplan.SemanticPredicatePreAggregation,
			OwnerNodeID: "revenue",
			Proof:       semanticplan.SemanticPredicateProofNodeSemantics,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := string(payload)
	for _, want := range []string{`"nodes"`, `"node_ids"`, `"owner_node_id"`, `"node_semantics"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("JSON = %s, want key %s", got, want)
		}
	}
	for _, stale := range []string{`"stages"`, `"stage_ids"`, `"owner_stage_id"`, `"stage_semantics"`} {
		if strings.Contains(got, stale) {
			t.Fatalf("JSON = %s, contains stale key %s", got, stale)
		}
	}
}

func TestExplainSemanticPlanDAGBuildsDeterministicLineage(t *testing.T) {
	graph := *semanticPlanForTest(semanticplan.SemanticPlan{}, canonicalNodeFixtures([]semanticNodeFixture{
		{
			ID:                        "revenue",
			Kind:                      semanticplan.SemanticPlanNodeSourceAggregate,
			Boundary:                  semanticplan.SemanticPlanNodeBoundarySourceAggregate,
			PredicateBoundaryEvidence: semanticplan.PredicateBoundaryEvidence(semanticplan.SemanticPlanNodeBoundarySourceAggregate),
			Metrics:                   []string{"revenue"},
			RequiredDatasets:          []string{"orders"},
		},
		{
			ID:                        "previous_revenue",
			Kind:                      semanticplan.SemanticPlanNodeTimeOffset,
			Boundary:                  semanticplan.SemanticPlanNodeBoundarySemanticIsolation,
			PredicateBoundaryEvidence: semanticplan.PredicateBoundaryEvidence(semanticplan.SemanticPlanNodeBoundarySemanticIsolation),
			Inputs:                    []semanticNodeInputFixture{{NodeID: "revenue"}},
			Metrics:                   []string{"previous_revenue"},
			RequiredDatasets:          []string{"orders"},
		},
	}))

	explanation, err := semanticplan.Explain(&graph)
	if err != nil {
		t.Fatal(err)
	}
	if len(explanation.Nodes) != 2 {
		t.Fatalf("stages = %d", len(explanation.Nodes))
	}
	want := semanticplan.SemanticOutputLineage{
		Kind:           semanticplan.SemanticOutputMetric,
		Name:           "previous_revenue",
		NodeIDs:        []string{"revenue", "previous_revenue"},
		SourceDatasets: []string{"orders"},
	}
	var got *semanticplan.SemanticOutputLineage
	for i := range explanation.Lineage {
		if explanation.Lineage[i].Kind == semanticplan.SemanticOutputMetric && explanation.Lineage[i].Name == want.Name {
			got = &explanation.Lineage[i]
			break
		}
	}
	if got == nil || !reflect.DeepEqual(*got, want) {
		t.Fatalf("lineage = %#v, want %#v", got, want)
	}
}

func TestExplainSemanticPlanDAGIncludesPredicateAndFanoutEvidence(t *testing.T) {
	graph := *semanticPlanForTest(semanticplan.SemanticPlan{

		Output: semanticplan.SemanticOutputContract{Predicates: []semanticplan.PostEvaluationPredicate{{Name: "revenue"}}}}, canonicalNodeFixtures([]semanticNodeFixture{
		{
			ID:                        "revenue",
			Kind:                      semanticplan.SemanticPlanNodeSourceAggregate,
			Boundary:                  semanticplan.SemanticPlanNodeBoundarySourceAggregate,
			PredicateBoundaryEvidence: semanticplan.PredicateBoundaryEvidence(semanticplan.SemanticPlanNodeBoundarySourceAggregate),
			Metrics:                   []string{"revenue"},
			RequiredDatasets:          []string{"orders"},
			Predicates: []semanticNodePredicateFixture{{
				Scope:       semanticplan.SemanticPredicatePreAggregation,
				OwnerNodeID: "revenue",
				Proof:       semanticplan.SemanticPredicateProofSourceOwnership,
				Predicate:   &semanticplan.Predicate{},
			}},
			SharedGrainEvidence: &semanticplan.MetricSharedGrainEvidence{
				Metric:            "revenue",
				GrainKey:          "scalar",
				RelationshipPaths: [][]string{{"orders_customer"}},
				FanoutSafe:        true,
			},
		},
	}))

	explanation, err := semanticplan.Explain(&graph)
	if err != nil {
		t.Fatal(err)
	}
	if got := explanation.Nodes[0].RelationshipPaths; !reflect.DeepEqual(got, [][]string{{"orders_customer"}}) {
		t.Fatalf("relationship paths = %v", got)
	}
	if explanation.Nodes[0].FanoutSafe == nil || !*explanation.Nodes[0].FanoutSafe {
		t.Fatalf("fanout proof = %#v", explanation.Nodes[0].FanoutSafe)
	}
	if len(explanation.Nodes[0].Predicates) != 1 || explanation.Nodes[0].Predicates[0].OwnerNodeID != "revenue" {
		t.Fatalf("stage predicates = %#v", explanation.Nodes[0].Predicates)
	}
	if len(explanation.Predicates) != 1 || explanation.Predicates[0].Scope != semanticplan.SemanticPredicateFinalOutput {
		t.Fatalf("final predicates = %#v", explanation.Predicates)
	}
}
