package optimizer_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/meaningforge/metis/planner/optimizer"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/query"
)

func TestDefaultOptimizerEliminatesExactProjectionAndFilterRedundancyToFixpoint(t *testing.T) {
	projection := semanticplan.Projection{
		Name:     "region",
		Kind:     semanticplan.ProjectionDimension,
		Dataset:  "customer",
		Datasets: []string{"customer"},
	}
	predicate := semanticplan.Predicate{
		Dataset: "orders",
		Filter: query.Filter{
			Field:    "orders.status",
			Operator: query.FilterEQ,
			Value:    "paid",
		},
	}
	input := &semanticplan.SemanticPlan{
		Projections: []semanticplan.Projection{projection, projection},
		Predicates:  []semanticplan.Predicate{predicate, predicate},
	}

	optimized, err := optimizer.Default().Optimize(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if len(optimized.Projections) != 1 || !reflect.DeepEqual(optimized.Projections[0], projection) {
		t.Fatalf("optimized projections = %#v", optimized.Projections)
	}
	if len(optimized.Predicates) != 1 || !reflect.DeepEqual(optimized.Predicates[0], predicate) {
		t.Fatalf("optimized predicates = %#v", optimized.Predicates)
	}
	if len(input.Projections) != 2 || len(input.Predicates) != 2 {
		t.Fatal("default optimizer mutated caller-owned redundant work")
	}

	wantTrace := []semanticplan.OptimizationStep{
		{Rule: "metric_projection_pruning", Changed: false},
		{Rule: "semantic_set_normalization", Changed: false},
		{Rule: "projection_deduplication", Changed: true},
		{Rule: "predicate_deduplication", Changed: true},
		{Rule: "predicate_pushdown", Changed: false},
		{Rule: "join_deduplication", Changed: false},
		{Rule: "unused_join_elimination", Changed: false},
		{Rule: "source_scan_fusion", Changed: false},
	}
	if !reflect.DeepEqual(optimized.OptimizationTrace, wantTrace) {
		t.Fatalf("optimization trace = %#v, want %#v", optimized.OptimizationTrace, wantTrace)
	}

	firstFingerprint, err := semanticplan.Fingerprint(optimized)
	if err != nil {
		t.Fatal(err)
	}
	optimizedAgain, err := optimizer.Default().Optimize(context.Background(), optimized)
	if err != nil {
		t.Fatal(err)
	}
	secondFingerprint, err := semanticplan.Fingerprint(optimizedAgain)
	if err != nil {
		t.Fatal(err)
	}
	if firstFingerprint != secondFingerprint {
		t.Fatalf("default optimizer fixpoint fingerprint changed: first=%s second=%s", firstFingerprint, secondFingerprint)
	}
	if len(optimizedAgain.Projections) != 1 || len(optimizedAgain.Predicates) != 1 {
		t.Fatalf("second optimization changed normalized work: projections=%#v predicates=%#v", optimizedAgain.Projections, optimizedAgain.Predicates)
	}
	for _, step := range optimizedAgain.OptimizationTrace {
		if step.Changed {
			t.Fatalf("already-normalized plan changed on second optimization: %#v", optimizedAgain.OptimizationTrace)
		}
	}
}
