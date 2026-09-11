package builder

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/serrors"
)

func TestValidateSemanticPlanRejectsNodeBoundaryKindMismatch(t *testing.T) {
	node := semanticTestNodeFixture(semanticNodeFixture{
		ID:       "revenue",
		Kind:     semanticplan.SemanticPlanNodeSourceAggregate,
		Boundary: semanticplan.SemanticPlanNodeBoundarySemanticIsolation,
	})
	graph := *semanticPlanForTest(semanticplan.SemanticPlan{}, canonicalNodeFixtures([]semanticNodeFixture{node}))
	plan := semanticPlanForTest(semanticplan.SemanticPlan{Root: semanticplan.DatasetRef{Name: "orders"}, Requested: graph.Requested}, semanticPlanNodeFixturesForTest(&graph))

	err := semanticplan.ValidateSemanticPlan(plan)
	if err == nil {
		t.Fatal("expected node boundary/kind mismatch to fail")
	}
	assertInternalInvariant(t, err, "boundary", map[string]any{"node": "revenue", "kind": "source_aggregate"})
}

func TestValidateSemanticPlanRejectsDuplicateCanonicalNodeMetadata(t *testing.T) {
	node := semanticTestNodeFixture(semanticNodeFixture{
		ID:               "revenue",
		Kind:             semanticplan.SemanticPlanNodeSourceAggregate,
		Boundary:         semanticplan.SemanticPlanNodeBoundarySourceAggregate,
		RequiredDatasets: []string{"orders", "orders"},
	})
	graph := *semanticPlanForTest(semanticplan.SemanticPlan{}, canonicalNodeFixtures([]semanticNodeFixture{node}))
	plan := semanticPlanForTest(semanticplan.SemanticPlan{Root: semanticplan.DatasetRef{Name: "orders"}, Requested: graph.Requested}, semanticPlanNodeFixturesForTest(&graph))

	err := semanticplan.ValidateSemanticPlan(plan)
	if err == nil {
		t.Fatal("expected duplicate canonical metadata to fail")
	}
	assertInternalInvariant(t, err, "duplicate", map[string]any{"node": "revenue", "scope": "required datasets"})
}

func TestValidateSemanticPlanRejectsDuplicateNodeInputs(t *testing.T) {
	source := semanticTestNodeFixture(semanticNodeFixture{
		ID:       "revenue",
		Kind:     semanticplan.SemanticPlanNodeSourceAggregate,
		Boundary: semanticplan.SemanticPlanNodeBoundarySourceAggregate,
	})
	derived := semanticTestNodeFixture(semanticNodeFixture{
		ID:       "margin",
		Kind:     semanticplan.SemanticPlanNodePostAggregate,
		Boundary: semanticplan.SemanticPlanNodeBoundaryPostAggregate,
		Inputs: []semanticNodeInputFixture{
			{NodeID: "revenue"},
			{NodeID: "revenue"},
		},
	})
	graph := *semanticPlanForTest(semanticplan.SemanticPlan{}, canonicalNodeFixtures([]semanticNodeFixture{source, derived}))
	plan := semanticPlanForTest(semanticplan.SemanticPlan{Root: semanticplan.DatasetRef{Name: "orders"}, Requested: graph.Requested}, semanticPlanNodeFixturesForTest(&graph))

	err := semanticplan.ValidateSemanticPlan(plan)
	if err == nil {
		t.Fatal("expected duplicate node input to fail")
	}
	assertInternalInvariant(t, err, "duplicate input", map[string]any{"node": "margin"})
}

