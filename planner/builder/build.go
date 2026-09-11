package builder

import (
	"context"
	"fmt"
	"reflect"

	"github.com/meaningforge/metis/expression"
	"github.com/meaningforge/metis/planner/evaluation"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/renderer"
	"github.com/meaningforge/metis/resolver"
	"github.com/meaningforge/metis/serrors"
)

// OptimizationMode tells the root planner which orchestration path applies to
// the fully constructed semantic plan.
type OptimizationMode uint8

const (
	OptimizationNone OptimizationMode = iota
	OptimizationDirect
	OptimizationSemanticDAG
)

// BuildResult is the builder-owned output consumed by planner orchestration.
type BuildResult struct {
	Plan             *semanticplan.SemanticPlan
	OptimizationMode OptimizationMode
}

// Build constructs the complete pre-optimization SemanticPlan.
func Build(ctx context.Context, q *resolver.SemanticQuerySpec, evaluationPlan *evaluation.MetricEvaluationPlan, metricEvaluationRequired bool, renderer renderer.Renderer) (*BuildResult, error) {
	dialect := ""
	if renderer != nil {
		dialect = renderer.ExpressionDialect()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if q == nil || q.Model == nil || q.Model.Model == nil {
		return nil, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "resolved semantic query is required"}
	}
	if renderer == nil || dialect == "" {
		return nil, &serrors.Error{Code: serrors.ErrTargetRequired, Message: "selected Renderer is required for semantic planning"}
	}
	root, ok := q.Model.Datasets[q.RootDataset]
	if !ok {
		return nil, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "root dataset is not present in semantic model", Details: map[string]any{"root_dataset": q.RootDataset}}
	}
	if err := validateResolvedExpressions(q); err != nil {
		return nil, err
	}
	if metricEvaluationRequired && evaluationPlan == nil {
		return nil, &serrors.Error{Code: serrors.ErrInternalInvariant, Message: "metric evaluation plan is required"}
	}
	if !metricEvaluationRequired && evaluationPlan != nil {
		return nil, &serrors.Error{Code: serrors.ErrInternalInvariant, Message: "metric-free query must not carry a metric evaluation plan"}
	}
	if evaluationPlan != nil {
		if err := evaluation.ValidateMetricEvaluationPlan(evaluationPlan); err != nil {
			return nil, &serrors.Error{Code: serrors.ErrInternalInvariant, Message: "metric evaluation plan validation failed", Details: map[string]any{"cause": err.Error()}}
		}
	}
	plan := &semanticplan.SemanticPlan{Model: semanticplan.ModelRef{Project: q.Project, Name: q.Model.Model.Name}, Root: semanticplan.DatasetRef{Name: root.Name, Source: root.Source}, Limit: q.Limit}
	if q.TimeSpine != nil && q.TimeSpine.Dataset != nil {
		plan.DenseCalendar = &semanticplan.DenseCalendarPlan{Dataset: semanticplan.DatasetRef{Name: q.TimeSpine.Dataset.Name, Source: q.TimeSpine.Dataset.Source}, TimeField: q.TimeSpine.TimeField, TimeExpression: q.TimeSpine.Expression.Source, QueryTimeDimension: q.TimeSpine.QueryTimeDimension, Grain: q.TimeSpine.RequestedGrain}
	}
	for _, d := range q.Dimensions {
		projection, group, err := PlanResolvedDimension(d)
		if err != nil {
			return nil, err
		}
		plan.Projections = append(plan.Projections, projection)
		plan.Groups = append(plan.Groups, group)
	}
	for _, m := range q.Metrics {
		plan.Projections = append(plan.Projections, semanticplan.Projection{Name: m.Name, Kind: semanticplan.ProjectionMetric, Metric: m.Metric, Datasets: append([]string(nil), m.Datasets...), Expression: m.Expression})
	}
	for _, f := range q.Filters {
		plan.Predicates = append(plan.Predicates, semanticplan.Predicate{Filter: f.Filter, Dataset: f.Dataset, Field: f.Field, Expression: f.Expression})
	}
	for _, s := range q.OrderBy {
		sortPlan := semanticplan.Sort{Name: s.Name, Direction: s.Direction, Metric: s.Metric, Field: s.Field, Dataset: s.Dataset, Expression: s.Expression}
		switch s.Kind {
		case resolver.OrderTargetMetric:
			sortPlan.Kind = semanticplan.SortMetric
		case resolver.OrderTargetDimension:
			sortPlan.Kind = semanticplan.SortDimension
		default:
			return nil, &serrors.Error{Code: serrors.ErrInvalidQuery, Message: "unsupported resolved order target", Details: map[string]any{"field": s.Name, "kind": s.Kind}}
		}
		plan.Sorts = append(plan.Sorts, sortPlan)
	}
	evaluationPredicates, err := conversionEvaluationPredicates(evaluationPlan, plan.Predicates)
	if err != nil {
		return nil, err
	}
	conversionRawPredicatesSuppressed := len(evaluationPredicates) != len(plan.Predicates)
	// Metric evaluation lowering. Dependency closure, metric kind, and the typed
	// evaluation payload were settled by evaluation before builder was called;
	// this step only turns that supplied authority into semantic construction.
	construction, err := lowerMetricEvaluationPlan(q, evaluationPlan, metricEvaluationRequired, plan.Groups, evaluationPredicates)
	if err != nil {
		return nil, err
	}

	// Source and node planning. It decides how each settled evaluation is
	// sourced -- roots, joins, predicate placement, grains, calendar domains --
	// and may not decide what any of them evaluates.
	if err := planSemanticSources(plan, &construction, q); err != nil {
		return nil, err
	}
	if RequiresComposed(construction.Nodes, construction.PostEvaluationPredicates) {
		// The DAG is installed on the plan before enrichment, not after it.
		// Enrichment rewrites node predicates and appends output predicates,
		// and doing that to the plan directly is what removes the question of
		// which of two copies the answer lives in.
		if err := semanticplan.InstallOwnedDAG(plan, construction.Requested, construction.Nodes, construction.OutputGrain, construction.PostEvaluationPredicates, nil); err != nil {
			return nil, fmt.Errorf("materialize semantic nodes: %w", err)
		}

		sharedGrain, err := resolvePlanSharedGrain(plan)
		if err != nil {
			return nil, err
		}
		plan.SharedGrain = sharedGrain
		if err := applyPlanSharedGrainEvidence(plan, sharedGrain); err != nil {
			return nil, &serrors.Error{Code: serrors.ErrInternalInvariant, Message: "semantic graph shared-grain enrichment failed", Details: map[string]any{"cause": err.Error()}}
		}
		if err := semanticplan.ValidateSemanticPlanDAG(plan); err != nil {
			return nil, &serrors.Error{Code: serrors.ErrInternalInvariant, Message: "semantic graph shared-grain validation failed", Details: map[string]any{"cause": err.Error()}}
		}
		if err := ApplyTemporalReadRanges(plan, plan.Groups, plan.Predicates); err != nil {
			return nil, err
		}
		ApplyDenseCalendarRanges(plan.DenseCalendar, plan, plan.Predicates)
		post := planPostEvaluationPredicates(plan)
		if len(post) > 0 {
			remaining, err := removePostEvaluationPredicates(plan.Predicates, post)
			if err != nil {
				return nil, err
			}
			plan.Predicates = remaining
		}
		if err := semanticplan.ValidateSemanticPlanDAG(plan); err != nil {
			return nil, &serrors.Error{Code: serrors.ErrInternalInvariant, Message: "semantic graph read-range validation failed", Details: map[string]any{"cause": err.Error()}}
		}

		if conversionRawPredicatesSuppressed {
			if err := semanticplan.ValidateSemanticPlan(plan); err != nil {
				return nil, &serrors.Error{Code: serrors.ErrInternalInvariant, Message: "semantic plan invariant validation failed", Details: map[string]any{"phase": "pre-lowering", "cause": err.Error()}}
			}
			return &BuildResult{Plan: plan, OptimizationMode: OptimizationNone}, nil
		}
		return &BuildResult{Plan: plan, OptimizationMode: OptimizationSemanticDAG}, nil
	}
	// Shared-grain feasibility is asked of the same canonical typed nodes,
	// requested outputs and grain that the direct plan will own.
	directNodes, err := semanticplan.OwnNodes(construction.Nodes, nil)
	if err != nil {
		return nil, fmt.Errorf("materialize direct semantic nodes: %w", err)
	}
	sharedGrain, err := resolvePlanSharedGrain(&semanticplan.SemanticPlan{
		Requested: construction.Requested,
		Nodes:     directNodes,
		Output:    semanticplan.SemanticOutputContract{Grain: cloneGroups(construction.OutputGrain)},
	})
	if err != nil {
		return nil, err
	}
	plan.SharedGrain = sharedGrain
	joins, err := BuildJoinTree(q.RootDataset, q.Relationships)
	if err != nil {
		return nil, err
	}
	for i := range joins {
		from, ok := q.Model.Datasets[joins[i].FromDataset]
		if !ok {
			return nil, &serrors.Error{Code: serrors.ErrInternalInvariant, Message: "join source dataset is not present in semantic model", Details: map[string]any{"dataset": joins[i].FromDataset}}
		}
		to, ok := q.Model.Datasets[joins[i].ToDataset]
		if !ok {
			return nil, &serrors.Error{Code: serrors.ErrInternalInvariant, Message: "join target dataset is not present in semantic model", Details: map[string]any{"dataset": joins[i].ToDataset}}
		}
		// Metric-bearing relationship paths were already admitted independently
		// for every source aggregate during semantic source planning, where the
		// selected aggregation and population evidence are both available. This
		// flat query-shape join is only a compact-lowering projection and must not
		// replace that metric-aware decision with the older structural guard.
		if evaluationPlan == nil {
			if err := validateNonFanoutJoin(q.Model.Datasets, joins[i]); err != nil {
				return nil, err
			}
		}
		joins[i].FromSource = from.Source
		joins[i].ToSource = to.Source
	}
	plan.Joins = joins
	// After joins, because a source-selection node does not carry its own source
	// the way a metric node does: its root and joins are the query's, and those
	// are not final until here. Metric nodes are unaffected by the
	// position -- they were complete when construction returned.
	if err := InstallDirectNodes(plan, construction.Requested, construction.Nodes, construction.OutputGrain, construction.PostEvaluationPredicates, sharedGrain, q); err != nil {
		return nil, err
	}
	return &BuildResult{Plan: plan, OptimizationMode: OptimizationDirect}, nil
}

