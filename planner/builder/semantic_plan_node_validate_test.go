package builder

import (
	"reflect"
	"testing"

	"github.com/meaningforge/metis/planner/optimizer"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/query"
)

func semanticTestNodeFixture(stage semanticNodeFixture) semanticNodeFixture {
	stage.PredicateBoundaryEvidence = semanticplan.PredicateBoundaryEvidence(stage.Boundary)
	return stage
}

func TestSemanticPlanNodePlannerMarksAggregateCompositionBoundaries(t *testing.T) {
	grain := query.TimeGrain("month")
	groups := []semanticplan.GroupBy{{Name: "order_month", Dataset: "orders", Grain: &grain}}
	graph := *semanticPlanForTest(semanticplan.SemanticPlan{}, canonicalNodeFixtures([]semanticNodeFixture{
		semanticTestNodeFixture(semanticNodeFixture{ID: "revenue", Kind: semanticplan.SemanticPlanNodeSourceAggregate, Boundary: semanticplan.SemanticPlanNodeBoundarySourceAggregate, OutputGrain: groups, ShareGroup: "source_001"}),
		semanticTestNodeFixture(semanticNodeFixture{ID: "cost", Kind: semanticplan.SemanticPlanNodeSourceAggregate, Boundary: semanticplan.SemanticPlanNodeBoundarySourceAggregate, OutputGrain: groups, ShareGroup: "source_001"}),
		semanticTestNodeFixture(semanticNodeFixture{ID: "margin", Kind: semanticplan.SemanticPlanNodePostAggregate, Boundary: semanticplan.SemanticPlanNodeBoundaryPostAggregate, Inputs: []semanticNodeInputFixture{{NodeID: "revenue", Grain: groups}, {NodeID: "cost", Grain: groups}}, OutputGrain: groups}),
	}))
	plan := &semanticplan.SemanticPlan{}
	semanticplan.SetOwnedDAG(plan, graph.Requested, graph.Nodes, graph.Output.Grain, graph.Output.Predicates)

	if err := semanticplan.ValidateSemanticPlanDAG(plan); err != nil {
		t.Fatal(err)
	}
	got, err := semanticNodeFixturesFromPlanNodes(plan.Nodes)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Boundary != semanticplan.SemanticPlanNodeBoundarySourceAggregate {
		t.Fatalf("source boundary = %q", got[0].Boundary)
	}
	if got[2].Boundary != semanticplan.SemanticPlanNodeBoundaryPostAggregate {
		t.Fatalf("derived boundary = %q", got[2].Boundary)
	}
	if got[0].ShareGroup != "source_001" || got[1].ShareGroup != "source_001" {
		t.Fatalf("share groups = %q, %q", got[0].ShareGroup, got[1].ShareGroup)
	}
}

func TestSemanticPlanNodePlannerRejectsIncompatibleDerivedGrain(t *testing.T) {
	month := query.TimeGrain("month")
	day := query.TimeGrain("day")
	monthGrain := []semanticplan.GroupBy{{Name: "order_time", Dataset: "orders", Grain: &month}}
	dayGrain := []semanticplan.GroupBy{{Name: "order_time", Dataset: "orders", Grain: &day}}
	graph := *semanticPlanForTest(semanticplan.SemanticPlan{}, canonicalNodeFixtures([]semanticNodeFixture{
		semanticTestNodeFixture(semanticNodeFixture{ID: "revenue", Kind: semanticplan.SemanticPlanNodeSourceAggregate, Boundary: semanticplan.SemanticPlanNodeBoundarySourceAggregate, OutputGrain: monthGrain}),
		semanticTestNodeFixture(semanticNodeFixture{ID: "growth", Kind: semanticplan.SemanticPlanNodePostAggregate, Boundary: semanticplan.SemanticPlanNodeBoundaryPostAggregate, Inputs: []semanticNodeInputFixture{{NodeID: "revenue", Grain: monthGrain}}, OutputGrain: dayGrain}),
	}))
	if err := semanticplan.ValidateSemanticPlanDAG(&graph); err == nil {
		t.Fatal("expected incompatible derived grain to fail")
	}
}