func TestValidateSemanticPlanGraphContractIncludesNodeReachability(t *testing.T) {
	node := semanticTestNodeFixture(semanticNodeFixture{
		ID:               "revenue",
		Kind:             semanticplan.SemanticPlanNodeSourceAggregate,
		Boundary:         semanticplan.SemanticPlanNodeBoundarySourceAggregate,
		Root:             semanticplan.DatasetRef{Name: "orders"},
		RequiredDatasets: []string{"customers"},
	})
	graph := *semanticPlanForTest(semanticplan.SemanticPlan{}, canonicalNodeFixtures([]semanticNodeFixture{node}))
	plan := semanticPlanForTest(semanticplan.SemanticPlan{Root: semanticplan.DatasetRef{Name: "orders"}, Requested: graph.Requested}, semanticPlanNodeFixturesForTest(&graph))

	err := semanticplan.ValidateSemanticPlan(plan)
	if err == nil {
		t.Fatal("expected unreachable node dataset to fail")
	}
	if !strings.Contains(err.Error(), "requires unreachable dataset") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSemanticPlanFingerprintNormalizesSetMetadata(t *testing.T) {
	left := *semanticPlanForTest(semanticplan.SemanticPlan{Requested: []string{"revenue", "cost"}}, canonicalNodeFixtures([]semanticNodeFixture{semanticTestNodeFixture(semanticNodeFixture{
		ID:               "revenue",
		Kind:             semanticplan.SemanticPlanNodeSourceAggregate,
		Boundary:         semanticplan.SemanticPlanNodeBoundarySourceAggregate,
		Metrics:          []string{"revenue", "gross_revenue"},
		Dimensions:       []string{"country", "channel"},
		SourceRoots:      []string{"orders", "payments"},
		RequiredDatasets: []string{"orders", "payments"},
	})}))
	right := semanticplan.ClonePlan(&left)
	right.Requested = []string{"cost", "revenue"}
	mutateSemanticPlanNodeFixturesForTest(right, func(nodes []semanticNodeFixture) {
		nodes[0].Metrics = []string{"gross_revenue", "revenue"}
		nodes[0].Dimensions = []string{"channel", "country"}
		nodes[0].SourceRoots = []string{"payments", "orders"}
		nodes[0].RequiredDatasets = []string{"payments", "orders"}
	})

	leftFingerprint, err := semanticplan.Fingerprint(&left)
	if err != nil {
		t.Fatal(err)
	}
	rightFingerprint, err := semanticplan.Fingerprint(right)
	if err != nil {
		t.Fatal(err)
	}
	if leftFingerprint != rightFingerprint {
		t.Fatalf("set-like metadata ordering changed graph fingerprint: left=%q right=%q", leftFingerprint, rightFingerprint)
	}
	leftNodes := semanticPlanNodeFixturesForTest(&left)
	if !reflect.DeepEqual(left.Requested, []string{"revenue", "cost"}) || !reflect.DeepEqual(leftNodes[0].Metrics, []string{"revenue", "gross_revenue"}) {
		t.Fatalf("fingerprinting mutated caller-owned graph: %#v", left)
	}
}

func TestSemanticPlanFingerprintPreservesSequenceSensitiveStructure(t *testing.T) {
	sourceA := semanticTestNodeFixture(semanticNodeFixture{ID: "a", Kind: semanticplan.SemanticPlanNodeSourceAggregate, Boundary: semanticplan.SemanticPlanNodeBoundarySourceAggregate})
	sourceB := semanticTestNodeFixture(semanticNodeFixture{ID: "b", Kind: semanticplan.SemanticPlanNodeSourceAggregate, Boundary: semanticplan.SemanticPlanNodeBoundarySourceAggregate})
	derived := semanticTestNodeFixture(semanticNodeFixture{
		ID:       "ratio",
		Kind:     semanticplan.SemanticPlanNodePostAggregate,
		Boundary: semanticplan.SemanticPlanNodeBoundaryPostAggregate,
		Inputs: []semanticNodeInputFixture{
			{NodeID: "a"},
			{NodeID: "b"},
		},
	})
	left := *semanticPlanForTest(semanticplan.SemanticPlan{Requested: []string{"ratio"}}, canonicalNodeFixtures([]semanticNodeFixture{sourceA, sourceB, derived}))
	right := semanticplan.ClonePlan(&left)
	mutateSemanticPlanNodeFixturesForTest(right, func(nodes []semanticNodeFixture) {
		nodes[2].Inputs = []semanticNodeInputFixture{{NodeID: "b"}, {NodeID: "a"}}
	})

	leftFingerprint, err := semanticplan.Fingerprint(&left)
	if err != nil {
		t.Fatal(err)
	}
	rightFingerprint, err := semanticplan.Fingerprint(right)
	if err != nil {
		t.Fatal(err)
	}
	if leftFingerprint == rightFingerprint {
		t.Fatal("dependency input order was erased from canonical graph fingerprint")
	}
}

func TestSemanticExplainIgnoresOptimizerAnnotations(t *testing.T) {
	graph := *semanticPlanForTest(semanticplan.SemanticPlan{Requested: []string{"revenue", "cost"}}, canonicalNodeFixtures([]semanticNodeFixture{
		semanticTestNodeFixture(semanticNodeFixture{
			ID:               "revenue",
			Kind:             semanticplan.SemanticPlanNodeSourceAggregate,
			Boundary:         semanticplan.SemanticPlanNodeBoundarySourceAggregate,
			Metrics:          []string{"revenue"},
			Root:             semanticplan.DatasetRef{Name: "orders", Source: "analytics.orders"},
			SourceRoots:      []string{"orders"},
			RequiredDatasets: []string{"orders"},
		}),
		semanticTestNodeFixture(semanticNodeFixture{
			ID:               "cost",
			Kind:             semanticplan.SemanticPlanNodeSourceAggregate,
			Boundary:         semanticplan.SemanticPlanNodeBoundarySourceAggregate,
			Metrics:          []string{"cost"},
			Root:             semanticplan.DatasetRef{Name: "orders", Source: "analytics.orders"},
			SourceRoots:      []string{"orders"},
			RequiredDatasets: []string{"orders"},
		}),
	}))
	if err := semanticplan.ValidateDAGContract(&graph); err != nil {
		t.Fatalf("base canonical graph: %v", err)
	}
	optimized := semanticplan.ClonePlan(&graph)
	mutateSemanticPlanNodeFixturesForTest(optimized, func(nodes []semanticNodeFixture) {
		nodes[0].ShareGroup = "source_001"
		nodes[1].ShareGroup = "source_001"
	})
	if err := semanticplan.ValidateDAGContract(optimized); err != nil {
		t.Fatalf("optimized canonical graph: %v", err)
	}

	baseExplain, err := semanticplan.Explain(&graph)
	if err != nil {
		t.Fatal(err)
	}
	optimizedExplain, err := semanticplan.Explain(optimized)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(baseExplain, optimizedExplain) {
		t.Fatalf("semantic explanation changed for optimizer-only annotation:\nbase=%#v\noptimized=%#v", baseExplain, optimizedExplain)
	}

	baseFingerprint, err := semanticplan.Fingerprint(&graph)
	if err != nil {
		t.Fatal(err)
	}
	optimizedFingerprint, err := semanticplan.Fingerprint(optimized)
	if err != nil {
		t.Fatal(err)
	}
	if baseFingerprint == optimizedFingerprint {
		t.Fatal("structural graph fingerprint ignored optimizer annotation")
	}
}

func TestPlanDAGContractIsSharedByValidationAndExplain(t *testing.T) {
	graph := *semanticPlanForTest(semanticplan.SemanticPlan{Requested: []string{"revenue", "revenue"}}, canonicalNodeFixtures([]semanticNodeFixture{semanticTestNodeFixture(semanticNodeFixture{
		ID:       "revenue",
		Kind:     semanticplan.SemanticPlanNodeSourceAggregate,
		Boundary: semanticplan.SemanticPlanNodeBoundarySourceAggregate,
		Metrics:  []string{"revenue"},
	})}))
	plan := &semanticplan.SemanticPlan{}
	semanticplan.SetOwnedDAG(plan, graph.Requested, graph.Nodes, graph.Output.Grain, graph.Output.Predicates)
	if err := semanticplan.ValidateSemanticPlan(plan); err == nil {
		t.Fatal("semanticplan.ValidateSemanticPlan accepted non-canonical graph metadata")
	}
	if _, err := semanticplan.Explain(plan); err == nil {
		t.Fatal("semanticplan.Explain accepted graph rejected by canonical plan validation")
	}
}

// assertInternalInvariant checks the failure by its stable code and structured
// details rather than by prose. The details are what a caller can actually act
// on, and asserting them keeps these tests from breaking on rewording.
func assertInternalInvariant(t *testing.T, err error, messageFragment string, wantDetails map[string]any) {
	t.Helper()
	var metisErr *serrors.Error
	if !errors.As(err, &metisErr) {
		t.Fatalf("error = %v, want a Metis error", err)
	}
	if metisErr.Code != serrors.ErrInternalInvariant {
		t.Fatalf("code = %q, want %q", metisErr.Code, serrors.ErrInternalInvariant)
	}
	if messageFragment != "" && !strings.Contains(metisErr.Message, messageFragment) {
		t.Fatalf("message = %q, want it to mention %q", metisErr.Message, messageFragment)
	}
	for key, want := range wantDetails {
		if got := metisErr.Details[key]; got != want {
			t.Fatalf("details[%q] = %v, want %v", key, got, want)
		}
	}
}
