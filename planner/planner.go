package planner

import (
	"context"

	"github.com/meaningforge/metis/planner/builder"
	"github.com/meaningforge/metis/planner/evaluation"
	"github.com/meaningforge/metis/planner/optimizer"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/renderer"
	"github.com/meaningforge/metis/resolver"
	"github.com/meaningforge/metis/serrors"
)

// Planner owns top-level planning orchestration and runtime observation.
type Planner struct {
	optimizer       *optimizer.Optimizer
	runtimeObserver RuntimeObserver
}

func New(optimizers ...*optimizer.Optimizer) *Planner {
	selected := optimizer.Default()
	if len(optimizers) != 0 {
		selected = optimizers[0]
	}
	return &Planner{optimizer: selected}
}

// Plan constructs a SemanticPlan through builder and applies the selected
// optimizer. SemanticPlan itself is owned and exposed by semanticplan.
func (p *Planner) Plan(ctx context.Context, q *resolver.SemanticQuerySpec, selectedRenderer renderer.Renderer) (result *semanticplan.SemanticPlan, err error) {
	return p.planPrepared(ctx, q, nil, false, selectedRenderer, "", nil)
}

// PlanPrepared resumes orchestration after application preflight has consumed
// the one metric-evaluation plan. Bound relation policies enter before the
// optimizer; no Principal or policy adapter crosses the planner boundary.
func (p *Planner) PlanPrepared(ctx context.Context, q *resolver.SemanticQuerySpec, metricPlan *evaluation.MetricEvaluationPlan, selectedRenderer renderer.Renderer, scope string, policies map[string]*semanticplan.RelationPolicy) (*semanticplan.SemanticPlan, error) {
	return p.planPrepared(ctx, q, metricPlan, true, selectedRenderer, scope, policies)
}

func (p *Planner) planPrepared(ctx context.Context, q *resolver.SemanticQuerySpec, evaluationPlan *evaluation.MetricEvaluationPlan, prepared bool, selectedRenderer renderer.Renderer, scope string, policies map[string]*semanticplan.RelationPolicy) (result *semanticplan.SemanticPlan, err error) {
	parentCtx := ctx
	dialect := ""
	if selectedRenderer != nil {
		dialect = selectedRenderer.ExpressionDialect()
	}
	ctx, finishPlanning := p.startPlanning(parentCtx, dialect)
	planningFinished := false
	defer func() {
		if !planningFinished {
			observationErr := err
			if observationErr == nil {
				observationErr = errPlanningIncomplete
			}
			finishPlanning(observationErr)
		}
	}()

	metricEvaluationRequired := evaluation.RequiresMetricEvaluation(q)
	if !prepared {
		evaluationPlan, err = evaluation.BuildMetricEvaluationPlan(q)
	}
	if err != nil {
		return nil, &serrors.Error{Code: serrors.ErrInternalInvariant, Message: "metric evaluation plan validation failed", Details: map[string]any{"cause": err.Error()}}
	}
	built, err := builder.Build(ctx, q, evaluationPlan, metricEvaluationRequired, selectedRenderer)
	if err != nil {
		return nil, err
	}
	if scope != "" {
		built.Plan, err = semanticplan.WithRelationPolicies(built.Plan, scope, policies)
		if err != nil {
			return nil, err
		}
	} else if len(policies) != 0 {
		return nil, serrors.Internal("relation policy operation scope is required", nil)
	}
	if err := semanticplan.ValidateSemanticPlan(built.Plan); err != nil {
		return nil, err
	}
	finishPlanning(nil)
	planningFinished = true

	switch built.OptimizationMode {
	case builder.OptimizationNone:
		return built.Plan, nil
	case builder.OptimizationSemanticDAG:
		result, err = p.optimizeSemanticPlan(parentCtx, built.Plan, dialect)
		if err != nil {
			return nil, &serrors.Error{Code: serrors.ErrInternalInvariant, Message: "semantic graph optimization failed", Details: map[string]any{"cause": err.Error()}}
		}
		return result, nil
	default:
		return p.optimize(parentCtx, built.Plan, dialect)
	}
}

func (p *Planner) optimize(ctx context.Context, plan *semanticplan.SemanticPlan, dialect string) (result *semanticplan.SemanticPlan, err error) {
	ctx, finishOptimization := p.startOptimization(ctx, dialect)
	optimizationFinished := false
	defer func() {
		observationErr := err
		if observationErr == nil && !optimizationFinished {
			observationErr = errOptimizationIncomplete
		}
		finishOptimization(semanticPlanChanged(plan, result), observationErr)
	}()
	if _, err := validatedSemanticPlan(plan, "pre-optimization"); err != nil {
		return nil, err
	}
	if p == nil || p.optimizer == nil {
		result = plan
		optimizationFinished = true
		return result, nil
	}
	optimized, err := p.optimizer.Optimize(ctx, plan)
	if err != nil {
		return nil, &serrors.Error{Code: serrors.ErrInternalInvariant, Message: "semantic plan optimization failed", Details: map[string]any{"cause": err.Error()}}
	}
	result, err = validatedSemanticPlan(optimized, "post-optimization")
	optimizationFinished = true
	return result, err
}

func (p *Planner) optimizeSemanticPlan(ctx context.Context, plan *semanticplan.SemanticPlan, dialect string) (result *semanticplan.SemanticPlan, err error) {
	ctx, finishOptimization := p.startOptimization(ctx, dialect)
	optimizationFinished := false
	defer func() {
		observationErr := err
		if observationErr == nil && !optimizationFinished {
			observationErr = errOptimizationIncomplete
		}
		finishOptimization(semanticPlanChanged(plan, result), observationErr)
	}()
	selected := p.optimizer
	if selected == nil {
		selected = optimizer.NewCanonical()
	}
	result, err = selected.OptimizeSemanticPlan(ctx, plan)
	if err != nil {
		return nil, err
	}
	result, err = validatedSemanticPlan(result, "post-optimization")
	optimizationFinished = true
	return result, err
}

func validatedSemanticPlan(plan *semanticplan.SemanticPlan, phase string) (*semanticplan.SemanticPlan, error) {
	if err := semanticplan.ValidateSemanticPlan(plan); err != nil {
		return nil, &serrors.Error{Code: serrors.ErrInternalInvariant, Message: "semantic plan invariant validation failed", Details: map[string]any{"phase": phase, "cause": err.Error()}}
	}
	return plan, nil
}