func TestSemanticPlanNodePlannerRequiresAggregateBeforeCrossJoin(t *testing.T) {
	month := query.TimeGrain("month")
	monthGrain := []semanticplan.GroupBy{{Name: "order_time", Dataset: "orders", Grain: &month}}
	graph := *semanticPlanForTest(semanticplan.SemanticPlan{}, canonicalNodeFixtures([]semanticNodeFixture{
		semanticTestNodeFixture(semanticNodeFixture{ID: "revenue", Kind: semanticplan.SemanticPlanNodeSourceAggregate, Boundary: semanticplan.SemanticPlanNodeBoundarySourceAggregate, OutputGrain: monthGrain}),
		semanticTestNodeFixture(semanticNodeFixture{ID: "cost", Kind: semanticplan.SemanticPlanNodeSourceAggregate, Boundary: semanticplan.SemanticPlanNodeBoundarySourceAggregate}),
		semanticTestNodeFixture(semanticNodeFixture{ID: "ratio", Kind: semanticplan.SemanticPlanNodeCrossJoinAggregates, Boundary: semanticplan.SemanticPlanNodeBoundaryAggregateComposition, Inputs: []semanticNodeInputFixture{{NodeID: "revenue", Grain: monthGrain}, {NodeID: "cost"}}}),
	}))
	if err := semanticplan.ValidateSemanticPlanDAG(&graph); err == nil {
		t.Fatal("expected non-scalar cross-join input to fail")
	}
}

func TestSemanticPlanFingerprintNormalizesSetLikeSourceRequirements(t *testing.T) {
	first := *semanticPlanForTest(semanticplan.SemanticPlan{}, canonicalNodeFixtures([]semanticNodeFixture{semanticTestNodeFixture(semanticNodeFixture{
		ID: "revenue", Kind: semanticplan.SemanticPlanNodeSourceAggregate, Boundary: semanticplan.SemanticPlanNodeBoundarySourceAggregate,
		SourceRoots: []string{"customers", "orders"}, RequiredDatasets: []string{"orders", "customers"},
	})}))
	second := semanticplan.ClonePlan(&first)
	mutateSemanticPlanNodeFixturesForTest(second, func(stages []semanticNodeFixture) {
		stages[0].SourceRoots = []string{"orders", "customers", "orders"}
		stages[0].RequiredDatasets = []string{"customers", "orders"}
	})
	firstFingerprint, err := semanticplan.Fingerprint(&first)
	if err != nil {
		t.Fatal(err)
	}
	secondFingerprint, err := semanticplan.Fingerprint(second)
	if err != nil {
		t.Fatal(err)
	}
	if firstFingerprint != secondFingerprint {
		t.Fatalf("equivalent graph fingerprints differ: %q != %q", firstFingerprint, secondFingerprint)
	}
	if got := semanticPlanNodeFixturesForTest(&first)[0].SourceRoots; !reflect.DeepEqual(got, []string{"customers", "orders"}) {
		t.Fatalf("fingerprinting mutated caller graph: %#v", got)
	}
}

func TestSemanticPlanNodePlannerRejectsUnorderedDependency(t *testing.T) {
	graph := *semanticPlanForTest(semanticplan.SemanticPlan{}, canonicalNodeFixtures([]semanticNodeFixture{
		semanticTestNodeFixture(semanticNodeFixture{ID: "ratio", Kind: semanticplan.SemanticPlanNodePostAggregate, Boundary: semanticplan.SemanticPlanNodeBoundaryPostAggregate, Inputs: []semanticNodeInputFixture{{NodeID: "revenue"}}}),
		semanticTestNodeFixture(semanticNodeFixture{ID: "revenue", Kind: semanticplan.SemanticPlanNodeSourceAggregate, Boundary: semanticplan.SemanticPlanNodeBoundarySourceAggregate}),
	}))
	if err := semanticplan.ValidateSemanticPlanDAG(&graph); err == nil {
		t.Fatal("expected unordered dependency to fail")
	}
}

