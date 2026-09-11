package planner

import (
	"context"
	"errors"
	"reflect"

	"github.com/meaningforge/metis/planner/semanticplan"
)

var (
	errPlanningIncomplete     = errors.New("planning terminated before completion")
	errOptimizationIncomplete = errors.New("optimization terminated before completion")
)

// RuntimeObserver marks the two lifecycle boundaries owned by Planner without
// coupling the semantic package to an observability backend. Implementations
// must treat these callbacks as best-effort and must not mutate plan state.
type RuntimeObserver interface {
	StartPlanning(context.Context, string) (context.Context, func(error))
	StartOptimization(context.Context, string) (context.Context, func(bool, error))
}

func (p *Planner) WithRuntimeObserver(observer RuntimeObserver) *Planner {
	if p != nil {
		p.runtimeObserver = observer
	}
	return p
}

func (p *Planner) startPlanning(ctx context.Context, dialect string) (context.Context, func(error)) {
	if p == nil || p.runtimeObserver == nil {
		return ctx, func(error) {}
	}
	var observed context.Context
	var finish func(error)
	func() {
		defer func() { _ = recover() }()
		observed, finish = p.runtimeObserver.StartPlanning(ctx, dialect)
	}()
	if observed == nil {
		observed = ctx
	}
	if finish == nil {
		finish = func(error) {}
	}
	return observed, func(err error) {
		defer func() { _ = recover() }()
		finish(err)
	}
}

func (p *Planner) startOptimization(ctx context.Context, dialect string) (context.Context, func(bool, error)) {
	if p == nil || p.runtimeObserver == nil {
		return ctx, func(bool, error) {}
	}
	var observed context.Context
	var finish func(bool, error)
	func() {
		defer func() { _ = recover() }()
		observed, finish = p.runtimeObserver.StartOptimization(ctx, dialect)
	}()
	if observed == nil {
		observed = ctx
	}
	if finish == nil {
		finish = func(bool, error) {}
	}
	return observed, func(changed bool, err error) {
		defer func() { _ = recover() }()
		finish(changed, err)
	}
}

func semanticPlanChanged(before, after *semanticplan.SemanticPlan) bool {
	if before == nil || after == nil {
		return before != after
	}
	left := *before
	right := *after
	left.OptimizationTrace = nil
	right.OptimizationTrace = nil
	return !reflect.DeepEqual(left, right)
}
