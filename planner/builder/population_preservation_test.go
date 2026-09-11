package builder

import (
	"reflect"
	"testing"

	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner/semanticplan"
)

func TestDerivePopulationPreservationEvidenceKeepsObligationsIndependent(t *testing.T) {
	join := populationTestJoin("orders", "items")
	evidence := mustDerivePopulationEvidence(t,
		"orders",
		[]string{"orders"},
		[]semanticplan.GroupBy{{Dataset: "orders"}},
		[]semanticplan.Predicate{{Dataset: "orders"}},
		[]semanticplan.Join{join},
	)
	if len(evidence) != 1 {
		t.Fatalf("evidence count = %d, want 1", len(evidence))
	}
	if evidence[0].Preserved() {
		t.Fatal("inner join totality must remain unproven")
	}
	want := []semanticplan.PopulationPreservationObligationEvidence{
		{Obligation: semanticplan.PopulationPreservationJoinRows, Status: semanticplan.PopulationPreservationUnproven, Proof: semanticplan.PopulationProofInnerJoinTotalityUnproven},
		{Obligation: semanticplan.PopulationPreservationFilterPlacement, Status: semanticplan.PopulationPreservationProven, Proof: semanticplan.PopulationProofRootDatasetOwned},
		{Obligation: semanticplan.PopulationPreservationGrouping, Status: semanticplan.PopulationPreservationProven, Proof: semanticplan.PopulationProofRootDatasetOwned},
		{Obligation: semanticplan.PopulationPreservationExpressionReferences, Status: semanticplan.PopulationPreservationProven, Proof: semanticplan.PopulationProofRootDatasetOwned},
	}
	if !reflect.DeepEqual(evidence[0].Obligations, want) {
		t.Fatalf("obligations = %#v, want %#v", evidence[0].Obligations, want)
	}
	if err := semanticplan.ValidatePopulationPreservationEvidence(evidence[0]); err != nil {
		t.Fatalf("derived evidence rejected: %v", err)
	}
}

func TestDerivePopulationPreservationEvidenceFailsClosedForJoinedDependencies(t *testing.T) {
	join := populationTestJoin("orders", "items")
	evidence := mustDerivePopulationEvidence(t,
		"orders",
		[]string{"orders", "items"},
		[]semanticplan.GroupBy{{Dataset: "items"}},
		[]semanticplan.Predicate{{Dataset: "items"}},
		[]semanticplan.Join{join},
	)
	for _, obligation := range evidence[0].Obligations[1:] {
		if obligation.Status != semanticplan.PopulationPreservationUnproven || obligation.Proof != semanticplan.PopulationProofJoinedDatasetDependent {
			t.Fatalf("joined dependency obligation = %#v, want fail-closed evidence", obligation)
		}
	}
}

func TestValidateSemanticPlanRequiresPopulationEvidenceForEverySourceJoin(t *testing.T) {
	join := semanticplan.Join{Relationship: &ossie.Relationship{Name: "orders_to_customer"}, FromDataset: "orders", ToDataset: "customer"}
	plan := &semanticplan.SemanticPlan{Nodes: []semanticplan.SemanticPlanNode{semanticplan.SourceAggregateNode{
		Base: semanticplan.SemanticPlanNodeBase{
			ID:                        "revenue",
			Boundary:                  semanticplan.SemanticPlanNodeBoundarySourceAggregate,
			PredicateBoundaryEvidence: semanticplan.PredicateBoundaryEvidence(semanticplan.SemanticPlanNodeBoundarySourceAggregate),
		},
		Source:      semanticplan.SemanticSourceState{Joins: []semanticplan.Join{join}},
		MetricState: semanticplan.SemanticMetricState{Metrics: []string{"revenue"}},
	}}}
	if err := semanticplan.ValidateSemanticPlanDAG(plan); err == nil {
		t.Fatal("source join without population-preservation evidence was accepted")
	}
}