func TestSemanticPlanNodePlannerProjectsTypedPredicateOwnership(t *testing.T) {
	predicate := semanticplan.Predicate{Dataset: "orders", Filter: query.Filter{Field: "region", Operator: query.FilterEQ, Value: "APAC"}}
	post := semanticplan.PostEvaluationPredicate{Name: "revenue", Filter: query.Filter{Field: "revenue", Operator: query.FilterGT, Value: float64(100)}}
	graph := *semanticPlanForTest(semanticplan.SemanticPlan{

		Output: semanticplan.SemanticOutputContract{Predicates: []semanticplan.PostEvaluationPredicate{post}}}, canonicalNodeFixtures([]semanticNodeFixture{semanticTestNodeFixture(semanticNodeFixture{
		ID: "revenue", Kind: semanticplan.SemanticPlanNodeSourceAggregate, Boundary: semanticplan.SemanticPlanNodeBoundarySourceAggregate,
		Predicates: []semanticNodePredicateFixture{{Scope: semanticplan.SemanticPredicatePreAggregation, OwnerNodeID: "revenue", Proof: semanticplan.SemanticPredicateProofSourceOwnership, Predicate: &predicate}},
	})}))
	plan := &semanticplan.SemanticPlan{}
	semanticplan.SetOwnedDAG(plan, graph.Requested, graph.Nodes, graph.Output.Grain, graph.Output.Predicates)
	if err := semanticplan.ValidateSemanticPlanDAG(plan); err != nil {
		t.Fatal(err)
	}
	got, err := semanticNodeFixturesFromPlanNodes(plan.Nodes)
	if err != nil {
		t.Fatal(err)
	}
	stagePredicate := got[0].Predicates[0]
	if stagePredicate.Scope != semanticplan.SemanticPredicatePreAggregation || stagePredicate.OwnerNodeID != "revenue" || stagePredicate.Proof != semanticplan.SemanticPredicateProofSourceOwnership {
		t.Fatalf("stage predicate ownership = %#v", stagePredicate)
	}
	finalPredicate := plan.Output.Predicates[0]
	if finalPredicate.Name != "revenue" {
		t.Fatalf("final predicate = %#v", finalPredicate)
	}
}

func TestSemanticPlanNodePlannerBlocksPredicateMovementAcrossSemanticIsolation(t *testing.T) {
	graph := *semanticPlanForTest(semanticplan.SemanticPlan{}, canonicalNodeFixtures([]semanticNodeFixture{
		semanticTestNodeFixture(semanticNodeFixture{ID: "revenue", Kind: semanticplan.SemanticPlanNodeSourceAggregate, Boundary: semanticplan.SemanticPlanNodeBoundarySourceAggregate}),
		semanticTestNodeFixture(semanticNodeFixture{ID: "running_revenue", Kind: semanticplan.SemanticPlanNodeCumulativeWindow, Boundary: semanticplan.SemanticPlanNodeBoundarySemanticIsolation, Inputs: []semanticNodeInputFixture{{NodeID: "revenue"}}}),
	}))
	plan := &semanticplan.SemanticPlan{}
	semanticplan.SetOwnedDAG(plan, graph.Requested, graph.Nodes, graph.Output.Grain, graph.Output.Predicates)
	if err := semanticplan.ValidateSemanticPlanDAG(plan); err != nil {
		t.Fatal(err)
	}
	got, err := semanticNodeFixturesFromPlanNodes(plan.Nodes)
	if err != nil {
		t.Fatal(err)
	}
	evidence := got[1].PredicateBoundaryEvidence
	if evidence.Movement != semanticplan.SemanticPredicateBoundaryBlocked || evidence.Proof != semanticplan.SemanticPredicateProofNodeSemantics {
		t.Fatalf("semantic isolation predicate boundary = %#v", evidence)
	}
}

func TestSemanticPlanNodePlannerRejectsAmbiguousPredicateRepresentation(t *testing.T) {
	predicate := semanticplan.Predicate{Dataset: "orders", Filter: query.Filter{Field: "region", Operator: query.FilterEQ, Value: "APAC"}}
	post := semanticplan.PostEvaluationPredicate{Name: "region", Filter: predicate.Filter}
	stage := semanticTestNodeFixture(semanticNodeFixture{ID: "revenue", Kind: semanticplan.SemanticPlanNodeSourceAggregate, Boundary: semanticplan.SemanticPlanNodeBoundarySourceAggregate})
	stage.Predicates = []semanticNodePredicateFixture{{Scope: semanticplan.SemanticPredicatePreAggregation, OwnerNodeID: "revenue", Proof: semanticplan.SemanticPredicateProofSourceOwnership, Predicate: &predicate, Post: &post}}
	if err := semanticplan.ValidateSemanticPlanDAG(semanticPlanForTest(semanticplan.SemanticPlan{}, canonicalNodeFixtures([]semanticNodeFixture{stage}))); err == nil {
		t.Fatal("expected ambiguous predicate representation to fail")
	}
}

