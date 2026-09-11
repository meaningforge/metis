package semantic

import (
	"context"
	"errors"

	"github.com/meaningforge/metis/app/auth"
	"github.com/meaningforge/metis/app/observability"
	"github.com/meaningforge/metis/app/service/policy"
	"github.com/meaningforge/metis/compiler/artifact"
	"github.com/meaningforge/metis/planner/evaluation"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/renderer"
	"github.com/meaningforge/metis/resolver"
	"github.com/meaningforge/metis/serrors"
)

// WithDataAccessPolicy installs an application policy before serving requests.
// Nil fails closed. REST/MCP never accept a policy or security context in DTOs.
func (s *CompileService) WithDataAccessPolicy(adapter policy.DataAccessPolicy) *CompileService {
	if s != nil {
		s.dataAccessPolicy = adapter
	}
	return s
}

func dataPolicyError(err error) error {
	code, message := serrors.ErrInvalidDataAccessPolicy, "data access policy is invalid"
	if errors.Is(err, policy.ErrDenied) {
		code, message = serrors.ErrDataAccessDenied, "data access is denied"
	}
	if errors.Is(err, policy.ErrUnavailable) {
		code, message = serrors.ErrDataAccessPolicyUnavailable, "data access policy is unavailable"
	}
	return &serrors.Error{Code: code, Message: message}
}

type preparedDataPolicy struct {
	scope  string
	models map[string]map[string]*semanticplan.RelationPolicy
}

func (s *CompileService) prepareDataPolicy(ctx context.Context, work []policy.ResolvedWork) (preparedDataPolicy, error) {
	// This explicit, stateless compatibility adapter has no entitlements or
	// conditional decision. Preserve unrestricted deployments, including local
	// compile-only callers and expression forms outside the V1 policy binder.
	switch adapter := s.dataAccessPolicy.(type) {
	case policy.NoRestrictionDataAccessPolicy:
		return preparedDataPolicy{}, nil
	case *policy.NoRestrictionDataAccessPolicy:
		if adapter != nil {
			return preparedDataPolicy{}, nil
		}
		return preparedDataPolicy{}, dataPolicyError(policy.ErrUnavailable)
	}
	if s.dataAccessPolicy == nil {
		return preparedDataPolicy{}, dataPolicyError(policy.ErrUnavailable)
	}
	principal, ok := auth.PrincipalFromContext(ctx)
	if !ok || principal == nil {
		return preparedDataPolicy{}, dataPolicyError(policy.ErrDenied)
	}
	if len(work) == 0 {
		return preparedDataPolicy{}, dataPolicyError(policy.ErrInvalid)
	}
	req, err := policy.CollectWorkload(*principal, work[0].Query.Project, work)
	if err != nil {
		return preparedDataPolicy{}, dataPolicyError(err)
	}
	snapshot, err := policy.Evaluate(ctx, s.dataAccessPolicy, req)
	if err != nil {
		return preparedDataPolicy{}, dataPolicyError(err)
	}
	models := map[string]*resolver.SemanticQuerySpec{}
	for _, item := range work {
		models[item.Query.Model.Model.Name] = item.Query
	}
	bound, err := policy.Bind(snapshot, req, models)
	if err != nil {
		return preparedDataPolicy{}, dataPolicyError(err)
	}
	if snapshot.Effect() == policy.Unrestricted {
		return preparedDataPolicy{}, nil
	}
	out := preparedDataPolicy{scope: snapshot.ScopeID(), models: map[string]map[string]*semanticplan.RelationPolicy{}}
	for _, source := range bound {
		predicates := make([]semanticplan.RelationPredicate, len(source.Predicates))
		for i, predicate := range source.Predicates {
			predicates[i] = semanticplan.RelationPredicate{Field: predicate.Field.Field, Column: predicate.Column, Datatype: predicate.Datatype, Operator: predicate.Operator, Values: predicate.Values}
		}
		constraint, err := semanticplan.NewRelationPolicy(out.scope, source.Dataset.Dataset, predicates)
		if err != nil {
			return preparedDataPolicy{}, dataPolicyError(err)
		}
		if out.models[source.Dataset.Model] == nil {
			out.models[source.Dataset.Model] = map[string]*semanticplan.RelationPolicy{}
		}
		out.models[source.Dataset.Model][source.Dataset.Dataset] = constraint
	}
	return out, nil
}