func TestExplainSemanticPlanCarriesPopulationPreservationEvidence(t *testing.T) {
	join := populationTestJoin("orders", "customer")
	evidence := mustDerivePopulationEvidence(t, "orders", []string{"orders"}, nil, nil, []semanticplan.Join{join})
	plan := &semanticplan.SemanticPlan{Nodes: []semanticplan.SemanticPlanNode{semanticplan.SourceAggregateNode{
		Base: semanticplan.SemanticPlanNodeBase{
			ID:                        "revenue",
			Boundary:                  semanticplan.SemanticPlanNodeBoundarySourceAggregate,
			PredicateBoundaryEvidence: semanticplan.PredicateBoundaryEvidence(semanticplan.SemanticPlanNodeBoundarySourceAggregate),
		},
		Source: semanticplan.SemanticSourceState{
			Joins:                          []semanticplan.Join{join},
			PopulationPreservationEvidence: evidence,
		},
		MetricState: semanticplan.SemanticMetricState{Metrics: []string{"revenue"}},
	}}}

	explanation, err := semanticplan.Explain(plan)
	if err != nil {
		t.Fatal(err)
	}
	got := explanation.Nodes[0].PopulationPreservation
	if len(got) != 1 || got[0].Relationship != "orders_to_customer" || got[0].Preserved {
		t.Fatalf("population-preservation explanation = %#v", got)
	}
}

func TestPopulationPreservationEvidenceChangesSemanticPlanFingerprint(t *testing.T) {
	join := populationTestJoin("orders", "customer")
	baseEvidence := mustDerivePopulationEvidence(t, "orders", []string{"orders"}, nil, nil, []semanticplan.Join{join})
	plan := &semanticplan.SemanticPlan{Nodes: []semanticplan.SemanticPlanNode{semanticplan.SourceAggregateNode{
		Base: semanticplan.SemanticPlanNodeBase{
			ID:                        "revenue",
			Boundary:                  semanticplan.SemanticPlanNodeBoundarySourceAggregate,
			PredicateBoundaryEvidence: semanticplan.PredicateBoundaryEvidence(semanticplan.SemanticPlanNodeBoundarySourceAggregate),
		},
		Source: semanticplan.SemanticSourceState{Joins: []semanticplan.Join{join}, PopulationPreservationEvidence: baseEvidence},
	}}}
	changed := semanticplan.ClonePlan(plan)
	node := changed.Nodes[0].(semanticplan.SourceAggregateNode)
	node.Source.PopulationPreservationEvidence[0].Obligations[1] = semanticplan.PopulationPreservationObligationEvidence{
		Obligation: semanticplan.PopulationPreservationFilterPlacement,
		Status:     semanticplan.PopulationPreservationUnproven,
		Proof:      semanticplan.PopulationProofJoinedDatasetDependent,
	}
	changed.Nodes[0] = node

	baseFingerprint, err := semanticplan.Fingerprint(plan)
	if err != nil {
		t.Fatal(err)
	}
	changedFingerprint, err := semanticplan.Fingerprint(changed)
	if err != nil {
		t.Fatal(err)
	}
	if baseFingerprint == changedFingerprint {
		t.Fatalf("population-preservation evidence did not change fingerprint: %s", baseFingerprint)
	}
}

func TestCloneSemanticSourceStateOwnsPopulationPreservationObligations(t *testing.T) {
	join := populationTestJoin("orders", "customer")
	original := semanticplan.SemanticSourceState{
		Joins:                          []semanticplan.Join{join},
		PopulationPreservationEvidence: mustDerivePopulationEvidence(t, "orders", []string{"orders"}, nil, nil, []semanticplan.Join{join}),
	}
	cloned := semanticplan.CloneSourceState(original)
	cloned.PopulationPreservationEvidence[0].Obligations[0].Proof = semanticplan.PopulationProofJoinedDatasetDependent
	if original.PopulationPreservationEvidence[0].Obligations[0].Proof != semanticplan.PopulationProofInnerJoinTotalityUnproven {
		t.Fatal("cloned population-preservation obligations alias source state")
	}
}

func populationTestJoin(from, to string) semanticplan.Join {
	return semanticplan.Join{Relationship: &ossie.Relationship{
		Name: "orders_to_" + to, From: from, To: to,
		FromColumns: []string{"entity_id"}, ToColumns: []string{"entity_id"},
	}, FromDataset: from, ToDataset: to}
}

func mustDerivePopulationEvidence(t *testing.T, root string, expressionDatasets []string, groups []semanticplan.GroupBy, predicates []semanticplan.Predicate, joins []semanticplan.Join) []semanticplan.PopulationPreservationEvidence {
	t.Helper()
	datasets := map[string]*ossie.Dataset{}
	for _, join := range joins {
		datasets[join.FromDataset] = &ossie.Dataset{Name: join.FromDataset, PrimaryKey: []string{"entity_id"}}
		datasets[join.ToDataset] = &ossie.Dataset{Name: join.ToDataset, PrimaryKey: []string{"entity_id"}}
	}
	evidence, err := derivePopulationPreservationEvidence(datasets, root, expressionDatasets, groups, predicates, joins)
	if err != nil {
		t.Fatal(err)
	}
	return evidence
}