// planSemanticSources is the source-node planning phase of the planner's
// evaluation-to-plan boundary. It runs after metric evaluation construction has
// settled dependency closure, metric kind, and typed payloads, and it consumes
// those results rather than rederiving them.
//
// The steps and their order are unchanged from when they were inline in Plan;
// what is new is that the boundary has a name and a checked contract.
func planSemanticSources(plan *semanticplan.SemanticPlan, input *ConstructionInput, q *resolver.SemanticQuerySpec) error {
	settled := CaptureEvaluationSeam(input.Nodes)

	if err := ValidateConversionQueryGrains(input.Nodes, plan.Groups); err != nil {
		return err
	}
	if err := ValidateConversionFilterOwnership(input.Nodes, plan.Predicates); err != nil {
		return err
	}
	// Each of these plans a custom-calendar domain onto the node that declares
	// it. They used to also return the same values keyed by node id, for three
	// plan fields that were rebuilt from the nodes after every optimizer pass.
	customDense, err := EnrichCustomCalendars(input.Nodes, plan.Groups)
	if err != nil {
		return err
	}
	plan.CustomDenseCalendar = customDense
	if err := applySemanticMetricDefinitionFilters(input, q); err != nil {
		return err
	}
	if err := ValidateGrainToDateQueryGrains(input.Nodes, plan.Groups); err != nil {
		return err
	}
	if err := applySemanticSemiAdditiveInputGrains(input, q); err != nil {
		return err
	}

	return RequireEvaluationSeam(settled, input.Nodes)
}

