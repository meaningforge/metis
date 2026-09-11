package planner

import (
	"context"
	"strings"
	"testing"

	"github.com/meaningforge/metis/planner/optimizer"
	"github.com/meaningforge/metis/planner/semanticplan"
)

func TestRuntimeObserverPanicsCannotChangePlannerControlFlow(t *testing.T) {
	planner := New().WithRuntimeObserver(panicRuntimeObserver{})
	ctx, finishPlanning := planner.startPlanning(context.Background(), "DORIS")
	if ctx == nil {
		t.Fatal("planning context is nil")
	}
	finishPlanning(nil)
	ctx, finishOptimization := planner.startOptimization(context.Background(), "DORIS")
	if ctx == nil {
		t.Fatal("optimization context is nil")
	}
	finishOptimization(true, nil)

	planner.WithRuntimeObserver(finishPanicRuntimeObserver{})
	_, finishPlanning = planner.startPlanning(context.Background(), "DORIS")
	finishPlanning(nil)
	_, finishOptimization = planner.startOptimization(context.Background(), "DORIS")
	finishOptimization(false, nil)
}

func TestSemanticPlanChangedIgnoresOptimizationTraceButDetectsShape(t *testing.T) {
	before := &semanticplan.SemanticPlan{Requested: []string{"revenue"}}
	after := &semanticplan.SemanticPlan{Requested: []string{"revenue"}, OptimizationTrace: []semanticplan.OptimizationStep{{Rule: "probe", Changed: true}}}
	if semanticPlanChanged(before, after) {
		t.Fatal("optimization trace alone classified as a plan rewrite")
	}
	after.Requested = append(after.Requested, "orders")
	if !semanticPlanChanged(before, after) {
		t.Fatal("plan shape change classified as noop")
	}
}

func TestOptimizerPanicNaturallyPropagatesAfterObservationFinishes(t *testing.T) {
	observer := &capturingRuntimeObserver{}
	planner := New(optimizer.NewCanonical(panickingSemanticPlanRule{})).WithRuntimeObserver(observer)

	func() {
		defer func() {
			if recovered := recover(); recovered != "optimizer defect" {
				t.Fatalf("recovered panic = %#v", recovered)
			}
		}()
		plan := &semanticplan.SemanticPlan{
			Root:      semanticplan.DatasetRef{Name: "orders", Source: "analytics.orders"},
			Requested: []string{"revenue"},
			Nodes: []semanticplan.SemanticPlanNode{semanticplan.SourceAggregateNode{
				Base: semanticplan.SemanticPlanNodeBase{ID: "revenue", Boundary: semanticplan.SemanticPlanNodeBoundarySourceAggregate},
				Source: semanticplan.SemanticSourceState{
					Root:             semanticplan.DatasetRef{Name: "orders", Source: "analytics.orders"},
					SourceRoots:      []string{"orders"},
					RequiredDatasets: []string{"orders"},
				},
			}},
		}
		_, _ = planner.optimizeSemanticPlan(t.Context(), plan, "DORIS")
	}()

	if observer.optimizationErr == nil || !strings.Contains(observer.optimizationErr.Error(), "terminated before completion") {
		t.Fatalf("optimization observation error = %v", observer.optimizationErr)
	}
}

type panicRuntimeObserver struct{}

type capturingRuntimeObserver struct {
	optimizationErr error
}

func (*capturingRuntimeObserver) StartPlanning(ctx context.Context, _ string) (context.Context, func(error)) {
	return ctx, func(error) {}
}

func (o *capturingRuntimeObserver) StartOptimization(ctx context.Context, _ string) (context.Context, func(bool, error)) {
	return ctx, func(_ bool, err error) { o.optimizationErr = err }
}

type panickingSemanticPlanRule struct{}

func (panickingSemanticPlanRule) Name() string { return "panicking_semantic_plan_rule" }
func (panickingSemanticPlanRule) Apply(*semanticplan.SemanticPlan) (bool, error) {
	panic("legacy optimizer path must not run")
}
func (panickingSemanticPlanRule) ApplySemanticPlan(*optimizer.State) (bool, error) {
	panic("optimizer defect")
}

func (panicRuntimeObserver) StartPlanning(context.Context, string) (context.Context, func(error)) {
	panic("planning observer")
}

type finishPanicRuntimeObserver struct{}

func (finishPanicRuntimeObserver) StartPlanning(ctx context.Context, _ string) (context.Context, func(error)) {
	return ctx, func(error) { panic("planning finish") }
}

func (finishPanicRuntimeObserver) StartOptimization(ctx context.Context, _ string) (context.Context, func(bool, error)) {
	return ctx, func(bool, error) { panic("optimization finish") }
}

func (panicRuntimeObserver) StartOptimization(context.Context, string) (context.Context, func(bool, error)) {
	panic("optimization observer")
}
