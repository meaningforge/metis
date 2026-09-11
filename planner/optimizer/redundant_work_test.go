package optimizer_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/meaningforge/metis/planner/optimizer"
	"github.com/meaningforge/metis/planner/semanticplan"
)

func TestProjectionDeduplicationRemovesOnlySemanticDuplicates(t *testing.T) {
	duplicate := semanticplan.Projection{
		Name:     "region",
		Kind:     semanticplan.ProjectionDimension,
		Dataset:  "customer",
		Datasets: []string{"customer"},
	}
	differentDataset := duplicate
	differentDataset.Dataset = "account"
	differentDataset.Datasets = []string{"account"}

	input := &semanticplan.SemanticPlan{
		Projections: []semanticplan.Projection{duplicate, duplicate, differentDataset},
	}
	optimized, err := optimizer.NewCanonical(optimizer.ProjectionDeduplicationRule{}).Optimize(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	want := []semanticplan.Projection{duplicate, differentDataset}
	if !reflect.DeepEqual(optimized.Projections, want) {
		t.Fatalf("projections = %#v, want %#v", optimized.Projections, want)
	}
	if len(input.Projections) != 3 {
		t.Fatalf("optimizer mutated caller projections: %#v", input.Projections)
	}
}

func TestProjectionDeduplicationKeepsSameNameWithDifferentSemantics(t *testing.T) {
	input := &semanticplan.SemanticPlan{Projections: []semanticplan.Projection{
		{Name: "value", Kind: semanticplan.ProjectionDimension, Dataset: "orders", Datasets: []string{"orders"}},
		{Name: "value", Kind: semanticplan.ProjectionMetric, Datasets: []string{"orders"}},
	}}
	optimized, err := optimizer.NewCanonical(optimizer.ProjectionDeduplicationRule{}).Optimize(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if len(optimized.Projections) != 2 {
		t.Fatalf("same-name projections with different semantics were collapsed: %#v", optimized.Projections)
	}
}

func TestProjectionDeduplicationUsesCanonicalDatasetSets(t *testing.T) {
	input := &semanticplan.SemanticPlan{Projections: []semanticplan.Projection{
		{Name: "margin", Kind: semanticplan.ProjectionMetric, Datasets: []string{"payments", "orders", "orders"}},
		{Name: "margin", Kind: semanticplan.ProjectionMetric, Datasets: []string{"orders", "payments"}},
	}}
	optimized, err := optimizer.NewCanonical(optimizer.ProjectionDeduplicationRule{}).Optimize(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if len(optimized.Projections) != 1 {
		t.Fatalf("canonical-equivalent projection sets were not deduplicated: %#v", optimized.Projections)
	}
	if want := []string{"orders", "payments"}; !reflect.DeepEqual(optimized.Projections[0].Datasets, want) {
		t.Fatalf("canonical datasets = %v, want %v", optimized.Projections[0].Datasets, want)
	}
}