func validateResolvedExpressions(q *resolver.SemanticQuerySpec) error {
	require := func(kind, name string, resolved expression.ResolvedExpression, analysis bool) error {
		if !resolved.IsResolved() {
			return &serrors.Error{Code: serrors.ErrInternalInvariant, Message: "semantic plan input contains an unresolved expression", Details: map[string]any{"kind": kind, "name": name}}
		}
		if analysis && resolved.Analysis == nil {
			return &serrors.Error{Code: serrors.ErrInternalInvariant, Message: "resolved metric expression is missing semantic analysis", Details: map[string]any{"kind": kind, "name": name, "source_dialect": resolved.SourceDialect}}
		}
		return nil
	}
	for _, metric := range q.Metrics {
		if err := require("metric", metric.Name, metric.Expression, true); err != nil {
			return err
		}
	}
	for _, dimension := range q.Dimensions {
		if err := require("dimension", dimension.Name, dimension.Expression, false); err != nil {
			return err
		}
	}
	for _, filter := range q.Filters {
		if filter.Kind == resolver.FilterTargetField {
			if err := require("filter", filter.Filter.Field, filter.Expression, false); err != nil {
				return err
			}
		}
	}
	for _, order := range q.OrderBy {
		if err := require("order", order.Name, order.Expression, order.Kind == resolver.OrderTargetMetric); err != nil {
			return err
		}
	}
	if q.TimeSpine != nil {
		if err := require("time_spine", q.TimeSpine.QueryTimeDimension, q.TimeSpine.Expression, false); err != nil {
			return err
		}
	}
	return nil
}

func removePostEvaluationPredicates(predicates []semanticplan.Predicate, post []semanticplan.PostEvaluationPredicate) ([]semanticplan.Predicate, error) {
	if len(post) == 0 {
		return predicates, nil
	}
	remaining := append([]semanticplan.Predicate(nil), predicates...)
	for _, candidate := range post {
		matched := -1
		for i, predicate := range remaining {
			if sameFilterIdentity(candidate.Filter, predicate.Filter) {
				matched = i
				break
			}
		}
		if matched < 0 {
			return nil, &serrors.Error{
				Code:    serrors.ErrInternalInvariant,
				Message: "post-evaluation predicate conservation failed",
				Details: map[string]any{
					"field":    candidate.Filter.Field,
					"operator": candidate.Filter.Operator,
				},
			}
		}
		remaining = append(remaining[:matched], remaining[matched+1:]...)
	}
	return remaining, nil
}

func sameFilterIdentity(left, right query.Filter) bool {
	return left.Field == right.Field &&
		left.Operator == right.Operator &&
		reflect.DeepEqual(left.Value, right.Value)
}
