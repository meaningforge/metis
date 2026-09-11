package semanticplan_test

import (
	"strings"
	"testing"

	"github.com/meaningforge/metis/expression"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner/semanticplan"
)

func TestValidateSemanticPlanRejectsMissingEvaluationDependency(t *testing.T) {
	plan := &semanticplan.SemanticPlan{
		Root:      semanticplan.DatasetRef{Name: "orders", Source: "orders"},
		Requested: []string{"margin"},
		Nodes: []semanticplan.SemanticPlanNode{semanticplan.PostAggregateNode{
			Base: semanticplan.SemanticPlanNodeBase{
				ID:       "margin",
				Boundary: semanticplan.SemanticPlanNodeBoundaryPostAggregate,
				PredicateBoundaryEvidence: semanticplan.SemanticPredicateBoundaryEvidence{
					Movement: semanticplan.SemanticPredicateBoundaryBlocked,
					Proof:    semanticplan.SemanticPredicateProofNodeSemantics,
				},
				Inputs: []semanticplan.SemanticPlanNodeInput{{NodeID: "revenue"}, {NodeID: "cost"}},
			},
			Expression: expression.NewResolvedExpression("ANSI_SQL", "revenue - cost"),
		}},
	}
	err := semanticplan.ValidateSemanticPlan(plan)
	if err == nil || !strings.Contains(err.Error(), "missing or unordered dependency") {
		t.Fatalf("error = %v, want missing or unordered dependency failure", err)
	}
}

func TestValidateSemanticPlanRejectsUnreachableSourceDataset(t *testing.T) {
	plan := &semanticplan.SemanticPlan{
		Root:      semanticplan.DatasetRef{Name: "orders", Source: "orders"},
		Requested: []string{"revenue"},
		Nodes: []semanticplan.SemanticPlanNode{semanticplan.SourceAggregateNode{
			Base: semanticplan.SemanticPlanNodeBase{
				ID:       "revenue",
				Boundary: semanticplan.SemanticPlanNodeBoundarySourceAggregate,
				PredicateBoundaryEvidence: semanticplan.SemanticPredicateBoundaryEvidence{
					Movement: semanticplan.SemanticPredicateBoundaryAllowedWithProof,
					Proof:    semanticplan.SemanticPredicateProofDatasetReachability,
				},
			},
			Source: semanticplan.SemanticSourceState{
				Root:             semanticplan.DatasetRef{Name: "orders", Source: "orders"},
				RequiredDatasets: []string{"customer"},
			},
			Expression: expression.NewResolvedExpression("ANSI_SQL", "revenue"),
		}},
	}
	err := semanticplan.ValidateSemanticPlan(plan)
	if err == nil || !strings.Contains(err.Error(), "requires unreachable dataset") {
		t.Fatalf("error = %v, want unreachable dataset failure", err)
	}
}

func TestValidateSemanticPlanRejectsInvalidSourceShareGroupMembership(t *testing.T) {
	plan := &semanticplan.SemanticPlan{
		Root: semanticplan.DatasetRef{Name: "orders", Source: "orders"},
		Nodes: []semanticplan.SemanticPlanNode{
			semanticplan.SourceAggregateNode{
				Base: semanticplan.SemanticPlanNodeBase{
					ID:       "revenue",
					Boundary: semanticplan.SemanticPlanNodeBoundarySourceAggregate,
					PredicateBoundaryEvidence: semanticplan.SemanticPredicateBoundaryEvidence{
						Movement: semanticplan.SemanticPredicateBoundaryAllowedWithProof,
						Proof:    semanticplan.SemanticPredicateProofDatasetReachability,
					},
				},
				Source: semanticplan.SemanticSourceState{
					Root:             semanticplan.DatasetRef{Name: "orders", Source: "orders"},
					SourceRoots:      []string{"orders"},
					RequiredDatasets: []string{"orders"},
				},
				MetricState: semanticplan.SemanticMetricState{ShareGroup: "source_001"},
			},
			semanticplan.SourceAggregateNode{
				Base: semanticplan.SemanticPlanNodeBase{
					ID:       "cost",
					Boundary: semanticplan.SemanticPlanNodeBoundarySourceAggregate,
					PredicateBoundaryEvidence: semanticplan.SemanticPredicateBoundaryEvidence{
						Movement: semanticplan.SemanticPredicateBoundaryAllowedWithProof,
						Proof:    semanticplan.SemanticPredicateProofDatasetReachability,
					},
				},
				Source: semanticplan.SemanticSourceState{
					Root:             semanticplan.DatasetRef{Name: "costs", Source: "costs"},
					SourceRoots:      []string{"costs"},
					RequiredDatasets: []string{"costs"},
				},
				MetricState: semanticplan.SemanticMetricState{ShareGroup: "source_001"},
			},
		},
	}
	err := semanticplan.ValidateSemanticPlan(plan)
	if err == nil {
		t.Fatal("expected incompatible shared source group to fail validation")
	}
}