func TestSemanticPlanFingerprintIncludesPredicatePlacementEvidence(t *testing.T) {
	predicate := semanticplan.Predicate{Dataset: "orders", Filter: query.Filter{Field: "region", Operator: query.FilterEQ, Value: "APAC"}}
	first := *semanticPlanForTest(semanticplan.SemanticPlan{}, canonicalNodeFixtures([]semanticNodeFixture{semanticTestNodeFixture(semanticNodeFixture{
		ID: "revenue", Kind: semanticplan.SemanticPlanNodeSourceAggregate, Boundary: semanticplan.SemanticPlanNodeBoundarySourceAggregate,
		Predicates: []semanticNodePredicateFixture{{Scope: semanticplan.SemanticPredicatePreAggregation, OwnerNodeID: "revenue", Proof: semanticplan.SemanticPredicateProofSourceOwnership, Predicate: &predicate}},
	})}))
	second := semanticplan.ClonePlan(&first)
	mutateSemanticPlanNodeFixturesForTest(second, func(stages []semanticNodeFixture) {
		stages[0].Predicates[0].Scope = semanticplan.SemanticPredicateSourceRead
	})
	firstFingerprint, err := semanticplan.Fingerprint(&first)
	if err != nil {
		t.Fatal(err)
	}
	secondFingerprint, err := semanticplan.Fingerprint(second)
	if err != nil {
		t.Fatal(err)
	}
	if firstFingerprint == secondFingerprint {
		t.Fatal("predicate placement change must alter semantic graph fingerprint")
	}
}

func TestPredicatePushdownConsumesSemanticPlanNodeBoundaryEvidence(t *testing.T) {
	predicate := semanticplan.Predicate{Dataset: "orders", Filter: query.Filter{Field: "region", Operator: query.FilterEQ, Value: "APAC"}}
	graph := *semanticPlanForTest(semanticplan.SemanticPlan{}, canonicalNodeFixtures([]semanticNodeFixture{semanticTestNodeFixture(semanticNodeFixture{
		ID: "revenue", Kind: semanticplan.SemanticPlanNodeSourceAggregate, Boundary: semanticplan.SemanticPlanNodeBoundarySourceAggregate, RequiredDatasets: []string{"orders"},
	})}))
	plan := &semanticplan.SemanticPlan{Predicates: []semanticplan.Predicate{predicate}}
	semanticplan.SetOwnedDAG(plan, graph.Requested, graph.Nodes, graph.Output.Grain, graph.Output.Predicates)
	changed, err := (optimizer.PredicatePushdownRule{}).Apply(plan)
	if err != nil {
		t.Fatal(err)
	}
	if !changed || len(plan.Predicates) != 0 {
		t.Fatalf("predicate pushdown changed=%v remaining=%d", changed, len(plan.Predicates))
	}
	stages := semanticPlanNodeFixturesForTest(plan)
	if len(stages[0].Predicates) != 1 || stages[0].Predicates[0].Predicate == nil || !reflect.DeepEqual(*stages[0].Predicates[0].Predicate, predicate) {
		t.Fatalf("source stage did not receive predicate: %#v", stages[0].Predicates)
	}
}

func TestPredicatePushdownDoesNotInferAcrossSemanticIsolation(t *testing.T) {
	predicate := semanticplan.Predicate{Dataset: "orders", Filter: query.Filter{Field: "region", Operator: query.FilterEQ, Value: "APAC"}}
	graph := *semanticPlanForTest(semanticplan.SemanticPlan{}, canonicalNodeFixtures([]semanticNodeFixture{
		semanticTestNodeFixture(semanticNodeFixture{ID: "revenue", Kind: semanticplan.SemanticPlanNodeSourceAggregate, Boundary: semanticplan.SemanticPlanNodeBoundarySourceAggregate, RequiredDatasets: []string{"orders"}}),
		semanticTestNodeFixture(semanticNodeFixture{ID: "running_revenue", Kind: semanticplan.SemanticPlanNodeCumulativeWindow, Boundary: semanticplan.SemanticPlanNodeBoundarySemanticIsolation, Inputs: []semanticNodeInputFixture{{NodeID: "revenue"}}}),
	}))
	plan := &semanticplan.SemanticPlan{Predicates: []semanticplan.Predicate{predicate}}
	semanticplan.SetOwnedDAG(plan, graph.Requested, graph.Nodes, graph.Output.Grain, graph.Output.Predicates)
	changed, err := (optimizer.PredicatePushdownRule{}).Apply(plan)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("predicate movement across semantic isolation must require planner-owned placement")
	}
	if len(plan.Predicates) != 1 || len(semanticPlanNodeFixturesForTest(plan)[0].Predicates) != 0 {
		t.Fatal("predicate placement changed despite semantic isolation boundary")
	}
}
