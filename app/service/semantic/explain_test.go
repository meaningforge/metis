package semantic

import (
	"reflect"
	"testing"

	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/query"
)

func TestBuildQueryExplanationConsumesSemanticPlanNodeEvidence(t *testing.T) {
	metric := &ossie.Metric{Name: "previous_revenue", Datatype: ossie.DataTypeDecimal}
	predicate := semanticplan.Predicate{Dataset: "orders", Filter: query.Filter{Field: "status", Operator: query.FilterEQ, Value: "paid"}}
	limit := 20
	plan := &semanticplan.SemanticPlan{
		Model:       semanticplan.ModelRef{Project: "finance", Name: "sales"},
		Projections: []semanticplan.Projection{{Name: metric.Name, Kind: semanticplan.ProjectionMetric, Metric: metric}},
		Requested:   []string{metric.Name},
		Nodes: []semanticplan.SemanticPlanNode{
			semanticplan.SourceAggregateNode{
				Base: semanticplan.SemanticPlanNodeBase{
					ID:       "revenue",
					Boundary: semanticplan.SemanticPlanNodeBoundarySourceAggregate,
					PredicateBoundaryEvidence: semanticplan.SemanticPredicateBoundaryEvidence{
						Movement: semanticplan.SemanticPredicateBoundaryAllowedWithProof,
						Proof:    semanticplan.SemanticPredicateProofDatasetReachability,
					},
					Predicates: []semanticplan.SemanticPlanNodePredicate{{
						Scope:       semanticplan.SemanticPredicatePreAggregation,
						OwnerNodeID: "revenue",
						Proof:       semanticplan.SemanticPredicateProofSourceOwnership,
						Predicate:   &predicate,
					}},
				},
				Source: semanticplan.SemanticSourceState{
					RequiredDatasets: []string{"orders"},
					SourceRoots:      []string{"orders"},
					Root:             semanticplan.DatasetRef{Name: "orders", Source: "orders"},
				},
				MetricState: semanticplan.SemanticMetricState{Metrics: []string{"revenue"}},
				Metric:      &ossie.Metric{Name: "revenue", Datatype: ossie.DataTypeDecimal},
			},
			semanticplan.TimeOffsetNode{
				Base: semanticplan.SemanticPlanNodeBase{
					ID:       metric.Name,
					Boundary: semanticplan.SemanticPlanNodeBoundarySemanticIsolation,
					PredicateBoundaryEvidence: semanticplan.SemanticPredicateBoundaryEvidence{
						Movement: semanticplan.SemanticPredicateBoundaryBlocked,
						Proof:    semanticplan.SemanticPredicateProofNodeSemantics,
					},
					Inputs: []semanticplan.SemanticPlanNodeInput{{NodeID: "revenue"}},
				},
				Source:      semanticplan.SemanticSourceState{RequiredDatasets: []string{"orders"}},
				MetricState: semanticplan.SemanticMetricState{Metrics: []string{metric.Name}},
				Metric:      metric,
			},
		},
		Limit: &limit,
	}
	explanation, err := buildQueryExplanation(plan)
	if err != nil {
		t.Fatal(err)
	}
	if explanation.Project != "finance" || explanation.Model != "sales" {
		t.Fatalf("identity = %#v", explanation)
	}
	if !reflect.DeepEqual(explanation.Metrics, []string{"previous_revenue"}) {
		t.Fatalf("metrics = %v", explanation.Metrics)
	}
	kinds := explanationStepKinds(explanation.Steps)
	want := []ExplanationStepKind{
		ExplanationSource,
		ExplanationAggregation,
		ExplanationQueryFilter,
		ExplanationTimeAlignment,
		ExplanationTimeOffset,
		ExplanationLimit,
	}
	if !reflect.DeepEqual(kinds, want) {
		t.Fatalf("step kinds = %v, want %v", kinds, want)
	}
	if len(explanation.SemanticPlan.Nodes) != 2 {
		t.Fatalf("semantic stages = %#v", explanation.SemanticPlan.Nodes)
	}
	var lineage *semanticplan.SemanticOutputLineage
	for i := range explanation.SemanticPlan.Lineage {
		candidate := &explanation.SemanticPlan.Lineage[i]
		if candidate.Kind == semanticplan.SemanticOutputMetric && candidate.Name == "previous_revenue" {
			lineage = candidate
			break
		}
	}
	if lineage == nil || !reflect.DeepEqual(lineage.NodeIDs, []string{"revenue", "previous_revenue"}) {
		t.Fatalf("lineage = %#v", lineage)
	}
	for _, step := range explanation.Steps {
		if step.Kind != ExplanationQueryFilter {
			continue
		}
		if got := step.Details["owner_node_id"]; got != "revenue" {
			t.Fatalf("query-filter owner_node_id = %#v", got)
		}
		if _, stale := step.Details["owner_stage_id"]; stale {
			t.Fatalf("query-filter details contain stale owner_stage_id: %#v", step.Details)
		}
		return
	}
	t.Fatal("query-filter explanation step not found")
}

func TestBuildQueryExplanationRejectsMissingInputs(t *testing.T) {
	if _, err := buildQueryExplanation(nil); err == nil {
		t.Fatal("expected missing plan error")
	}
}

func explanationStepKinds(steps []ExplanationStep) []ExplanationStepKind {
	out := make([]ExplanationStepKind, 0, len(steps))
	for _, step := range steps {
		out = append(out, step.Kind)
	}
	return out
}