func TestValidateSemanticPlanAcceptsReachablePlan(t *testing.T) {
	relationship := &ossie.Relationship{Name: "orders_to_customer", From: "orders", To: "customer"}
	plan := &semanticplan.SemanticPlan{
		Root:        semanticplan.DatasetRef{Name: "orders", Source: "orders"},
		Joins:       []semanticplan.Join{{Relationship: relationship, FromDataset: "orders", ToDataset: "customer"}},
		Projections: []semanticplan.Projection{{Name: "region", Kind: semanticplan.ProjectionDimension, Dataset: "customer", Expression: expression.NewResolvedExpression("ANSI_SQL", "region")}},
	}
	if err := semanticplan.ValidateSemanticPlan(plan); err != nil {
		t.Fatalf("semanticplan.ValidateSemanticPlan() error = %v", err)
	}
}

func TestValidateSemanticPlanUsesNodesAsCanonicalDAGAuthority(t *testing.T) {
	plan := &semanticplan.SemanticPlan{
		Root:      semanticplan.DatasetRef{Name: "orders", Source: "orders"},
		Requested: []string{"revenue"},
		Nodes: []semanticplan.SemanticPlanNode{semanticplan.SourceAggregateNode{
			Base: semanticplan.SemanticPlanNodeBase{
				ID:       "revenue",
				Boundary: semanticplan.SemanticPlanNodeBoundarySourceAggregate,
				PredicateBoundaryEvidence: semanticplan.SemanticPredicateBoundaryEvidence{
					Movement: semanticplan.SemanticPredicateBoundaryAllowedWithProof,
					Proof:    semanticplan.SemanticPredicateProofDatasetReachability,
				},
			},
			Source: semanticplan.SemanticSourceState{
				Root:             semanticplan.DatasetRef{Name: "orders", Source: "orders"},
				RequiredDatasets: []string{"orders"},
				SourceRoots:      []string{"orders"},
			},
		}},
	}
	if err := semanticplan.ValidateSemanticPlan(plan); err != nil {
		t.Fatalf("canonical node validation failed: %v", err)
	}
}

func TestValidateSemanticPlanRejectsInvalidConcreteNodeState(t *testing.T) {
	plan := &semanticplan.SemanticPlan{
		Root:      semanticplan.DatasetRef{Name: "orders", Source: "orders"},
		Requested: []string{"revenue"},
		Nodes: []semanticplan.SemanticPlanNode{semanticplan.SemiAdditiveNode{
			Base: semanticplan.SemanticPlanNodeBase{ID: "revenue", Boundary: semanticplan.SemanticPlanNodeBoundarySemanticIsolation},
			Spec: ossie.SemiAdditiveMetricSpec{Aggregation: "median"},
		}},
	}
	err := semanticplan.ValidateSemanticPlan(plan)
	if err == nil || !strings.Contains(err.Error(), "invalid typed node") {
		t.Fatalf("error = %v, want invalid typed-node validation failure", err)
	}
}