func (s *CompileService) prepareQueryWork(ctx context.Context, queries []query.SemanticQuery, selected renderer.Renderer) ([]policy.ResolvedWork, error) {
	work := make([]policy.ResolvedWork, 0, len(queries))
	for _, semanticQuery := range queries {
		resolved, err := s.resolveQueryWithRenderer(ctx, semanticQuery, selected)
		if err != nil {
			return nil, err
		}
		metricPlan, err := evaluation.BuildMetricEvaluationPlan(resolved)
		if err != nil {
			return nil, serrors.Internal("metric evaluation planning is invalid", nil)
		}
		work = append(work, policy.ResolvedWork{Query: resolved, Evaluation: metricPlan})
	}
	return work, nil
}

func (s *CompileService) planQueryWork(ctx context.Context, work []policy.ResolvedWork, selected renderer.Renderer) ([]*semanticplan.SemanticPlan, error) {
	prepared, err := s.prepareDataPolicy(ctx, work)
	if err != nil {
		return nil, err
	}
	plans := make([]*semanticplan.SemanticPlan, 0, len(work))
	for _, item := range work {
		plan, err := s.planner.PlanPrepared(ctx, item.Query, item.Evaluation, selected, prepared.scope, prepared.models[item.Query.Model.Model.Name])
		if err != nil {
			if prepared.scope != "" {
				return nil, dataPolicyError(err)
			}
			return nil, err
		}
		if plan.PolicyScope != prepared.scope {
			return nil, dataPolicyError(policy.ErrInvalid)
		}
		plans = append(plans, plan)
	}
	return plans, nil
}

func (s *CompileService) prepareQueriesWithRenderer(ctx context.Context, queries []query.SemanticQuery, selected renderer.Renderer) ([]*semanticplan.SemanticPlan, error) {
	work, err := s.prepareQueryWork(ctx, queries, selected)
	if err != nil {
		return nil, err
	}
	return s.planQueryWork(ctx, work, selected)
}

func (s *CompileService) compileQueriesWithRenderer(ctx context.Context, queries []query.SemanticQuery, selected renderer.Renderer) ([]*artifact.CompiledQuery, error) {
	plans, err := s.prepareQueriesWithRenderer(ctx, queries, selected)
	if err != nil {
		return nil, err
	}
	compiled := make([]*artifact.CompiledQuery, len(plans))
	for i, plan := range plans {
		compiled[i], err = s.observeCompilation(ctx, selected, func(ctx context.Context, dialect observability.Dialect) (*artifact.CompiledQuery, error) {
			return observeCompilePhase(s.pipeline, ctx, observability.PhaseRendering, dialect, func(ctx context.Context) (*artifact.CompiledQuery, error) {
				return s.compilePreparedPlan(ctx, queries[i], plan, selected)
			})
		})
		if err != nil {
			return nil, err
		}
	}
	return compiled, nil
}

func (s *CompileService) compilePreparedPlan(ctx context.Context, q query.SemanticQuery, plan *semanticplan.SemanticPlan, selected renderer.Renderer) (*artifact.CompiledQuery, error) {
	compiled, err := s.compiler.CompileWithRenderer(ctx, plan, selected)
	if err != nil {
		if plan.PolicyScope != "" {
			return nil, dataPolicyError(err)
		}
		return nil, err
	}
	if compiled != nil && s.discovery != nil {
		compiled.Warnings, err = s.discovery.semanticQueryGovernanceWarnings(q)
	}
	return compiled, err
}
