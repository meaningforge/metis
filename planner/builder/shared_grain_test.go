package builder

import (
	"errors"
	"reflect"
	"testing"

	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/query"
)

func TestResolveSharedGrainDeterministicallyAlignsCompatibleMetrics(t *testing.T) {
	day := query.TimeGrainDay
	grainA := []semanticplan.GroupBy{
		{Name: "orders.region", Dataset: "orders"},
		{Name: "orders.order_time", Dataset: "orders", Grain: &day},
	}
	grainB := []semanticplan.GroupBy{
		{Name: "orders.order_time", Dataset: "orders", Grain: &day},
		{Name: "orders.region", Dataset: "orders"},
	}

	got, err := ResolveSharedGrain([]semanticplan.SharedGrainCandidate{
		{Metric: "revenue", RootDataset: "orders", OutputGrain: grainA, RelationshipPath: []string{"orders_customer"}, FanoutSafe: true},
		{Metric: "cost", RootDataset: "line_items", OutputGrain: grainB, RelationshipPath: []string{"line_items_order", "orders_customer"}, FanoutSafe: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.GrainKey == "" || len(got.Metrics) != 2 {
		t.Fatalf("resolution=%#v", got)
	}
	if got.Metrics[0].Metric != "cost" || got.Metrics[1].Metric != "revenue" {
		t.Fatalf("metric evidence order=%#v", got.Metrics)
	}
	if !reflect.DeepEqual(got.Grain, grainB) {
		t.Fatalf("owned canonical grain=%#v, want first deterministic metric grain %#v", got.Grain, grainB)
	}

	grainB[0].Name = "mutated"
	if got.Grain[0].Name == "mutated" {
		t.Fatal("resolution retained caller-owned grain slice")
	}
}

func TestResolveSharedGrainRejectsIncompatibleMetricGrain(t *testing.T) {
	day := query.TimeGrainDay
	week := query.TimeGrainWeek
	_, err := ResolveSharedGrain([]semanticplan.SharedGrainCandidate{
		{Metric: "revenue", RootDataset: "orders", OutputGrain: []semanticplan.GroupBy{{Name: "orders.order_time", Dataset: "orders", Grain: &day}}, FanoutSafe: true},
		{Metric: "weekly_inventory", RootDataset: "inventory", OutputGrain: []semanticplan.GroupBy{{Name: "inventory.snapshot_time", Dataset: "inventory", Grain: &week}}, FanoutSafe: true},
	})
	var grainErr *semanticplan.SharedGrainError
	if !errors.As(err, &grainErr) || grainErr.Failure != semanticplan.SharedGrainIncompatible || grainErr.Metric != "weekly_inventory" {
		t.Fatalf("err=%T %#v", err, err)
	}
}

func TestResolveSharedGrainFailsClosedOnAmbiguousRelationshipPath(t *testing.T) {
	_, err := ResolveSharedGrain([]semanticplan.SharedGrainCandidate{{
		Metric: "revenue", RootDataset: "orders", FanoutSafe: true, PathAlternatives: 2,
	}})
	var grainErr *semanticplan.SharedGrainError
	if !errors.As(err, &grainErr) || grainErr.Failure != semanticplan.SharedGrainAmbiguousPath {
		t.Fatalf("err=%T %#v", err, err)
	}
}

func TestResolveSharedGrainFailsClosedOnPerMetricFanout(t *testing.T) {
	_, err := ResolveSharedGrain([]semanticplan.SharedGrainCandidate{{
		Metric: "revenue", RootDataset: "orders", RelationshipPath: []string{"orders_items"}, FanoutSafe: false,
	}})
	var grainErr *semanticplan.SharedGrainError
	if !errors.As(err, &grainErr) || grainErr.Failure != semanticplan.SharedGrainFanoutUnsafe || grainErr.Metric != "revenue" {
		t.Fatalf("err=%T %#v", err, err)
	}
}
