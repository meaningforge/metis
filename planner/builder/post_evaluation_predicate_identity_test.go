package builder

import (
	"testing"

	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/query"
)

func TestRemovePostEvaluationPredicatesUsesFullFilterIdentity(t *testing.T) {
	predicates := []semanticplan.Predicate{
		{Filter: query.Filter{Field: "revenue", Operator: query.FilterGT, Value: 100}},
		{Filter: query.Filter{Field: "revenue", Operator: query.FilterGT, Value: 500}},
	}
	post := []semanticplan.PostEvaluationPredicate{
		{Filter: query.Filter{Field: "revenue", Operator: query.FilterGT, Value: 100}},
	}

	remaining, err := removePostEvaluationPredicates(predicates, post)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 1 || remaining[0].Filter.Value != 500 {
		t.Fatalf("unexpected remaining predicates: %#v", remaining)
	}
}

func TestRemovePostEvaluationPredicatesConsumesDuplicateOneForOne(t *testing.T) {
	filter := query.Filter{Field: "revenue", Operator: query.FilterGT, Value: 100}
	predicates := []semanticplan.Predicate{{Filter: filter}, {Filter: filter}}
	post := []semanticplan.PostEvaluationPredicate{{Filter: filter}}

	remaining, err := removePostEvaluationPredicates(predicates, post)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 1 {
		t.Fatalf("expected one duplicate predicate to remain, got %#v", remaining)
	}
}

func TestRemovePostEvaluationPredicatesFailsOnUnmatchedPredicate(t *testing.T) {
	predicates := []semanticplan.Predicate{
		{Filter: query.Filter{Field: "revenue", Operator: query.FilterGT, Value: 500}},
	}
	post := []semanticplan.PostEvaluationPredicate{
		{Filter: query.Filter{Field: "revenue", Operator: query.FilterGT, Value: 100}},
	}

	if _, err := removePostEvaluationPredicates(predicates, post); err == nil {
		t.Fatal("expected unmatched post-evaluation predicate to fail")
	}
}
