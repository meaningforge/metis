package optimizer_test

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/meaningforge/metis/planner/optimizer"
	"github.com/meaningforge/metis/planner/semanticplan"
)

type consumeSeedRule struct{}

func (consumeSeedRule) Name() string { return "consume_seed" }
func (consumeSeedRule) Apply(plan *semanticplan.SemanticPlan) (bool, error) {
	if len(plan.Projections) != 1 || plan.Projections[0].Name != "seed" {
		return false, nil
	}
	plan.Projections[0].Name = "done"
	return true, nil
}

type produceSeedRule struct{}

func (produceSeedRule) Name() string { return "produce_seed" }
func (produceSeedRule) Apply(plan *semanticplan.SemanticPlan) (bool, error) {
	if len(plan.Projections) != 0 {
		return false, nil
	}
	plan.Projections = append(plan.Projections, semanticplan.Projection{Name: "seed"})
	return true, nil
}

type alwaysChangedRule struct {
	calls *int
}

func (alwaysChangedRule) Name() string { return "always_changed" }
func (r alwaysChangedRule) Apply(*semanticplan.SemanticPlan) (bool, error) {
	(*r.calls)++
	return true, nil
}

func TestOptimizerRunsConfiguredRulesToDeterministicFixpoint(t *testing.T) {
	input := &semanticplan.SemanticPlan{}
	optimized, err := optimizer.NewCanonical(consumeSeedRule{}, produceSeedRule{}).Optimize(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if got := optimized.Projections; len(got) != 1 || got[0].Name != "done" {
		t.Fatalf("optimized projections = %#v", got)
	}
	if len(input.Projections) != 0 {
		t.Fatalf("optimizer mutated caller input: %#v", input.Projections)
	}
	wantTrace := []semanticplan.OptimizationStep{
		{Rule: "consume_seed", Changed: false},
		{Rule: "produce_seed", Changed: true},
		{Rule: "consume_seed", Changed: true},
		{Rule: "produce_seed", Changed: false},
	}
	if !reflect.DeepEqual(optimized.OptimizationTrace, wantTrace) {
		t.Fatalf("optimization trace = %#v, want %#v", optimized.OptimizationTrace, wantTrace)
	}
}

func TestOptimizerRejectsNonConvergingRuleSet(t *testing.T) {
	calls := 0
	_, err := optimizer.NewCanonical(alwaysChangedRule{calls: &calls}).Optimize(context.Background(), &semanticplan.SemanticPlan{})
	if err == nil || !strings.Contains(err.Error(), "optimizer did not converge") {
		t.Fatalf("non-convergence error = %v", err)
	}
	if calls != 16 {
		t.Fatalf("rule calls = %d, want 16 bounded passes", calls)
	}
}

func TestSourceScanFusionIsIdempotentUnderFixpointExecution(t *testing.T) {
	input := &semanticplan.SemanticPlan{Nodes: []semanticplan.SemanticPlanNode{
		sourceAggregateNode("revenue", "orders", "orders"),
		sourceAggregateNode("refunds", "orders", "orders"),
	}}
	optimized, err := optimizer.NewCanonical(optimizer.SourceScanFusionRule{}).Optimize(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if len(optimized.Nodes) != 2 {
		t.Fatalf("optimized nodes = %#v", optimized.Nodes)
	}
	first := optimized.Nodes[0].(semanticplan.SourceAggregateNode)
	second := optimized.Nodes[1].(semanticplan.SourceAggregateNode)
	shareGroup := first.MetricState.ShareGroup
	if shareGroup == "" || second.MetricState.ShareGroup != shareGroup {
		t.Fatalf("source fusion share groups = %q, %q", first.MetricState.ShareGroup, second.MetricState.ShareGroup)
	}
	wantTrace := []semanticplan.OptimizationStep{{Rule: "source_scan_fusion", Changed: true}}
	if !reflect.DeepEqual(optimized.OptimizationTrace, wantTrace) {
		t.Fatalf("optimization trace = %#v, want %#v", optimized.OptimizationTrace, wantTrace)
	}
}
