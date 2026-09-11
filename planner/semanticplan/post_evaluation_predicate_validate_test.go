package semanticplan_test

import (
	"strings"
	"testing"

	"github.com/meaningforge/metis/expression"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/query"
)

func TestValidateSemanticPlanRejectsPostEvaluationPredicateWithoutOutputName(t *testing.T) {
	plan := stagedPostEvaluationPredicatePlan(semanticplan.PostEvaluationPredicate{Filter: query.Filter{Field: "revenue", Operator: query.FilterEQ, Value: 100}})
	err := semanticplan.ValidateSemanticPlan(plan)
	if err == nil || !strings.Contains(err.Error(), "post-evaluation predicate output name is required") {
		t.Fatalf("error = %v, want missing post-evaluation output name failure", err)
	}
}

func TestValidateSemanticPlanRejectsPostEvaluationPredicateForMissingOutput(t *testing.T) {
	plan := stagedPostEvaluationPredicatePlan(semanticplan.PostEvaluationPredicate{Name: "missing_output", Filter: query.Filter{Field: "missing_output", Operator: query.FilterEQ, Value: 100}})
	err := semanticplan.ValidateSemanticPlan(plan)
	if err == nil || !strings.Contains(err.Error(), "post-evaluation predicate references a missing output") {
		t.Fatalf("error = %v, want missing post-evaluation output failure", err)
	}
}

func TestValidateSemanticPlanAcceptsPostEvaluationPredicateForEvaluationMetric(t *testing.T) {
	plan := stagedPostEvaluationPredicatePlan(semanticplan.PostEvaluationPredicate{Name: "revenue", Filter: query.Filter{Field: "revenue", Operator: query.FilterEQ, Value: 100}})
	if err := semanticplan.ValidateSemanticPlan(plan); err != nil {
		t.Fatalf("semanticplan.ValidateSemanticPlan() error = %v", err)
	}
}

func TestValidateSemanticPlanAcceptsPostEvaluationPredicateForOutputGroup(t *testing.T) {
	plan := stagedPostEvaluationPredicatePlan(semanticplan.PostEvaluationPredicate{Name: "orders.created_at", Filter: query.Filter{Field: "orders.created_at", Operator: query.FilterEQ, Value: "2026-08-16"}})
	group := semanticplan.GroupBy{Name: "orders.created_at", Dataset: "orders", Expression: expression.NewResolvedExpression("ANSI_SQL", "created_at")}
	plan.Groups = []semanticplan.GroupBy{group}
	plan.Output.Grain = []semanticplan.GroupBy{group}
	if err := semanticplan.ValidateSemanticPlan(plan); err != nil {
		t.Fatalf("semanticplan.ValidateSemanticPlan() error = %v", err)
	}
}

func stagedPostEvaluationPredicatePlan(predicate semanticplan.PostEvaluationPredicate) *semanticplan.SemanticPlan {
	return &semanticplan.SemanticPlan{
		Root:      semanticplan.DatasetRef{Name: "orders", Source: "orders"},
		Requested: []string{"revenue"},
		Output:    semanticplan.SemanticOutputContract{Predicates: []semanticplan.PostEvaluationPredicate{predicate}},
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
			},
			Expression: expression.NewResolvedExpression("ANSI_SQL", "revenue"),
		}},
	}
}
